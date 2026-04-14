package engine

import (
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "test-agent"},
		Spec: types.AgentSpec{
			Model:        "gpt-4o",
			SystemPrompt: "You are helpful.",
		},
	}
}

func TestResponseGenerator_StaticContent(t *testing.T) {
	g := NewResponseGenerator()
	agent := testAgent()
	scenario := &types.Scenario{
		Name:     "greeting",
		Response: types.ScenarioResponse{Content: "Hello! How can I help?"},
	}

	resp, err := g.Generate(agent, scenario, TemplateContext{})
	require.NoError(t, err)
	assert.Equal(t, "Hello! How can I help?", resp.Content)
	assert.Equal(t, "test-agent", resp.AgentName)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.Equal(t, "greeting", resp.ScenarioName)
	assert.Equal(t, "You are helpful.", resp.SystemPrompt)
}

func TestResponseGenerator_TemplateContent(t *testing.T) {
	g := NewResponseGenerator()
	agent := testAgent()
	scenario := &types.Scenario{
		Name:     "echo",
		Response: types.ScenarioResponse{Content: "You said: {{.Message}}"},
	}

	ctx := TemplateContext{
		Agent:   agent,
		Message: "hello world",
	}
	resp, err := g.Generate(agent, scenario, ctx)
	require.NoError(t, err)
	assert.Equal(t, "You said: hello world", resp.Content)
}

func TestResponseGenerator_TemplateWithTurnNumber(t *testing.T) {
	g := NewResponseGenerator()
	agent := testAgent()
	scenario := &types.Scenario{
		Name:     "turn-info",
		Response: types.ScenarioResponse{Content: "Turn {{.TurnNumber}}"},
	}

	resp, err := g.Generate(agent, scenario, TemplateContext{TurnNumber: 3})
	require.NoError(t, err)
	assert.Equal(t, "Turn 3", resp.Content)
}

func TestResponseGenerator_TemplateFunctions(t *testing.T) {
	g := NewResponseGenerator()
	agent := testAgent()

	tests := []struct {
		name     string
		template string
		check    func(t *testing.T, content string)
	}{
		{
			name:     "now",
			template: "Time: {{now}}",
			check:    func(t *testing.T, c string) { assert.Contains(t, c, "Time: 20") },
		},
		{
			name:     "uuid",
			template: "ID: {{uuid}}",
			check: func(t *testing.T, c string) {
				assert.Contains(t, c, "ID: ")
				assert.Len(t, strings.TrimPrefix(c, "ID: "), 36)
			},
		},
		{
			name:     "random_int",
			template: "N: {{random_int 1 100}}",
			check:    func(t *testing.T, c string) { assert.Contains(t, c, "N: ") },
		},
		{
			name:     "random_string",
			template: "S: {{random_string 8}}",
			check: func(t *testing.T, c string) {
				s := strings.TrimPrefix(c, "S: ")
				assert.Len(t, s, 8)
			},
		},
		{
			name:     "date_offset",
			template: `Date: {{date_offset 3 "days"}}`,
			check:    func(t *testing.T, c string) { assert.Contains(t, c, "Date: 20") },
		},
		{
			name:     "upper",
			template: `{{upper "hello"}}`,
			check:    func(t *testing.T, c string) { assert.Equal(t, "HELLO", c) },
		},
		{
			name:     "lower",
			template: `{{lower "HELLO"}}`,
			check:    func(t *testing.T, c string) { assert.Equal(t, "hello", c) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &types.Scenario{Name: "test", Response: types.ScenarioResponse{Content: tt.template}}
			resp, err := g.Generate(agent, sc, TemplateContext{Agent: agent})
			require.NoError(t, err)
			tt.check(t, resp.Content)
		})
	}
}

func TestResponseGenerator_InvalidTemplate(t *testing.T) {
	g := NewResponseGenerator()
	agent := testAgent()
	scenario := &types.Scenario{
		Name:     "bad",
		Response: types.ScenarioResponse{Content: "{{.BadField}}"},
	}

	_, err := g.Generate(agent, scenario, TemplateContext{})
	assert.Error(t, err)
}

func TestResponseGenerator_ToolCalls(t *testing.T) {
	g := NewResponseGenerator()
	agent := testAgent()
	scenario := &types.Scenario{
		Name: "with-tools",
		Response: types.ScenarioResponse{
			Content: "Let me look that up.",
			ToolCalls: []types.ToolCallSpec{
				{Name: "search", Arguments: map[string]any{"q": "test"}},
			},
		},
	}

	resp, err := g.Generate(agent, scenario, TemplateContext{})
	require.NoError(t, err)
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "search", resp.ToolCalls[0].Name)
}

func TestResponseGenerator_Metadata(t *testing.T) {
	g := NewResponseGenerator()
	agent := testAgent()
	scenario := &types.Scenario{
		Name: "meta",
		Response: types.ScenarioResponse{
			Content:  "ok",
			Metadata: map[string]any{"key": "value"},
		},
	}

	resp, err := g.Generate(agent, scenario, TemplateContext{})
	require.NoError(t, err)
	assert.Equal(t, "value", resp.Metadata["key"])
}
