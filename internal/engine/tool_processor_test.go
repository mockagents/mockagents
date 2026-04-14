package engine

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func weatherTool() types.ToolDefinition {
	return types.ToolDefinition{
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
				Response: map[string]any{"temperature": 72, "condition": "sunny"},
			},
			{
				Match:    map[string]any{"location": "SF"},
				Response: map[string]any{"temperature": 65, "condition": "cloudy"},
			},
			{
				Match: map[string]any{"location": "INVALID"},
				Error: &types.ToolError{Code: "NOT_FOUND", Message: "Location not found"},
			},
			{
				IsDefault: true,
				Response:  map[string]any{"temperature": 70, "condition": "moderate"},
			},
		},
	}
}

func TestToolCallProcessor_ExactMatch(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{weatherTool()}
	calls := []types.ToolCallSpec{
		{Name: "get_weather", Arguments: map[string]any{"location": "NYC"}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "get_weather", results[0].ToolName)
	assert.False(t, results[0].IsError)
	assert.NotEmpty(t, results[0].ID)

	resp, ok := results[0].Response.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 72, resp["temperature"])
	assert.Equal(t, "sunny", resp["condition"])
}

func TestToolCallProcessor_SecondMatch(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{weatherTool()}
	calls := []types.ToolCallSpec{
		{Name: "get_weather", Arguments: map[string]any{"location": "SF"}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)

	resp, ok := results[0].Response.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 65, resp["temperature"])
}

func TestToolCallProcessor_DefaultFallback(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{weatherTool()}
	calls := []types.ToolCallSpec{
		{Name: "get_weather", Arguments: map[string]any{"location": "Tokyo"}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].IsError)

	resp, ok := results[0].Response.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 70, resp["temperature"])
	assert.Equal(t, "moderate", resp["condition"])
}

