package engine

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func weatherAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "weather-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Tools: []types.ToolDefinition{
				{
					Name:        "get_weather",
					Description: "Get weather for a location",
					Parameters: types.JSONSchemaObject{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{"type": "string"},
						},
						"required": []any{"location"},
					},
					Responses: []types.ToolResponseRule{
						{
							Match:    map[string]any{"location": "NYC"},
							Response: map[string]any{"temp": 72, "condition": "sunny"},
						},
						{
							Match: map[string]any{"location": "INVALID"},
							Error: &types.ToolError{Code: "NOT_FOUND", Message: "Unknown location"},
						},
						{
							IsDefault: true,
							Response:  map[string]any{"temp": 70, "condition": "moderate"},
						},
					},
				},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "weather-error",
						Match: &types.MatchRule{ContentContains: "invalid location"},
						Response: types.ScenarioResponse{
							Content: "Checking...",
							ToolCalls: []types.ToolCallSpec{
								{Name: "get_weather", Arguments: map[string]any{"location": "INVALID"}},
							},
						},
					},
					{
						Name:  "multi-tool",
						Match: &types.MatchRule{ContentContains: "compare"},
						Response: types.ScenarioResponse{
							Content: "Comparing weather...",
							ToolCalls: []types.ToolCallSpec{
								{Name: "get_weather", Arguments: map[string]any{"location": "NYC"}},
								{Name: "get_weather", Arguments: map[string]any{"location": "SF"}},
							},
						},
					},
					{
						Name:  "weather-query",
						Match: &types.MatchRule{ContentContains: "weather"},
						Response: types.ScenarioResponse{
							Content: "I'll check the weather for you.",
							ToolCalls: []types.ToolCallSpec{
								{Name: "get_weather", Arguments: map[string]any{"location": "NYC"}},
							},
						},
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

func TestEngineIntegration_ToolCallsInResponse(t *testing.T) {
	eng := newTestEngine(weatherAgent())

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "weather-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "What's the weather in NYC?"}},
	})
	require.NoError(t, err)

	assert.Equal(t, "I'll check the weather for you.", resp.Content)
	assert.Equal(t, "weather-query", resp.ScenarioName)

	// Tool calls should be present.
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "get_weather", resp.ToolCalls[0].Name)

	// Tool results should be resolved.
	require.Len(t, resp.ToolResults, 1)
	assert.Equal(t, "get_weather", resp.ToolResults[0].ToolName)
	assert.False(t, resp.ToolResults[0].IsError)
	assert.NotEmpty(t, resp.ToolResults[0].ID)

	result, ok := resp.ToolResults[0].Response.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 72, result["temp"])
	assert.Equal(t, "sunny", result["condition"])
}

func TestEngineIntegration_ToolCallErrorResponse(t *testing.T) {
	eng := newTestEngine(weatherAgent())

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "weather-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "check invalid location please"}},
	})
	require.NoError(t, err)

	require.Len(t, resp.ToolResults, 1)
	assert.True(t, resp.ToolResults[0].IsError)
	assert.Equal(t, "NOT_FOUND", resp.ToolResults[0].Error.Code)
}

func TestEngineIntegration_ParallelToolCalls(t *testing.T) {
	eng := newTestEngine(weatherAgent())

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "weather-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "compare weather in cities"}},
	})
	require.NoError(t, err)

	require.Len(t, resp.ToolCalls, 2)
	require.Len(t, resp.ToolResults, 2)

	// Results should match call order.
	assert.Equal(t, "get_weather", resp.ToolResults[0].ToolName)
	assert.Equal(t, "get_weather", resp.ToolResults[1].ToolName)

	// Unique IDs.
	assert.NotEqual(t, resp.ToolResults[0].ID, resp.ToolResults[1].ID)
}

func TestEngineIntegration_NoToolCallsNoResults(t *testing.T) {
	eng := newTestEngine(weatherAgent())

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "weather-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)

	assert.Equal(t, "default", resp.ScenarioName)
	assert.Empty(t, resp.ToolCalls)
	assert.Empty(t, resp.ToolResults)
}

func TestEngineIntegration_ToolValidation(t *testing.T) {
	agent := weatherAgent()
	agent.Spec.Tools[0].Validate = true

	eng := newTestEngine(agent)

	// Valid request — should succeed.
	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "weather-agent",
		SessionID: "sess-valid",
		Messages:  []RequestMessage{{Role: "user", Content: "What's the weather?"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.ToolResults, 1)
	assert.False(t, resp.ToolResults[0].IsError)
}

func TestEngineIntegration_ToolValidation_InvalidParams(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "val-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Tools: []types.ToolDefinition{
				{
					Name:     "strict_tool",
					Validate: true,
					Parameters: types.JSONSchemaObject{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
						},
						"required": []any{"name"},
					},
					Responses: []types.ToolResponseRule{
						{IsDefault: true, Response: "ok"},
					},
				},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name: "call-strict",
						Match: &types.MatchRule{ContentContains: "test"},
						Response: types.ScenarioResponse{
							Content: "calling...",
							ToolCalls: []types.ToolCallSpec{
								// Missing required "name" param, wrong type for what's sent.
								{Name: "strict_tool", Arguments: map[string]any{}},
							},
						},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "default"},
					},
				},
			},
		},
	}

	eng := newTestEngine(agent)
	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "val-agent",
		SessionID: "sess-val",
		Messages:  []RequestMessage{{Role: "user", Content: "test this tool"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.ToolResults, 1)
	assert.True(t, resp.ToolResults[0].IsError)
	assert.Equal(t, "INVALID_PARAMETERS", resp.ToolResults[0].Error.Code)
	assert.Contains(t, resp.ToolResults[0].Error.Message, "missing required")
}

func TestEngineIntegration_ErrorInjection(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "chaos-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Tools: []types.ToolDefinition{
				{
					Name:      "flaky",
					ErrorRate: 1.0, // Always fails.
					Responses: []types.ToolResponseRule{
						{IsDefault: true, Response: "should not reach"},
					},
				},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "use-flaky",
						Match: &types.MatchRule{ContentContains: "test"},
						Response: types.ScenarioResponse{
							Content:   "calling flaky tool...",
							ToolCalls: []types.ToolCallSpec{{Name: "flaky", Arguments: map[string]any{}}},
						},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "default"},
					},
				},
			},
		},
	}

	eng := newTestEngine(agent)
	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "chaos-agent",
		SessionID: "sess-chaos",
		Messages:  []RequestMessage{{Role: "user", Content: "test flaky tool"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.ToolResults, 1)
	assert.True(t, resp.ToolResults[0].IsError)
	assert.Equal(t, "INJECTED_ERROR", resp.ToolResults[0].Error.Code)
}
