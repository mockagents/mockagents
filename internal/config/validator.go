package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

var (
	validProtocols = []string{"openai-chat-completions", "anthropic-messages", "google-gemini", "ollama-chat", "bedrock-converse"}
	metadataNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	toolNameRe     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
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
	v.validateChaos(ctx, def)
	v.validateStreaming(ctx, def)
	v.validateStrictTools(ctx, def)

	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}

// validateStrictTools checks the strict-tools knob block (round-11): the
// level must be one of types.StrictToolLevels (kept in sync with the JSON
// schema enum).
func (v *Validator) validateStrictTools(ctx *validationContext, def *types.AgentDefinition) {
	st := def.Spec.Behavior.StrictTools
	if st == nil || st.Level == "" {
		return
	}
	for _, l := range types.StrictToolLevels {
		if st.Level == l {
			return
		}
	}
	ctx.addError("spec.behavior.strict_tools.level",
		fmt.Sprintf("unknown level %q", st.Level),
		fmt.Sprintf("Use one of: %s.", strings.Join(types.StrictToolLevels, ", ")))
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

		// A response must carry SOMETHING: content, a refusal, or tool calls.
		// (Refusal-only and tool-call-only responses are valid — FB-03.)
		if sc.Response.Content == "" && sc.Response.Refusal == "" && len(sc.Response.ToolCalls) == 0 {
			ctx.addError(field+".response.content", "response content is required",
				"Add content text (or a refusal / tool_calls) for this scenario's response.")
		}

		for j, tc := range sc.Response.ToolCalls {
			if tc.Name == "" {
				ctx.addError(fmt.Sprintf("%s.response.tool_calls.%d.name", field, j),
					"tool_call name is required",
					"Add the snake_case name of the tool to call.")
			}
		}

		if sc.Match != nil {
			v.validateMatchRule(ctx, sc.Match, field+".match")
		}

		if h := sc.Response.Hallucination; h != nil && h.Type != "" {
			valid := false
			for _, t := range types.HallucinationTypes {
				if h.Type == t {
					valid = true
					break
				}
			}
			if !valid {
				ctx.addError(field+".response.hallucination.type",
					fmt.Sprintf("invalid hallucination type %q", h.Type),
					fmt.Sprintf("Use one of: %s", strings.Join(types.HallucinationTypes, ", ")))
			}
		}
	}
}

func (v *Validator) validateStreaming(ctx *validationContext, def *types.AgentDefinition) {
	s := def.Spec.Behavior.Streaming
	if s == nil {
		return
	}
	// Distribution percentiles must be configured in pairs, p95 >= p50 (FB-05).
	// A lone percentile silently disables the distribution or collapses it to a
	// constant, so reject it rather than mislead a load-test author.
	checkPair := func(p50, p95 int, p50Field, p95Field string) {
		switch {
		case (p50 > 0) != (p95 > 0):
			set, missing := p50Field, p95Field
			if p95 > 0 {
				set, missing = p95Field, p50Field
			}
			ctx.addError("spec.behavior.streaming."+set,
				fmt.Sprintf("%s requires %s to also be set", set, missing),
				fmt.Sprintf("Set both %s and %s for distribution-based timing, or neither.", p50Field, p95Field))
		case p50 > 0 && p95 < p50:
			ctx.addError("spec.behavior.streaming."+p95Field,
				fmt.Sprintf("%s (%d) must be >= %s (%d)", p95Field, p95, p50Field, p50),
				"The 95th percentile must be at least the median.")
		}
	}
	checkPair(s.TTFTP50Ms, s.TTFTP95Ms, "ttft_p50_ms", "ttft_p95_ms")
	checkPair(s.ITLP50Ms, s.ITLP95Ms, "itl_p50_ms", "itl_p95_ms")
}

// maxChaosMs bounds every chaos delay an agent may declare (latency draws and
// the synthetic timeout sleep). It mirrors the engine's own cap on a single
// latency draw and the MCP/A2A validators' limits, so a typo such as
// `timeout_ms: 600000` cannot park request goroutines for ten minutes.
const maxChaosMs = 60_000

// latencyDistributions is the accepted chaos.latency.distribution set. It is
// mirrored in schema/mockagents-v1-agent.json (asserted by the schema parity
// test); "" means "infer from min/max".
var latencyDistributions = []string{"fixed", "uniform", "normal"}

