package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func defaultOptions() options {
	return options{
		BaselinePath:   "base.json",
		CandidatePath:  "cand.json",
		BytesTolerance: 0.20,
		NsThreshold:    0.25,
		NsFloorNs:      1000,
	}
}

func report(results ...Result) *Report {
	return &Report{SchemaVersion: "1", GoVersion: "go1.26.4", GOOS: "linux", GOARCH: "amd64", Package: "./internal/engine/...", Results: results}
}

func writeReport(t *testing.T, dir, name string, r any) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	good := writeReport(t, dir, "good.json", report(Result{Name: "BenchmarkA", NsPerOp: 10}))
	cases := []struct {
		name    string
		path    string
		content string
		wantErr string
	}{
		{name: "missing file", path: filepath.Join(dir, "nope.json"), wantErr: "nope.json"},
		{name: "malformed json", content: `{"schema_version":`, wantErr: "parsing"},
		{name: "wrong schema", content: `{"schema_version":"2","results":[{"name":"BenchmarkA"}]}`, wantErr: `unsupported schema_version "2"`},
		{name: "schema absent", content: `{"results":[{"name":"BenchmarkA"}]}`, wantErr: `unsupported schema_version ""`},
		{name: "numeric schema", content: `{"schema_version":1,"results":[]}`, wantErr: "parsing"},
		{name: "no results", content: `{"schema_version":"1","results":[]}`, wantErr: "no benchmark results"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".json")
				if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			r, err := load(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("load err = %v, want it to contain %q", err, tc.wantErr)
			}
			if r != nil {
				t.Fatalf("report = %+v, want nil on error", r)
			}
		})
	}
	r, err := load(good)
	if err != nil {
		t.Fatalf("load good: %v", err)
	}
	if len(r.Results) != 1 || r.Results[0].Name != "BenchmarkA" || r.Results[0].NsPerOp != 10 {
		t.Fatalf("report = %+v", r)
	}
}

