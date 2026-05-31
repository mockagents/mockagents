package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
}

// RequestMessage is a single message from the client request.
type RequestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	return &Engine{
		Registry:      registry,
		States:        store,
		Matcher:       NewScenarioMatcher(),
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

	// 2. Extract latest user message.
	userMsg := latestUserMessage(req.Messages)
	if userMsg == "" {
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
		matchResult := e.Matcher.MatchWithCaptures(agent.Spec.Behavior.Scenarios, userMsg, turnCount)

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
	e.States.Save(session)

	// 8. Inject latency now that the real work is done. Sleeping here keeps
	// behavior visible to clients without blocking error injection above.
	// The sleep is cut short if the client cancels.
	if e.Chaos != nil {
		e.Chaos.After(ctx, agent)
	}

	return resp, nil
}

// resolveAgent finds the agent by name first, then by model field.
// Kept for callers that still use the no-tenant-context fast path
// (most legacy tests and the in-process Go SDK) — equivalent to
// resolveAgentForTenant("").
func (e *Engine) resolveAgent(req *InboundRequest) (*types.AgentDefinition, error) {
	return e.resolveAgentForTenant(req, "")
}

// resolveAgentForTenant is the tenant-aware variant. Empty tenantID
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

func latestUserMessage(messages []RequestMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
