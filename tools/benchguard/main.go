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
	"flag"
	"fmt"
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

func main() {
	baselinePath := flag.String("baseline", "docs/benchmarks/latest.json", "committed baseline report")
	candidatePath := flag.String("candidate", "", "freshly measured report to compare (required)")
	bytesTolerance := flag.Float64("bytes-tolerance", 0.20, "max fractional B/op drift before failing (measured noise on unchanged code: 7.8%)")
	gateNs := flag.Bool("gate-ns", false, "also fail on ns/op drift — only meaningful on a dedicated fixed-clock runner")
	nsThreshold := flag.Float64("ns-threshold", 0.25, "max fractional ns/op drift when -gate-ns is set")
	nsFloorNs := flag.Float64("ns-floor", 1000, "with -gate-ns, benchmarks below this baseline ns/op stay informational")
	summaryPath := flag.String("summary", "", "optional path to append a Markdown summary (e.g. $GITHUB_STEP_SUMMARY)")
	flag.Parse()

	if *candidatePath == "" {
		fmt.Fprintln(os.Stderr, "benchguard: -candidate is required")
		os.Exit(2)
	}

	base, err := load(*baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchguard:", err)
		os.Exit(2)
	}
	cand, err := load(*candidatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchguard:", err)
		os.Exit(2)
	}

	baseIdx, candIdx := index(base), index(cand)

	var failures, notes []string

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
			if bDrift > *bytesTolerance || bDrift < -*bytesTolerance {
				failures = append(failures, fmt.Sprintf(
					"%s: B/op %d -> %d (%+.0f%%, over %.0f%% tolerance)",
					name, b.BytesPerOp, c.BytesPerOp, bDrift*100, *bytesTolerance*100))
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
		if drift <= *nsThreshold && drift >= -*nsThreshold {
			continue
		}
		subMicro := b.NsPerOp < *nsFloorNs
		if *gateNs && !subMicro && drift > *nsThreshold {
			failures = append(failures, fmt.Sprintf(
				"%s: ns/op %.1f -> %.1f (%+.0f%%, over %.0f%% threshold)",
				name, b.NsPerOp, c.NsPerOp, drift*100, *nsThreshold*100))
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

	var out strings.Builder
	fmt.Fprintf(&out, "## Benchmark guard\n\n")
	fmt.Fprintf(&out, "Baseline `%s` (%s %s/%s) vs candidate `%s` (%s %s/%s) — %d benchmarks compared.\n\n",
		*baselinePath, base.GoVersion, base.GOOS, base.GOARCH,
		*candidatePath, cand.GoVersion, cand.GOOS, cand.GOARCH, len(baseIdx))

	if len(failures) > 0 {
		fmt.Fprintf(&out, "### ❌ %d blocking change(s)\n\n", len(failures))
		for _, f := range failures {
			fmt.Fprintf(&out, "- %s\n", f)
		}
		fmt.Fprintf(&out, "\nIf these are intentional, regenerate the baseline in this PR:\n"+
			"```bash\ngo run ./tools/benchreport -pkg %s -out docs/benchmarks\n```\n", base.Package)
	} else {
		fmt.Fprintf(&out, "### ✅ No blocking changes\n\nallocs/op and B/op match the baseline exactly.\n")
	}
	if len(notes) > 0 {
		fmt.Fprintf(&out, "\n### Notes (non-blocking)\n\n")
		for _, n := range notes {
			fmt.Fprintf(&out, "- %s\n", n)
		}
	}

	fmt.Print(out.String())
	if *summaryPath != "" {
		f, err := os.OpenFile(*summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(out.String())
			_ = f.Close()
		}
	}

	if len(failures) > 0 {
		os.Exit(1)
	}
}
