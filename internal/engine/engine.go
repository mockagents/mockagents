package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/observability"
	"github.com/mockagents/mockagents/internal/types"
	"go.opentelemetry.io/otel/attribute"
)

var (
	ErrAgentNotFound = errors.New("agent not found")
	ErrEmptyMessage  = errors.New("empty user message")
)

// InboundRequest represents a parsed incoming request to the mock engine.
type InboundRequest struct {
	AgentName string           `json:"agent_name,omitempty"`
	Model     string           `json:"model,omitempty"`
	SessionID string           `json:"session_id"`
	Messages  []RequestMessage `json:"messages"`
	Stream    bool             `json:"stream,omitempty"`
	// ToolChoice is the provider-neutral tool_choice contract, populated by
	// each adapter from its wire spelling (round-11 widened round-9's
	// none-only flag). None is always honored (R9-5); Required/Name/
	// AllowedNames/ParallelDisabled are enforced only under the strict-tools
	// knob (see StrictToolsFor).
	ToolChoice ToolChoice `json:"tool_choice,omitzero"`
}

// ToolChoice is the parsed tool_choice + parallel-call contract shared by
// every protocol: OpenAI "none"/"required"/named function (+
// parallel_tool_calls:false), Anthropic {type: none|any|tool} (+
// disable_parallel_tool_use), Gemini functionCallingConfig mode NONE/ANY (+
// allowedFunctionNames).
type ToolChoice struct {
	// None: the client explicitly forbade tool calls.
	None bool `json:"none,omitempty"`
	// Required: the model MUST call at least one tool (OpenAI "required",
	// Anthropic "any", Gemini ANY). Also set when Name forces a specific one.
	Required bool `json:"required,omitempty"`
	// Name forces one specific tool (OpenAI named function, Anthropic
	// {type:"tool"}, a single-entry allowedFunctionNames).
	Name string `json:"name,omitempty"`
	// AllowedNames limits which tools may be called (Gemini
	// allowedFunctionNames under mode ANY).
	AllowedNames []string `json:"allowed_names,omitempty"`
	// ParallelDisabled caps the response to at most one tool call
	// (parallel_tool_calls:false / disable_parallel_tool_use:true).
	ParallelDisabled bool `json:"parallel_disabled,omitempty"`
}

// RequestMessage is a single message from the client request.
type RequestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ImageCount is the number of image content parts the message carried
	// (A-05). It is carried out-of-band so the flattened Content text stays pure
	// (no markers leaking into regex matching, templates, or token counts).
	ImageCount int `json:"image_count,omitempty"`
	// IsToolResult marks a message that carries a tool result back to the
	// model (role:"tool", an Anthropic tool_result block, a Gemini
	// functionResponse part, a Responses function_call_output item). Carried
	// out-of-band so each adapter's matchable-text flattening stays untouched
	// — the tool-loop convergence guard keys on it (round-9).
	IsToolResult bool `json:"is_tool_result,omitempty"`
	// ToolCalls carries the tool calls an assistant message ECHOED BACK in
	// the request history — the fingerprint material the convergence guard
	// compares the newly generated calls against.
	ToolCalls []EchoedToolCall `json:"tool_calls,omitempty"`
	// ToolResultIDs are the tool-call ids this tool-result message references
	// (tool_call_id / tool_use_id / call_id; the function NAME on Gemini,
	// which has no ids). Strict-mode round-trip validation matches them
	// against prior EchoedToolCall.IDs (round-11 R9-15).
	ToolResultIDs []string `json:"tool_result_ids,omitempty"`
}

