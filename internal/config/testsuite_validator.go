package config

import (
	"fmt"
	"regexp"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// ValidateTestSuite runs rule-based validation against a
// TestSuiteDefinition. Mirrors the shape of ValidatePipeline and the
// agent Validator.Validate so `mockagents validate` and the GUI
// editor can run every document kind through the same plumbing.
// Returns nil on success.
//
// Rules:
//   - apiVersion required and equal to mockagents/v1
//   - kind required and equal to TestSuite
//   - metadata.name required, kebab-case, ≤63 chars
//   - exactly one of spec.target.agent / spec.target.pipeline
//   - spec.cases non-empty
//   - every case has a name and at least one step
//   - every step has a role + content
//   - every assertion has a known type, the right fields for that
//     type, and valid max_ms (> 0) when type is latency_ms_lt
func ValidateTestSuite(def *types.TestSuiteDefinition, filePath string, node *yaml.Node) *ValidationErrorList {
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
			fmt.Sprintf("Add kind: %s", types.TestSuiteKind))
	} else if def.Kind != types.TestSuiteKind {
		ctx.addError("kind",
			fmt.Sprintf("unsupported kind %q", def.Kind),
			fmt.Sprintf("Use kind: %s", types.TestSuiteKind))
	}
	validateMetadataName(ctx, def.Metadata.Name, "metadata.name", "test-suite")
	validateTestSuiteTarget(ctx, def)
	validateTestSuiteCases(ctx, def)

	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}

// validateMetadataName is shared across the Pipeline/TestSuite/
// MCPServer validators. The agent validator has its own copy that
// predates the shared helper and stays in place for zero-churn.
func validateMetadataName(ctx *validationContext, name, field, suggestionSuffix string) {
	if name == "" {
		ctx.addError(field, "required field missing",
			fmt.Sprintf("Add a kebab-case name, e.g. %s: my-%s", field, suggestionSuffix))
		return
	}
	if len(name) > 63 {
		ctx.addError(field,
			fmt.Sprintf("name exceeds 63 characters (got %d)", len(name)),
			"Shorten the name to 63 characters or fewer.")
	}
	if !metadataNameRe.MatchString(name) {
		ctx.addError(field,
			fmt.Sprintf("invalid name %q: must be lowercase kebab-case", name),
			"Use lowercase letters, numbers, and hyphens only.")
	}
}

func validateTestSuiteTarget(ctx *validationContext, def *types.TestSuiteDefinition) {
	agent := def.Spec.Target.Agent
	pipeline := def.Spec.Target.Pipeline
	if agent == "" && pipeline == "" {
		ctx.addError("spec.target", "test suite has no target",
			"Set exactly one of spec.target.agent or spec.target.pipeline.")
		return
	}
	if agent != "" && pipeline != "" {
		ctx.addError("spec.target",
			"test suite has both agent and pipeline target",
			"Remove one of spec.target.agent or spec.target.pipeline — exactly one is allowed.")
	}
}

func validateTestSuiteCases(ctx *validationContext, def *types.TestSuiteDefinition) {
	if len(def.Spec.Cases) == 0 {
		ctx.addError("spec.cases", "test suite declares no cases",
			"Add at least one entry under spec.cases with a name and at least one step.")
		return
	}
	seenNames := make(map[string]int, len(def.Spec.Cases))
	for i, c := range def.Spec.Cases {
		field := fmt.Sprintf("spec.cases[%d]", i)
		if c.Name == "" {
			ctx.addError(field+".name", "required field missing",
				"Every test case needs a name (unique within the suite).")
		} else if prev, dup := seenNames[c.Name]; dup {
			ctx.addError(field+".name",
				fmt.Sprintf("duplicate case name %q (first seen at spec.cases[%d])", c.Name, prev),
				"Each case name must be unique within a test suite.")
		} else {
			seenNames[c.Name] = i
		}
		if len(c.Steps) == 0 {
			ctx.addError(field+".steps", "case has no steps",
				"Add at least one step with a role and content.")
		} else {
			for j, s := range c.Steps {
				stepField := fmt.Sprintf("%s.steps[%d]", field, j)
				if s.Role == "" {
					ctx.addError(stepField+".role", "required field missing",
						"Set role to user, assistant, or system.")
				}
				if s.Content == "" {
					ctx.addError(stepField+".content", "required field missing",
						"Give the step a non-empty content string.")
				}
			}
		}
		for j, a := range c.Assertions {
			validateTestAssertion(ctx, fmt.Sprintf("%s.assertions[%d]", field, j), &a)
		}
	}
}

