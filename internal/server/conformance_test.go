package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- OpenAI Conformance Tests ---

func TestConformance_OpenAI_ResponseFormat(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("openai-agent", "gpt-4o"))

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	// TC-302: Required OpenAI fields.
	assert.Contains(t, result["id"], "chatcmpl-")
	assert.Equal(t, "chat.completion", result["object"])
	assert.NotNil(t, result["created"])
	assert.Equal(t, "gpt-4o", result["model"])
	assert.NotNil(t, result["choices"])
	assert.NotNil(t, result["usage"])

	// Validate choices structure.
	choices := result["choices"].([]any)
	require.Len(t, choices, 1)
	choice := choices[0].(map[string]any)
	assert.Equal(t, float64(0), choice["index"])
	assert.NotNil(t, choice["message"])
	assert.NotEmpty(t, choice["finish_reason"])

	msg := choice["message"].(map[string]any)
	assert.Equal(t, "assistant", msg["role"])

	// Validate usage structure.
	usage := result["usage"].(map[string]any)
	assert.NotNil(t, usage["prompt_tokens"])
	assert.NotNil(t, usage["completion_tokens"])
	assert.NotNil(t, usage["total_tokens"])
}

func TestConformance_OpenAI_ToolCallsFormat(t *testing.T) {
	agent := testFullAgent("tool-agent", "gpt-4o")
	agent.Spec.Tools = []types.ToolDefinition{
		{Name: "search", Responses: []types.ToolResponseRule{{IsDefault: true, Response: "ok"}}},
	}
	agent.Spec.Behavior.Scenarios = []types.Scenario{
		{
			Name:  "tool-use",
			Match: &types.MatchRule{ContentContains: "search"},
			Response: types.ScenarioResponse{
				Content:   "Searching.",
				ToolCalls: []types.ToolCallSpec{{Name: "search", Arguments: map[string]any{"q": "test"}}},
			},
		},
		{Name: "default", Response: types.ScenarioResponse{Content: "hi"}},
	}

	_, addr := setupTestServer(t, agent)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"search for test"}]}`
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	choices := result["choices"].([]any)
	choice := choices[0].(map[string]any)
	assert.Equal(t, "tool_calls", choice["finish_reason"])

	msg := choice["message"].(map[string]any)
	toolCalls, ok := msg["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, toolCalls, 1)

	tc := toolCalls[0].(map[string]any)
	assert.NotEmpty(t, tc["id"])
	assert.Equal(t, "function", tc["type"])
	fn := tc["function"].(map[string]any)
	assert.Equal(t, "search", fn["name"])
	assert.NotEmpty(t, fn["arguments"])
}

func TestConformance_OpenAI_StreamingFormat(t *testing.T) {
	agent := testFullAgent("stream-agent", "gpt-4o")
	agent.Spec.Behavior.Streaming = &types.StreamingConfig{Enabled: true, ChunkSize: 2, ChunkDelayMs: 0}

	_, addr := setupTestServer(t, agent)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`
	resp, err := http.Post(addr+"/v1/engines/process", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Parse SSE and verify format.
	var chunks []map[string]any
	var hasDone bool
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)

	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "data: [DONE]") {
			hasDone = true
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(line[6:]), &chunk); err == nil {
			chunks = append(chunks, chunk)
		}
	}

	assert.True(t, hasDone, "stream should end with [DONE]")
	require.GreaterOrEqual(t, len(chunks), 3) // role + content + finish

	// First chunk should have role.
	firstChoices := chunks[0]["choices"].([]any)
	firstDelta := firstChoices[0].(map[string]any)["delta"].(map[string]any)
	assert.Equal(t, "assistant", firstDelta["role"])

	// All chunks should have consistent ID and object.
	id := chunks[0]["id"]
	for _, chunk := range chunks {
		assert.Equal(t, id, chunk["id"])
		assert.Equal(t, "chat.completion.chunk", chunk["object"])
	}
}

// --- Anthropic Conformance Tests ---

func TestConformance_Anthropic_ResponseFormat(t *testing.T) {
	agent := testFullAgent("anthropic-agent", "claude-3")
	agent.Spec.Protocol = "anthropic-messages"
	_, addr := setupTestServer(t, agent)

	body := `{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":1024}`
	req, _ := http.NewRequest("POST", addr+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "test-key")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	// TC-402: Required Anthropic fields.
	assert.Contains(t, result["id"].(string), "msg_")
	assert.Equal(t, "message", result["type"])
	assert.Equal(t, "assistant", result["role"])
	assert.NotNil(t, result["content"])
	assert.Equal(t, "claude-3", result["model"])
	assert.NotEmpty(t, result["stop_reason"])
	assert.NotNil(t, result["usage"])

	// Validate content blocks.
	content := result["content"].([]any)
	require.GreaterOrEqual(t, len(content), 1)
	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.NotEmpty(t, block["text"])

	// Validate usage.
	usage := result["usage"].(map[string]any)
	assert.NotNil(t, usage["input_tokens"])
	assert.NotNil(t, usage["output_tokens"])
}

