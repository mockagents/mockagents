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
		{
			name:     "title",
			template: `{{title "hello WORLD"}}`,
			check:    func(t *testing.T, c string) { assert.Equal(t, "Hello World", c) },
		},
		{
			name:     "title_non_ascii",
			template: `{{title "über"}}`,
			check:    func(t *testing.T, c string) { assert.Equal(t, "Über", c) },
		},
		{
			name:     "title_empty",
			template: `{{title ""}}`,
			check:    func(t *testing.T, c string) { assert.Equal(t, "", c) },
		},
		{
			name:     "title_collapses_whitespace",
			template: `{{title "a   b"}}`,
			check:    func(t *testing.T, c string) { assert.Equal(t, "A B", c) },
		},
		{
			name:     "fake_phone",
			template: "P: {{fake_phone}}",
			check: func(t *testing.T, c string) {
				p := strings.TrimPrefix(c, "P: ")
				assert.Regexp(t, `^\(\d{3}\) 555-\d{4}$`, p)
			},
		},
		{
			name:     "fake_company",
			template: "C: {{fake_company}}",
			check: func(t *testing.T, c string) {
				assert.Regexp(t, `^C: \w+ \w+$`, c)
			},
		},
		{
			name:     "fake_username",
			template: "U: {{fake_username}}",
			check: func(t *testing.T, c string) {
				u := strings.TrimPrefix(c, "U: ")
				assert.Regexp(t, `^[a-z]+_[a-z]+\d{1,2}$`, u)
			},
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

func TestRandomString_Clamped(t *testing.T) {
	// Non-positive lengths must yield "" rather than panicking on make([]byte, n<0).
	assert.Equal(t, "", randomString(0))
	assert.Equal(t, "", randomString(-1))
	assert.Equal(t, "", randomString(-1e9))

	// In-range lengths are honored exactly.
	assert.Len(t, randomString(8), 8)
	assert.Len(t, randomString(maxRandomStringLen), maxRandomStringLen)

	// Oversized requests are capped so {{ random_string 1e9 }} can't allocate GBs.
	assert.Len(t, randomString(maxRandomStringLen+1), maxRandomStringLen)
	assert.Len(t, randomString(1_000_000_000), maxRandomStringLen)
}

func TestRandomInt_DocumentedRange(t *testing.T) {
	// F-RG-008: degenerate min>=max returns min with no diagnostic.
	assert.Equal(t, 10, randomInt(10, 5))
	assert.Equal(t, 5, randomInt(5, 5))

	// Documented as INCLUSIVE [min,max]: over many draws of a 2-wide range
	// both endpoints must appear.
	sawMin, sawMax := false, false
	for i := 0; i < 200; i++ {
		switch randomInt(1, 2) {
		case 1:
			sawMin = true
		case 2:
			sawMax = true
		default:
			t.Fatal("randomInt(1,2) returned a value outside [1,2]")
		}
	}
	assert.True(t, sawMin && sawMax, "inclusive range should yield both 1 and 2")
}

func TestRandomFloat_DocumentedRange(t *testing.T) {
	// F-RG-003: degenerate min>=max returns min.
	assert.Equal(t, 2.0, randomFloat(2, 1))
	assert.Equal(t, 5.0, randomFloat(5, 5))

	// Documented as HALF-OPEN [min,max): max is never returned.
	for i := 0; i < 500; i++ {
		v := randomFloat(0, 1)
		assert.GreaterOrEqual(t, v, 0.0)
		assert.Less(t, v, 1.0)
	}
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
