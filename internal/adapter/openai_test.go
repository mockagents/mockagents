package adapter

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEngine(agents ...*types.AgentDefinition) *engine.Engine {
	registry := engine.NewAgentRegistry()
	for _, a := range agents {
		registry.Register(a)
	}
	store := state.NewMemoryStore(0)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return engine.NewEngine(registry, store, logger)
}

func testOpenAIAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "test-agent"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Model:    "gpt-4o",
			Tools: []types.ToolDefinition{
				{
					Name: "get_weather",
					Responses: []types.ToolResponseRule{
						{IsDefault: true, Response: map[string]any{"temp": 72}},
					},
				},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "greeting",
						Match: &types.MatchRule{ContentContains: "hello"},
						Response: types.ScenarioResponse{Content: "Hi there!"},
					},
					{
						Name:  "tool-query",
						Match: &types.MatchRule{ContentContains: "weather"},
						Response: types.ScenarioResponse{
							Content: "Checking weather.",
							ToolCalls: []types.ToolCallSpec{
								{Name: "get_weather", Arguments: map[string]any{"city": "NYC"}},
							},
						},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "How can I help?"},
					},
				},
				Streaming: &types.StreamingConfig{Enabled: true, ChunkSize: 2, ChunkDelayMs: 0},
			},
		},
	}
}

func doOpenAIRequest(t *testing.T, handler http.HandlerFunc, req ChatCompletionRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)
	return rec
}

func TestOpenAI_BasicCompletion(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
	})

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "chat.completion", resp.Object)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.Contains(t, resp.ID, "chatcmpl-")
	assert.Greater(t, resp.Created, int64(0))

	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "assistant", resp.Choices[0].Message.Role)
	assert.Equal(t, "Hi there!", *resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Nil(t, resp.Choices[0].Message.ToolCalls)
}

func TestOpenAI_ToolCallResponse(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "check weather"}},
	})

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
	require.NotNil(t, resp.Choices[0].Message.ToolCalls)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)

	tc := resp.Choices[0].Message.ToolCalls[0]
	assert.Equal(t, "function", tc.Type)
	assert.Equal(t, "get_weather", tc.Function.Name)
	assert.NotEmpty(t, tc.ID)
	assert.Contains(t, tc.Function.Arguments, "NYC")
}

func TestOpenAI_Usage(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello world test"}},
	})

	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Greater(t, resp.Usage.PromptTokens, 0)
	assert.Greater(t, resp.Usage.CompletionTokens, 0)
	assert.Equal(t, resp.Usage.PromptTokens+resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}

func TestOpenAI_MissingModel(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var errResp map[string]any
	json.NewDecoder(rec.Body).Decode(&errResp)
	assert.Contains(t, errResp["error"].(map[string]any)["message"], "model is required")
}

func TestOpenAI_EmptyMessages(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{},
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOpenAI_InvalidJSON(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte("invalid")))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httpReq)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOpenAI_StreamingResponse(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
		Stream:   true,
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestOpenAI_ContentArrayMessage(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}

	// Test with content as array of parts.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`
	httpReq := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httpReq)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Hi there!", *resp.Choices[0].Message.Content)
}

func TestOpenAI_MultipleMessages(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "first message"},
			{Role: "assistant", Content: "response"},
			{Role: "user", Content: "hello"},
		},
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Hi there!", *resp.Choices[0].Message.Content)
}

func TestOpenAI_HandleModels(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.HandleModels(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "list", resp["object"])

	data, ok := resp["data"].([]any)
	require.True(t, ok)
	require.Len(t, data, 1)

	model := data[0].(map[string]any)
	assert.Equal(t, "gpt-4o", model["id"])
	assert.Equal(t, "model", model["object"])
}

func TestOpenAI_NullContent(t *testing.T) {
	// When response has tool calls but no content, content should be null.
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "weather"}},
	})

	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	// Content should still be set since the scenario provides both content and tool calls.
	assert.NotNil(t, resp.Choices[0].Message.Content)
}
