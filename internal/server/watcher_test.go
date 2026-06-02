package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestWatcherUnregistersDeletedFile(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Debounce = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	path := writeAgentYAML(t, dir, "to-delete", "bye")
	if !waitFor(t, 2*time.Second, func() bool {
		return eng.Registry.Get("to-delete") != nil
	}) {
		t.Fatalf("watcher did not register to-delete")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		return eng.Registry.Get("to-delete") == nil
	}) {
		t.Fatalf("watcher did not unregister the deleted agent")
	}
}

func TestWatcher_RenameDoesNotDropAgent(t *testing.T) {
	// A file rename (old path removed, new path added) must not drop the
	// agent, even in the worst-case ordering where the new path registers
	// before the old path's removal is processed. Driven via direct
	// reloadFile calls so the ordering is deterministic (no fsnotify timing).
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))

	foo := writeAgentYAML(t, dir, "echo", "hi") // dir/echo.yaml, agent name "echo"
	w.reloadFile(foo)
	if eng.Registry.Get("echo") == nil {
		t.Fatal("agent echo should be registered from the initial file")
	}

	// Rename the file: bar.yaml now holds the "echo" agent; foo is gone.
	bar := filepath.Join(dir, "bar.yaml")
	if err := os.Rename(foo, bar); err != nil {
		t.Fatalf("rename: %v", err)
	}
	w.reloadFile(bar) // new path registers "echo" first...
	w.reloadFile(foo) // ...then the old path's removal is processed.

	if eng.Registry.Get("echo") == nil {
		t.Error("agent echo was dropped by a file rename; removeIfUnclaimed should have kept it")
	}
}

func TestWatcher_RenamedAgentNameRemovesOld(t *testing.T) {
	// Editing a file to change metadata.name in place must unregister the old
	// name and register the new one.
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))

	path := filepath.Join(dir, "agent.yaml")
	write := func(name string) {
		body := "apiVersion: mockagents/v1\nkind: Agent\nmetadata:\n  name: " + name +
			"\nspec:\n  protocol: openai-chat-completions\n  behavior:\n    scenarios:\n" +
			"      - name: default\n        response:\n          content: \"x\"\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	write("old-name")
	w.reloadFile(path)
	if eng.Registry.Get("old-name") == nil {
		t.Fatal("old-name should be registered")
	}

	write("new-name")
	w.reloadFile(path)
	if eng.Registry.Get("new-name") == nil {
		t.Error("new-name should be registered after the in-file rename")
	}
	if eng.Registry.Get("old-name") != nil {
		t.Error("old-name should have been unregistered after the in-file rename")
	}
}

// TestWatcher_StopCancelsPendingReload verifies that a reload still sitting
// in the debounce window when Stop is called is cancelled, not applied
// (F-WT-001). A long debounce makes the window deterministic: we arm the
// timer, Stop before it fires, then confirm the agent never registers even
// after the debounce interval has elapsed.
func TestWatcher_StopCancelsPendingReload(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Debounce = 500 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	writeAgentYAML(t, dir, "pending", "hi")
	// Wait until the debounce timer is armed but has not yet fired.
	if !waitFor(t, 2*time.Second, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.pending) > 0
	}) {
		t.Fatal("watcher never armed a pending reload")
	}

	w.Stop() // must stop the pending timer and not register the agent

	// Give the original debounce window time to elapse; a leaked timer would
	// register the agent in this interval.
	time.Sleep(700 * time.Millisecond)
	if eng.Registry.Get("pending") != nil {
		t.Error("pending reload fired after Stop; timer was not cancelled")
	}
}

// TestWatcher_ConcurrentStop hammers Stop from several goroutines to prove
// the stopOnce guard makes it race-safe and non-blocking (F-WT-003).
func TestWatcher_ConcurrentStop(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Stop()
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Stop deadlocked")
	}
}

// TestWatcher_PathNormalization confirms that the same file referenced with a
// redundant "./" prefix collapses onto one fileAgents key (F-WT-004), so a
// later canonical-path delete unregisters the agent.
func TestWatcher_PathNormalization(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))

	clean := writeAgentYAML(t, dir, "norm", "hi") // dir/norm.yaml
	messy := filepath.Join(dir, ".", "norm.yaml") // same file, non-canonical
	w.reloadFile(messy)
	if eng.Registry.Get("norm") == nil {
		t.Fatal("agent norm should be registered")
	}

	w.mu.Lock()
	_, hasClean := w.fileAgents[filepath.Clean(clean)]
	keyCount := len(w.fileAgents)
	w.mu.Unlock()
	if !hasClean {
		t.Error("fileAgents not keyed by the canonical path")
	}
	if keyCount != 1 {
		t.Errorf("expected exactly one fileAgents key, got %d", keyCount)
	}
}
