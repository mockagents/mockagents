package engine

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAgentE008() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "test-agent"},
		Spec:     types.AgentSpec{Model: "gpt-4o"},
	}
}

// --- New Template Functions ---

func TestTemplate_RandomFloat(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ random_float 1.0 10.0 }}`}}
	resp, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	require.NoError(t, err)

	val, err := strconv.ParseFloat(strings.TrimSpace(resp.Content), 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, val, 1.0)
	assert.Less(t, val, 10.0)
}

func TestTemplate_RandomChoice(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ random_choice "yes" "no" "maybe" }}`}}
	resp, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	require.NoError(t, err)
	assert.Contains(t, []string{"yes", "no", "maybe"}, strings.TrimSpace(resp.Content))
}

func TestTemplate_RandomChoiceEmpty(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ random_choice }}`}}
	resp, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(resp.Content))
}

func TestTemplate_ToJSON(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ to_json .Agent.Metadata }}`}}
	agent := testAgentE008()
	resp, err := g.Generate(agent, sc, TemplateContext{Agent: agent})
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &result))
	assert.Equal(t, "test-agent", result["name"])
}

func TestTemplate_Timestamp(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ timestamp }}`}}
	resp, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	require.NoError(t, err)

	ts, err := strconv.ParseInt(strings.TrimSpace(resp.Content), 10, 64)
	require.NoError(t, err)
	assert.InDelta(t, time.Now().Unix(), ts, 5)
}

func TestTemplate_DateFormat(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ date_format "2006-01-02" }}`}}
	resp, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	require.NoError(t, err)

	expected := time.Now().UTC().Format("2006-01-02")
	assert.Equal(t, expected, strings.TrimSpace(resp.Content))
}

func TestTemplate_FakeName(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ fake_name }}`}}
	resp, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	require.NoError(t, err)

	parts := strings.Fields(resp.Content)
	assert.Len(t, parts, 2, "fake_name should produce first + last name")
}

func TestTemplate_FakeEmail(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `{{ fake_email }}`}}
	resp, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	require.NoError(t, err)

	email := strings.TrimSpace(resp.Content)
	assert.Contains(t, email, "@")
	assert.Contains(t, email, ".")
}

// --- Session Variables in Templates ---

func TestTemplate_SessionVars(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{
		Content: `Hello {{ index .Vars "user_name" }}!`,
	}}
	ctx := TemplateContext{
		Vars: map[string]any{"user_name": "Alice"},
	}
	resp, err := g.Generate(testAgentE008(), sc, ctx)
	require.NoError(t, err)
	assert.Equal(t, "Hello Alice!", resp.Content)
}

func TestTemplate_SessionVarsEmpty(t *testing.T) {
	g := NewResponseGenerator()
	// Template accessing .Vars when it's nil should work with empty map.
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: `Vars: {{ len .Vars }}`}}
	ctx := TemplateContext{Vars: map[string]any{}}
	resp, err := g.Generate(testAgentE008(), sc, ctx)
	require.NoError(t, err)
	assert.Equal(t, "Vars: 0", resp.Content)
}

// --- Regex Capture Groups ---

func TestTemplate_RegexCaptures(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{
		Content: `City: {{ index .Match "city" }}`,
	}}
	ctx := TemplateContext{
		Match: map[string]string{"city": "NYC"},
	}
	resp, err := g.Generate(testAgentE008(), sc, ctx)
	require.NoError(t, err)
	assert.Equal(t, "City: NYC", resp.Content)
}

// --- Template Caching ---

func TestTemplate_CachingWorks(t *testing.T) {
	g := NewResponseGenerator()
	template := `Turn: {{ .TurnNumber }}`
	sc := &types.Scenario{Name: "t", Response: types.ScenarioResponse{Content: template}}

	// First call — populates cache.
	resp1, err := g.Generate(testAgentE008(), sc, TemplateContext{TurnNumber: 1})
	require.NoError(t, err)
	assert.Equal(t, "Turn: 1", resp1.Content)

	// Second call — should use cached template.
	resp2, err := g.Generate(testAgentE008(), sc, TemplateContext{TurnNumber: 2})
	require.NoError(t, err)
	assert.Equal(t, "Turn: 2", resp2.Content)
}

// --- Error Handling ---

func TestTemplate_MalformedTemplate(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "bad", Response: types.ScenarioResponse{Content: `{{ .Invalid`}}
	_, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing template")
}

func TestTemplate_MissingField(t *testing.T) {
	g := NewResponseGenerator()
	sc := &types.Scenario{Name: "bad", Response: types.ScenarioResponse{Content: `{{ .NonexistentField }}`}}
	_, err := g.Generate(testAgentE008(), sc, TemplateContext{})
	assert.Error(t, err)
}
