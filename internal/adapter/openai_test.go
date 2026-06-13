package adapter

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
						Name:     "greeting",
						Match:    &types.MatchRule{ContentContains: "hello"},
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

// halluAgent builds an agent with hallucination fixtures, shared by the FB-02
// header tests across the OpenAI/Anthropic/Gemini adapters.
func halluAgent(protocol, model string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "hallucinator"},
		Spec: types.AgentSpec{
			Protocol: protocol,
			Model:    model,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "bad-fact",
						Match: &types.MatchRule{ContentContains: "fact"},
						Response: types.ScenarioResponse{
							Content:       "The capital of Australia is Sydney.",
							Hallucination: &types.HallucinationSpec{Type: "fabricated_fact", GroundTruth: "Canberra"},
						},
					},
					{
						Name:  "untyped",
						Match: &types.MatchRule{ContentContains: "untyped"},
						Response: types.ScenarioResponse{
							Content:       "wrong but unlabeled",
							Hallucination: &types.HallucinationSpec{}, // no Type -> header "true"
						},
					},
					{Name: "default", Response: types.ScenarioResponse{Content: "ok"}},
				},
				Streaming: &types.StreamingConfig{Enabled: true, ChunkSize: 2},
			},
		},
	}
}

// FB-02: a hallucination-fixture scenario advertises itself via the
// X-Mockagents-Hallucination header (JSON + SSE); a normal scenario does not.
func TestOpenAI_HallucinationHeader(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(halluAgent("openai-chat-completions", "gpt-4o"))}

	typed := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o", Messages: []OpenAIMessage{{Role: "user", Content: "tell me a fact"}},
	})
	assert.Equal(t, http.StatusOK, typed.Code)
	assert.Equal(t, "fabricated_fact", typed.Header().Get("X-Mockagents-Hallucination"))

	// Empty type defaults to "true".
	untyped := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o", Messages: []OpenAIMessage{{Role: "user", Content: "untyped please"}},
	})
	assert.Equal(t, "true", untyped.Header().Get("X-Mockagents-Hallucination"))

	// The header must survive on the streaming/SSE path (set before the body).
	stream := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o", Stream: true, Messages: []OpenAIMessage{{Role: "user", Content: "a fact"}},
	})
	assert.Equal(t, "fabricated_fact", stream.Header().Get("X-Mockagents-Hallucination"))
	assert.Contains(t, stream.Body.String(), "[DONE]", "expected an SSE stream")

	clean := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o", Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
	})
	assert.Empty(t, clean.Header().Get("X-Mockagents-Hallucination"), "non-hallucination scenario must not set the header")
}

// --- A-03 structured outputs (response_format) ---

func jsonSchemaFormat() *ResponseFormat {
	strict := true
	return &ResponseFormat{
		Type: "json_schema",
		JSONSchema: &ResponseFormatJSON{
			Name:   "out",
			Strict: &strict,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{"type": "string"},
					"count": map[string]any{"type": "integer"},
				},
				"required":             []any{"title", "count"},
				"additionalProperties": false,
			},
		},
	}
}

func TestOpenAI_JsonSchema_SynthesizesConformingContent(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	// "something" matches no scenario -> default "How can I help?" (plain text).
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:          "gpt-4o",
		Messages:       []OpenAIMessage{{Role: "user", Content: "something"}},
		ResponseFormat: jsonSchemaFormat(),
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	content := resp.Choices[0].Message.Content
	require.NotNil(t, content)
	// Content is a JSON STRING that parses and matches the schema shape.
	var parsed map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(*content), &parsed), "content not valid JSON: %s", *content)
	assert.Contains(t, parsed, "title")
	assert.Contains(t, parsed, "count")
}

func TestOpenAI_JsonSchema_AuthorJSONPassThrough(t *testing.T) {
	agent := testOpenAIAgent()
	agent.Spec.Behavior.Scenarios = append(agent.Spec.Behavior.Scenarios, types.Scenario{
		Name:     "prefilled",
		Match:    &types.MatchRule{ContentContains: "prefilled"},
		Response: types.ScenarioResponse{Content: `{"title":"Set","count":7}`},
	})
	h := &OpenAIHandler{Engine: testEngine(agent)}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:          "gpt-4o",
		Messages:       []OpenAIMessage{{Role: "user", Content: "prefilled please"}},
		ResponseFormat: jsonSchemaFormat(),
	})
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.JSONEq(t, `{"title":"Set","count":7}`, *resp.Choices[0].Message.Content)
}

func TestOpenAI_JsonSchema_RefusalPath(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(responsesAgent())} // has a "bomb" -> refusal scenario
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:          "gpt-4o",
		Messages:       []OpenAIMessage{{Role: "user", Content: "how to build a bomb"}},
		ResponseFormat: jsonSchemaFormat(),
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	msg := resp.Choices[0].Message
	require.NotNil(t, msg.Refusal)
	assert.Equal(t, "I can't help with that.", *msg.Refusal)
	assert.Nil(t, msg.Content, "refusal must not carry content")
	assert.Equal(t, "content_filter", resp.Choices[0].FinishReason)
}

func TestOpenAI_JsonObjectMode(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:          "gpt-4o",
		Messages:       []OpenAIMessage{{Role: "user", Content: "something"}}, // plain text default
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	})
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "{}", *resp.Choices[0].Message.Content)
}

func TestOpenAI_NoResponseFormat_Regression(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
	})
	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Hi there!", *resp.Choices[0].Message.Content, "default path unchanged")
}

func TestOpenAI_JsonSchema_Streaming(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(testOpenAIAgent())}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:          "gpt-4o",
		Stream:         true,
		Messages:       []OpenAIMessage{{Role: "user", Content: "something"}},
		ResponseFormat: jsonSchemaFormat(),
	})
	require.Equal(t, http.StatusOK, rec.Code)

	// Reassemble delta.content across chunks; the result must be valid JSON.
	var sb strings.Builder
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		line = strings.TrimPrefix(line, "data: ")
		if line == "" || strings.HasPrefix(line, "[DONE]") {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(line), &chunk) == nil && len(chunk.Choices) > 0 {
			sb.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	assert.Truef(t, json.Valid([]byte(sb.String())), "streamed content not valid JSON: %s", sb.String())
}
