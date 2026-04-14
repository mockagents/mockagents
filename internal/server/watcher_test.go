package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
)

// writeAgentYAML drops a minimal mockagents/v1 Agent file into dir.
func writeAgentYAML(t *testing.T, dir, name, response string) string {
	t.Helper()
	body := "apiVersion: mockagents/v1\n" +
		"kind: Agent\n" +
		"metadata:\n" +
		"  name: " + name + "\n" +
		"spec:\n" +
		"  protocol: openai-chat-completions\n" +
		"  behavior:\n" +
		"    scenarios:\n" +
		"      - name: default\n" +
		"        response:\n" +
		"          content: \"" + response + "\"\n"
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// newTestEngineFromReg wires a throwaway engine around a fresh registry.
func newTestEngineFromReg() *engine.Engine {
	reg := engine.NewAgentRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return engine.NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL), logger)
}

// waitFor polls a predicate at 20ms intervals until it returns true or
// the timeout elapses. Gives fsnotify time to deliver events without
// hardcoding a fragile sleep.
func waitFor(t *testing.T, timeout time.Duration, pred func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestWatcherPicksUpNewFile(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Small debounce so tests don't wait on real-world intervals.
	w.Debounce = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	writeAgentYAML(t, dir, "echo-one", "hello world")

	if !waitFor(t, 2*time.Second, func() bool {
		return eng.Registry.Get("echo-one") != nil
	}) {
		t.Fatalf("watcher did not register echo-one in time")
	}
	if got := eng.Registry.Get("echo-one"); got.Spec.Behavior.Scenarios[0].Response.Content != "hello world" {
		t.Errorf("unexpected scenario content: %+v", got.Spec.Behavior.Scenarios[0].Response)
	}
}

func TestWatcherUpdatesOnRewrite(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Debounce = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	writeAgentYAML(t, dir, "echo-two", "v1")
	if !waitFor(t, 2*time.Second, func() bool {
		got := eng.Registry.Get("echo-two")
		return got != nil && got.Spec.Behavior.Scenarios[0].Response.Content == "v1"
	}) {
		t.Fatalf("watcher did not register initial version")
	}

	writeAgentYAML(t, dir, "echo-two", "v2")
	if !waitFor(t, 2*time.Second, func() bool {
		got := eng.Registry.Get("echo-two")
		return got != nil && got.Spec.Behavior.Scenarios[0].Response.Content == "v2"
	}) {
		t.Fatalf("watcher did not pick up the rewrite")
	}
}

func TestWatcherIgnoresInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Debounce = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	// First, seed a valid agent so the registry has a baseline to
	// preserve.
	writeAgentYAML(t, dir, "echo-three", "baseline")
	if !waitFor(t, 2*time.Second, func() bool { return eng.Registry.Get("echo-three") != nil }) {
		t.Fatal("baseline not registered")
	}

	// Write garbage into the same path — the watcher should log a
	// warning and keep the previous definition rather than throw
	// the known-good state away.
	path := filepath.Join(dir, "echo-three.yaml")
	if err := os.WriteFile(path, []byte("not yaml at all: [::"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Give the watcher a moment — we're asserting the absence of
	// destructive behavior, so short sleep is acceptable.
	time.Sleep(200 * time.Millisecond)
	got := eng.Registry.Get("echo-three")
	if got == nil {
		t.Fatal("baseline was wiped by invalid rewrite")
	}
	if got.Spec.Behavior.Scenarios[0].Response.Content != "baseline" {
		t.Errorf("baseline content replaced: %+v", got.Spec.Behavior.Scenarios[0].Response)
	}
}

func TestWatcherSkipsHiddenAndTempFiles(t *testing.T) {
	if !isAgentFile("/tmp/good.yaml") {
		t.Error(".yaml should be accepted")
	}
	if isAgentFile("/tmp/.hidden.yaml") {
		t.Error("dotfiles should be skipped")
	}
	if isAgentFile("/tmp/agent.yaml~") {
		t.Error("editor backup files should be skipped")
	}
	if isAgentFile("/tmp/notes.md") {
		t.Error(".md files should be skipped")
	}
}

func TestWatcherStopIdempotent(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	w.Stop()
	w.Stop() // must not panic or block

	// Guard against OS-dependent absolute path assumptions.
	if !strings.Contains(dir, string(os.PathSeparator)) {
		t.Errorf("unexpected temp dir shape: %s", dir)
	}
}
