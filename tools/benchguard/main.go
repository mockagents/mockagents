// Command benchguard compares a freshly-measured benchmark report against the
// committed baseline (docs/benchmarks/latest.json) and fails CI on the changes
// that actually mean something.
//
// Usage:
//
//	go run ./tools/benchreport -pkg ./internal/engine/... -out /tmp/bench   # measure
//	go run ./tools/benchguard -baseline docs/benchmarks/latest.json \
//	    -candidate /tmp/bench/latest.json                                   # compare
//
// # What it gates on, and why — every threshold below is measured, not assumed
//
// A guard that cries wolf gets switched off, so the defaults were calibrated by
// running the full suite twice against IDENTICAL code and observing the spread:
//
//   - allocs/op: 17/17 benchmarks identical. Perfectly stable, and stable across
//     three QA cycles besides. Gated EXACTLY — any change fails.
//   - B/op: drifts up to 7.8% run-to-run on unchanged code, and only in the
//     ProcessRequest_* family, which builds variable-size responses (buffer
//     growth and map iteration shift the per-op average). Gated with a
//     tolerance (default 20%) — wide enough to never fire on that noise, tight
//     enough that a genuinely new allocation shows up.
//   - ns/op: NOT gated by default. Cycle 3 measured the same benchmark at
//     49-52 ns one day and 77-94 ns the next, in isolation, on identical code;
//     the identical-code run here moved microsecond-scale rows by up to 81%.
//     Reported as notes so a human can eyeball a trend. Teams with a dedicated
//     fixed-clock runner can opt in with -gate-ns.
//
// The result is a guard that is precise rather than sensitive: it should be
// silent until something real changes, which is what makes it worth keeping.
//
// A benchmark present in the baseline but missing from the candidate is a FAIL:
// that is exactly how a silent tooling bug once dropped 2 of 17 rows without
// anyone noticing (see docs/benchmarks/README.md). A benchmark present only in
// the candidate is a note, not a failure — adding a benchmark shouldn't break CI.
//
// When a change is intentional, regenerate docs/benchmarks/latest.json in the
// same commit; the diff becomes the review artifact.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Result mirrors tools/benchreport's schema-v1 result shape.
type Result struct {
	Name         string  `json:"name"`
	Iterations   int64   `json:"iterations"`
	NsPerOp      float64 `json:"ns_per_op"`
	BytesPerOp   int64   `json:"bytes_per_op,omitempty"`
	AllocsPerOp  int64   `json:"allocs_per_op,omitempty"`
	OpsPerSecond float64 `json:"ops_per_second"`
}

// Report mirrors tools/benchreport's schema-v1 envelope.
type Report struct {
	SchemaVersion string   `json:"schema_version"`
	GoVersion     string   `json:"go_version"`
	GOOS          string   `json:"goos"`
	GOARCH        string   `json:"goarch"`
	Package       string   `json:"package"`
	Results       []Result `json:"results"`
}

func load(path string) (*Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if r.SchemaVersion != "1" {
		return nil, fmt.Errorf("%s: unsupported schema_version %q (want \"1\")", path, r.SchemaVersion)
	}
	if len(r.Results) == 0 {
		return nil, fmt.Errorf("%s: no benchmark results", path)
	}
	return &r, nil
}

func index(r *Report) map[string]Result {
	m := make(map[string]Result, len(r.Results))
	for _, x := range r.Results {
		m[x.Name] = x
	}
	return m
}

// options are the comparison knobs, one per command-line flag.
type options struct {
	BaselinePath   string
	CandidatePath  string
	BytesTolerance float64
	GateNs         bool
	NsThreshold    float64
	NsFloorNs      float64
	SummaryPath    string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process exit, so the flag handling, exit codes and
// output are testable: 0 = no blocking change, 1 = blocking change, 2 = usage
// or input error.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("benchguard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.StringVar(&o.BaselinePath, "baseline", "docs/benchmarks/latest.json", "committed baseline report")
	fs.StringVar(&o.CandidatePath, "candidate", "", "freshly measured report to compare (required)")
	fs.Float64Var(&o.BytesTolerance, "bytes-tolerance", 0.20, "max fractional B/op drift before failing (measured noise on unchanged code: 7.8%)")
	fs.BoolVar(&o.GateNs, "gate-ns", false, "also fail on ns/op drift — only meaningful on a dedicated fixed-clock runner")
	fs.Float64Var(&o.NsThreshold, "ns-threshold", 0.25, "max fractional ns/op drift when -gate-ns is set")
	fs.Float64Var(&o.NsFloorNs, "ns-floor", 1000, "with -gate-ns, benchmarks below this baseline ns/op stay informational")
	fs.StringVar(&o.SummaryPath, "summary", "", "optional path to append a Markdown summary (e.g. $GITHUB_STEP_SUMMARY)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if o.CandidatePath == "" {
		fmt.Fprintln(stderr, "benchguard: -candidate is required")
		return 2
	}

	base, err := load(o.BaselinePath)
	if err != nil {
		fmt.Fprintln(stderr, "benchguard:", err)
		return 2
	}
	cand, err := load(o.CandidatePath)
	if err != nil {
		fmt.Fprintln(stderr, "benchguard:", err)
		return 2
	}

	failures, notes := compare(base, cand, o)
	summary := renderSummary(base, cand, o, failures, notes)

	fmt.Fprint(stdout, summary)
	if o.SummaryPath != "" {
		f, err := os.OpenFile(o.SummaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(summary)
			_ = f.Close()
		}
	}

	if len(failures) > 0 {
		return 1
	}
	return 0
}

