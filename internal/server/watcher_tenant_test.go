package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
)

// writeTenantAgentYAML drops a tenant-owned Agent file into dir using the same
// `<tenant>.<name>.yaml` naming the write API persists with.
func writeTenantAgentYAML(t *testing.T, dir, tenant, name, response string) string {
	t.Helper()
	body := "apiVersion: mockagents/v1\n" +
		"kind: Agent\n" +
		"metadata:\n" +
		"  name: " + name + "\n" +
		"  tenant_id: " + tenant + "\n" +
		"spec:\n" +
		"  protocol: openai-chat-completions\n" +
		"  behavior:\n" +
		"    scenarios:\n" +
		"      - name: default\n" +
		"        response:\n" +
		"          content: \"" + response + "\"\n"
	path := filepath.Join(dir, tenant+"."+name+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// bootLoad registers a file the way cmd/mockagents does at startup: parsed
// once and registered with its source path, before any watcher exists.
func bootLoad(t *testing.T, eng *engine.Engine, path string) {
	t.Helper()
	result, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("boot load %s: %v", path, err)
	}
	config.ApplyDefaults(result.Definition)
	eng.Registry.RegisterWithSource(result.Definition, result.FilePath)
}

// TestWatcher_TenantDeleteKeepsGlobalAgent is the audit H-01 regression. A
// global `foo` is boot-loaded; tenant A then creates its own `foo` (allowed:
// names are unique per tenant bucket, not globally) and later deletes it the
// way the write API does — registry entry first, then the file. The watcher's
// reaction to the file removal must not take the global `foo` with it.
func TestWatcher_TenantDeleteKeepsGlobalAgent(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	bootLoad(t, eng, writeAgentYAML(t, dir, "foo", "global"))

	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Debounce = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	tenantFile := writeTenantAgentYAML(t, dir, "ten-a", "foo", "tenant a")
	if !waitFor(t, 2*time.Second, func() bool {
		return eng.Registry.GetOwnedForTenant("foo", "ten-a") != nil
	}) {
		t.Fatal("watcher did not register tenant a's foo")
	}

	// The write API's DeleteAgent: unregister, then remove the backing file.
	if err := eng.Registry.RemoveForTenant("foo", "ten-a"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tenantFile); err != nil {
		t.Fatal(err)
	}

	// Give the watcher time to process the Remove event, then check the
	// global agent survived. Polling for its ABSENCE would pass trivially, so
	// wait for the watcher's own bookkeeping to drop the path instead.
	if !waitFor(t, 2*time.Second, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		_, tracked := w.fileAgents[filepath.Clean(tenantFile)]
		return !tracked
	}) {
		t.Fatal("watcher never processed the tenant file's removal")
	}
	if eng.Registry.Get("foo") == nil {
		t.Fatal("global foo was removed when tenant a deleted its own foo (H-01)")
	}
	if eng.Registry.GetOwnedForTenant("foo", "ten-a") != nil {
		t.Fatal("tenant a's foo should be gone")
	}
}

// TestWatcher_SeedsBootLoadedFiles: a file the server loaded at boot, before
// the watcher existed, is still unregistered when it is deleted. Previously
// only files the watcher had seen change were tracked.
func TestWatcher_SeedsBootLoadedFiles(t *testing.T) {
	dir := t.TempDir()
	eng := newTestEngineFromReg()
	bootFile := writeAgentYAML(t, dir, "boot-agent", "hi")
	bootLoad(t, eng, bootFile)

	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Debounce = 20 * time.Millisecond
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	if err := os.Remove(bootFile); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		return eng.Registry.Get("boot-agent") == nil
	}) {
		t.Fatal("deleting a boot-loaded file did not unregister its agent")
	}
}

// TestWatcher_SeedIgnoresNestedSources: the watch is non-recursive, so a
// source under a subdirectory is not adopted (an event for it can never
// arrive, and adopting it would only confuse the claim check).
func TestWatcher_SeedIgnoresNestedSources(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	eng := newTestEngineFromReg()
	bootLoad(t, eng, writeAgentYAML(t, nested, "nested-agent", "hi"))
	bootLoad(t, eng, writeAgentYAML(t, dir, "flat-agent", "hi"))

	w := NewAgentDirWatcher(dir, eng, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.seedFromRegistry()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.fileAgents) != 1 {
		t.Fatalf("seeded %d files, want 1 (flat only): %v", len(w.fileAgents), w.fileAgents)
	}
	for _, k := range w.fileAgents {
		if k.name != "flat-agent" || k.tenant != "" {
			t.Fatalf("seeded key = %+v, want flat-agent/global", k)
		}
	}
}