func TestCompare(t *testing.T) {
	gateNs := defaultOptions()
	gateNs.GateNs = true

	cases := []struct {
		name         string
		base, cand   []Result
		opts         options
		wantFailures []string
		wantNotes    []string
	}{
		{
			name: "identical is silent",
			base: []Result{{Name: "BenchmarkA", NsPerOp: 100, BytesPerOp: 64, AllocsPerOp: 2}},
			cand: []Result{{Name: "BenchmarkA", NsPerOp: 100, BytesPerOp: 64, AllocsPerOp: 2}},
			opts: defaultOptions(),
		},
		{
			name: "noise within envelope is silent",
			base: []Result{{Name: "BenchmarkA", NsPerOp: 1000, BytesPerOp: 1000, AllocsPerOp: 5}},
			cand: []Result{{Name: "BenchmarkA", NsPerOp: 1250, BytesPerOp: 1200, AllocsPerOp: 5}},
			opts: defaultOptions(),
		},
		{
			name:         "any allocs/op change fails, both directions",
			base:         []Result{{Name: "BenchmarkUp", NsPerOp: 1, AllocsPerOp: 2}, {Name: "BenchmarkDown", NsPerOp: 1, AllocsPerOp: 2}},
			cand:         []Result{{Name: "BenchmarkUp", NsPerOp: 1, AllocsPerOp: 3}, {Name: "BenchmarkDown", NsPerOp: 1, AllocsPerOp: 1}},
			opts:         defaultOptions(),
			wantFailures: []string{"BenchmarkDown: allocs/op 2 -> 1", "BenchmarkUp: allocs/op 2 -> 3"},
		},
		{
			name: "B/op over tolerance fails both directions",
			base: []Result{{Name: "BenchmarkGrow", NsPerOp: 1, BytesPerOp: 100}, {Name: "BenchmarkShrink", NsPerOp: 1, BytesPerOp: 100}},
			cand: []Result{{Name: "BenchmarkGrow", NsPerOp: 1, BytesPerOp: 121}, {Name: "BenchmarkShrink", NsPerOp: 1, BytesPerOp: 79}},
			opts: defaultOptions(),
			wantFailures: []string{
				"BenchmarkGrow: B/op 100 -> 121 (+21%, over 20% tolerance)",
				"BenchmarkShrink: B/op 100 -> 79 (-21%, over 20% tolerance)",
			},
		},
		{
			name: "custom bytes tolerance",
			base: []Result{{Name: "BenchmarkA", NsPerOp: 1, BytesPerOp: 100}},
			cand: []Result{{Name: "BenchmarkA", NsPerOp: 1, BytesPerOp: 106}},
			opts: func() options { o := defaultOptions(); o.BytesTolerance = 0.05; return o }(),
			wantFailures: []string{
				"BenchmarkA: B/op 100 -> 106 (+6%, over 5% tolerance)",
			},
		},
		{
			name:         "zero-allocation baseline that starts allocating fails",
			base:         []Result{{Name: "BenchmarkA", NsPerOp: 1}},
			cand:         []Result{{Name: "BenchmarkA", NsPerOp: 1, BytesPerOp: 8}},
			opts:         defaultOptions(),
			wantFailures: []string{"BenchmarkA: B/op 0 -> 8 (was zero-allocation)"},
		},
		{
			name:         "missing benchmark fails, new benchmark is a note",
			base:         []Result{{Name: "BenchmarkGone", NsPerOp: 1}, {Name: "BenchmarkKept", NsPerOp: 1}},
			cand:         []Result{{Name: "BenchmarkKept", NsPerOp: 1}, {Name: "BenchmarkAdded", NsPerOp: 1}},
			opts:         defaultOptions(),
			wantFailures: []string{"BenchmarkGone: MISSING from candidate (was it renamed, or did output parsing break?)"},
			wantNotes:    []string{"BenchmarkAdded: new benchmark, not in baseline (re-run benchreport to adopt it)"},
		},
		{
			name: "ns/op drift is informational by default",
			base: []Result{{Name: "BenchmarkSlow", NsPerOp: 2000}, {Name: "BenchmarkTiny", NsPerOp: 50}},
			cand: []Result{{Name: "BenchmarkSlow", NsPerOp: 4000}, {Name: "BenchmarkTiny", NsPerOp: 20}},
			opts: defaultOptions(),
			wantNotes: []string{
				"BenchmarkSlow: ns/op 2000.0 -> 4000.0 (+100%, µs-scale — informational)",
				"BenchmarkTiny: ns/op 50.0 -> 20.0 (-60%, sub-µs — informational)",
			},
		},
		{
			name: "gate-ns fails a µs-scale regression only",
			base: []Result{
				{Name: "BenchmarkRegressed", NsPerOp: 2000},
				{Name: "BenchmarkImproved", NsPerOp: 2000},
				{Name: "BenchmarkSubMicro", NsPerOp: 500},
				{Name: "BenchmarkSteady", NsPerOp: 2000},
			},
			cand: []Result{
				{Name: "BenchmarkRegressed", NsPerOp: 3000},
				{Name: "BenchmarkImproved", NsPerOp: 1000},
				{Name: "BenchmarkSubMicro", NsPerOp: 5000},
				{Name: "BenchmarkSteady", NsPerOp: 2400},
			},
			opts:         gateNs,
			wantFailures: []string{"BenchmarkRegressed: ns/op 2000.0 -> 3000.0 (+50%, over 25% threshold)"},
			wantNotes: []string{
				"BenchmarkImproved: ns/op 2000.0 -> 1000.0 (-50%, µs-scale — informational)",
				"BenchmarkSubMicro: ns/op 500.0 -> 5000.0 (+900%, sub-µs — informational)",
			},
		},
		{
			name: "zero baseline ns/op skips ns comparison",
			base: []Result{{Name: "BenchmarkA", NsPerOp: 0}},
			cand: []Result{{Name: "BenchmarkA", NsPerOp: 1e9}},
			opts: gateNs,
		},
		{
			name: "drift exactly at a threshold is within the envelope",
			base: []Result{{Name: "BenchmarkA", NsPerOp: 2000, BytesPerOp: 100}},
			cand: []Result{{Name: "BenchmarkA", NsPerOp: 2500, BytesPerOp: 120}},
			opts: gateNs,
		},
		{
			name: "multiple failures for one benchmark are all reported",
			base: []Result{{Name: "BenchmarkA", NsPerOp: 2000, BytesPerOp: 100, AllocsPerOp: 1}},
			cand: []Result{{Name: "BenchmarkA", NsPerOp: 9000, BytesPerOp: 500, AllocsPerOp: 4}},
			opts: gateNs,
			wantFailures: []string{
				"BenchmarkA: allocs/op 1 -> 4",
				"BenchmarkA: B/op 100 -> 500 (+400%, over 20% tolerance)",
				"BenchmarkA: ns/op 2000.0 -> 9000.0 (+350%, over 25% threshold)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failures, notes := compare(report(tc.base...), report(tc.cand...), tc.opts)
			if !reflect.DeepEqual(failures, tc.wantFailures) {
				t.Errorf("failures:\n got %q\nwant %q", failures, tc.wantFailures)
			}
			if !reflect.DeepEqual(notes, tc.wantNotes) {
				t.Errorf("notes:\n got %q\nwant %q", notes, tc.wantNotes)
			}
		})
	}
}

func TestCompareFailuresAreInNameOrder(t *testing.T) {
	base := report(Result{Name: "BenchmarkC"}, Result{Name: "BenchmarkA"}, Result{Name: "BenchmarkB"})
	cand := report(Result{Name: "BenchmarkZ"})
	failures, _ := compare(base, cand, defaultOptions())
	var got []string
	for _, f := range failures {
		got = append(got, strings.SplitN(f, ":", 2)[0])
	}
	if want := []string{"BenchmarkA", "BenchmarkB", "BenchmarkC"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("failure order = %v, want %v", got, want)
	}
}