// compare applies the gating rules described in the package comment and
// returns the blocking failures (in baseline-name order) and the sorted
// non-blocking notes.
func compare(base, cand *Report, o options) (failures, notes []string) {
	baseIdx, candIdx := index(base), index(cand)

	names := make([]string, 0, len(baseIdx))
	for n := range baseIdx {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		b := baseIdx[name]
		c, ok := candIdx[name]
		if !ok {
			// Never silently tolerate a vanished row — that's how a logging
			// bug once dropped 2 of 17 benchmarks unnoticed.
			failures = append(failures, fmt.Sprintf(
				"%s: MISSING from candidate (was it renamed, or did output parsing break?)", name))
			continue
		}
		if c.AllocsPerOp != b.AllocsPerOp {
			failures = append(failures, fmt.Sprintf(
				"%s: allocs/op %d -> %d", name, b.AllocsPerOp, c.AllocsPerOp))
		}
		if b.BytesPerOp > 0 {
			bDrift := float64(c.BytesPerOp-b.BytesPerOp) / float64(b.BytesPerOp)
			if bDrift > o.BytesTolerance || bDrift < -o.BytesTolerance {
				failures = append(failures, fmt.Sprintf(
					"%s: B/op %d -> %d (%+.0f%%, over %.0f%% tolerance)",
					name, b.BytesPerOp, c.BytesPerOp, bDrift*100, o.BytesTolerance*100))
			}
		} else if c.BytesPerOp != b.BytesPerOp {
			// A benchmark that allocated nothing now allocates: always notable.
			failures = append(failures, fmt.Sprintf(
				"%s: B/op %d -> %d (was zero-allocation)", name, b.BytesPerOp, c.BytesPerOp))
		}
		if b.NsPerOp <= 0 {
			continue
		}
		drift := (c.NsPerOp - b.NsPerOp) / b.NsPerOp
		if drift <= o.NsThreshold && drift >= -o.NsThreshold {
			continue
		}
		subMicro := b.NsPerOp < o.NsFloorNs
		if o.GateNs && !subMicro && drift > o.NsThreshold {
			failures = append(failures, fmt.Sprintf(
				"%s: ns/op %.1f -> %.1f (%+.0f%%, over %.0f%% threshold)",
				name, b.NsPerOp, c.NsPerOp, drift*100, o.NsThreshold*100))
			continue
		}
		scale := "µs-scale"
		if subMicro {
			scale = "sub-µs"
		}
		notes = append(notes, fmt.Sprintf(
			"%s: ns/op %.1f -> %.1f (%+.0f%%, %s — informational)",
			name, b.NsPerOp, c.NsPerOp, drift*100, scale))
	}

	for name := range candIdx {
		if _, ok := baseIdx[name]; !ok {
			notes = append(notes, fmt.Sprintf(
				"%s: new benchmark, not in baseline (re-run benchreport to adopt it)", name))
		}
	}
	sort.Strings(notes)
	return failures, notes
}

// renderSummary formats the comparison as the Markdown block printed to
// stdout and appended to the -summary file.
func renderSummary(base, cand *Report, o options, failures, notes []string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "## Benchmark guard\n\n")
	fmt.Fprintf(&out, "Baseline `%s` (%s %s/%s) vs candidate `%s` (%s %s/%s) — %d benchmarks compared.\n\n",
		o.BaselinePath, base.GoVersion, base.GOOS, base.GOARCH,
		o.CandidatePath, cand.GoVersion, cand.GOOS, cand.GOARCH, len(index(base)))

	if len(failures) > 0 {
		fmt.Fprintf(&out, "### ❌ %d blocking change(s)\n\n", len(failures))
		for _, f := range failures {
			fmt.Fprintf(&out, "- %s\n", f)
		}
		fmt.Fprintf(&out, "\nIf these are intentional, regenerate the baseline in this PR:\n"+
			"```bash\ngo run ./tools/benchreport -pkg %s -out docs/benchmarks\n```\n", base.Package)
	} else {
		fmt.Fprintf(&out, "### ✅ No blocking changes\n\nallocs/op matches exactly and B/op stays within the configured %.0f%% tolerance.\n", o.BytesTolerance*100)
	}
	if len(notes) > 0 {
		fmt.Fprintf(&out, "\n### Notes (non-blocking)\n\n")
		for _, n := range notes {
			fmt.Fprintf(&out, "- %s\n", n)
		}
	}
	return out.String()
}
