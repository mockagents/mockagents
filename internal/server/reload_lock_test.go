package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

// newReloadServer exposes reload alongside the write routes so the two can be
// driven against one registry.
func newReloadServer(t *testing.T, agentsDir string) (*httptest.Server, *Handlers) {
	t.Helper()
	reg := engine.NewAgentRegistry()
	eng := engine.NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := &Handlers{
		Engine:    eng,
		AgentsDir: agentsDir,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agents", h.CreateAgent)
	mux.HandleFunc("DELETE /api/v1/agents/{name}", h.DeleteAgent)
	mux.HandleFunc("POST /api/v1/agents/{name}/reload", h.ReloadAgent)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func writeYAML(t *testing.T, path, name, model, greeting string) {
	t.Helper()
	body := strings.Join([]string{
		"apiVersion: mockagents/v1",
		"kind: Agent",
		"metadata:",
		"  name: " + name,
		"spec:",
		"  protocol: openai-chat-completions",
		"  model: " + model,
		"  behavior:",
		"    scenarios:",
		"      - name: default",
		"        response:",
		"          content: \"" + greeting + "\"",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestReloadReadsTheTrackedSource is the audit M-03 guard for which file a
// reload reads. The registry records the file each agent came from; reload
// used to re-scan the whole directory and take whichever document matched the
// name, so a second file declaring the same name could be loaded over the
// agent the caller asked to reload.
func TestReloadReadsTheTrackedSource(t *testing.T) {
	dir := t.TempDir()
	tracked := filepath.Join(dir, "tracked.yaml")
	decoy := filepath.Join(dir, "decoy.yaml")
	writeYAML(t, tracked, "support", "gpt-4o", "from the tracked file")
	writeYAML(t, decoy, "support", "gpt-4o", "from the decoy")

	srv, h := newReloadServer(t, dir)
	// Register the agent against the tracked file, the way startup does.
	loaded := loadOne(t, tracked)
	h.Engine.Registry.RegisterWithSource(loaded, tracked)

	resp, err := http.Post(srv.URL+"/api/v1/agents/support/reload", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reload status = %d, want 200", resp.StatusCode)
	}

	got := h.Engine.Registry.Get("support")
	if got == nil {
		t.Fatal("agent vanished after reload")
	}
	if content := got.Spec.Behavior.Scenarios[0].Response.Content; !strings.Contains(content, "tracked") {
		t.Errorf("reload loaded the wrong file: scenario content = %q", content)
	}
	if src := h.Engine.Registry.Source("support", ""); src != tracked {
		t.Errorf("source moved to %q, want %q", src, tracked)
	}
}

// TestReloadRejectsARenamedDefinition: if the tracked file no longer declares
// this agent, reloading it would register a different definition under the
// caller's authority. That is a conflict, not a reload.
func TestReloadRejectsARenamedDefinition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "support.yaml")
	writeYAML(t, path, "support", "gpt-4o", "hi")

	srv, h := newReloadServer(t, dir)
	h.Engine.Registry.RegisterWithSource(loadOne(t, path), path)

	// The file is edited to define a different agent.
	writeYAML(t, path, "renamed", "gpt-4o", "hi")

	resp, err := http.Post(srv.URL+"/api/v1/agents/support/reload", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
	if got := h.Engine.Registry.Get("renamed"); got != nil {
		t.Error("reload registered the renamed definition")
	}
}

// TestReloadDoesNotResurrectADeletedAgent is the reason the lock exists
// (audit M-03). A reload that interleaves with a DELETE could re-register the
// definition the delete had just removed, bringing back an agent the operator
// believed was gone. Whichever order the two land in, the agent must be absent
// once both have returned: delete-then-reload gives a 404 reload, and
// reload-then-delete removes what the reload restored.
func TestReloadDoesNotResurrectADeletedAgent(t *testing.T) {
	for i := 0; i < 40; i++ {
		dir := t.TempDir()
		path := filepath.Join(dir, "support.yaml")
		writeYAML(t, path, "support", "gpt-4o", "hi")

		srv, h := newReloadServer(t, dir)
		h.Engine.Registry.RegisterWithSource(loadOne(t, path), path)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/agents/support", nil)
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}()
		go func() {
			defer wg.Done()
			if resp, err := http.Post(srv.URL+"/api/v1/agents/support/reload", "application/json", nil); err == nil {
				resp.Body.Close()
			}
		}()
		wg.Wait()

		if got := h.Engine.Registry.Get("support"); got != nil {
			t.Fatalf("iteration %d: a concurrent reload resurrected the deleted agent", i)
		}
	}
}

// loadOne parses a single agent definition file the way startup does.
func loadOne(t *testing.T, path string) *types.AgentDefinition {
	t.Helper()
	result, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	config.ApplyDefaults(result.Definition)
	return result.Definition
}
