package config

import (
	"fmt"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// validA2ATaskStates lists the terminal/intermediate task states a response may
// declare. Keep in sync with internal/a2a.
var validA2ATaskStates = []string{
	"submitted", "working", "input-required", "completed", "canceled", "failed", "rejected",
}

const maxA2ALatencyMs = 60_000

// ValidateA2AServer runs rule-based validation against an A2AServerDefinition
// (NF-04). Mirrors ValidateMCPServer so every document kind flows through the
// same plumbing. Returns nil on success.
//
// Rules:
//   - apiVersion required and equal to mockagents/v1
//   - kind required and equal to A2AServer
//   - metadata.name required, kebab-case, ≤63 chars
//   - spec.card.name required (the Agent Card must identify the agent)
//   - every skill has an id
//   - every response has either a `match` substring OR `default: true` (not
//     both); at most one default; a declared `state` must be a known task state
func ValidateA2AServer(def *types.A2AServerDefinition, filePath string, node *yaml.Node) *ValidationErrorList {
	ctx := &validationContext{file: filePath, node: node}

	if def.APIVersion == "" {
		ctx.addError("apiVersion", "required field missing",
			fmt.Sprintf("Add apiVersion: %s", types.AgentAPIVersion))
	} else if def.APIVersion != types.AgentAPIVersion {
		ctx.addError("apiVersion",
			fmt.Sprintf("unsupported version %q", def.APIVersion),
			fmt.Sprintf("Use apiVersion: %s", types.AgentAPIVersion))
	}
	if def.Kind == "" {
		ctx.addError("kind", "required field missing",
			fmt.Sprintf("Add kind: %s", types.A2AServerKind))
	} else if def.Kind != types.A2AServerKind {
		ctx.addError("kind",
			fmt.Sprintf("unsupported kind %q", def.Kind),
			fmt.Sprintf("Use kind: %s", types.A2AServerKind))
	}
	validateMetadataName(ctx, def.Metadata.Name, "metadata.name", "a2a-server")
	if def.Spec.Faults.LatencyMs < 0 || def.Spec.Faults.LatencyMs > maxA2ALatencyMs {
		ctx.addError("spec.faults.latency_ms", fmt.Sprintf("latency_ms must be between 0 and %d", maxA2ALatencyMs), "")
	}
	if def.Spec.Faults.TimeoutMs < 0 || def.Spec.Faults.TimeoutMs > maxA2ALatencyMs {
		ctx.addError("spec.faults.timeout_ms", fmt.Sprintf("timeout_ms must be between 0 and %d", maxA2ALatencyMs), "")
	}
	if rate := def.Spec.Faults.Rate; rate != nil && (*rate < 0 || *rate > 1) {
		ctx.addError("spec.faults.rate", "rate must be between 0 and 1", "")
	}
	for operation, rate := range def.Spec.Faults.OperationRates {
		if operation == "" || rate < 0 || rate > 1 {
			ctx.addError("spec.faults.operation_rates", "operation names must be non-empty and rates between 0 and 1", "")
		}
	}
	for sequence, rate := range def.Spec.Faults.SequenceRates {
		if sequence == 0 || rate < 0 || rate > 1 {
			ctx.addError("spec.faults.sequence_rates", "sequence indexes must start at 1 and rates be between 0 and 1", "")
		}
	}

	if def.Spec.Card.Name == "" {
		ctx.addError("spec.card.name", "required field missing",
			"Every A2A Agent Card needs a name.")
	}
	for i, sk := range def.Spec.Card.Skills {
		if sk.ID == "" {
			ctx.addError(fmt.Sprintf("spec.card.skills[%d].id", i),
				"required field missing", "Every skill needs an id.")
		}
	}

	var sawDefault bool
	for i, r := range def.Spec.Responses {
		field := fmt.Sprintf("spec.responses[%d]", i)
		if r.Match == "" && !r.Default {
			ctx.addError(field, "response has neither `match` nor `default: true`",
				"Either add a `match:` substring or set `default: true`.")
		}
		if r.Match != "" && r.Default {
			ctx.addError(field, "response sets both `match` and `default: true`",
				"Pick one: `match:` (selective) or `default: true` (fallback).")
		}
		if r.Default {
			if sawDefault {
				ctx.addError(field, "multiple default responses",
					"Only one response may set `default: true`.")
			}
			sawDefault = true
		}
		if r.State != "" && !a2aStateKnown(r.State) {
			ctx.addError(field+".state",
				fmt.Sprintf("unknown task state %q", r.State),
				"Use one of: completed, failed, input-required, canceled, working, submitted, rejected.")
		}
	}

	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}

func a2aStateKnown(s string) bool {
	for _, v := range validA2ATaskStates {
		if v == s {
			return true
		}
	}
	return false
}
