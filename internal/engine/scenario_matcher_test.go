package engine

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(v int) *int { return &v }

func TestScenarioMatcher_ContentContains(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "order", Match: &types.MatchRule{ContentContains: "order status"}, Response: types.ScenarioResponse{Content: "order response"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "default response"}},
	}

	sc, err := m.Match(scenarios, "What is my order status?", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "order", sc.Name)
}

func TestScenarioMatcher_ContentContainsCaseInsensitive(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "greeting", Match: &types.MatchRule{ContentContains: "hello"}, Response: types.ScenarioResponse{Content: "hi"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	sc, err := m.Match(scenarios, "HELLO there!", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "greeting", sc.Name)
}

func TestScenarioMatcher_ContentRegex(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "email", Match: &types.MatchRule{ContentRegex: `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`}, Response: types.ScenarioResponse{Content: "email found"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	sc, err := m.Match(scenarios, "my email is test@example.com", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "email", sc.Name)
}

func TestScenarioMatcher_TurnNumber(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "first-turn", Match: &types.MatchRule{TurnNumber: intPtr(1)}, Response: types.ScenarioResponse{Content: "first"}},
		{Name: "third-turn", Match: &types.MatchRule{TurnNumber: intPtr(3)}, Response: types.ScenarioResponse{Content: "third"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	sc, err := m.Match(scenarios, "hi", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "first-turn", sc.Name)

	sc, err = m.Match(scenarios, "hi", 3, 0)
	require.NoError(t, err)
	assert.Equal(t, "third-turn", sc.Name)

	sc, err = m.Match(scenarios, "hi", 5, 0)
	require.NoError(t, err)
	assert.Equal(t, "default", sc.Name)
}

func TestScenarioMatcher_MultipleConditionsAND(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{
			Name: "specific",
			Match: &types.MatchRule{
				ContentContains: "help",
				TurnNumber:      intPtr(2),
			},
			Response: types.ScenarioResponse{Content: "specific"},
		},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	// Both conditions met.
	sc, err := m.Match(scenarios, "I need help", 2, 0)
	require.NoError(t, err)
	assert.Equal(t, "specific", sc.Name)

	// Only content matches, not turn.
	sc, err = m.Match(scenarios, "I need help", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "default", sc.Name)
}

func TestScenarioMatcher_DefaultScenario(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "specific", Match: &types.MatchRule{ContentContains: "xyz"}, Response: types.ScenarioResponse{Content: "specific"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	sc, err := m.Match(scenarios, "hello", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "default", sc.Name)
}

func TestScenarioMatcher_NoMatch(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "specific", Match: &types.MatchRule{ContentContains: "xyz"}, Response: types.ScenarioResponse{Content: "specific"}},
	}

	_, err := m.Match(scenarios, "hello", 1, 0)
	assert.ErrorIs(t, err, ErrNoMatchingScenario)
}

func TestScenarioMatcher_FirstMatchWins(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "first", Match: &types.MatchRule{ContentContains: "hello"}, Response: types.ScenarioResponse{Content: "first"}},
		{Name: "second", Match: &types.MatchRule{ContentContains: "hello"}, Response: types.ScenarioResponse{Content: "second"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	sc, err := m.Match(scenarios, "hello world", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "first", sc.Name)
}

func TestScenarioMatcher_EmptyScenarios(t *testing.T) {
	m := NewScenarioMatcher()
	_, err := m.Match(nil, "hello", 1, 0)
	assert.ErrorIs(t, err, ErrNoMatchingScenario)
}

func TestScenarioMatcher_InvalidRegexNoMatch(t *testing.T) {
	m := NewScenarioMatcher()
	scenarios := []types.Scenario{
		{Name: "bad-regex", Match: &types.MatchRule{ContentRegex: "[invalid"}, Response: types.ScenarioResponse{Content: "bad"}},
		{Name: "default", Response: types.ScenarioResponse{Content: "default"}},
	}

	sc, err := m.Match(scenarios, "test", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, "default", sc.Name)
}

func TestScenarioMatcher_BadRegexIsLogged(t *testing.T) {
	// F-SM-001: a content_regex that fails to compile must be logged (so it is
	// diagnosable rather than a silent non-match) and logged only once.
	var buf bytes.Buffer
	m := NewScenarioMatcher()
	m.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	scenarios := []types.Scenario{
		{Name: "bad", Match: &types.MatchRule{ContentRegex: "[invalid"}},
	}

	// First evaluation: non-match (no default scenario) plus a logged warning.
	assert.Nil(t, m.MatchWithCaptures(scenarios, "anything", 1, 0))
	assert.Contains(t, buf.String(), "failed to compile")
	assert.Contains(t, buf.String(), "[invalid")

	// Second evaluation: still a non-match, but the bad pattern is cached so it
	// is neither recompiled nor re-logged.
	sizeAfterFirst := buf.Len()
	assert.Nil(t, m.MatchWithCaptures(scenarios, "anything", 1, 0))
	assert.Equal(t, sizeAfterFirst, buf.Len(), "bad pattern should log once, not per request")
}

// TestScenarioMatcher_LowerContainsMemoized is the PERF-08 guard: a static
// content_contains literal is lower-cased once, so repeat matches don't
// re-allocate a lowered copy per request.
func TestScenarioMatcher_LowerContainsMemoized(t *testing.T) {
	m := NewScenarioMatcher()
	if got := m.lowerContains("Hello World"); got != "hello world" {
		t.Fatalf("lowerContains = %q, want %q", got, "hello world")
	}
	// After the first call has cached the literal, subsequent lookups are an
	// allocation-free map read (no per-request strings.ToLower).
	allocs := testing.AllocsPerRun(100, func() { _ = m.lowerContains("Hello World") })
	if allocs != 0 {
		t.Errorf("lowerContains allocated %.1f/op after warmup, want 0 (PERF-08)", allocs)
	}
}