func TestRenderSummary(t *testing.T) {
	base, cand := report(Result{Name: "BenchmarkA"}, Result{Name: "BenchmarkB"}), report(Result{Name: "BenchmarkA"})
	cand.GOOS = "windows"
	o := defaultOptions()

	t.Run("pass", func(t *testing.T) {
		out := renderSummary(base, cand, o, nil, nil)
		for _, want := range []string{
			"## Benchmark guard\n\n",
			"Baseline `base.json` (go1.26.4 linux/amd64) vs candidate `cand.json` (go1.26.4 windows/amd64) — 2 benchmarks compared.",
			"### ✅ No blocking changes",
			"within the configured 20% tolerance.",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("summary missing %q:\n%s", want, out)
			}
		}
		for _, unwanted := range []string{"❌", "Notes (non-blocking)", "regenerate the baseline"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("passing summary contains %q:\n%s", unwanted, out)
			}
		}
	})

	t.Run("fail with notes", func(t *testing.T) {
		out := renderSummary(base, cand, o, []string{"f1", "f2"}, []string{"n1"})
		for _, want := range []string{
			"### ❌ 2 blocking change(s)\n\n- f1\n- f2\n",
			"go run ./tools/benchreport -pkg ./internal/engine/... -out docs/benchmarks",
			"### Notes (non-blocking)\n\n- n1\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("summary missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "✅") {
			t.Errorf("failing summary claims success:\n%s", out)
		}
	})
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	base := writeReport(t, dir, "base.json", report(
		Result{Name: "BenchmarkA", NsPerOp: 100, BytesPerOp: 64, AllocsPerOp: 2},
	))
	same := writeReport(t, dir, "same.json", report(
		Result{Name: "BenchmarkA", NsPerOp: 180, BytesPerOp: 64, AllocsPerOp: 2},
	))
	worse := writeReport(t, dir, "worse.json", report(
		Result{Name: "BenchmarkA", NsPerOp: 100, BytesPerOp: 64, AllocsPerOp: 3},
	))
	malformed := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(malformed, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"help", []string{"-h"}, 0, "", "-bytes-tolerance"},
		{"unknown flag", []string{"-nope"}, 2, "", "flag provided but not defined"},
		{"candidate required", []string{"-baseline", base}, 2, "", "-candidate is required"},
		{"baseline unreadable", []string{"-baseline", filepath.Join(dir, "missing.json"), "-candidate", same}, 2, "", "benchguard:"},
		{"candidate malformed", []string{"-baseline", base, "-candidate", malformed}, 2, "", "parsing"},
		{"within envelope", []string{"-baseline", base, "-candidate", same}, 0, "No blocking changes", ""},
		{"ns gated but sub-µs", []string{"-baseline", base, "-candidate", same, "-gate-ns"}, 0, "sub-µs — informational", ""},
		{"ns gated with low floor", []string{"-baseline", base, "-candidate", same, "-gate-ns", "-ns-floor", "1"}, 1, "ns/op 100.0 -> 180.0", ""},
		{"regression", []string{"-baseline", base, "-candidate", worse}, 1, "allocs/op 2 -> 3", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", code, tc.wantCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Errorf("stdout missing %q:\n%s", tc.wantStdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr missing %q:\n%s", tc.wantStderr, stderr.String())
			}
			if tc.wantCode == 2 && stdout.Len() != 0 {
				t.Errorf("usage/input error printed a summary:\n%s", stdout.String())
			}
		})
	}
}

func TestRunAppendsSummaryFile(t *testing.T) {
	dir := t.TempDir()
	base := writeReport(t, dir, "base.json", report(Result{Name: "BenchmarkA", NsPerOp: 1}))
	summary := filepath.Join(dir, "summary.md")
	if err := os.WriteFile(summary, []byte("previous step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	for i := 0; i < 2; i++ {
		stdout.Reset()
		if code := run([]string{"-baseline", base, "-candidate", base, "-summary", summary}, &stdout, &bytes.Buffer{}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	}
	got, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	want := "previous step\n" + stdout.String() + stdout.String()
	if string(got) != want {
		t.Fatalf("summary file:\n%s\nwant prior content plus two appended summaries", got)
	}
}

func TestRunSummaryFileErrorIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	base := writeReport(t, dir, "base.json", report(Result{Name: "BenchmarkA", NsPerOp: 1}))
	// A directory cannot be opened for append; the gate result must still
	// come from the comparison, not from the summary write.
	var stdout bytes.Buffer
	if code := run([]string{"-baseline", base, "-candidate", base, "-summary", dir}, &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "No blocking changes") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestIndexLastDuplicateWins(t *testing.T) {
	idx := index(report(Result{Name: "BenchmarkA", NsPerOp: 1}, Result{Name: "BenchmarkA", NsPerOp: 2}))
	if len(idx) != 1 || idx["BenchmarkA"].NsPerOp != 2 {
		t.Fatalf("index = %+v", idx)
	}
}
