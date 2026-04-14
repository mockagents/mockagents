package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

var (
	validProtocols  = []string{"openai-chat-completions", "anthropic-messages"}
	metadataNameRe  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	toolNameRe      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// ValidationError represents a single validation problem with location context.
type ValidationError struct {
	File       string `json:"file"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
	Field      string `json:"field"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

func (e *ValidationError) Error() string {
	loc := e.File
	if e.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", e.File, e.Line, e.Column)
	}
	s := fmt.Sprintf("%s: %s: %s", loc, e.Field, e.Message)
	if e.Suggestion != "" {
		s += "\n  Suggestion: " + e.Suggestion
	}
	return s
}

// ValidationErrorList collects multiple validation errors.
type ValidationErrorList struct {
	Errors []*ValidationError
}

func (l *ValidationErrorList) Error() string {
	var b strings.Builder
	for i, e := range l.Errors {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(e.Error())
	}
	return b.String()
}

func (l *ValidationErrorList) HasErrors() bool {
	return len(l.Errors) > 0
}

// validationContext accumulates errors during validation.
type validationContext struct {
	file   string
	node   *yaml.Node
	errors []*ValidationError
}

func (ctx *validationContext) addError(field, message, suggestion string) {
	line, col := LineColOf(ctx.node, field)
	ctx.errors = append(ctx.errors, &ValidationError{
		File:       ctx.file,
		Line:       line,
		Column:     col,
		Field:      field,
		Message:    message,
		Suggestion: suggestion,
	})
}

// Validator performs programmatic validation of agent definitions.
type Validator struct{}

// Validate runs all validation rules against an AgentDefinition.
// It always collects every error before returning.
// Returns nil if valid.
func (v *Validator) Validate(def *types.AgentDefinition, filePath string, node *yaml.Node) *ValidationErrorList {
	ctx := &validationContext{file: filePath, node: node}

	v.validateAPIVersion(ctx, def)
	v.validateKind(ctx, def)
	v.validateMetadata(ctx, def)
	v.validateProtocol(ctx, def)
	v.validateScenarios(ctx, def)
	v.validateTools(ctx, def)
	v.validateCrossReferences(ctx, def)

	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}

func (v *Validator) validateAPIVersion(ctx *validationContext, def *types.AgentDefinition) {
	if def.APIVersion == "" {
		ctx.addError("apiVersion", "required field missing",
			fmt.Sprintf("Add apiVersion: %s", types.AgentAPIVersion))
		return
	}
	if def.APIVersion != types.AgentAPIVersion {
		ctx.addError("apiVersion",
			fmt.Sprintf("unsupported version %q", def.APIVersion),
			fmt.Sprintf("Use apiVersion: %s", types.AgentAPIVersion))
	}
}

func (v *Validator) validateKind(ctx *validationContext, def *types.AgentDefinition) {
	if def.Kind == "" {
		ctx.addError("kind", "required field missing",
			fmt.Sprintf("Add kind: %s", types.AgentKind))
		return
	}
	if def.Kind != types.AgentKind {
		ctx.addError("kind",
			fmt.Sprintf("unsupported kind %q", def.Kind),
			fmt.Sprintf("Use kind: %s", types.AgentKind))
	}
}

func (v *Validator) validateMetadata(ctx *validationContext, def *types.AgentDefinition) {
	if def.Metadata.Name == "" {
		ctx.addError("metadata.name", "required field missing",
			"Add a kebab-case name, e.g. metadata.name: my-agent")
		return
	}
	if len(def.Metadata.Name) > 63 {
		ctx.addError("metadata.name",
			fmt.Sprintf("name exceeds 63 characters (got %d)", len(def.Metadata.Name)),
			"Shorten the agent name to 63 characters or fewer.")
	}
	if !metadataNameRe.MatchString(def.Metadata.Name) {
		ctx.addError("metadata.name",
			fmt.Sprintf("invalid name %q: must be lowercase kebab-case", def.Metadata.Name),
			"Use lowercase letters, numbers, and hyphens only (e.g. my-agent-1).")
	}
}

func (v *Validator) validateProtocol(ctx *validationContext, def *types.AgentDefinition) {
	if def.Spec.Protocol == "" {
		ctx.addError("spec.protocol", "required field missing",
			fmt.Sprintf("Set spec.protocol to one of: %s", strings.Join(validProtocols, ", ")))
		return
	}
	for _, p := range validProtocols {
		if def.Spec.Protocol == p {
			return
		}
	}
	ctx.addError("spec.protocol",
		fmt.Sprintf("invalid protocol %q", def.Spec.Protocol),
		fmt.Sprintf("Must be one of: %s", strings.Join(validProtocols, ", ")))
}

func (v *Validator) validateScenarios(ctx *validationContext, def *types.AgentDefinition) {
	scenarios := def.Spec.Behavior.Scenarios
	if len(scenarios) == 0 {
		ctx.addError("spec.behavior.scenarios", "at least one scenario is required",
			"Add a scenario with a name and response content.")
		return
	}

	names := make(map[string]bool)
	for i, sc := range scenarios {
		field := fmt.Sprintf("spec.behavior.scenarios.%d", i)

		if sc.Name == "" {
			ctx.addError(field+".name", "scenario name is required",
				"Add a unique name for this scenario.")
		} else if names[sc.Name] {
			ctx.addError(field+".name",
				fmt.Sprintf("duplicate scenario name %q", sc.Name),
				"Each scenario must have a unique name.")
		} else {
			names[sc.Name] = true
		}

		if sc.Response.Content == "" {
			ctx.addError(field+".response.content", "response content is required",
				"Add content text for this scenario's response.")
		}

		if sc.Match != nil {
			v.validateMatchRule(ctx, sc.Match, field+".match")
		}
	}
}

func (v *Validator) validateMatchRule(ctx *validationContext, rule *types.MatchRule, field string) {
	if rule.ContentContains != "" && rule.ContentRegex != "" {
		ctx.addError(field,
			"content_contains and content_regex are mutually exclusive",
			"Use only one of content_contains or content_regex per match rule.")
	}
	if rule.ContentRegex != "" {
		if _, err := regexp.Compile(rule.ContentRegex); err != nil {
			ctx.addError(field+".content_regex",
				fmt.Sprintf("invalid regex: %s", err),
				"Fix the regular expression syntax.")
		}
	}
}

func (v *Validator) validateTools(ctx *validationContext, def *types.AgentDefinition) {
	names := make(map[string]bool)
	for i, tool := range def.Spec.Tools {
		field := fmt.Sprintf("spec.tools.%d", i)

		if tool.Name == "" {
			ctx.addError(field+".name", "tool name is required",
				"Add a snake_case name for this tool.")
			continue
		}
		if !toolNameRe.MatchString(tool.Name) {
			ctx.addError(field+".name",
				fmt.Sprintf("invalid tool name %q: must be snake_case", tool.Name),
				"Use lowercase letters, numbers, and underscores (e.g. lookup_order).")
		}
		if names[tool.Name] {
			ctx.addError(field+".name",
				fmt.Sprintf("duplicate tool name %q", tool.Name),
				"Each tool must have a unique name within an agent.")
		}
		names[tool.Name] = true

		v.validateJSONSchema(ctx, tool.Parameters, field+".parameters")
	}
}

func (v *Validator) validateJSONSchema(ctx *validationContext, schema types.JSONSchemaObject, field string) {
	if len(schema) == 0 {
		return
	}
	if _, ok := schema["type"]; !ok {
		ctx.addError(field,
			"JSON Schema missing 'type' field",
			"Add a 'type' field to the parameters schema (e.g. type: object).")
	}
}

func (v *Validator) validateCrossReferences(ctx *validationContext, def *types.AgentDefinition) {
	toolNames := make(map[string]bool)
	for _, tool := range def.Spec.Tools {
		if tool.Name != "" {
			toolNames[tool.Name] = true
		}
	}

	for i, sc := range def.Spec.Behavior.Scenarios {
		for j, tc := range sc.Response.ToolCalls {
			if tc.Name != "" && !toolNames[tc.Name] {
				field := fmt.Sprintf("spec.behavior.scenarios.%d.response.tool_calls.%d.name", i, j)
				ctx.addError(field,
					fmt.Sprintf("tool_call references undefined tool %q", tc.Name),
					fmt.Sprintf("Define tool %q in spec.tools or correct the reference.", tc.Name))
			}
		}
	}
}
