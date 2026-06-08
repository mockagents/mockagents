package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
)

// newAgentWriteServer builds a single-tenant server exposing the agent write +
// read routes, backed by an empty registry and the given agents dir ("" = no
// persistence). Returns the test server and the live handlers.
func newAgentWriteServer(t *testing.T, agentsDir string) (*httptest.Server, *Handlers) {
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
	mux.HandleFunc("PUT /api/v1/agents/{name}", h.PutAgent)
	mux.HandleFunc("DELETE /api/v1/agents/{name}", h.DeleteAgent)
	mux.HandleFunc("GET /api/v1/agents/{name}", h.GetAgent)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func agentYAML(name, model string) string {
	return strings.Join([]string{
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
		"          content: \"hi from " + name + "\"",
		"",
	}, "\n")
}

func doReq(t *testing.T, method, url, contentType, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestCreateAgent_ServesImmediatelyAndPersists(t *testing.T) {
	dir := t.TempDir()
	srv, h := newAgentWriteServer(t, dir)

	code, body := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", agentYAML("hot-bot", "hot-model"))
	if code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", code, body)
	}
	// Serves immediately: registry resolves by name and by model.
	if h.Engine.Registry.GetForTenant("hot-bot", "") == nil {
		t.Error("agent not registered after create")
	}
	if h.Engine.Registry.GetByModel("hot-model") == nil {
		t.Error("agent not resolvable by model after create")
	}
	// Persisted to disk.
	if _, err := os.Stat(filepath.Join(dir, "hot-bot.yaml")); err != nil {
		t.Errorf("expected persisted file: %v", err)
	}
	if !strings.Contains(body, `"persisted":true`) {
		t.Errorf("expected persisted:true, body=%s", body)
	}
}

func TestCreateAgent_DuplicateConflict(t *testing.T) {
	srv, _ := newAgentWriteServer(t, t.TempDir())
	doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", agentYAML("dup", "m1"))
	code, _ := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", agentYAML("dup", "m2"))
	if code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want 409", code)
	}
}

func TestCreateAgent_InvalidValidation(t *testing.T) {
	srv, _ := newAgentWriteServer(t, t.TempDir())
	// Missing scenarios → validator rejects with 422.
	bad := strings.Join([]string{
		"apiVersion: mockagents/v1",
		"kind: Agent",
		"metadata: { name: broken }",
		"spec: { protocol: openai-chat-completions, model: m, behavior: { scenarios: [] } }",
		"",
	}, "\n")
	code, body := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", bad)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid create status = %d (body=%s), want 422", code, body)
	}
}

func TestCreateAgent_WrongKind(t *testing.T) {
	srv, _ := newAgentWriteServer(t, t.TempDir())
	pipeline := "apiVersion: mockagents/v1\nkind: Pipeline\nmetadata: { name: notanagent }\nspec: {}\n"
	code, _ := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", pipeline)
	if code != http.StatusBadRequest {
		t.Fatalf("wrong-kind create status = %d, want 400", code)
	}
}

func TestCreateAgent_AcceptsJSON(t *testing.T) {
	srv, h := newAgentWriteServer(t, t.TempDir())
	jsonBody := `{"apiVersion":"mockagents/v1","kind":"Agent","metadata":{"name":"json-bot"},` +
		`"spec":{"protocol":"openai-chat-completions","model":"jm",` +
		`"behavior":{"scenarios":[{"name":"default","response":{"content":"hi"}}]}}}`
	code, body := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/json", jsonBody)
	if code != http.StatusCreated {
		t.Fatalf("json create status = %d, body=%s", code, body)
	}
	if h.Engine.Registry.GetForTenant("json-bot", "") == nil {
		t.Error("json agent not registered")
	}
}

func TestPutAgent_CreateThenUpdate(t *testing.T) {
	srv, h := newAgentWriteServer(t, t.TempDir())

	// PUT new → 201 created.
	code, body := doReq(t, "PUT", srv.URL+"/api/v1/agents/put-bot", "application/yaml", agentYAML("put-bot", "v1"))
	if code != http.StatusCreated || !strings.Contains(body, `"status":"created"`) {
		t.Fatalf("put-new status = %d body=%s", code, body)
	}
	// PUT existing → 200 updated, new model live.
	code, body = doReq(t, "PUT", srv.URL+"/api/v1/agents/put-bot", "application/yaml", agentYAML("put-bot", "v2"))
	if code != http.StatusOK || !strings.Contains(body, `"status":"updated"`) {
		t.Fatalf("put-existing status = %d body=%s", code, body)
	}
	if h.Engine.Registry.GetByModel("v2") == nil {
		t.Error("updated model v2 not live")
	}
	if h.Engine.Registry.GetByModel("v1") != nil {
		t.Error("stale model v1 still resolvable after update")
	}
}

func TestPutAgent_NameMismatch(t *testing.T) {
	srv, _ := newAgentWriteServer(t, t.TempDir())
	code, _ := doReq(t, "PUT", srv.URL+"/api/v1/agents/path-name", "application/yaml", agentYAML("body-name", "m"))
	if code != http.StatusBadRequest {
		t.Fatalf("name-mismatch status = %d, want 400", code)
	}
}

func TestPutAgent_UnsafePathName(t *testing.T) {
	srv, _ := newAgentWriteServer(t, t.TempDir())
	// A name with an uppercase/space is rejected before any filesystem use.
	code, _ := doReq(t, "PUT", srv.URL+"/api/v1/agents/Bad_Name", "application/yaml", agentYAML("Bad_Name", "m"))
	if code != http.StatusBadRequest {
		t.Fatalf("unsafe-path status = %d, want 400", code)
	}
}

func TestDeleteAgent_RemovesAndUnserves(t *testing.T) {
	dir := t.TempDir()
	srv, h := newAgentWriteServer(t, dir)
	doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", agentYAML("del-bot", "dm"))
	if _, err := os.Stat(filepath.Join(dir, "del-bot.yaml")); err != nil {
		t.Fatalf("precondition: file should exist: %v", err)
	}

	code, _ := doReq(t, "DELETE", srv.URL+"/api/v1/agents/del-bot", "", "")
	if code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", code)
	}
	if h.Engine.Registry.GetForTenant("del-bot", "") != nil {
		t.Error("agent still registered after delete")
	}
	if _, err := os.Stat(filepath.Join(dir, "del-bot.yaml")); !os.IsNotExist(err) {
		t.Errorf("persisted file should be removed, stat err=%v", err)
	}
	// Deleting again → 404.
	code, _ = doReq(t, "DELETE", srv.URL+"/api/v1/agents/del-bot", "", "")
	if code != http.StatusNotFound {
		t.Fatalf("re-delete status = %d, want 404", code)
	}
}

func TestCreateAgent_NoPersistenceWhenNoDir(t *testing.T) {
	srv, h := newAgentWriteServer(t, "") // no agents dir
	code, body := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", agentYAML("mem-bot", "mm"))
	if code != http.StatusCreated {
		t.Fatalf("create status = %d", code)
	}
	if !strings.Contains(body, `"persisted":false`) {
		t.Errorf("expected persisted:false with no AgentsDir, body=%s", body)
	}
	if h.Engine.Registry.GetForTenant("mem-bot", "") == nil {
		t.Error("in-memory agent not registered")
	}
}
