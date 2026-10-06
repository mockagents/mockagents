package config

import (
	"fmt"
	"slices"

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

// LintDocuments returns cross-document WARNINGS (review C-13). Today: two
// agents in one tenant that claim the same spec.model. The registry routes a
// model to the lexicographically smallest agent name, so the others are only
// reachable by name; `start` logged this, but `validate` — even with
// --strict — never said a word.
func LintDocuments(docs *Documents) []*ValidationError {
	if docs == nil {
		return nil
	}
	type claim struct{ names []string }
	byModel := map[string]*claim{}
	for _, r := range docs.Agents {
		if r == nil || r.Definition == nil || r.Definition.Spec.Model == "" {
			continue
		}
		key := r.Definition.Metadata.TenantID + "\x00" + r.Definition.Spec.Model
		c := byModel[key]
		if c == nil {
			c = &claim{}
			byModel[key] = c
		}
		c.names = append(c.names, r.Definition.Metadata.Name)
	}
	var warnings []*ValidationError
	for _, r := range docs.Agents {
		if r == nil || r.Definition == nil || r.Definition.Spec.Model == "" {
			continue
		}
		c := byModel[r.Definition.Metadata.TenantID+"\x00"+r.Definition.Spec.Model]
		if len(c.names) < 2 {
			continue
		}
		winner := slices.Min(c.names)
		if r.Definition.Metadata.Name == winner {
			continue
		}
		ctx := &validationContext{file: r.FilePath, node: r.Node}
		ctx.addError("spec.model",
			fmt.Sprintf("model %q is also claimed by agent %q, which answers requests for it; this agent is reachable only by name", r.Definition.Spec.Model, winner),
			"Give each agent a distinct spec.model.")
		warnings = append(warnings, ctx.errors...)
	}
	return warnings
}
