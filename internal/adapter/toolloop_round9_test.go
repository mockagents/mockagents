// Round-9 tool-loop convergence tests: the standard SDK agent loop (ask →
// tool call → send result → follow-up) must converge on every HTTP surface.
// The engine's convergence guard consumes an IDENTICAL re-issue directly
// after a tool result; different calls (deliberate chains) and fresh user
// turns still go out.
package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolLoopAgent(protocol, model string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "loop-" + protocol},
		Spec: types.AgentSpec{
			Protocol: protocol,
			Model:    model,
			Tools: []types.ToolDefinition{{
				Name:      "get_weather",
				Responses: []types.ToolResponseRule{{IsDefault: true, Response: map[string]any{"temp": 72}}},
			}},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "tool-query",
						Match: &types.MatchRule{ContentContains: "weather"},
						Response: types.ScenarioResponse{
							Content:   "Checking.",
							ToolCalls: []types.ToolCallSpec{{Name: "get_weather", Arguments: map[string]any{"city": "NYC"}}},
						},
					},
					{Name: "default", Response: types.ScenarioResponse{Content: "Done."}},
				},
			},
		},
	}
}

// R9-1: the canonical OpenAI chat-completions loop converges; a DIFFERENT
// echoed call (deliberate chain) is not suppressed; a fresh user turn
// re-arms the tool call.
func TestToolLoop_OpenAIConverges(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(toolLoopAgent("openai-chat-completions", "gpt-4o"))}

	// Turn 1: the question → tool_calls.
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []OpenAIMessage{{Role: "user", Content: "what is the weather in NYC?"}},
	})
	var turn1 ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&turn1))
	require.Equal(t, "tool_calls", turn1.Choices[0].FinishReason)
	call := turn1.Choices[0].Message.ToolCalls[0]

	// Turn 2: the standard loop leg — echo the assistant call + the tool
	// result. The identical re-issue must be consumed.
	loopLeg := []OpenAIMessage{
		{Role: "user", Content: "what is the weather in NYC?"},
		{Role: "assistant", ToolCalls: []OpenAIToolCall{call}},
		{Role: "tool", Content: `{"temp":72}`, ToolCallID: call.ID},
	}
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{Model: "gpt-4o", Messages: loopLeg})
	var turn2 ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&turn2))
	assert.Equal(t, "stop", turn2.Choices[0].FinishReason, "the loop must converge")
	assert.Nil(t, turn2.Choices[0].Message.ToolCalls, "the identical call must not be re-issued")

	// A DIFFERENT echoed call (multi-step chain) is not suppressed.
	chainLeg := []OpenAIMessage{
		{Role: "user", Content: "what is the weather in NYC?"},
		{Role: "assistant", ToolCalls: []OpenAIToolCall{{ID: call.ID, Type: "function",
			Function: OpenAIFunctionCall{Name: "get_weather", Arguments: `{"city":"LA"}`}}}},
		{Role: "tool", Content: `{"temp":65}`, ToolCallID: call.ID},
	}
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{Model: "gpt-4o", Messages: chainLeg})
	var chain ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&chain))
	assert.Equal(t, "tool_calls", chain.Choices[0].FinishReason,
		"a different echoed call must not suppress the scenario's call")

	// A fresh user turn after the loop re-arms the tool call.
	fresh := append(loopLeg, OpenAIMessage{Role: "user", Content: "and the weather tomorrow?"})
	rec = doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{Model: "gpt-4o", Messages: fresh})
	var turn3 ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&turn3))
	assert.Equal(t, "tool_calls", turn3.Choices[0].FinishReason, "a fresh user turn must re-arm the call")
}