func validateTestAssertion(ctx *validationContext, field string, a *types.TestAssertion) {
	if a.Type == "" {
		ctx.addError(field+".type", "required field missing",
			"Set type to one of: "+assertionTypeList+".")
		return
	}
	switch a.Type {
	case types.AssertToolCall:
		if a.Tool == "" {
			ctx.addError(field+".tool", "tool_call assertion missing tool name",
				"Set assertions[].tool to the name of an expected tool call.")
		}
	case types.AssertToolCallArgs:
		if a.Tool == "" {
			ctx.addError(field+".tool", "tool_call_args assertion missing tool name",
				"Set assertions[].tool to the name of the tool whose arguments you assert.")
		}
		if len(a.Args) == 0 {
			ctx.addError(field+".arguments", "tool_call_args assertion missing arguments",
				"Set assertions[].arguments to the (possibly dotted-path) argument values to match.")
		}
	case types.AssertResponseContains:
		if a.Value == "" {
			ctx.addError(field+".value", "response_contains assertion missing value",
				"Set assertions[].value to the substring the response must contain.")
		}
	case types.AssertResponseMatches:
		if a.Value == "" {
			ctx.addError(field+".value", "response_matches assertion missing value",
				"Set assertions[].value to the regular expression the response must match.")
		} else if _, err := regexp.Compile(a.Value); err != nil {
			ctx.addError(field+".value",
				fmt.Sprintf("response_matches value is not a valid regular expression: %v", err),
				"Fix the regular expression in assertions[].value.")
		}
	case types.AssertScenarioMatched:
		if a.Value == "" {
			ctx.addError(field+".value", "scenario_matched assertion missing scenario name",
				"Set assertions[].value to the scenario name that must have fired.")
		}
	case types.AssertLatencyMsLT:
		if a.MaxMs <= 0 {
			ctx.addError(field+".max_ms", "latency_ms_lt assertion needs a positive max_ms",
				"Set assertions[].max_ms to the latency budget in milliseconds.")
		}
	case types.AssertToolCallCount:
		if a.Count == nil {
			ctx.addError(field+".count", "tool_call_count assertion missing count",
				"Set assertions[].count to the exact number of tool calls expected (0 for none).")
		} else if *a.Count < 0 {
			ctx.addError(field+".count", "tool_call_count count must not be negative",
				"Set assertions[].count to a non-negative integer.")
		}
	case types.AssertToolCallSequence, types.AssertNodeSequence:
		if len(a.Sequence) == 0 {
			ctx.addError(field+".sequence",
				fmt.Sprintf("%s assertion missing sequence", a.Type),
				"Set assertions[].sequence to the ordered list of expected names/ids.")
		}
	case types.AssertNoToolCall, types.AssertRefusal:
		// no required fields (refusal's optional `value` narrows the match)
	default:
		ctx.addError(field+".type",
			fmt.Sprintf("unknown assertion type %q", a.Type),
			"Supported types: "+assertionTypeList+".")
	}
}

// assertionTypeList is the human-readable roster of supported assertion types,
// kept in one place so the "required"/"unknown" hints stay in sync.
const assertionTypeList = "tool_call, tool_call_args, no_tool_call, response_contains, " +
	"response_matches, scenario_matched, refusal, latency_ms_lt, tool_call_count, " +
	"tool_call_sequence, node_sequence"
