package engine

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/internal/toolschema"
	"github.com/mockagents/mockagents/internal/types"
)

// StrictMode is the effective enforcement mode of one strict-tools dimension.
type StrictMode int

const (
	// StrictOff — lenient, today's default behavior.
	StrictOff StrictMode = iota
	// StrictWarn — run the check, log a warning and surface it via the
	// X-Mockagents-Strict-Violation response header; the request succeeds.
	StrictWarn
	// StrictEnforce — fail the request with the provider's real 400 shape.
	StrictEnforce
)

// StrictToolModes is the resolved per-dimension mode set for one request.
type StrictToolModes struct {
	IDs        StrictMode // round-trip tool id validation (R9-15)
	ToolChoice StrictMode // required/named forcing + parallel cap (R9-16a)
	Schemas    StrictMode // strict:true schema-subset validation (R9-16b)
}

// Any reports whether any dimension is active at all — hot-path early-out.
func (m StrictToolModes) Any() bool {
	return m.IDs != StrictOff || m.ToolChoice != StrictOff || m.Schemas != StrictOff
}

// ParseStrictLevel maps a level string to a StrictMode. "1"/"true" are
// accepted as "strict" for MOCKAGENTS_REALTIME_STRICT-style boolean muscle
// memory; anything unrecognized (including "") is off.
func ParseStrictLevel(s string) StrictMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "warn":
		return StrictWarn
	case "strict", "1", "true":
		return StrictEnforce
	}
	return StrictOff
}

// envStrictLevel caches the MOCKAGENTS_STRICT_TOOLS fleet default; read once
// per process like the other env knobs.
var envStrictLevel = sync.OnceValue(func() StrictMode {
	return ParseStrictLevel(os.Getenv("MOCKAGENTS_STRICT_TOOLS"))
})

// StrictToolsFor resolves the effective strict-tools modes for an agent:
// the agent's spec.behavior.strict_tools block when present, else the
// MOCKAGENTS_STRICT_TOOLS env default, else off (YAML > env > off).
func StrictToolsFor(agent *types.AgentDefinition) StrictToolModes {
	var cfg *types.StrictToolsConfig
	if agent != nil {
		cfg = agent.Spec.Behavior.StrictTools
	}
	return resolveStrictTools(cfg, envStrictLevel())
}

// StrictToolError is a strict-tools violation. The engine detects it
// provider-neutrally; each adapter renders its provider's real 400 body from
// the structured fields (the ChaosError translation precedent). Providers
// disagree on which SIDE of an id mismatch they report — OpenAI blames the
// unanswered call ids, Anthropic the unexpected result id — so the error
// carries both findings and each adapter picks its rendering.
type StrictToolError struct {
	// Check is the knob dimension: "ids", "tool_choice", or "schemas".
	Check string
	// Kind is the primary violation for logs and the warn header: "orphan"
	// (a tool result with no preceding tool calls), "unknown" (a result
	// referencing an id never echoed), "unanswered" (echoed call ids with no
	// results), plus tool_choice/schema kinds from later checks.
	Kind string
	// Index is the offending message index (orphan/unknown).
	Index int
	// UnknownID is the result id that matched no echoed call (unknown/orphan
	// when the result carried one).
	UnknownID string
	// UnansweredIDs are echoed call ids no tool result answered.
	UnansweredIDs []string
	// Message is the provider-neutral text (logs + warn header).
	Message string
}

func (e *StrictToolError) Error() string { return e.Message }

// AsStrictToolError unwraps a StrictToolError, or nil.
func AsStrictToolError(err error) *StrictToolError {
	var se *StrictToolError
	if errors.As(err, &se) {
		return se
	}
	return nil
}

