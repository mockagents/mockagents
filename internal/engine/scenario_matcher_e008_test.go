package engine

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Regex Capture Groups ---

func TestMatchWithCaptures_NamedGroups(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{
			Name:     "weather",
			Match:    &types.MatchRule{ContentRegex: `weather in (?P<city>\w+)`},
			Response: types.ScenarioResponse{Content: "Weather for {{ index .Match \"city\" }}"},
		},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	result := m.MatchWithCaptures(scenarios, "What's the weather in NYC?", 1)
	require.NotNil(t, result)
	assert.Equal(t, "weather", result.Scenario.Name)
	assert.Equal(t, "NYC", result.Captures["city"])
}

func TestMatchWithCaptures_MultipleGroups(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{
			Name:     "order",
			Match:    &types.MatchRule{ContentRegex: `order (?P<order_id>ORD-\d+) for (?P<customer>\w+)`},
			Response: types.ScenarioResponse{Content: "ok"},
		},
	}

	result := m.MatchWithCaptures(scenarios, "Check order ORD-12345 for Alice", 1)
	require.NotNil(t, result)
	assert.Equal(t, "ORD-12345", result.Captures["order_id"])
	assert.Equal(t, "Alice", result.Captures["customer"])
}

func TestMatchWithCaptures_NoGroups(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{
			Name:     "simple",
			Match:    &types.MatchRule{ContentContains: "hello"},
			Response: types.ScenarioResponse{Content: "hi"},
		},
	}

	result := m.MatchWithCaptures(scenarios, "hello", 1)
	require.NotNil(t, result)
	assert.Empty(t, result.Captures)
}

func TestMatchWithCaptures_Default(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "default", Response: types.ScenarioResponse{Content: "fallback"}},
	}

	result := m.MatchWithCaptures(scenarios, "anything", 1)
	require.NotNil(t, result)
	assert.Equal(t, "default", result.Scenario.Name)
	assert.Nil(t, result.Captures)
}

func TestMatchWithCaptures_NoMatch(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{
			Name:     "specific",
			Match:    &types.MatchRule{ContentContains: "xyz"},
			Response: types.ScenarioResponse{Content: "nope"},
		},
	}

	result := m.MatchWithCaptures(scenarios, "hello", 1)
	assert.Nil(t, result)
}

// --- Regex Caching ---

func TestRegexCaching(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{
			Name:     "pattern",
			Match:    &types.MatchRule{ContentRegex: `\d+`},
			Response: types.ScenarioResponse{Content: "matched"},
		},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	// First call — compiles and caches.
	result1 := m.MatchWithCaptures(scenarios, "order 123", 1)
	require.NotNil(t, result1)
	assert.Equal(t, "pattern", result1.Scenario.Name)

	// Second call — uses cached regex.
	result2 := m.MatchWithCaptures(scenarios, "item 456", 1)
	require.NotNil(t, result2)
	assert.Equal(t, "pattern", result2.Scenario.Name)
}

func TestRegexCaching_InvalidRegex(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{
			Name:     "bad",
			Match:    &types.MatchRule{ContentRegex: `[invalid`},
			Response: types.ScenarioResponse{Content: "bad"},
		},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	result := m.MatchWithCaptures(scenarios, "test", 1)
	require.NotNil(t, result)
	assert.Equal(t, "default", result.Scenario.Name)
}