func TestToolCallProcessor_ErrorMatch(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{weatherTool()}
	calls := []types.ToolCallSpec{
		{Name: "get_weather", Arguments: map[string]any{"location": "INVALID"}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.True(t, results[0].IsError)
	assert.Equal(t, "NOT_FOUND", results[0].Error.Code)
	assert.Equal(t, "Location not found", results[0].Error.Message)
}

func TestToolCallProcessor_ToolNotFound(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{weatherTool()}
	calls := []types.ToolCallSpec{
		{Name: "nonexistent_tool", Arguments: map[string]any{}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrToolNotFound)
	require.Len(t, results, 1)
	assert.True(t, results[0].IsError)
	assert.Equal(t, "TOOL_NOT_FOUND", results[0].Error.Code)
}

func TestToolCallProcessor_NoResponses_GlobalFallback(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{
		{Name: "empty_tool"},
	}
	calls := []types.ToolCallSpec{
		{Name: "empty_tool", Arguments: map[string]any{"key": "val"}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].IsError)

	resp, ok := results[0].Response.(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "ok", resp["status"])
}

func TestToolCallProcessor_ParallelToolCalls(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{
		weatherTool(),
		{
			Name: "get_time",
			Responses: []types.ToolResponseRule{
				{IsDefault: true, Response: map[string]any{"time": "12:00"}},
			},
		},
	}
	calls := []types.ToolCallSpec{
		{Name: "get_weather", Arguments: map[string]any{"location": "NYC"}},
		{Name: "get_time", Arguments: map[string]any{}},
		{Name: "get_weather", Arguments: map[string]any{"location": "SF"}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Results should be in order.
	assert.Equal(t, "get_weather", results[0].ToolName)
	assert.Equal(t, "get_time", results[1].ToolName)
	assert.Equal(t, "get_weather", results[2].ToolName)

	// Each should have a unique ID.
	ids := map[string]bool{}
	for _, r := range results {
		assert.NotEmpty(t, r.ID)
		ids[r.ID] = true
	}
	assert.Len(t, ids, 3, "all tool call IDs should be unique")
}

func TestToolCallProcessor_EmptyToolCalls(t *testing.T) {
	p := NewToolCallProcessor()
	results, err := p.ProcessToolCalls(nil, nil)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestToolCallProcessor_UnspecifiedParamsIgnored(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{weatherTool()}
	// Match block only specifies "location", extra args should be ignored.
	calls := []types.ToolCallSpec{
		{Name: "get_weather", Arguments: map[string]any{
			"location": "NYC",
			"units":    "fahrenheit",
		}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].IsError)

	resp, ok := results[0].Response.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 72, resp["temperature"])
}

func TestToolCallProcessor_FirstMatchWins(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{
		{
			Name: "search",
			Responses: []types.ToolResponseRule{
				{Match: map[string]any{"q": "test"}, Response: "first match"},
				{Match: map[string]any{"q": "test"}, Response: "second match"},
				{IsDefault: true, Response: "default"},
			},
		},
	}
	calls := []types.ToolCallSpec{
		{Name: "search", Arguments: map[string]any{"q": "test"}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "first match", results[0].Response)
}

func TestToolCallProcessor_ErrorInjection_Always(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{
		{
			Name:      "flaky_tool",
			ErrorRate: 1.0, // Always inject error.
			Responses: []types.ToolResponseRule{
				{IsDefault: true, Response: "should not see this"},
			},
		},
	}
	calls := []types.ToolCallSpec{
		{Name: "flaky_tool", Arguments: map[string]any{}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].IsError)
	assert.Equal(t, "INJECTED_ERROR", results[0].Error.Code)
}

func TestToolCallProcessor_ErrorInjection_Never(t *testing.T) {
	p := NewToolCallProcessor()
	tools := []types.ToolDefinition{
		{
			Name:      "stable_tool",
			ErrorRate: 0.0,
			Responses: []types.ToolResponseRule{
				{IsDefault: true, Response: "success"},
			},
		},
	}
	calls := []types.ToolCallSpec{
		{Name: "stable_tool", Arguments: map[string]any{}},
	}

	results, err := p.ProcessToolCalls(calls, tools)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].IsError)
	assert.Equal(t, "success", results[0].Response)
}

// --- resolveToolResponse unit tests ---

func TestResolveToolResponse_NoRules(t *testing.T) {
	resp, toolErr := resolveToolResponse(nil, nil)
	assert.Nil(t, resp)
	assert.Nil(t, toolErr)
}

func TestResolveToolResponse_DefaultOnly(t *testing.T) {
	rules := []types.ToolResponseRule{
		{IsDefault: true, Response: "default value"},
	}
	resp, toolErr := resolveToolResponse(rules, nil)
	assert.Nil(t, toolErr)
	assert.Equal(t, "default value", resp)
}

func TestResolveToolResponse_DefaultWithError(t *testing.T) {
	rules := []types.ToolResponseRule{
		{IsDefault: true, Error: &types.ToolError{Code: "ERR", Message: "default error"}},
	}
	resp, toolErr := resolveToolResponse(rules, nil)
	assert.Nil(t, resp)
	assert.Equal(t, "ERR", toolErr.Code)
}

// --- matchArgs unit tests ---

func TestMatchArgs_ExactMatch(t *testing.T) {
	assert.True(t, matchArgs(
		map[string]any{"a": "1", "b": "2"},
		map[string]any{"a": "1", "b": "2", "c": "3"},
	))
}

func TestMatchArgs_MissingKey(t *testing.T) {
	assert.False(t, matchArgs(
		map[string]any{"a": "1", "missing": "2"},
		map[string]any{"a": "1"},
	))
}

func TestMatchArgs_DifferentValue(t *testing.T) {
	assert.False(t, matchArgs(
		map[string]any{"a": "1"},
		map[string]any{"a": "2"},
	))
}

func TestMatchArgs_EmptyMatch(t *testing.T) {
	assert.False(t, matchArgs(map[string]any{}, map[string]any{"a": "1"}))
}

func TestMatchArgs_NumericComparison(t *testing.T) {
	assert.True(t, matchArgs(
		map[string]any{"count": 5},
		map[string]any{"count": 5},
	))
}

// --- valuesEqual tests ---

func TestValuesEqual(t *testing.T) {
	assert.True(t, valuesEqual("hello", "hello"))
	assert.True(t, valuesEqual(42, 42))
	assert.True(t, valuesEqual(3.14, 3.14))
	assert.True(t, valuesEqual(true, true))
	assert.False(t, valuesEqual("hello", "world"))
	assert.False(t, valuesEqual(1, 2))
}

// --- generateToolCallID ---

func TestGenerateToolCallID_Format(t *testing.T) {
	id := generateToolCallID()
	assert.Contains(t, id, "call_")
	assert.Greater(t, len(id), 10)
}

func TestGenerateToolCallID_Unique(t *testing.T) {
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := generateToolCallID()
		assert.False(t, ids[id], "duplicate ID generated: %s", id)
		ids[id] = true
	}
}
