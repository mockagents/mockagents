// Round-11 strict-tools tests: round-trip tool id validation (R9-15) under
// the off/warn/strict knob, with each provider's real 400 body.
package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strictAgent is the round-9 tool-loop agent with a strict_tools block.
func strictAgent(protocol, model, level string) *types.AgentDefinition {
	a := toolLoopAgent(protocol, model)
	a.Spec.Behavior.StrictTools = &types.StrictToolsConfig{Level: level}
	return a
}

// Bad-id round trips → the provider's REAL 400 under strict; the valid loop
// still converges.
func TestStrictIDs_OpenAI(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(strictAgent("openai-chat-completions", "gpt-4o", "strict"))}

	// Orphan role:"tool" (no assistant tool_calls at all) → the verbatim
	// real-API text, including its own "preceeding" misspelling.
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "what is the weather?"},
			{Role: "tool", Content: `{"temp":72}`, ToolCallID: "call_orphan"},
		},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "preceeding message with 'tool_calls'")
	assert.Contains(t, rec.Body.String(), "invalid_request_error")

	// Mismatched tool_call_id → the unanswered-ids 400.
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "what is the weather?"},
			{Role: "assistant", ToolCalls: []OpenAIToolCall{{ID: "call_real", Type: "function",
				Function: OpenAIFunctionCall{Name: "get_weather", Arguments: `{"city":"NYC"}`}}}},
			{Role: "tool", Content: `{"temp":72}`, ToolCallID: "call_BOGUS"},
		},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "did not have response messages: call_real")

	// The valid loop leg still succeeds and converges.
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "what is the weather?"},
			{Role: "assistant", ToolCalls: []OpenAIToolCall{{ID: "call_real", Type: "function",
				Function: OpenAIFunctionCall{Name: "get_weather", Arguments: `{"city":"NYC"}`}}}},
			{Role: "tool", Content: `{"temp":72}`, ToolCallID: "call_real"},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// warn mode: the same violation succeeds but is surfaced via the
// X-Mockagents-Strict-Violation header (the CI-migration tier).
func TestStrictIDs_WarnModeHeader(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(strictAgent("openai-chat-completions", "gpt-4o", "warn"))}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: `{"temp":72}`, ToolCallID: "call_orphan"},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get(HeaderStrictViolation), "tool result without any preceding tool calls")

	// A clean request carries no header.
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get(HeaderStrictViolation))
}

// Default (no knob): today's lenient behavior is unchanged.
func TestStrictIDs_OffByDefault(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(toolLoopAgent("openai-chat-completions", "gpt-4o"))}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: `{"temp":72}`, ToolCallID: "call_orphan"},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Header().Get(HeaderStrictViolation))
}

func TestStrictIDs_Anthropic(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(strictAgent("anthropic-messages", "claude-3-opus", "strict"))}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:     "claude-3-opus",
		MaxTokens: 100,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "what is the weather?"},
			{Role: "assistant", Content: []any{map[string]any{
				"type": "tool_use", "id": "toolu_real", "name": "get_weather",
				"input": map[string]any{"city": "NYC"}}}},
			{Role: "user", Content: []any{map[string]any{
				"type": "tool_result", "tool_use_id": "toolu_BOGUS", "content": "72F"}}},
		},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "unexpected `tool_use_id` found in `tool_result` blocks: toolu_BOGUS")

	// The valid id passes.
	rec = doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:     "claude-3-opus",
		MaxTokens: 100,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "what is the weather?"},
			{Role: "assistant", Content: []any{map[string]any{
				"type": "tool_use", "id": "toolu_real", "name": "get_weather",
				"input": map[string]any{"city": "NYC"}}}},
			{Role: "user", Content: []any{map[string]any{
				"type": "tool_result", "tool_use_id": "toolu_real", "content": "72F"}}},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestStrictIDs_Gemini(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(strictAgent("google-gemini", "gemini-2.0-flash", "strict"))}
	rec := doGeminiRequest(t, h, "gemini-2.0-flash", "generateContent", GeminiRequest{
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: "what is the weather?"}}},
			{Role: "model", Parts: []GeminiPart{{FunctionCall: &GeminiFunctionCall{
				Name: "get_weather", Args: map[string]any{"city": "NYC"}}}}},
			{Role: "user", Parts: []GeminiPart{{FunctionResponse: &GeminiFunctionResponse{
				Name: "TOTALLY_DIFFERENT", Response: map[string]any{"temp": 72}}}}},
		},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "number of function response parts")
	assert.Contains(t, rec.Body.String(), "INVALID_ARGUMENT")

	// Matching name passes.
	rec = doGeminiRequest(t, h, "gemini-2.0-flash", "generateContent", GeminiRequest{
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: "what is the weather?"}}},
			{Role: "model", Parts: []GeminiPart{{FunctionCall: &GeminiFunctionCall{
				Name: "get_weather", Args: map[string]any{"city": "NYC"}}}}},
			{Role: "user", Parts: []GeminiPart{{FunctionResponse: &GeminiFunctionResponse{
				Name: "get_weather", Response: map[string]any{"temp": 72}}}}},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// Responses: a bogus call_id 400s; the stored-state loop (previous_response_id
// carrying the emitted call) stays legal — the real API's documented leniency.
func TestStrictIDs_Responses(t *testing.T) {
	h := NewResponsesHandler(testEngine(strictAgent("openai-chat-completions", "gpt-4o", "strict")), newConversationStore())

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleResponses(rec, req)
		return rec
	}

	// Bogus call_id with no prior state → the real 400.
	rec := post(`{"model":"gpt-4o","input":[
		{"type":"message","role":"user","content":"what is the weather?"},
		{"type":"function_call_output","call_id":"call_NEVER_ISSUED","output":"{\"temp\":72}"}]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "No tool call found for function call output with call_id call_NEVER_ISSUED")

	// The stored-state loop: turn 1 emits the call; answering it via
	// previous_response_id (lone output) is legal and converges.
	rec = post(`{"model":"gpt-4o","input":"what is the weather in NYC?"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var turn1 map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &turn1))
	respID := turn1["id"].(string)
	var callID string
	for _, it := range turn1["output"].([]any) {
		if m := it.(map[string]any); m["type"] == "function_call" {
			callID = m["call_id"].(string)
		}
	}
	require.NotEmpty(t, callID, "turn 1 must emit a function_call")

	rec = post(`{"model":"gpt-4o","previous_response_id":"` + respID + `","input":[
		{"type":"function_call_output","call_id":"` + callID + `","output":"{\"temp\":72}"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
