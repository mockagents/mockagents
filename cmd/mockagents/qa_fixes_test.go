package main

// Regression tests for the QA-reported environment bugs:
//   - expandSuiteArg: `mockagents test 'tests/*.yaml'` must expand globs
//     in-process (zsh quoting, Windows shells, docker run args).
//   - dataPath: MOCKAGENTS_DATA_DIR must relocate on-disk SQLite state so
//     the server can log when the working directory is read-only.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandSuiteArg_PlainPathPassesThrough(t *testing.T) {
	got, err := expandSuiteArg("tests/planner-suite.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "tests/planner-suite.yaml" {
		t.Errorf("plain path should pass through untouched, got %v", got)
	}
}

func TestExpandSuiteArg_GlobExpandsSorted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b-suite.yaml", "a-suite.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("kind: TestSuite"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := expandSuiteArg(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 matches, got %v", got)
	}
	if filepath.Base(got[0]) != "a-suite.yaml" || filepath.Base(got[1]) != "b-suite.yaml" {
		t.Errorf("matches should be sorted for a deterministic run order, got %v", got)
	}
}

func TestExpandSuiteArg_NoMatchErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := expandSuiteArg(filepath.Join(dir, "*.yaml")); err == nil {
		t.Error("unmatched glob should error rather than silently run zero suites")
	}
}

func TestDataPath_DefaultIsWorkingDirectory(t *testing.T) {
	t.Setenv("MOCKAGENTS_DATA_DIR", "")
	if got := dataPath(".mockagents.db"); got != ".mockagents.db" {
		t.Errorf("unset MOCKAGENTS_DATA_DIR must preserve the relative-path default, got %q", got)
	}
}

func TestDataPath_EnvRelocatesAndCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state") // does not exist yet
	t.Setenv("MOCKAGENTS_DATA_DIR", dir)
	got := dataPath(".mockagents.db")
	if want := filepath.Join(dir, ".mockagents.db"); got != want {
		t.Errorf("dataPath = %q, want %q", got, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("dataPath should create the data directory: %v", err)
	}
}
