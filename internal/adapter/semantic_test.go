package adapter

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// semanticAgent builds an agent exercising the FB-03 semantic error modes:
// a forced finish_reason ("truncate" -> length), an assistant refusal
// ("refuse"), and a tool call with malformed raw arguments ("bad args").
func semanticAgent(model, protocol string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: model},
		Spec: types.AgentSpec{
			Protocol: protocol,
			Model:    model,
			Tools: []types.ToolDefinition{
				{Name: "get_weather", Responses: []types.ToolResponseRule{{IsDefault: true, Response: map[string]any{"t": 1}}}},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{Name: "trunc", Match: &types.MatchRule{ContentContains: "truncate"},
						Response: types.ScenarioResponse{Content: "partial answer that got cut", FinishReason: "length"}},
					{Name: "refuse", Match: &types.MatchRule{ContentContains: "refuse"},
						Response: types.ScenarioResponse{Refusal: "I can't help with that"}},
					{Name: "badargs", Match: &types.MatchRule{ContentContains: "bad args"},
						Response: types.ScenarioResponse{Content: "calling", ToolCalls: []types.ToolCallSpec{
							{Name: "get_weather", RawArguments: `{"city":`}, // deliberately invalid JSON
						}}},
					{Name: "default", Response: types.ScenarioResponse{Content: "ok"}},
				},
			},
		},
	}
}

func TestOpenAI_SemanticErrorModes(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(semanticAgent("sem-oa", "openai-chat-completions"))}
	ask := func(msg string) ChatCompletionResponse {
		rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
			Model: "sem-oa", Messages: []OpenAIMessage{{Role: "user", Content: msg}},
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var resp ChatCompletionResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		return resp
	}

	// finish_reason: length (truncation).
	assert.Equal(t, "length", ask("please truncate this").Choices[0].FinishReason)

	// refusal: structured field set, content null.
	r := ask("please refuse this").Choices[0].Message
	require.NotNil(t, r.Refusal)
	assert.Equal(t, "I can't help with that", *r.Refusal)
	assert.Nil(t, r.Content)

	// malformed tool-call arguments emitted verbatim.
	tc := ask("send bad args").Choices[0].Message.ToolCalls
	require.Len(t, tc, 1)
	assert.Equal(t, `{"city":`, tc[0].Function.Arguments)
}

func TestAnthropic_SemanticErrorModes(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(semanticAgent("sem-an", "anthropic-messages"))}
	ask := func(msg string) AnthropicResponse {
		rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
			Model: "sem-an", Messages: []AnthropicMessage{{Role: "user", Content: msg}}, MaxTokens: 16,
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var resp AnthropicResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		return resp
	}

	// finish_reason length -> stop_reason max_tokens.
	assert.Equal(t, "max_tokens", ask("please truncate this").StopReason)

	// refusal -> text block + refusal stop reason.
	rr := ask("please refuse this")
	assert.Equal(t, "refusal", rr.StopReason)
	require.NotEmpty(t, rr.Content)
	assert.Equal(t, "I can't help with that", rr.Content[0].Text)
}

func TestGemini_SemanticErrorModes(t *testing.T) {
	h := &GeminiHandler{Engine: testEngine(semanticAgent("sem-gm", "google-gemini"))}
	ask := func(msg string) GeminiResponse {
		rec := doGeminiRequest(t, h, "sem-gm", "generateContent", GeminiRequest{
			Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: msg}}}},
		})
		require.Equal(t, http.StatusOK, rec.Code)
		var resp GeminiResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		return resp
	}

	// finish_reason length -> MAX_TOKENS.
	assert.Equal(t, "MAX_TOKENS", ask("please truncate this").Candidates[0].FinishReason)

	// refusal surfaces as a text part.
	g := ask("please refuse this")
	require.NotEmpty(t, g.Candidates[0].Content.Parts)
	assert.Equal(t, "I can't help with that", g.Candidates[0].Content.Parts[0].Text)
}