// EchoedToolCall is one tool call present in the request history (the client
// echoing what the model previously called). Arguments is the parsed object;
// RawArguments keeps the verbatim string for payloads that do not parse
// (scenario-planted malformed JSON must fingerprint verbatim).
type EchoedToolCall struct {
	// ID is the wire id the call was echoed under (call_/toolu_; the function
	// NAME on Gemini, which addresses calls by name). Strict-mode round-trip
	// validation matches tool results against these (round-11).
	ID           string         `json:"id,omitempty"`
	Name         string         `json:"name"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	RawArguments string         `json:"raw_arguments,omitempty"`
}

// EchoToolCall builds an EchoedToolCall from the wire's name + arguments
// string, parsing the arguments and keeping the raw form for unparseable
// payloads. Shared by every adapter that echoes assistant tool calls.
func EchoToolCall(name, rawArgs string) EchoedToolCall {
	e := EchoedToolCall{Name: name, RawArguments: rawArgs}
	var m map[string]any
	if rawArgs != "" && json.Unmarshal([]byte(rawArgs), &m) == nil {
		e.Arguments = m
	}
	return e
}

// Engine orchestrates the mock agent request processing pipeline.
type Engine struct {
	Registry      *AgentRegistry
	States        state.Store
	Matcher       *ScenarioMatcher
	Generator     *ResponseGenerator
	ToolProcessor *ToolCallProcessor
	Chaos         *ChaosInjector
	Logger        *slog.Logger
}

// NewEngine creates a new Engine with all required components.
func NewEngine(registry *AgentRegistry, store state.Store, logger *slog.Logger) *Engine {
	matcher := NewScenarioMatcher()
	matcher.log = logger // surface bad content_regex through the request logger (F-SM-001)
	return &Engine{
		Registry:      registry,
		States:        store,
		Matcher:       matcher,
		Generator:     NewResponseGenerator(),
		ToolProcessor: NewToolCallProcessor(),
		Chaos:         NewChaosInjector(),
		Logger:        logger,
	}
}

// ProcessRequest runs the full request processing pipeline:
// 1. Resolve agent
// 2. Get/create session
// 3. Match scenario
// 4. Generate response
// 5. Process tool calls
// 6. Update state
func (e *Engine) ProcessRequest(req *InboundRequest) (*Response, error) {
	return e.ProcessRequestContext(context.Background(), req)
}

// ProcessRequestContext is the context-aware variant of ProcessRequest.
// HTTP adapters thread the incoming request context through here so
// trace spans link to the server span emitted by HTTPMiddleware.
func (e *Engine) ProcessRequestContext(ctx context.Context, req *InboundRequest) (*Response, error) {
	// Cache the flag once per request so the hot path does not re-read
	// the package-level variable (cheap but still an indirection).
	// When tracing is disabled we skip every SetAttributes / RecordError
	// call site below — the attribute.KeyValue construction and the
	// varargs slice allocation are the measurable costs, not the
	// StartSpan call itself (which bottoms out in a noop provider).
	traceOn := observability.IsEnabled()

	ctx, span := observability.StartSpan(ctx, "engine.process_request")
	defer span.End()

	// 1. Resolve agent by name or model. The tenant id (when set on
	// the context by the HTTP layer) constrains lookups so a caller
	// from tenant A cannot accidentally invoke tenant B's agents.
	tenantID := TenantIDFromContext(ctx)
	agent, err := e.resolveAgentForTenant(req, tenantID)
	if err != nil {
		if traceOn {
			observability.RecordError(span, err)
		}
		return nil, err
	}
	if traceOn {
		span.SetAttributes(
			attribute.String("agent.name", agent.Metadata.Name),
			attribute.String("agent.model", agent.Spec.Model),
			attribute.String("agent.protocol", agent.Spec.Protocol),
		)
	}

	// Bail out cheaply if the client already cancelled or timed out before
	// we do any work (chaos sleeps, matching, generation).
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 1a. Chaos injection runs before any work so 429s and injected errors
	// are cheap and latency is tacked on at the end. Injected sleeps honor
	// ctx cancellation (see ChaosInjector.sleep).
	if e.Chaos != nil {
		if chaosErr := e.Chaos.Before(ctx, agent); chaosErr != nil {
			e.Logger.Info("chaos injected", "agent", agent.Metadata.Name, "error", chaosErr)
			if traceOn {
				observability.RecordError(span, chaosErr)
			}
			return nil, chaosErr
		}
	}

	// 2. Extract latest user message. An image-only turn (A-05) has empty text
	// but a non-zero image count, and a tool-RESULT turn may legitimately be
	// empty (a tool returning "" — round-9 R9-8; real APIs accept it): neither
	// must be rejected as an empty message. An empty tool result simply falls
	// through to the default scenario.
	userMsg := latestUserMessage(req.Messages)
	imageCount := latestUserImageCount(req.Messages)
	if userMsg == "" && imageCount == 0 && !latestUserIsToolResult(req.Messages) {
		if traceOn {
			observability.RecordError(span, ErrEmptyMessage)
		}
		return nil, ErrEmptyMessage
	}

	// 3. Get or create session. Session state is namespaced by tenant and
	// agent so neither two tenants (X-02) nor two agents (X-03) that
	// independently pick the same client session_id share conversation
	// history or variables. Anonymous / single-tenant callers share the
	// empty-tenant namespace, so single-tenant behavior is unchanged.
	session := e.States.GetOrCreate(scopedSessionKey(tenantID, agent.Metadata.Name, req.SessionID), agent.Metadata.Name)
	var resp *Response
	if err := session.ApplyTurn(userMsg, func(turnCount int, variables map[string]any) (string, []state.ToolCallMsg, error) {
		// 4. Match scenario.
		matchResult := e.Matcher.MatchWithCaptures(agent.Spec.Behavior.Scenarios, userMsg, turnCount, imageCount)

		var scenario *types.Scenario
		var captures map[string]string
		if matchResult != nil {
			scenario = matchResult.Scenario
			captures = matchResult.Captures
		} else {
			// Built-in fallback when no scenario matches and no default defined.
			e.Logger.Warn("no matching scenario, using built-in fallback",
				"agent", agent.Metadata.Name,
				"message", truncate(userMsg, 100),
			)
			scenario = &types.Scenario{
				Name: "_fallback",
				Response: types.ScenarioResponse{
					Content: fmt.Sprintf("Mock response from %s", agent.Metadata.Name),
				},
			}
		}

		e.Logger.Info("scenario matched",
			"agent", agent.Metadata.Name,
			"scenario", scenario.Name,
			"turn", turnCount,
		)

		// 5. Generate response.
		tmplCtx := TemplateContext{
			Agent:      agent,
			Message:    userMsg,
			TurnNumber: turnCount,
			// The logical client id, not the tenant-namespaced store key.
			SessionID: req.SessionID,
			Vars:      variables,
			Match:     captures,
		}
		generated, err := e.Generator.Generate(agent, scenario, tmplCtx)
		if err != nil {
			wrapped := fmt.Errorf("generating response for agent %q: %w", agent.Metadata.Name, err)
			if traceOn {
				observability.RecordError(span, wrapped)
			}
			return "", nil, wrapped
		}
		resp = generated

		// Tool-loop convergence (round-9): scenario matching keys on the last
		// USER message, so a request whose tail is a tool result would receive
		// the IDENTICAL tool calls again — a client agent loop (answer every
		// call) would never converge on any HTTP surface. An identical
		// re-issue directly after a tool result is consumed (the model "used"
		// the output — the reply becomes content-only); a DIFFERENT call, a
		// deliberate multi-step chain, still goes out.
		if len(resp.ToolCalls) > 0 && tailIsToolResult(req.Messages) &&
			sameToolCalls(resp.ToolCalls, lastEchoedToolCalls(req.Messages)) {
			e.Logger.Info("tool-loop convergence: identical re-issue after a tool result consumed",
				"agent", agent.Metadata.Name, "scenario", scenario.Name)
			resp.ToolCalls = nil
		}

		// tool_choice "none" (round-9 R9-5): the client explicitly forbade
		// tool calls for this request — real APIs honor it strictly.
		if req.ToolChoice.None && len(resp.ToolCalls) > 0 {
			e.Logger.Info("tool_choice none: scenario tool calls suppressed",
				"agent", agent.Metadata.Name, "scenario", scenario.Name)
			resp.ToolCalls = nil
		}

		if traceOn {
			span.SetAttributes(
				attribute.String("agent.scenario", scenario.Name),
				attribute.Int("agent.tool_calls", len(resp.ToolCalls)),
			)
		}

		// 6. Process tool calls — resolve responses against tool definitions.
		// Best-effort by design: every result carries its own IsError/Error,
		// so an unresolved or failed call surfaces to the client as an error
		// *result* rather than failing the whole turn (mirroring how a real
		// API returns tool calls whose execution failed). We log so the two
		// silent-failure modes the old guard hid stay visible.
		if len(resp.ToolCalls) > 0 {
			if len(agent.Spec.Tools) == 0 {
				// Misconfiguration: the scenario emitted tool calls but the
				// agent defines no tools, so every call resolves to an error
				// result below instead of being silently dropped.
				e.Logger.Warn("scenario emitted tool calls but agent declares no tools",
					"agent", agent.Metadata.Name,
					"tool_calls", len(resp.ToolCalls),
				)
			}
			results, err := e.ToolProcessor.ProcessToolCalls(resp.ToolCalls, agent.Spec.Tools)
			if err != nil {
				e.Logger.Warn("tool call processing returned errors (surfaced as error results)",
					"agent", agent.Metadata.Name,
					"error", err,
				)
			}
			resp.ToolResults = results

			e.Logger.Info("tool calls processed",
				"agent", agent.Metadata.Name,
				"count", len(results),
			)
		}

		toolCallMsgs := make([]state.ToolCallMsg, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			toolCallMsgs = append(toolCallMsgs, state.ToolCallMsg{
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		return resp.Content, toolCallMsgs, nil
	}); err != nil {
		return nil, err
	}
	// No explicit save (F-ST-004/X-06): `session` is the store's own pointer
	// and ApplyTurn mutated it under the session lock, so the changes are
	// already persisted. The previous States.Save here was redundant and
	// could resurrect a session that Cleanup/Get concurrently evicted.

	// 8. Inject latency now that the real work is done. Sleeping here keeps
	// behavior visible to clients without blocking error injection above.
	// The sleep is cut short if the client cancels.
	if e.Chaos != nil {
		e.Chaos.After(ctx, agent)
	}

	return resp, nil
}

// resolveAgentForTenant finds the agent by name first, then by model
// field. Empty tenantID
// reproduces the v0.1 behavior (global agents only); a non-empty
// tenantID returns global ∪ that tenant's own agents.
func (e *Engine) resolveAgentForTenant(req *InboundRequest, tenantID string) (*types.AgentDefinition, error) {
	if req.AgentName != "" {
		if agent := e.Registry.GetForTenant(req.AgentName, tenantID); agent != nil {
			return agent, nil
		}
	}
	if req.Model != "" {
		if agent := e.Registry.GetByModelForTenant(req.Model, tenantID); agent != nil {
			return agent, nil
		}
	}
	// If only one agent is visible to this caller, use it as the
	// default. Tenant-bound callers skip this fallback so "the
	// tenant happens to own one agent" never becomes an implicit
	// default — that would let a misrouted request silently land on
	// the wrong agent. Anonymous callers retain v0.1 semantics so
	// single-agent demos keep working without specifying a model.
	if tenantID == "" {
		agents := e.Registry.ListForTenant("")
		if len(agents) == 1 {
			return agents[0], nil
		}
	}
	identifier := req.AgentName
	if identifier == "" {
		identifier = req.Model
	}
	return nil, fmt.Errorf("%w: %q (available: %v)",
		ErrAgentNotFound, identifier, e.Registry.ListNamesForTenant(tenantID))
}

// scopedSessionKey namespaces a client-supplied session id by tenant and
// agent so session state cannot leak across tenants (review finding X-02)
// or across agents that happen to reuse the same session_id (X-03). The
// separator is NUL: tenant ids are server-generated and never contain
// NUL, so the first NUL delimits the tenant exactly and no client-chosen
// agent name or session_id can forge another tenant's namespace. An empty
// tenantID (anonymous / single-tenant mode) keeps single-tenant behavior
// unchanged.
func scopedSessionKey(tenantID, agentName, sessionID string) string {
	return tenantID + "\x00" + agentName + "\x00" + sessionID
}

// latestUserIsToolResult reports whether the latest user-role message carries
// a tool result (Anthropic tool_result / Gemini functionResponse turns
// flatten into user messages) — those may legitimately be empty (R9-8).
func latestUserIsToolResult(messages []RequestMessage) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].IsToolResult
		}
	}
	return false
}

// tailIsToolResult reports whether the request's last non-system message
// carries a tool result — the trigger condition for the convergence guard.
func tailIsToolResult(messages []RequestMessage) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "system" {
			continue
		}
		return messages[i].IsToolResult
	}
	return false
}

// lastEchoedToolCalls returns the most recent assistant-echoed tool calls in
// the request history (fingerprint material for the convergence guard).
func lastEchoedToolCalls(messages []RequestMessage) []EchoedToolCall {
	for i := len(messages) - 1; i >= 0; i-- {
		if len(messages[i].ToolCalls) > 0 {
			return messages[i].ToolCalls
		}
	}
	return nil
}

// sameToolCalls compares generated tool calls against the client's echo.
// Names must match in order; arguments compare PARSED and JSON-normalized
// (one side is a client echo — whitespace/key order/number typing must not
// defeat the comparison), except scenario-planted RawArguments (malformed by
// design), which compare verbatim.
func sameToolCalls(gen []types.ToolCallSpec, echoed []EchoedToolCall) bool {
	if len(echoed) == 0 || len(gen) != len(echoed) {
		return false
	}
	for i, g := range gen {
		e := echoed[i]
		if g.Name != e.Name {
			return false
		}
		if g.RawArguments != "" {
			var m map[string]any
			if json.Unmarshal([]byte(g.RawArguments), &m) != nil {
				// Unparseable by design: verbatim comparison only.
				if g.RawArguments != e.RawArguments {
					return false
				}
				continue
			}
			if !equalArgs(m, e.Arguments) {
				return false
			}
			continue
		}
		if !equalArgs(g.Arguments, e.Arguments) {
			return false
		}
	}
	return true
}

// equalArgs compares two argument maps after a JSON round-trip normalization
// (YAML-decoded scenario args carry int values where a JSON echo carries
// float64 — reflect.DeepEqual alone would never match).
func equalArgs(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(normalizeArgs(a), normalizeArgs(b))
}

func normalizeArgs(m map[string]any) map[string]any {
	b, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return m
	}
	return out
}

func latestUserMessage(messages []RequestMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// latestUserImageCount returns the image-part count of the latest user message
// (A-05), so the matcher can evaluate a has_image rule against the same turn the
// matched text comes from.
func latestUserImageCount(messages []RequestMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].ImageCount
		}
	}
	return 0
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
