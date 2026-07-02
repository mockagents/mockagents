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

// tool_choice forcing (R9-16a): under strict, required/named ALWAYS yields a
// tool call — synthesized when the scenario emitted none — with the
// staff-confirmed finish_reason "stop" on the OpenAI family.
func TestStrictToolChoice_OpenAIForcing(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(strictAgent("openai-chat-completions", "gpt-4o", "strict"))}
	tools := []OpenAITool{{Type: "function", Function: OpenAIFunction{Name: "get_weather"}}}

	// "required" on a non-tool scenario → synthesized call + finish "stop".
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:      "gpt-4o",
		Messages:   []OpenAIMessage{{Role: "user", Content: "hello"}},
		Tools:      tools,
		ToolChoice: "required",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Len(t, out.Choices[0].Message.ToolCalls, 1, "required must force a call")
	assert.Equal(t, "get_weather", out.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Equal(t, "stop", out.Choices[0].FinishReason, "forced calls report finish_reason stop, not tool_calls")

	// Named forcing replaces a different scenario call.
	toolsTwo := append(tools, OpenAITool{Type: "function", Function: OpenAIFunction{Name: "get_time"}})
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:      "gpt-4o",
		Messages:   []OpenAIMessage{{Role: "user", Content: "what is the weather?"}},
		Tools:      toolsTwo,
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "get_time"}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	out = ChatCompletionResponse{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Len(t, out.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "get_time", out.Choices[0].Message.ToolCalls[0].Function.Name,
		"the named tool wins over the scenario's call")

	// A named tool absent from the request's tools → the verified 400.
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:      "gpt-4o",
		Messages:   []OpenAIMessage{{Role: "user", Content: "hello"}},
		Tools:      tools,
		ToolChoice: map[string]any{"type": "function", "function": map[string]any{"name": "no_such_tool"}},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Tool choice 'no_such_tool' not found in 'tools' parameter.")
	assert.Contains(t, rec.Body.String(), `"param":"tool_choice"`)
}

// parallel_tool_calls:false caps a multi-call scenario to exactly one call.
func TestStrictParallel_OpenAICap(t *testing.T) {
	agent := strictAgent("openai-chat-completions", "gpt-4o", "strict")
	agent.Spec.Behavior.Scenarios[0].Response.ToolCalls = []types.ToolCallSpec{
		{Name: "get_weather", Arguments: map[string]any{"city": "NYC"}},
		{Name: "get_weather", Arguments: map[string]any{"city": "LA"}},
	}
	h := &OpenAIHandler{Engine: testEngine(agent)}

	f := false
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:             "gpt-4o",
		Messages:          []OpenAIMessage{{Role: "user", Content: "what is the weather?"}},
		ParallelToolCalls: &f,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	assert.Len(t, out.Choices[0].Message.ToolCalls, 1, "parallel_tool_calls:false = exactly zero or one")

	// Without the param both calls go out.
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "what is the weather?"}},
	})
	out = ChatCompletionResponse{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	assert.Len(t, out.Choices[0].Message.ToolCalls, 2)
}

// Anthropic {type:"any"} forces a tool_use with stop_reason "tool_use";
// disable_parallel_tool_use rides inside tool_choice.
func TestStrictToolChoice_Anthropic(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(strictAgent("anthropic-messages", "claude-3-opus", "strict"))}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model:     "claude-3-opus",
		MaxTokens: 100,
		Messages:  []AnthropicMessage{{Role: "user", Content: "hello"}},
		Tools:     []AnthropicTool{{Name: "get_weather"}},
		ToolChoice: map[string]any{
			"type": "any",
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out AnthropicResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	assert.Equal(t, "tool_use", out.StopReason, "forced Anthropic calls report stop_reason tool_use")
	var sawToolUse bool
	for _, c := range out.Content {
		if c.Type == "tool_use" {
			sawToolUse = true
			assert.Equal(t, "get_weather", c.Name)
		}
	}
	assert.True(t, sawToolUse, "type any must force a tool_use block")
}

// Gemini mode ANY + allowedFunctionNames forces the allowed function.
func TestStrictToolChoice_GeminiAny(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(strictAgent("google-gemini", "gemini-2.0-flash", "strict"))}
	rec := doGeminiRequest(t, h, "gemini-2.0-flash", "generateContent", GeminiRequest{
		Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "hello"}}}},
		Tools: []GeminiToolDeclaration{{FunctionDeclarations: []GeminiFunctionDecl{
			{Name: "get_weather"}, {Name: "get_time"}}}},
		ToolConfig: &GeminiToolConfig{FunctionCallingConfig: &GeminiFunctionCallingConfig{
			Mode: "ANY", AllowedFunctionNames: []string{"get_time"}}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"functionCall"`)
	assert.Contains(t, rec.Body.String(), "get_time")
}

// Warn mode observes forcing without changing the response.
func TestStrictToolChoice_WarnDoesNotMutate(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(strictAgent("openai-chat-completions", "gpt-4o", "warn"))}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:      "gpt-4o",
		Messages:   []OpenAIMessage{{Role: "user", Content: "hello"}},
		Tools:      []OpenAITool{{Type: "function", Function: OpenAIFunction{Name: "get_weather"}}},
		ToolChoice: "required",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	assert.Nil(t, out.Choices[0].Message.ToolCalls, "warn mode must not synthesize")
	assert.Contains(t, rec.Header().Get(HeaderStrictViolation), "required but the scenario emitted no tool call")
}

// strict:true function schemas are validated at request time (R9-16b):
// strict-incompatible schemas 400 with the real "Invalid schema for
// function…" text; the same schema without strict passes.
func TestStrictSchemas_OpenAI(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(strictAgent("openai-chat-completions", "gpt-4o", "strict"))}
	strict := true
	badParams := map[string]any{
		"type": "object",
		// no additionalProperties:false; required doesn't cover all keys
		"required":   []any{"city"},
		"properties": map[string]any{"city": map[string]any{"type": "string"}, "days": map[string]any{"type": "integer"}},
	}

	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
		Tools: []OpenAITool{{Type: "function", Function: OpenAIFunction{
			Name: "search", Parameters: badParams, Strict: &strict}}},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Invalid schema for function 'search': In context=(), 'additionalProperties' is required to be supplied and to be false.")

	// Without strict the same schema is accepted (shallow validation only —
	// matching the real API's strict:false leniency).
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
		Tools:    []OpenAITool{{Type: "function", Function: OpenAIFunction{Name: "search", Parameters: badParams}}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// A conforming strict schema passes.
	goodParams := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"city"},
		"properties":           map[string]any{"city": map[string]any{"type": "string"}},
	}
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "hello"}},
		Tools: []OpenAITool{{Type: "function", Function: OpenAIFunction{
			Name: "search", Parameters: goodParams, Strict: &strict}}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// The Responses surface validates its flat strict tools the same way.
func TestStrictSchemas_Responses(t *testing.T) {
	h := NewResponsesHandler(testEngine(strictAgent("openai-chat-completions", "gpt-4o", "strict")), newConversationStore())
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{
		"model":"gpt-4o","input":"hello",
		"tools":[{"type":"function","name":"search","strict":true,
			"parameters":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleResponses(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Invalid schema for function 'search'")
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