// R9-2: the Responses API previous_response_id tool loop converges (the
// store now persists the emitted function_call fingerprint).
func TestToolLoop_ResponsesConverges(t *testing.T) {
	h := NewResponsesHandler(testEngine(toolLoopAgent("openai-chat-completions", "gpt-4o")), newConversationStore())

	post := func(body string) map[string]any {
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleResponses(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}
	outputTypes := func(resp map[string]any) []string {
		var out []string
		for _, it := range resp["output"].([]any) {
			out = append(out, it.(map[string]any)["type"].(string))
		}
		return out
	}

	turn1 := post(`{"model":"gpt-4o","input":"what is the weather in NYC?"}`)
	require.Contains(t, outputTypes(turn1), "function_call")
	var callID string
	for _, it := range turn1["output"].([]any) {
		if m := it.(map[string]any); m["type"] == "function_call" {
			callID = m["call_id"].(string)
		}
	}

	turn2 := post(`{"model":"gpt-4o","previous_response_id":"` + turn1["id"].(string) + `",
		"input":[{"type":"function_call_output","call_id":"` + callID + `","output":"{\"temp\":72}"}]}`)
	assert.NotContains(t, outputTypes(turn2), "function_call", "the loop must converge")
	assert.Contains(t, outputTypes(turn2), "message")
}

// R9-3: the Anthropic loop converges even when the tool result echoes the
// trigger word (the common re-trigger spin).
func TestToolLoop_AnthropicRetriggerConverges(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(toolLoopAgent("anthropic-messages", "claude-3-opus"))}

	post := func(body string) map[string]any {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "mock-key")
		req.Header.Set("anthropic-version", "2023-06-01")
		rec := httptest.NewRecorder()
		h.HandleMessages(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	turn1 := post(`{"model":"claude-3-opus","max_tokens":100,
		"messages":[{"role":"user","content":"what is the weather in NYC?"}]}`)
	require.Equal(t, "tool_use", turn1["stop_reason"])
	var toolUseID string
	for _, b := range turn1["content"].([]any) {
		if m := b.(map[string]any); m["type"] == "tool_use" {
			toolUseID = m["id"].(string)
		}
	}
	require.NotEmpty(t, toolUseID)

	// The tool result ECHOES the trigger word "weather" — the flattened text
	// re-matches the tool scenario, but the identical re-issue is consumed.
	turn2 := post(`{"model":"claude-3-opus","max_tokens":100,"messages":[
		{"role":"user","content":"what is the weather in NYC?"},
		{"role":"assistant","content":[{"type":"tool_use","id":"` + toolUseID + `","name":"get_weather","input":{"city":"NYC"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + toolUseID + `","content":"weather is sunny, 72"}]}]}`)
	assert.Equal(t, "end_turn", turn2["stop_reason"], "the retrigger loop must converge")
	for _, b := range turn2["content"].([]any) {
		assert.NotEqual(t, "tool_use", b.(map[string]any)["type"], "the identical call must not be re-issued")
	}
}

// R9-4: the Gemini loop converges — the functionResponse tool NAME no longer
// re-triggers the scenario, and role:"function" result turns count as tool
// results.
func TestToolLoop_GeminiConverges(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(toolLoopAgent("google-gemini", "gemini-1.5-pro"))}

	post := func(body string) map[string]any {
		req := httptest.NewRequest("POST", "/v1beta/models/gemini-1.5-pro:generateContent", strings.NewReader(body))
		req.SetPathValue("modelmethod", "gemini-1.5-pro:generateContent")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.HandleGenerate(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}
	hasFunctionCall := func(resp map[string]any) bool {
		parts := resp["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
		for _, p := range parts {
			if _, ok := p.(map[string]any)["functionCall"]; ok {
				return true
			}
		}
		return false
	}

	turn1 := post(`{"contents":[{"role":"user","parts":[{"text":"what is the weather in NYC?"}]}]}`)
	require.True(t, hasFunctionCall(turn1), "turn 1 must emit the functionCall")

	for _, role := range []string{"user", "function"} {
		turn2 := post(`{"contents":[
			{"role":"user","parts":[{"text":"what is the weather in NYC?"}]},
			{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"NYC"}}}]},
			{"role":"` + role + `","parts":[{"functionResponse":{"name":"get_weather","response":{"temp":72}}}]}]}`)
		assert.False(t, hasFunctionCall(turn2), "the loop must converge (result role %q)", role)
	}
}

// R9-5: tool_choice "none" (and its per-provider spellings) suppresses tool
// calls — the standard forced-final-answer escape hatch real APIs honor
// strictly.
func TestToolChoiceNoneSuppressesCalls(t *testing.T) {
	// OpenAI chat completions.
	h := &OpenAIHandler{Engine: testEngine(toolLoopAgent("openai-chat-completions", "gpt-4o"))}
	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model:      "gpt-4o",
		Messages:   []OpenAIMessage{{Role: "user", Content: "what is the weather?"}},
		ToolChoice: "none",
	})
	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Nil(t, resp.Choices[0].Message.ToolCalls, "tool_choice none must suppress calls")

	// Anthropic {type:"none"}.
	ah := &AnthropicHandler{Engine: testEngine(toolLoopAgent("anthropic-messages", "claude-3-opus"))}
	areq := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"claude-3-opus","max_tokens":100,"tool_choice":{"type":"none"},
		  "messages":[{"role":"user","content":"what is the weather?"}]}`))
	areq.Header.Set("Content-Type", "application/json")
	areq.Header.Set("x-api-key", "mock-key")
	arec := httptest.NewRecorder()
	ah.HandleMessages(arec, areq)
	require.Equal(t, http.StatusOK, arec.Code)
	var amsg map[string]any
	require.NoError(t, json.Unmarshal(arec.Body.Bytes(), &amsg))
	assert.Equal(t, "end_turn", amsg["stop_reason"])

	// Gemini functionCallingConfig mode NONE.
	gh := &GeminiHandler{Engine: testEngine(toolLoopAgent("google-gemini", "gemini-1.5-pro"))}
	greq := httptest.NewRequest("POST", "/v1beta/models/gemini-1.5-pro:generateContent", strings.NewReader(
		`{"toolConfig":{"functionCallingConfig":{"mode":"NONE"}},
		  "contents":[{"role":"user","parts":[{"text":"what is the weather?"}]}]}`))
	greq.SetPathValue("modelmethod", "gemini-1.5-pro:generateContent")
	greq.Header.Set("Content-Type", "application/json")
	grec := httptest.NewRecorder()
	gh.HandleGenerate(grec, greq)
	require.Equal(t, http.StatusOK, grec.Code)
	assert.NotContains(t, grec.Body.String(), "functionCall")

	// Responses API.
	rh := NewResponsesHandler(testEngine(toolLoopAgent("openai-chat-completions", "gpt-4o")), newConversationStore())
	rreq := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(
		`{"model":"gpt-4o","tool_choice":"none","input":"what is the weather?"}`))
	rreq.Header.Set("Content-Type", "application/json")
	rrec := httptest.NewRecorder()
	rh.HandleResponses(rrec, rreq)
	require.Equal(t, http.StatusOK, rrec.Code)
	assert.NotContains(t, rrec.Body.String(), `"function_call"`)
}

// The guard's fingerprint survives JSON number-typing differences: the
// scenario's YAML-decoded int args must match a client echo parsed as
// float64.
func TestToolLoop_FingerprintNumberNormalization(t *testing.T) {
	agent := toolLoopAgent("openai-chat-completions", "gpt-4o")
	agent.Spec.Behavior.Scenarios[0].Response.ToolCalls[0].Arguments = map[string]any{"limit": 5}
	h := &OpenAIHandler{Engine: testEngine(agent)}

	rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "weather please"},
			{Role: "assistant", ToolCalls: []OpenAIToolCall{{ID: "call_x", Type: "function",
				Function: OpenAIFunctionCall{Name: "get_weather", Arguments: `{"limit": 5}`}}}},
			{Role: "tool", Content: "ok", ToolCallID: "call_x"},
		},
	})
	var resp ChatCompletionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "stop", resp.Choices[0].FinishReason,
		"int-vs-float64 argument typing must not defeat the fingerprint")
}

var _ = bytes.MinRead // keep bytes imported for doOpenAIRequest siblings