func (v *Validator) validateChaos(ctx *validationContext, def *types.AgentDefinition) {
	c := def.Spec.Behavior.Chaos
	if c == nil {
		return
	}
	if c.Preset != "" && !isChaosPreset(c.Preset) {
		ctx.addError("spec.behavior.chaos.preset",
			fmt.Sprintf("unknown chaos preset %q", c.Preset),
			fmt.Sprintf("Use one of: %s", strings.Join(types.ChaosPresets, ", ")))
	}
	// The bounds below mirror the JSON schema, which nothing loads at runtime:
	// until they were enforced here, `status_code: 42` passed `validate`, the
	// GUI editor and the write API, and then panicked net/http's WriteHeader
	// on every request to the agent (audit M-01).
	if l := c.Latency; l != nil {
		if l.Distribution != "" && !slices.Contains(latencyDistributions, l.Distribution) {
			ctx.addError("spec.behavior.chaos.latency.distribution",
				fmt.Sprintf("unknown latency distribution %q", l.Distribution),
				fmt.Sprintf("Use one of: %s.", strings.Join(latencyDistributions, ", ")))
		}
		for field, ms := range map[string]int{"min_ms": l.MinMs, "max_ms": l.MaxMs, "mean_ms": l.MeanMs, "stddev_ms": l.StddevMs} {
			if ms < 0 || ms > maxChaosMs {
				ctx.addError("spec.behavior.chaos.latency."+field,
					fmt.Sprintf("%s must be between 0 and %d", field, maxChaosMs), "")
			}
		}
		if l.MinMs > 0 && l.MaxMs > 0 && l.MaxMs < l.MinMs {
			ctx.addError("spec.behavior.chaos.latency.max_ms",
				"max_ms must be >= min_ms", "")
		}
	}
	if e := c.Errors; e != nil {
		if e.Rate < 0 || e.Rate > 1 {
			ctx.addError("spec.behavior.chaos.errors.rate",
				"rate must be in [0.0, 1.0]", "")
		}
		if e.StatusCode != 0 && (e.StatusCode < 400 || e.StatusCode > 599) {
			ctx.addError("spec.behavior.chaos.errors.status_code",
				"status_code must be between 400 and 599",
				"Injected errors are HTTP error statuses; net/http rejects codes outside 100-999 outright.")
		}
		for i, code := range e.StatusCodes {
			if code < 400 || code > 599 {
				ctx.addError(fmt.Sprintf("spec.behavior.chaos.errors.status_codes.%d", i),
					"status_codes entries must be between 400 and 599", "")
			}
		}
		if e.TimeoutMs < 0 || e.TimeoutMs > maxChaosMs {
			ctx.addError("spec.behavior.chaos.errors.timeout_ms",
				fmt.Sprintf("timeout_ms must be between 0 and %d", maxChaosMs),
				"The timeout fault really sleeps the request for this long.")
		}
		if e.FailFirst < 0 {
			ctx.addError("spec.behavior.chaos.errors.fail_first",
				"fail_first must be >= 0", "")
		}
	}
	if rl := c.RateLimit; rl != nil {
		if rl.Requests < 1 {
			ctx.addError("spec.behavior.chaos.rate_limit.requests",
				"requests must be >= 1", "")
		}
		if rl.WindowMs < 1 {
			ctx.addError("spec.behavior.chaos.rate_limit.window_ms",
				"window_ms must be >= 1",
				"A zero window silently disables the limiter.")
		}
	}
	if cc := c.Connection; cc != nil {
		if !connectionModes[cc.Mode] {
			ctx.addError("spec.behavior.chaos.connection.mode",
				fmt.Sprintf("unknown connection mode %q", cc.Mode),
				fmt.Sprintf("Use one of: %s.", strings.Join(connectionModeNames(), ", ")))
		}
		if cc.Rate < 0 || cc.Rate > 1 {
			ctx.addError("spec.behavior.chaos.connection.rate",
				"rate must be in [0.0, 1.0]", "")
		}
		if cc.FailFirst < 0 {
			ctx.addError("spec.behavior.chaos.connection.fail_first",
				"fail_first must be >= 0", "")
		}
		if cc.Rate == 0 && cc.FailFirst == 0 {
			ctx.addError("spec.behavior.chaos.connection",
				"connection fault has no trigger: set rate or fail_first",
				"Without a trigger the fault never fires.")
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
