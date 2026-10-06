package adapter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
)

func toolErrorAgent(protocol, model string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "orders"},
		Spec: types.AgentSpec{
			Protocol: protocol,
			Model:    model,
			Tools: []types.ToolDefinition{{
				Name: "lookup_order",
				Responses: []types.ToolResponseRule{
					{Match: map[string]any{"id": "missing"}, Error: &types.ToolError{Code: "NOT_FOUND", Message: "no such order"}},
					{IsDefault: true, Response: map[string]any{"status": "shipped"}},
				},
			}},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "missing",
						Match: &types.MatchRule{ContentContains: "missing"},
						Response: types.ScenarioResponse{
							Content:   "Looking it up.",
							ToolCalls: []types.ToolCallSpec{{Name: "lookup_order", Arguments: map[string]any{"id": "missing"}}},
						},
					},
					{
						Name:  "found",
						Match: &types.MatchRule{ContentContains: "found"},
						Response: types.ScenarioResponse{
							Content:   "Looking it up.",
							ToolCalls: []types.ToolCallSpec{{Name: "lookup_order", Arguments: map[string]any{"id": "o-1"}}},
						},
					},
				},
				Streaming: &types.StreamingConfig{Enabled: true, ChunkSize: 4, ChunkDelayMs: types.Ptr(0)},
			},
		},
	}
}

// K-04: a tool fixture that resolves to an error is visible on the wire, on
// both the JSON and the SSE path; a successful tool call sets no header.
func TestToolErrorsHeader_OpenAI(t *testing.T) {
	h := &OpenAIHandler{Engine: testEngine(toolErrorAgent("openai-chat-completions", "gpt-4o"))}

	for _, stream := range []bool{false, true} {
		rec := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
			Model: "gpt-4o", Stream: stream, Messages: []OpenAIMessage{{Role: "user", Content: "order missing"}},
		})
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "lookup_order=NOT_FOUND", rec.Header().Get(HeaderToolErrors), "stream=%v", stream)

		ok := doOpenAIRequest(t, h.HandleChatCompletions, ChatCompletionRequest{
			Model: "gpt-4o", Stream: stream, Messages: []OpenAIMessage{{Role: "user", Content: "order found"}},
		})
		assert.Empty(t, ok.Header().Get(HeaderToolErrors), "stream=%v", stream)
	}
}

func TestToolErrorsHeader_Anthropic(t *testing.T) {
	h := &AnthropicHandler{Engine: testEngine(toolErrorAgent("anthropic-messages", "claude-sonnet-4-20250514"))}
	rec := doAnthropicRequest(t, h.HandleMessages, AnthropicRequest{
		Model: "claude-sonnet-4-20250514", MaxTokens: 64,
		Messages: []AnthropicMessage{{Role: "user", Content: "order missing"}},
	})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "lookup_order=NOT_FOUND", rec.Header().Get(HeaderToolErrors))
}

func TestSetToolErrorsHeader_EscapesAndBounds(t *testing.T) {
	rec := httptest.NewRecorder()
	setToolErrorsHeader(rec, &engine.Response{ToolResults: []engine.ToolCallResult{
		{ToolName: "a", IsError: true, Error: &types.ToolError{Code: "bad code, really"}},
		{ToolName: "b"},                // not an error
		{ToolName: "c", IsError: true}, // error without a code
	}})
	assert.Equal(t, "a=bad+code%2C+really,c=", rec.Header().Get(HeaderToolErrors))

	rec = httptest.NewRecorder()
	many := make([]engine.ToolCallResult, 200)
	for i := range many {
		many[i] = engine.ToolCallResult{ToolName: "tool", IsError: true, Error: &types.ToolError{Code: "CODE"}}
	}
	setToolErrorsHeader(rec, &engine.Response{ToolResults: many})
	got := rec.Header().Get(HeaderToolErrors)
	assert.LessOrEqual(t, len(got), maxToolErrorsHeader)
	assert.True(t, strings.HasPrefix(got, "tool=CODE,tool=CODE"))
	assert.False(t, strings.HasSuffix(got, ","))

	rec = httptest.NewRecorder()
	setToolErrorsHeader(rec, nil)
	assert.Empty(t, rec.Header().Get(HeaderToolErrors))
}