func TestConformance_Anthropic_MissingAPIKey(t *testing.T) {
	agent := testFullAgent("auth-agent", "claude-3")
	agent.Spec.Protocol = "anthropic-messages"
	_, addr := setupTestServer(t, agent)

	body := `{"model":"claude-3","messages":[{"role":"user","content":"hello"}],"max_tokens":1024}`
	resp, err := http.Post(addr+"/v1/messages", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// --- Error Response Conformance ---

func TestConformance_OpenAI_InvalidJSON(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader("not json"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotNil(t, result["error"])
}

func TestConformance_OpenAI_MissingModel(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	body := `{"messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestConformance_OpenAI_EmptyMessages(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	body := `{"model":"gpt-4o","messages":[]}`
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- Models Endpoint ---

func TestConformance_OpenAI_ModelsEndpoint(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	resp, err := http.Get(addr + "/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "list", result["object"])
	data := result["data"].([]any)
	require.GreaterOrEqual(t, len(data), 1)
	model := data[0].(map[string]any)
	assert.Equal(t, "model", model["object"])
}

// --- Response Timing ---

func TestConformance_ResponseLatency(t *testing.T) {
	_, addr := setupTestServer(t, testFullAgent("test-agent", "gpt-4o"))

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`

	start := time.Now()
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	latency := time.Since(start)
	require.NoError(t, err)
	resp.Body.Close()

	// Non-streaming latency should be well under 100ms for a local mock.
	assert.Less(t, latency, 100*time.Millisecond,
		"non-streaming latency %v exceeds 100ms threshold", latency)
}

// --- Multi-Agent Routing ---

func TestConformance_MultiAgentRouting(t *testing.T) {
	agentA := testFullAgent("agent-alpha", "model-alpha")
	agentA.Spec.Behavior.Scenarios = []types.Scenario{
		{Name: "default", Response: types.ScenarioResponse{Content: "I am Alpha"}},
	}
	agentB := testFullAgent("agent-beta", "model-beta")
	agentB.Spec.Behavior.Scenarios = []types.Scenario{
		{Name: "default", Response: types.ScenarioResponse{Content: "I am Beta"}},
	}

	_, addr := setupTestServer(t, agentA, agentB)

	// Route to Alpha.
	body := `{"model":"model-alpha","messages":[{"role":"user","content":"hello"}]}`
	resp, err := http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	msg := result["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	assert.Contains(t, msg["content"], "Alpha")

	// Route to Beta.
	body = `{"model":"model-beta","messages":[{"role":"user","content":"hello"}]}`
	resp, err = http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	msg = result["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	assert.Contains(t, msg["content"], "Beta")
}

// --- E2E: Full Lifecycle ---

func TestE2E_FullRequestLifecycle(t *testing.T) {
	agent := testFullAgent("e2e-agent", "gpt-4o")
	agent.Spec.Behavior.Scenarios = []types.Scenario{
		{Name: "greeting", Match: &types.MatchRule{ContentContains: "hello"}, Response: types.ScenarioResponse{Content: "Hello from E2E!"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "Default E2E response"}},
	}

	_, addr := setupTestServer(t, agent)

	// 1. Health check.
	resp, err := http.Get(addr + "/api/v1/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 2. List agents.
	resp, err = http.Get(addr + "/api/v1/agents")
	require.NoError(t, err)
	var agents []map[string]any
	json.NewDecoder(resp.Body).Decode(&agents)
	resp.Body.Close()
	require.Len(t, agents, 1)
	assert.Equal(t, "e2e-agent", agents[0]["name"])

	// 3. OpenAI chat completion.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	resp, err = http.Post(addr+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var chatResult map[string]any
	json.NewDecoder(resp.Body).Decode(&chatResult)
	resp.Body.Close()
	content := chatResult["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"]
	assert.Contains(t, content, "Hello from E2E")

	// 4. Request ID present in every response.
	resp, err = http.Get(addr + "/api/v1/health")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Header.Get("X-Request-Id"))
	resp.Body.Close()

	// 5. CORS headers present.
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestE2E_SessionPersistence(t *testing.T) {
	agent := testFullAgent("session-agent", "gpt-4o")
	agent.Spec.Behavior.Scenarios = []types.Scenario{
		{Name: "default", Response: types.ScenarioResponse{Content: "Response"}},
	}

	_, addr := setupTestServer(t, agent)

	sessionID := fmt.Sprintf("e2e-sess-%d", time.Now().UnixNano())

	// Send two requests with the same session ID.
	for i := 0; i < 2; i++ {
		body := fmt.Sprintf(`{"agent_name":"session-agent","session_id":"%s","messages":[{"role":"user","content":"msg %d"}]}`, sessionID, i)
		resp, err := http.Post(addr+"/v1/engines/process", "application/json", strings.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}
}
