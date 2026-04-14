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

	// Start server in background.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	// Wait for server to be ready and get actual address.
	var addr string
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		if listenAddr := srv.ListenAddr(); listenAddr != ":0" {
			addr = fmt.Sprintf("http://%s", listenAddr)
			break
		}
	}
	require.NotEmpty(t, addr, "server did not start in time")

	t.Cleanup(func() {
		srv.Shutdown()
	})

	return srv, addr
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
