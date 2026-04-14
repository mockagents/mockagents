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
	ctx, span := observability.StartSpan(ctx, "engine.process_request")
	defer span.End()

	// 1. Resolve agent by name or model.
	agent, err := e.resolveAgent(req)
	if err != nil {
		observability.RecordError(span, err)
		return nil, err
	}
	span.SetAttributes(
		attribute.String("agent.name", agent.Metadata.Name),
		attribute.String("agent.model", agent.Spec.Model),
		attribute.String("agent.protocol", agent.Spec.Protocol),
	)

	// 1a. Chaos injection runs before any work so 429s and injected errors
	// are cheap and latency is tacked on at the end.
	if e.Chaos != nil {
		if chaosErr := e.Chaos.Before(agent); chaosErr != nil {
			e.Logger.Info("chaos injected", "agent", agent.Metadata.Name, "error", chaosErr)
			observability.RecordError(span, chaosErr)
			return nil, chaosErr
		}
	}
	_ = ctx

	// 2. Extract latest user message.
	userMsg := latestUserMessage(req.Messages)
	if userMsg == "" {
		observability.RecordError(span, ErrEmptyMessage)
		return nil, ErrEmptyMessage
	}

	// 3. Get or create session.
	session := e.States.GetOrCreate(req.SessionID, agent.Metadata.Name)
	session.AppendUserMessage(userMsg)

	// 4. Match scenario.
	matchResult := e.Matcher.MatchWithCaptures(agent.Spec.Behavior.Scenarios, userMsg, session.TurnCount)

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
		"turn", session.TurnCount,
	)

	// 5. Generate response.
	tmplCtx := TemplateContext{
		Agent:      agent,
		Message:    userMsg,
		TurnNumber: session.TurnCount,
		SessionID:  session.ID,
		Vars:       session.Variables,
		Match:      captures,
	}
	resp, err := e.Generator.Generate(agent, scenario, tmplCtx)
	if err != nil {
		wrapped := fmt.Errorf("generating response for agent %q: %w", agent.Metadata.Name, err)
		observability.RecordError(span, wrapped)
		return nil, wrapped
	}
	span.SetAttributes(
		attribute.String("agent.scenario", scenario.Name),
		attribute.Int("agent.tool_calls", len(resp.ToolCalls)),
	)

	// 6. Process tool calls — resolve responses against tool definitions.
	if len(resp.ToolCalls) > 0 && len(agent.Spec.Tools) > 0 {
		results, err := e.ToolProcessor.ProcessToolCalls(resp.ToolCalls, agent.Spec.Tools)
		if err != nil {
			e.Logger.Warn("tool call processing error",
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

	// 7. Update session state.
	var toolCallMsgs []state.ToolCallMsg
	for _, tc := range resp.ToolCalls {
		toolCallMsgs = append(toolCallMsgs, state.ToolCallMsg{
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}
	session.AppendAssistantMessage(resp.Content, toolCallMsgs)
	e.States.Save(session)

	// 8. Inject latency now that the real work is done. Sleeping here keeps
	// behavior visible to clients without blocking error injection above.
	if e.Chaos != nil {
		e.Chaos.After(agent)
	}

	return resp, nil
}

// resolveAgent finds the agent by name first, then by model field.
func (e *Engine) resolveAgent(req *InboundRequest) (*types.AgentDefinition, error) {
	if req.AgentName != "" {
		if agent := e.Registry.Get(req.AgentName); agent != nil {
			return agent, nil
		}
	}
	if req.Model != "" {
		if agent := e.Registry.GetByModel(req.Model); agent != nil {
			return agent, nil
		}
	}
	// If only one agent is registered, use it as default.
	agents := e.Registry.List()
	if len(agents) == 1 {
		return agents[0], nil
	}
	identifier := req.AgentName
	if identifier == "" {
		identifier = req.Model
	}
	return nil, fmt.Errorf("%w: %q (available: %v)", ErrAgentNotFound, identifier, e.Registry.ListNames())
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
