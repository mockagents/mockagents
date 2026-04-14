package engine

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_BuiltInFallback_NoDefaultScenario(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "no-default-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:     "specific",
						Match:    &types.MatchRule{ContentContains: "magic-keyword"},
						Response: types.ScenarioResponse{Content: "Specific response."},
					},
					// No default scenario!
				},
			},
		},
	}

	eng := newTestEngine(agent)

	// Message that doesn't match any scenario.
	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "no-default-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "random message"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "_fallback", resp.ScenarioName)
	assert.Contains(t, resp.Content, "Mock response from no-default-agent")
}

func TestEngine_RegexCapturesInTemplate(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "capture-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "order-lookup",
						Match: &types.MatchRule{ContentRegex: `order (?P<order_id>ORD-\d+)`},
						Response: types.ScenarioResponse{
							Content: `Looking up order {{ index .Match "order_id" }}`,
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

	eng := newTestEngine(agent)

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "capture-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "Check order ORD-12345 please"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "order-lookup", resp.ScenarioName)
	assert.Equal(t, "Looking up order ORD-12345", resp.Content)
}

func TestEngine_SessionVarsInTemplate(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "var-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: `Turn {{ .TurnNumber }}`},
					},
				},
			},
		},
	}

	eng := newTestEngine(agent)

	// Turn 1.
	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "var-agent",
		SessionID: "sess-vars",
		Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Turn 1", resp.Content)

	// Turn 2.
	resp, err = eng.ProcessRequest(&InboundRequest{
		AgentName: "var-agent",
		SessionID: "sess-vars",
		Messages:  []RequestMessage{{Role: "user", Content: "again"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Turn 2", resp.Content)
}

func TestEngine_TemplateFunctionsInResponse(t *testing.T) {
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "template-agent"},
		Spec: types.AgentSpec{
			Model: "gpt-4o",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "dynamic",
						Match: &types.MatchRule{ContentContains: "dynamic"},
						Response: types.ScenarioResponse{
							Content: `ID: {{ uuid }}, Name: {{ fake_name }}`,
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
		AgentName: "template-agent",
		SessionID: "sess-tmpl",
		Messages:  []RequestMessage{{Role: "user", Content: "give me dynamic response"}},
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "ID: ")
	assert.Contains(t, resp.Content, "Name: ")
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	assert.Contains(t, resp.Content, "-")
}

func TestEngine_FallbackProtocolValid(t *testing.T) {
	// Fallback response should still have valid agent metadata.
	agent := &types.AgentDefinition{
		Metadata: types.Metadata{Name: "proto-agent"},
		Spec: types.AgentSpec{
			Model:    "gpt-4o",
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:     "unreachable",
						Match:    &types.MatchRule{ContentContains: "xyzzy"},
						Response: types.ScenarioResponse{Content: "found"},
					},
				},
			},
		},
	}

	eng := newTestEngine(agent)
	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "proto-agent",
		SessionID: "sess-proto",
		Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "proto-agent", resp.AgentName)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.NotEmpty(t, resp.Content)
}
