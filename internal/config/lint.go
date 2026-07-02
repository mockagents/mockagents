package config

import (
	"fmt"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// Lint returns non-fatal WARNINGS (round-11): configuration that loads and
// runs but silently does nothing on this agent's protocol. Warnings never
// fail validation; `mockagents validate --strict` upgrades them to errors,
// and ValidateBytes surfaces them in its report for the GUI editor.
func (v *Validator) Lint(def *types.AgentDefinition, filePath string, node *yaml.Node) []*ValidationError {
	ctx := &validationContext{file: filePath, node: node}

	// raw_arguments is honored only on the OpenAI surfaces (arguments is a
	// raw JSON string there); Anthropic/Gemini render structured objects, so
	// a planted raw payload silently degrades to {} (R9-19).
	if def.Spec.Protocol != "" && def.Spec.Protocol != "openai-chat-completions" {
		for i, sc := range def.Spec.Behavior.Scenarios {
			for j, tc := range sc.Response.ToolCalls {
				if tc.RawArguments != "" {
					ctx.addError(
						fmt.Sprintf("spec.behavior.scenarios[%d].response.tool_calls[%d].raw_arguments", i, j),
						fmt.Sprintf("raw_arguments is OpenAI-only and is silently ignored on protocol %q (the call renders arguments {})", def.Spec.Protocol),
						"Use structured `arguments:` on this protocol, or switch the agent to openai-chat-completions.")
				}
			}
		}
	}

	return ctx.errors
}
