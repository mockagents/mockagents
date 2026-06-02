package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestServer(t *testing.T, agents ...*types.AgentDefinition) (*Server, string) {
	t.Helper()
	registry := engine.NewAgentRegistry()
	for _, a := range agents {
		registry.Register(a)
	}
	store := state.NewMemoryStore(5 * time.Minute)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(registry, store, logger)

	// Create temp agents dir with a YAML file for reload testing.
	agentsDir := t.TempDir()
	for _, a := range agents {
		writeAgentFile(t, agentsDir, a)
	}

	cfg := DefaultConfig()
	cfg.Port = 0 // Auto-assign port for testing.
	cfg.AgentsDir = agentsDir

	srv := New(eng, cfg, logger)

	// Bind synchronously in the test goroutine so the listener field
	// is fully initialized before the serve goroutine starts — that
	// way ListenAddr is race-free.
	require.NoError(t, srv.Listen(), "listen")
	addr := fmt.Sprintf("http://%s", srv.ListenAddr())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve() }()
	_ = errCh // serve errors surface via t.Cleanup → Shutdown

	t.Cleanup(func() {
		srv.Shutdown()
	})

	return srv, addr
}

func setupTenantServer(t *testing.T, agents ...*types.AgentDefinition) (*Server, string, string) {
	t.Helper()
	tenancyStore, err := tenancy.NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tenancyStore.Close() })
	tenant, err := tenancyStore.CreateTenant(t.Context(), "acme")
	require.NoError(t, err)
	key, err := tenancyStore.CreateAPIKey(t.Context(), tenant.ID, "admin", tenancy.RoleAdmin)
	require.NoError(t, err)

	registry := engine.NewAgentRegistry()
	for _, a := range agents {
		if a.Metadata.TenantID == "ten_acme" {
			a.Metadata.TenantID = tenant.ID
		}
		registry.Register(a)
	}
	store := state.NewMemoryStore(5 * time.Minute)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(registry, store, logger)

	agentsDir := t.TempDir()
	for _, a := range agents {
		writeAgentFile(t, agentsDir, a)
	}

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = agentsDir
	cfg.TenancyStore = tenancyStore

	srv := New(eng, cfg, logger)
	require.NoError(t, srv.Listen(), "listen")
	addr := fmt.Sprintf("http://%s", srv.ListenAddr())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve() }()
	_ = errCh
	t.Cleanup(func() { _ = srv.Shutdown() })

	return srv, addr, key.Plaintext
}

func assertModelIDs(t *testing.T, resp *http.Response, want []string) {
	t.Helper()
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	got := make([]string, 0, len(body.Data))
	for _, row := range body.Data {
		got = append(got, row.ID)
	}
	assert.ElementsMatch(t, want, got)
}

