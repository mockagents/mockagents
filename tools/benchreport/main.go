// Command benchreport runs the Go benchmarks for a given package set,
// parses the standard `go test -bench` output, and emits both a JSON
// artifact and a human-readable Markdown snapshot under
// docs/benchmarks/. Drops straight into CI: run it after a build, upload
// the JSON as a workflow artifact, and point reviewers at the Markdown.
//
// Usage:
//
//	go run ./tools/benchreport -pkg ./internal/engine/... -out docs/benchmarks
//
// The tool intentionally shells out to `go test` rather than importing
// the testing package so it can capture stdout deterministically and
// stay out of whatever hot-path init the benchmarked packages run.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Result is one parsed benchmark line in a normalized shape. ns/op is
// always filled; memory columns are optional because `-benchmem` is on
// by default but not guaranteed to succeed on every line (e.g. a
// benchmark that panics mid-run).
type Result struct {
	Name         string  `json:"name"`
	Iterations   int64   `json:"iterations"`
	NsPerOp      float64 `json:"ns_per_op"`
	BytesPerOp   int64   `json:"bytes_per_op,omitempty"`
	AllocsPerOp  int64   `json:"allocs_per_op,omitempty"`
	OpsPerSecond float64 `json:"ops_per_second"`
}

// Report is the top-level JSON envelope. Consumers can pin the schema
// version if the shape evolves.
type Report struct {
	SchemaVersion string    `json:"schema_version"`
	Timestamp     time.Time `json:"timestamp"`
	GoVersion     string    `json:"go_version"`
	GOOS          string    `json:"goos"`
	GOARCH        string    `json:"goarch"`
	Package       string    `json:"package"`
	Results       []Result  `json:"results"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr, execGoTest, time.Now))
}

// goTestRunner runs `go <args...>` and returns its stdout and stderr. It is a
// seam so run can be tested without shelling out to the toolchain.
type goTestRunner func(args []string) (stdout, stderr string, err error)

func execGoTest(args []string) (string, string, error) {
	cmd := exec.Command("go", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// run is main without the process exit: 0 on success, 1 on any failure
// (matching die), 2 on a flag parse error (matching flag.ExitOnError).
func run(args []string, stderr io.Writer, goTest goTestRunner, now func() time.Time) int {
	fs := flag.NewFlagSet("benchreport", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pkg := fs.String("pkg", "./internal/engine/...", "package pattern to benchmark")
	outDir := fs.String("out", "docs/benchmarks", "directory to write latest.json + latest.md into")
	benchTime := fs.String("benchtime", "1s", "value for go test -benchtime")
	count := fs.Int("count", 1, "value for go test -count")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	die := func(format string, args ...any) int {
		fmt.Fprintf(stderr, "benchreport: "+format+"\n", args...)
		return 1
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return die("mkdir %s: %v", *outDir, err)
	}

	goArgs := goTestArgs(*pkg, *benchTime, *count)
	fmt.Fprintf(stderr, "benchreport: running go %s\n", strings.Join(goArgs, " "))
	stdout, goStderr, err := goTest(goArgs)
	if err != nil {
		fmt.Fprint(stderr, goStderr)
		return die("go test failed: %v", err)
	}

	// The child `go test` inherits this process's GOMAXPROCS (same machine,
	// same environment), which is the -N suffix it appends to names.
	results := parseBenchOutput(stdout, runtime.GOMAXPROCS(0))
	if len(results) == 0 {
		return die("no benchmark results parsed — check %q actually contains benchmarks", *pkg)
	}

	report := Report{
		SchemaVersion: "1",
		Timestamp:     now().UTC(),
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		Package:       *pkg,
		Results:       results,
	}

	jsonPath := filepath.Join(*outDir, "latest.json")
	if err := writeJSON(jsonPath, report); err != nil {
		return die("writing %s: %v", jsonPath, err)
	}
	mdPath := filepath.Join(*outDir, "latest.md")
	if err := writeMarkdown(mdPath, report); err != nil {
		return die("writing %s: %v", mdPath, err)
	}

	fmt.Fprintf(stderr, "benchreport: wrote %d results to %s and %s\n",
		len(results), jsonPath, mdPath)
	return 0
}

// goTestArgs is the `go test` invocation: benchmarks only (-run ^$), with
// allocation stats, at the requested benchtime and count.
func goTestArgs(pkg, benchTime string, count int) []string {
	return []string{
		"test",
		"-run", "^$",
		"-bench", ".",
		"-benchmem",
		"-benchtime", benchTime,
		"-count", strconv.Itoa(count),
		pkg,
	}
}

// parseBenchOutput consumes `go test -bench` stdout and returns one
// Result per benchmark line. Lines we don't recognize (PASS, ok, build
// noise) are silently skipped.
//
// Expected format:
//
//	BenchmarkName-8   	1234567	      123.4 ns/op	      45 B/op	       2 allocs/op
//
// procs is the GOMAXPROCS the benchmarks ran with; see stripProcSuffix.
func parseBenchOutput(out string, procs int) []Result {
	var results []Result
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := stripProcSuffix(fields[0], procs)
		iters, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		ns, err := strconv.ParseFloat(fields[2], 64)
		if err != nil || fields[3] != "ns/op" {
			continue
		}
		r := Result{
			Name:       name,
			Iterations: iters,
			NsPerOp:    ns,
		}
		if ns > 0 {
			r.OpsPerSecond = 1e9 / ns
		}
		// Optional trailing columns.
		for i := 4; i+1 < len(fields); i += 2 {
			val := fields[i]
			unit := fields[i+1]
			switch unit {
			case "B/op":
				if n, err := strconv.ParseInt(val, 10, 64); err == nil {
					r.BytesPerOp = n
				}
			case "allocs/op":
				if n, err := strconv.ParseInt(val, 10, 64); err == nil {
					r.AllocsPerOp = n
				}
			}
		}
		results = append(results, r)
	}
	return results
}

// stripProcSuffix turns "BenchmarkX-8" into "BenchmarkX" so the results
// stay comparable across machines with different GOMAXPROCS. `go test`
// appends "-<procs>" only when procs > 1, so only that exact suffix is
// removed: a sub-benchmark whose own name ends in "-<digits>" (such as
// "BenchmarkX/size-10" on a one-CPU runner) keeps its name.
func stripProcSuffix(name string, procs int) string {
	if procs <= 1 {
		return name
	}
	suffix := "-" + strconv.Itoa(procs)
	if len(name) > len(suffix) && strings.HasSuffix(name, suffix) {
		return name[:len(name)-len(suffix)]
	}
	return name
}

func writeJSON(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeMarkdown(path string, r Report) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# MockAgents Benchmark Report\n\n")
	fmt.Fprintf(&buf, "_Generated %s_\n\n", r.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(&buf, "- **Go:** %s\n- **Platform:** %s/%s\n- **Package:** `%s`\n\n",
		r.GoVersion, r.GOOS, r.GOARCH, r.Package)
	fmt.Fprintln(&buf, "| Benchmark | Iterations | ns/op | ops/sec | B/op | allocs/op |")
	fmt.Fprintln(&buf, "| --- | ---: | ---: | ---: | ---: | ---: |")
	for _, row := range r.Results {
		fmt.Fprintf(&buf, "| `%s` | %d | %.1f | %s | %d | %d |\n",
			row.Name,
			row.Iterations,
			row.NsPerOp,
			humanInt(int64(row.OpsPerSecond)),
			row.BytesPerOp,
			row.AllocsPerOp,
		)
	}
	fmt.Fprintf(&buf, "\n> This file is regenerated by `make bench-report`. Do not hand-edit.\n")
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// humanInt formats an integer with comma separators so a reader can
// tell 1,200,000 from 120,000 at a glance.
func humanInt(n int64) string {
	if n < 0 {
		return "-" + humanInt(-n)
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var out strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		out.WriteString(s[:pre])
		if len(s) > pre {
			out.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		out.WriteString(s[i : i+3])
		if i+3 < len(s) {
			out.WriteByte(',')
		}
	}
	return out.String()
}
