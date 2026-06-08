package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAnthropicAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "claude-agent"},
		Spec: types.AgentSpec{
			Protocol: "anthropic-messages",
			Model:    "claude-3-opus",
			Tools: []types.ToolDefinition{
				{
					Name: "search",
					Responses: []types.ToolResponseRule{
						{IsDefault: true, Response: map[string]any{"results": []string{"a", "b"}}},
					},
				},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "greeting",
						Match: &types.MatchRule{ContentContains: "hello"},
						Response: types.ScenarioResponse{Content: "Bonjour!"},
					},
					{
						Name:  "search-query",
						Match: &types.MatchRule{ContentContains: "search"},
						Response: types.ScenarioResponse{
							Content: "Searching...",
							ToolCalls: []types.ToolCallSpec{
								{Name: "search", Arguments: map[string]any{"q": "test"}},
							},
						},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "How may I assist?"},
					},
				},
				Streaming: &types.StreamingConfig{Enabled: true, ChunkSize: 2, ChunkDelayMs: 0},
			},
		},
	}
}

func doAnthropicRequest(t *testing.T, handler http.HandlerFunc, req AnthropicRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Api-Key", "test-key")
	httpReq.Header.Set("Anthropic-Version", "2023-06-01")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	return rec
}

func TestAnthropic_BasicCompletion(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:     "claude-3-opus",
		Messages:  []AnthropicMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 1024,
	})

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AnthropicResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "message", resp.Type)
	assert.Equal(t, "assistant", resp.Role)
	assert.Equal(t, "claude-3-opus", resp.Model)
	assert.Contains(t, resp.ID, "msg_")
	assert.Equal(t, "end_turn", resp.StopReason)

	require.Len(t, resp.Content, 1)
	assert.Equal(t, "text", resp.Content[0].Type)
	assert.Equal(t, "Bonjour!", resp.Content[0].Text)
}

func TestAnthropic_ToolUseResponse(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:     "claude-3-opus",
		Messages:  []AnthropicMessage{{Role: "user", Content: "search for something"}},
		MaxTokens: 1024,
	})

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp AnthropicResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "tool_use", resp.StopReason)

	require.Len(t, resp.Content, 2)
	assert.Equal(t, "text", resp.Content[0].Type)
	assert.Equal(t, "Searching...", resp.Content[0].Text)
	assert.Equal(t, "tool_use", resp.Content[1].Type)
	assert.Equal(t, "search", resp.Content[1].Name)
	assert.NotEmpty(t, resp.Content[1].ID)
	assert.Equal(t, "test", resp.Content[1].Input["q"])
}

func TestAnthropic_Usage(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:     "claude-3-opus",
		Messages:  []AnthropicMessage{{Role: "user", Content: "hello world test"}},
		MaxTokens: 1024,
	})

	var resp AnthropicResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Greater(t, resp.Usage.InputTokens, 0)
	assert.Greater(t, resp.Usage.OutputTokens, 0)
}

func TestAnthropic_SystemPrompt(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:     "claude-3-opus",
		System:    "You are a helpful assistant.",
		Messages:  []AnthropicMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 1024,
	})

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAnthropic_MissingModel(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Messages: []AnthropicMessage{{Role: "user", Content: "hello"}},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAnthropic_EmptyMessages(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:    "claude-3-opus",
		Messages: []AnthropicMessage{},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAnthropic_MissingAPIKey(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	body, _ := json.Marshal(AnthropicRequest{
		Model:    "claude-3-opus",
		Messages: []AnthropicMessage{{Role: "user", Content: "hello"}},
	})
	httpReq := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	// No X-Api-Key or Authorization header.
	rec := httptest.NewRecorder()
	h.HandleMessages(rec, httpReq)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAnthropic_BearerAuth(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	body, _ := json.Marshal(AnthropicRequest{
		Model:    "claude-3-opus",
		Messages: []AnthropicMessage{{Role: "user", Content: "hello"}},
	})
	httpReq := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	h.HandleMessages(rec, httpReq)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAnthropic_InvalidJSON(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	httpReq := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader([]byte("bad")))
	httpReq.Header.Set("X-Api-Key", "test")
	rec := httptest.NewRecorder()
	h.HandleMessages(rec, httpReq)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAnthropic_StreamingResponse(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:    "claude-3-opus",
		Messages: []AnthropicMessage{{Role: "user", Content: "hello"}},
		Stream:   true,
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "event: message_start")
	assert.Contains(t, body, "event: message_stop")
}

func TestAnthropic_ContentBlocksRequest(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}

	// Test with content as array of blocks.
	body := `{"model":"claude-3-opus","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"max_tokens":1024}`
	httpReq := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader([]byte(body)))
	httpReq.Header.Set("X-Api-Key", "test")
	rec := httptest.NewRecorder()
	h.HandleMessages(rec, httpReq)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp AnthropicResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Bonjour!", resp.Content[0].Text)
}

func TestAnthropic_ToolResultMessage(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(testAnthropicAgent())}

	// Simulate a tool_result follow-up message.
	body := `{
		"model":"claude-3-opus",
		"messages":[
			{"role":"user","content":"search for something"},
			{"role":"assistant","content":[{"type":"text","text":"Searching..."},{"type":"tool_use","id":"toolu_123","name":"search","input":{"q":"test"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"{\"results\":[\"a\"]}"}]}
		],
		"max_tokens":1024
	}`
	httpReq := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader([]byte(body)))
	httpReq.Header.Set("X-Api-Key", "test")
	rec := httptest.NewRecorder()
	h.HandleMessages(rec, httpReq)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// FB-02: the Anthropic adapter also advertises hallucination fixtures.
func TestAnthropic_HallucinationHeader(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(halluAgent("anthropic-messages", "claude-3"))}
	post := func(text string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(AnthropicRequest{Model: "claude-3", MaxTokens: 16,
			Messages: []AnthropicMessage{{Role: "user", Content: text}}})
		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("X-Api-Key", "k")
		rec := httptest.NewRecorder()
		h.HandleMessages(rec, req)
		return rec
	}
	hit := post("tell me a fact")
	assert.Equal(t, http.StatusOK, hit.Code)
	assert.Equal(t, "fabricated_fact", hit.Header().Get("X-Mockagents-Hallucination"))
	assert.Empty(t, post("hi").Header().Get("X-Mockagents-Hallucination"))
}