func writeAgentFile(t *testing.T, dir string, agent *types.AgentDefinition) {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: mockagents/v1
kind: Agent
metadata:
  name: %s
spec:
  protocol: openai-chat-completions
  model: %s
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello from %s"
`, agent.Metadata.Name, agent.Spec.Model, agent.Metadata.Name)

	path := filepath.Join(dir, agent.Metadata.Name+".yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func testFullAgent(name, model string) *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: "mockagents/v1",
		Kind:       "Agent",
		Metadata:   types.Metadata{Name: name},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Model:    model,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:     "greeting",
						Match:    &types.MatchRule{ContentContains: "hello"},
						Response: types.ScenarioResponse{Content: "Hi there!"},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "How can I help?"},
					},
				},
			},
		},
	}
}

func TestServer_HealthCheck(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.NotEmpty(t, body["version"])
	assert.NotEmpty(t, body["uptime"])
}

func TestServer_DefaultConfigBindsLocalhost(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, DefaultHost, cfg.Host)
	assert.Equal(t, "127.0.0.1", cfg.Host)
}

func TestServer_ListAgents(t *testing.T) {
	_, addr := setupTestServer(t,
		testFullAgent("agent-alpha", "model-a"),
		testFullAgent("agent-bravo", "model-b"),
	)

	resp, err := http.Get(addr + "/api/v1/agents")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var agents []AgentSummary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&agents))
	assert.Len(t, agents, 2)
	assert.Equal(t, "agent-alpha", agents[0].Name)
	assert.Equal(t, "agent-bravo", agents[1].Name)
}

func TestServer_ModelsScopedByAuthenticatedTenant(t *testing.T) {
	global := testFullAgent("global-agent", "model-global")
	tenantAgent := testFullAgent("tenant-agent", "model-tenant")
	tenantAgent.Metadata.TenantID = "ten_acme"

	srv, addr, apiKey := setupTenantServer(t, global, tenantAgent)
	_ = srv

	resp, err := http.Get(addr + "/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assertModelIDs(t, resp, []string{"model-global"})

	req, err := http.NewRequest(http.MethodGet, addr+"/v1/models", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assertModelIDs(t, resp, []string{"model-global", "model-tenant"})
}

func TestServer_TenantHeaderCannotSelectTenantAgent(t *testing.T) {
	global := testFullAgent("global-agent", "model-global")
	global2 := testFullAgent("global-agent-two", "model-global-two")
	tenantAgent := testFullAgent("tenant-agent", "model-tenant")
	tenantAgent.Metadata.TenantID = "ten_acme"

	_, addr, _ := setupTenantServer(t, global, global2, tenantAgent)

	body, _ := json.Marshal(map[string]any{
		"model": "model-tenant",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	})
	req, err := http.NewRequest(http.MethodPost, addr+"/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mockagents-Tenant", "ten_acme")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestServer_ValidAPIKeySelectsTenantAgent(t *testing.T) {
	global := testFullAgent("global-agent", "model-global")
	tenantAgent := testFullAgent("tenant-agent", "model-tenant")
	tenantAgent.Metadata.TenantID = "ten_acme"

	_, addr, apiKey := setupTenantServer(t, global, tenantAgent)

	body, _ := json.Marshal(map[string]any{
		"model": "model-tenant",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	})
	req, err := http.NewRequest(http.MethodPost, addr+"/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServer_GetAgent(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/api/v1/agents/test-agent")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var agent types.AgentDefinition
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&agent))
	assert.Equal(t, "test-agent", agent.Metadata.Name)
}

func TestServer_GetAgent_NotFound(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/api/v1/agents/nonexistent")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Contains(t, body["error"], "not found")
	assert.NotNil(t, body["available_agents"])
}

func TestServer_ReloadAgent(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Post(addr+"/api/v1/agents/test-agent/reload", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "reloaded", body["status"])
}

func TestServer_ReloadAgent_NotFound(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Post(addr+"/api/v1/agents/nonexistent/reload", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestServer_ProcessRequest(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	body, _ := json.Marshal(engine.InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-1",
		Messages:  []engine.RequestMessage{{Role: "user", Content: "hello"}},
	})

	resp, err := http.Post(addr+"/v1/engines/process", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result engine.Response
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "Hi there!", result.Content)
	assert.Equal(t, "greeting", result.ScenarioName)
}

func TestServer_ProcessRequest_AgentNotFound(t *testing.T) {
	_, addr := setupTestServer(t,
		testFullAgent("agent-a", "model-a"),
		testFullAgent("agent-b", "model-b"),
	)

	body, _ := json.Marshal(engine.InboundRequest{
		AgentName: "nonexistent",
		SessionID: "sess-1",
		Messages:  []engine.RequestMessage{{Role: "user", Content: "hello"}},
	})

	resp, err := http.Post(addr+"/v1/engines/process", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestServer_ProcessRequest_EmptyBody(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Post(addr+"/v1/engines/process", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_RequestIDHeader(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEmpty(t, resp.Header.Get("X-Request-Id"))
}

func TestServer_CORSHeaders(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestServer_ContentTypeJSON(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

func TestServer_GracefulShutdown(t *testing.T) {
	srv, _ := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	err := srv.Shutdown()
	assert.NoError(t, err)
}

// TestReloadAgent_GatedInMultiTenant covers F-HD-001: in multi-tenant mode
// the reload route is now behind RequireRole(Editor), so an anonymous
// caller is refused (401) while an admin (editor+) still reloads.
func TestReloadAgent_GatedInMultiTenant(t *testing.T) {
	// A global agent (no tenant_id) so the on-disk file's empty tenant
	// matches the registered one (F-HD-002 tenant match).
	global := testFullAgent("gate-bot", "gate-model")
	_, addr, adminKey := setupTenantServer(t, global)

	// Anonymous reload → 401 (the route is gated now).
	req, _ := http.NewRequest("POST", addr+"/api/v1/agents/gate-bot/reload", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "anonymous reload must be 401")

	// Admin (editor+) reload still works.
	req2, _ := http.NewRequest("POST", addr+"/api/v1/agents/gate-bot/reload", nil)
	req2.Header.Set("Authorization", "Bearer "+adminKey)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "admin reload should succeed")
}