// validateRoundTripIDs enforces the round-trip tool id contract every real
// API applies (round-11 R9-15): a tool result must respond to a tool call
// echoed earlier in the history, and every echoed call must be answered
// before the conversation moves on. Returns nil when the history is
// coherent.
//
// Matching is by global id set (Gemini has no ids — adapters put the
// function NAME in both ID fields, so name matching falls out). Calls echoed
// with no id and results carrying no id are skipped — there is nothing to
// match — except that a tool result arriving before ANY tool call is always
// the orphan violation. A trailing assistant echo with nothing after it is
// exempt from the unanswered check (the client answers it on the next
// request).
func validateRoundTripIDs(msgs []RequestMessage) *StrictToolError {
	echoed := map[string]bool{}
	answered := map[string]bool{}
	var echoedOrder []string
	anyCalls := false
	var firstUnknown *StrictToolError

	for i, m := range msgs {
		if m.IsToolResult {
			if !anyCalls {
				se := &StrictToolError{
					Check: "ids", Kind: "orphan", Index: i,
					Message: fmt.Sprintf("messages[%d]: tool result without any preceding tool calls", i),
				}
				if len(m.ToolResultIDs) > 0 {
					se.UnknownID = m.ToolResultIDs[0]
				}
				return se
			}
			for _, id := range m.ToolResultIDs {
				answered[id] = true
				if !echoed[id] && firstUnknown == nil {
					firstUnknown = &StrictToolError{
						Check: "ids", Kind: "unknown", Index: i, UnknownID: id,
						Message: fmt.Sprintf("messages[%d]: tool result references unknown tool call id %q", i, id),
					}
				}
			}
		}
		for _, tc := range m.ToolCalls {
			anyCalls = true
			if tc.ID != "" && !echoed[tc.ID] {
				echoed[tc.ID] = true
				// A trailing assistant echo (no messages after it) is exempt.
				if i < len(msgs)-1 {
					echoedOrder = append(echoedOrder, tc.ID)
				}
			}
		}
	}

	var unanswered []string
	for _, id := range echoedOrder {
		if !answered[id] {
			unanswered = append(unanswered, id)
		}
	}

	switch {
	case firstUnknown != nil:
		firstUnknown.UnansweredIDs = unanswered
		return firstUnknown
	case len(unanswered) > 0:
		return &StrictToolError{
			Check: "ids", Kind: "unanswered", UnansweredIDs: unanswered,
			Message: fmt.Sprintf("tool call ids without tool results: %s", strings.Join(unanswered, ", ")),
		}
	}
	return nil
}

// validateToolChoiceName rejects a named tool_choice whose function is not
// among the REQUEST's declared tools — real APIs 400 this (round-11 R9-16a).
// The check keys on the request's own tools[] (what the real API validates
// against), not the agent's YAML; a request that declared no tools is
// skipped.
func validateToolChoiceName(tc ToolChoice, requestToolNames []string) *StrictToolError {
	if tc.Name == "" || len(requestToolNames) == 0 || slices.Contains(requestToolNames, tc.Name) {
		return nil
	}
	return &StrictToolError{
		Check: "tool_choice", Kind: "unknown_tool", UnknownID: tc.Name,
		Message: fmt.Sprintf("tool_choice %q not found in the request's tools", tc.Name),
	}
}

