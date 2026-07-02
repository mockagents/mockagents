package config

import (
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func lintAgent(protocol, rawArgs string) *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion,
		Kind:       types.AgentKind,
		Metadata:   types.Metadata{Name: "lint-test"},
		Spec: types.AgentSpec{
			Protocol: protocol,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name: "s1",
					Response: types.ScenarioResponse{
						Content: "hi",
						ToolCalls: []types.ToolCallSpec{{
							Name: "get_weather", RawArguments: rawArgs}},
					},
				}},
			},
			Tools: []types.ToolDefinition{{Name: "get_weather"}},
		},
	}
}

// R9-19: raw_arguments on a non-OpenAI protocol is a lint WARNING (it is
// silently ignored there), never an error.
func TestLintRawArgumentsProtocol(t *testing.T) {
	v := &Validator{}

	warns := v.Lint(lintAgent("anthropic-messages", `{"city":`), "a.yaml", nil)
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "OpenAI-only") {
		t.Errorf("anthropic + raw_arguments: warnings = %v, want the OpenAI-only warning", warns)
	}
	if errs := v.Validate(lintAgent("anthropic-messages", `{"city":`), "a.yaml", nil); errs != nil {
		t.Errorf("lint finding must not be a validation ERROR: %v", errs)
	}

	if warns := v.Lint(lintAgent("openai-chat-completions", `{"city":`), "a.yaml", nil); len(warns) != 0 {
		t.Errorf("openai + raw_arguments should not warn: %v", warns)
	}
	if warns := v.Lint(lintAgent("google-gemini", ""), "a.yaml", nil); len(warns) != 0 {
		t.Errorf("no raw_arguments should not warn: %v", warns)
	}
}

// The strict_tools level must be one of types.StrictToolLevels.
func TestValidateStrictToolsLevel(t *testing.T) {
	v := &Validator{}
	def := lintAgent("openai-chat-completions", "")
	def.Spec.Behavior.StrictTools = &types.StrictToolsConfig{Level: "banana"}
	errs := v.Validate(def, "a.yaml", nil)
	if errs == nil || !strings.Contains(errs.Error(), `unknown level "banana"`) {
		t.Errorf("bad level not rejected: %v", errs)
	}

	for _, level := range append([]string{""}, types.StrictToolLevels...) {
		def.Spec.Behavior.StrictTools = &types.StrictToolsConfig{Level: level}
		if errs := v.Validate(def, "a.yaml", nil); errs != nil {
			t.Errorf("level %q rejected: %v", level, errs)
		}
	}
}

// ValidateBytes surfaces lint findings in the Warnings field (GUI editor +
// POST /api/v1/config/validate).
func TestValidateBytesWarnings(t *testing.T) {
	report := ValidateBytes([]byte(`
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: warn-agent
spec:
  protocol: anthropic-messages
  behavior:
    scenarios:
      - name: s1
        response:
          content: hi
          tool_calls:
            - name: get_weather
              raw_arguments: '{"city":'
  tools:
    - name: get_weather
`))
	if len(report.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", report.Errors)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0].Message, "OpenAI-only") {
		t.Errorf("warnings = %v, want 1 OpenAI-only warning", report.Warnings)
	}
}