// applyStrictToolChoice enforces (strict) or observes (warn) the forcing
// contract after generation: under required/named/allowed forcing a real API
// ALWAYS returns a tool call, so strict mode synthesizes the forced call
// (args {}) when the scenario emitted none or the wrong one — the wire
// contract wins over the scenario. Synthesized responses drop the text
// content (real forced responses carry no preamble). ParallelDisabled caps
// the response to one call, mirroring "exactly zero or one". OpenAI-family
// forced calls report finish_reason "stop", not "tool_calls"
// (staff-confirmed round-11 ground truth). Warn mode never mutates — it
// returns the would-have descriptions.
func applyStrictToolChoice(tc ToolChoice, mode StrictMode, resp *Response, agent *types.AgentDefinition, requestToolNames []string) []string {
	if tc.None || mode == StrictOff {
		return nil
	}
	var warnings []string
	warn := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	synthesize := func(name string) {
		resp.ToolCalls = []types.ToolCallSpec{{Name: name, Arguments: map[string]any{}}}
		resp.Content = ""
	}
	allowed := func(name string) bool {
		return len(tc.AllowedNames) == 0 || slices.Contains(tc.AllowedNames, name)
	}

	if tc.Required {
		forced := false
		switch {
		case tc.Name != "" && (len(resp.ToolCalls) == 0 || resp.ToolCalls[0].Name != tc.Name):
			if mode == StrictEnforce {
				synthesize(tc.Name)
				forced = true
			} else {
				warn("tool_choice: forced tool %q was not the emitted call (strict would synthesize it)", tc.Name)
			}
		case tc.Name == "" && len(resp.ToolCalls) == 0:
			name := firstToolName(tc.AllowedNames, requestToolNames, agent)
			if name == "" {
				warn("tool_choice: required but no tools are declared to synthesize from")
				break
			}
			if mode == StrictEnforce {
				synthesize(name)
				forced = true
			} else {
				warn("tool_choice: required but the scenario emitted no tool call (strict would synthesize %q)", name)
			}
		case len(resp.ToolCalls) > 0 && !allowed(resp.ToolCalls[0].Name):
			if mode == StrictEnforce {
				synthesize(tc.AllowedNames[0])
				forced = true
			} else {
				warn("tool_choice: emitted call %q is outside allowedFunctionNames (strict would synthesize %q)",
					resp.ToolCalls[0].Name, tc.AllowedNames[0])
			}
		default:
			// The scenario naturally satisfied the forcing.
			forced = mode == StrictEnforce && len(resp.ToolCalls) > 0
		}
		if forced && resp.FinishReason == "" && strings.HasPrefix(agent.Spec.Protocol, "openai") {
			resp.FinishReason = "stop"
		}
	}

	if tc.ParallelDisabled && len(resp.ToolCalls) > 1 {
		if mode == StrictEnforce {
			resp.ToolCalls = resp.ToolCalls[:1]
		} else {
			warn("parallel: %d tool calls emitted under a parallel-disabled request (strict caps to 1)", len(resp.ToolCalls))
		}
	}
	return warnings
}

// validateStrictFunctionSchema checks one strict:true function tool against
// the structured-outputs subset (R9-16b). The message mirrors the real
// API's "Invalid schema for function 'x': In context=(…), …" text.
func validateStrictFunctionSchema(sf StrictFunction) *StrictToolError {
	errs := toolschema.ValidateStrictSubset(sf.Parameters)
	if len(errs) == 0 {
		return nil
	}
	return &StrictToolError{
		Check: "schemas", Kind: "invalid_schema", Index: sf.Index, UnknownID: sf.Name,
		Message: fmt.Sprintf("Invalid schema for function '%s': %s", sf.Name, errs[0]),
	}
}

// firstToolName picks the synthesis target: the allowlist first, then the
// request's declared tools, then the agent's own tool definitions.
func firstToolName(allowedNames, requestToolNames []string, agent *types.AgentDefinition) string {
	if len(allowedNames) > 0 {
		return allowedNames[0]
	}
	if len(requestToolNames) > 0 {
		return requestToolNames[0]
	}
	if agent != nil && len(agent.Spec.Tools) > 0 {
		return agent.Spec.Tools[0].Name
	}
	return ""
}

// resolveStrictTools is the pure worker (unit-testable without env
// manipulation). A block present without a level implies "strict" — writing
// the block turns it on; the level fills every dimension the author left
// unset, and a boolean set to false excludes that dimension.
func resolveStrictTools(cfg *types.StrictToolsConfig, envLevel StrictMode) StrictToolModes {
	level := envLevel
	if cfg != nil {
		if cfg.Level != "" {
			level = ParseStrictLevel(cfg.Level)
		} else {
			level = StrictEnforce
		}
	}
	dim := func(enabled *bool) StrictMode {
		if enabled != nil && !*enabled {
			return StrictOff
		}
		return level
	}
	if cfg == nil {
		return StrictToolModes{IDs: level, ToolChoice: level, Schemas: level}
	}
	return StrictToolModes{
		IDs:        dim(cfg.IDs),
		ToolChoice: dim(cfg.ToolChoice),
		Schemas:    dim(cfg.Schemas),
	}
}
