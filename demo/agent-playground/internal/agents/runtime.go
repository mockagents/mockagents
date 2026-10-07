// Package agents is the playground's agent runtime: a single Invoke call
// turns "agent name + input" into a finished answer. Along the way it
//
//   - routes the call to the SLM or LLM tier (package router);
//   - calls the model with retries, exponential backoff and a per-attempt
//     timeout (package retry), then walks the agent's fallback chain;
//   - streams deltas, downgrading to non-streaming if a stream breaks;
//   - runs the tool loop: validates model-produced arguments, asks a human
//     before side-effecting tools, executes tools in parallel and feeds
//     results back, bounded by max_tool_turns;
//   - continues answers cut off by finish_reason "length";
//   - records every step in the run trace and in metrics.
package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/llm"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/metrics"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/retry"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/router"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/trace"
)

// Runtime executes agent calls.
type Runtime struct {
	Config    *config.Store
	Providers map[string]llm.Provider
	Tools     *tools.Registry
	Metrics   *metrics.Registry
	Logger    *slog.Logger
	// Sleep replaces the retry backoff timer (tests). Nil uses real time.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewProviders builds the three provider clients against one base URL. For
// mockagents every provider shares the server; against real APIs, pass
// per-provider URLs and keys.
func NewProviders(baseURL, apiKey string) map[string]llm.Provider {
	if apiKey == "" {
		// Real provider APIs, and mockagents faithfully, reject a request
		// with no credential (Anthropic answers 401 "missing API key"). The
		// mock accepts any non-empty key, so send a placeholder.
		apiKey = PlaceholderAPIKey
	}
	opts := llm.Options{BaseURL: baseURL, APIKey: apiKey}
	return map[string]llm.Provider{
		"openai":    llm.NewOpenAI(opts),
		"anthropic": llm.NewAnthropic(opts),
		"gemini":    llm.NewGemini(opts),
	}
}

// PlaceholderAPIKey is sent when no provider key is configured.
const PlaceholderAPIKey = "sk-playground-mock"

// Event is a live progress notification (streamed to SSE clients).
type Event struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// Event types.
const (
	EventDelta        = "delta"
	EventReset        = "reset"
	EventRetry        = "retry"
	EventFallback     = "fallback"
	EventToolCall     = "tool_call"
	EventToolResult   = "tool_result"
	EventApproval     = "approval_requested"
	EventContinuation = "continuation"
	EventRoute        = "route"
)

// ApprovalRequest asks a human to allow a side-effecting tool call.
type ApprovalRequest struct {
	Agent     string         `json:"agent"`
	Tool      string         `json:"tool"`
	CallID    string         `json:"call_id"`
	Arguments map[string]any `json:"arguments"`
}

// Approval is the human's answer.
type Approval struct {
	Approved bool   `json:"approved"`
	Comment  string `json:"comment,omitempty"`
	Reviewer string `json:"reviewer,omitempty"`
}

// Approver blocks until a human decides (or ctx ends).
type Approver func(ctx context.Context, req ApprovalRequest) (Approval, error)

// Options tune one Invoke call. Zero values mean "use the agent's config".
type Options struct {
	// Tier is a caller override: "", "auto", "slm" or "llm".
	Tier string
	// Decision forces a routing decision (used for escalation).
	Decision *router.Decision
	// Retry overrides the agent's retry policy.
	Retry *retry.Policy
	// AttemptTimeout overrides the per-attempt timeout.
	AttemptTimeout time.Duration
	// Stream overrides the agent's streaming preference.
	Stream *bool
	// OnEvent receives live progress events.
	OnEvent func(Event)
	// Approver gates side-effecting tools. Nil declines them (fail safe).
	Approver Approver
	// SessionID pins the X-Session-Id. Empty generates a fresh one.
	SessionID string
	// History is prior conversation, placed before the input.
	History []llm.Message
	// MaxToolTurns overrides defaults.max_tool_turns.
	MaxToolTurns int
	// MaxContinuations bounds finish_reason=length continuations (default 2).
	MaxContinuations int
}

// ToolExecution records one tool call.
type ToolExecution struct {
	CallID     string    `json:"call_id"`
	Tool       string    `json:"tool"`
	Arguments  string    `json:"arguments"`
	Result     any       `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	Outcome    string    `json:"outcome"` // ok | error | invalid_arguments | declined | unknown_tool
	Approval   *Approval `json:"approval,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

// ModelCall records one model in the fallback chain.
type ModelCall struct {
	Model    config.ModelRef `json:"model"`
	Fallback bool            `json:"fallback"`
	Attempts int             `json:"attempts"`
	Outcome  string          `json:"outcome"` // ok | failed
	Error    string          `json:"error,omitempty"`
}

// Result is a finished agent call.
type Result struct {
	Agent          string            `json:"agent"`
	Output         string            `json:"output"`
	Tier           string            `json:"tier"`
	Model          config.ModelRef   `json:"model"`
	RouteReason    string            `json:"route_reason"`
	FinishReason   string            `json:"finish_reason"`
	Refused        bool              `json:"refused,omitempty"`
	Attempts       int               `json:"attempts"`
	Retries        int               `json:"retries"`
	FallbackUsed   bool              `json:"fallback_used"`
	ModelCalls     []ModelCall       `json:"model_calls"`
	ToolExecutions []ToolExecution   `json:"tool_executions,omitempty"`
	Continuations  int               `json:"continuations,omitempty"`
	StreamFallback bool              `json:"stream_fallback,omitempty"`
	Turns          int               `json:"turns"`
	Usage          llm.Usage         `json:"usage"`
	CostUSD        float64           `json:"cost_usd"`
	LatencyMS      int64             `json:"latency_ms"`
	MockHeaders    map[string]string `json:"mock_headers,omitempty"`
	SessionID      string            `json:"session_id"`
}

// Errors.
var (
	ErrToolLoop = errors.New("tool loop exceeded max_tool_turns")
)

// CallError is an agent call that failed on every model in its chain.
type CallError struct {
	Agent  string
	Models []string
	Last   error
}

func (e *CallError) Error() string {
	return fmt.Sprintf("agent %s failed on %s: %v", e.Agent, strings.Join(e.Models, " -> "), e.Last)
}

func (e *CallError) Unwrap() error { return e.Last }

// Invoke runs agent name on input.
func (rt *Runtime) Invoke(ctx context.Context, name, input string, opts Options) (*Result, error) {
	cfg := rt.Config.Current()
	a, ok := cfg.Agent(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", config.ErrUnknownAgent, name)
	}
	start := time.Now()
	ctx, span := trace.Start(ctx, "agent "+name, trace.KindAgent, map[string]any{"agent": name, "role": a.Role})
	res, err := rt.invoke(ctx, cfg, a, input, opts, span)
	if res != nil {
		res.LatencyMS = time.Since(start).Milliseconds()
		span.Set("tier", res.Tier)
		span.Set("model", res.Model.String())
		span.Set("attempts", res.Attempts)
		span.Set("retries", res.Retries)
		span.Set("fallback_used", res.FallbackUsed)
		span.Set("cost_usd", res.CostUSD)
		span.Set("tokens_in", res.Usage.InputTokens)
		span.Set("tokens_out", res.Usage.OutputTokens)
		if len(res.MockHeaders) > 0 {
			span.Set("mock_headers", res.MockHeaders)
		}
	}
	span.End(err)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	tier := ""
	if res != nil {
		tier = res.Tier
	}
	rt.Metrics.Inc("playground_llm_calls_total", "agent", name, "tier", tier, "outcome", outcome)
	if err != nil {
		return res, err
	}
	return res, nil
}

func (rt *Runtime) invoke(ctx context.Context, cfg *config.Config, a config.Agent, input string, opts Options, span *trace.Handle) (*Result, error) {
	// 1. Route.
	var decision router.Decision
	if opts.Decision != nil {
		decision = *opts.Decision
	} else {
		d, err := router.Route(cfg, a, opts.Tier)
		if err != nil {
			return nil, err
		}
		decision = d
	}
	_, rspan := trace.Start(ctx, "route", trace.KindRouter, map[string]any{
		"tier": decision.Tier, "model": decision.Model.String(), "reason": decision.Reason,
	})
	rspan.End(nil)
	emit(opts.OnEvent, EventRoute, map[string]any{"agent": a.Name, "tier": decision.Tier, "model": decision.Model.String(), "reason": decision.Reason})

	policy := cfg.RetryFor(a)
	if opts.Retry != nil {
		policy = *opts.Retry
	}
	attemptTimeout := time.Duration(cfg.Defaults.AttemptTimeoutMS) * time.Millisecond
	if a.AttemptTimeoutMS > 0 {
		attemptTimeout = time.Duration(a.AttemptTimeoutMS) * time.Millisecond
	}
	if opts.AttemptTimeout > 0 {
		attemptTimeout = opts.AttemptTimeout
	}
	stream := a.Stream
	if opts.Stream != nil {
		stream = *opts.Stream
	}
	maxTurns := cfg.Defaults.MaxToolTurns
	if opts.MaxToolTurns > 0 {
		maxTurns = opts.MaxToolTurns
	}
	maxCont := 2
	if opts.MaxContinuations > 0 {
		maxCont = opts.MaxContinuations
	}
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = NewID("sess")
	}

	res := &Result{Agent: a.Name, Tier: decision.Tier, Model: decision.Model, RouteReason: decision.Reason, SessionID: sessionID}
	chain := append([]config.ModelRef{decision.Model}, a.Fallback...)
	cs := &chainState{models: chain}

	messages := append([]llm.Message(nil), opts.History...)
	messages = append(messages, llm.Message{Role: "user", Content: input})
	toolSpecs := rt.Tools.Specs(a.Tools)
	var output []string

	for turn := 1; ; turn++ {
		if turn > maxTurns+maxCont {
			return res, fmt.Errorf("%w (%d)", ErrToolLoop, maxTurns)
		}
		res.Turns = turn
		req := llm.Request{
			System: a.SystemPrompt, Messages: messages, Tools: toolSpecs,
			Temperature: a.Temperature, MaxTokens: a.MaxTokens, SessionID: sessionID,
		}
		resp, err := rt.complete(ctx, cfg, a, cs, req, policy, attemptTimeout, stream, opts.OnEvent, res)
		if err != nil {
			return res, err
		}
		res.Usage.InputTokens += resp.Usage.InputTokens
		res.Usage.OutputTokens += resp.Usage.OutputTokens
		res.CostUSD += cost(cfg, cs.current(), resp.Usage)
		if len(resp.Headers) > 0 {
			if res.MockHeaders == nil {
				res.MockHeaders = map[string]string{}
			}
			for k, v := range resp.Headers {
				res.MockHeaders[k] = v
			}
		}
		if resp.Refusal != "" {
			res.Refused = true
			output = append(output, resp.Refusal)
			res.FinishReason = "content_filter"
			break
		}
		if len(resp.ToolCalls) == 0 {
			output = append(output, strings.TrimSpace(resp.Content))
			res.FinishReason = resp.FinishReason
			if resp.FinishReason == "length" && res.Continuations < maxCont {
				// Semantic error: the answer was cut off. Ask the model to continue.
				res.Continuations++
				span.Event("continuation", map[string]any{"n": res.Continuations})
				emit(opts.OnEvent, EventContinuation, map[string]any{"n": res.Continuations})
				messages = append(messages,
					llm.Message{Role: "assistant", Content: resp.Content},
					llm.Message{Role: "user", Content: "Please continue from where you stopped."})
				continue
			}
			break
		}
		if turn > maxTurns {
			return res, fmt.Errorf("%w (%d)", ErrToolLoop, maxTurns)
		}
		// Tool turn.
		messages = append(messages, llm.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
		toolMsgs, execs, declined, err := rt.runTools(ctx, a, resp.ToolCalls, opts)
		res.ToolExecutions = append(res.ToolExecutions, execs...)
		if err != nil {
			return res, err
		}
		messages = append(messages, toolMsgs...)
		if len(declined) > 0 {
			messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf(
				"The %s action was declined by a human reviewer. Do not retry it; tell the customer what happens next.",
				strings.Join(declined, ", "))})
		}
	}
	res.Output = strings.TrimSpace(strings.Join(output, " "))
	res.Model = cs.current()
	res.FallbackUsed = cs.idx > 0
	return res, nil
}

// chainState tracks the position in the fallback chain. Once a fallback
// model has answered, the rest of the conversation stays on it.
type chainState struct {
	models []config.ModelRef
	idx    int
}

func (c *chainState) current() config.ModelRef { return c.models[c.idx] }

// complete calls the current model with retries and walks the fallback
// chain on failure.
func (rt *Runtime) complete(ctx context.Context, cfg *config.Config, a config.Agent, cs *chainState, req llm.Request,
	policy retry.Policy, attemptTimeout time.Duration, stream bool, onEvent func(Event), res *Result) (*llm.Response, error) {
	var lastErr error
	var tried []string
	for ; cs.idx < len(cs.models); cs.idx++ {
		m := cs.models[cs.idx]
		tried = append(tried, m.String())
		provider, ok := rt.Providers[m.Provider]
		if !ok {
			lastErr = fmt.Errorf("no provider %q configured", m.Provider)
			continue
		}
		isFallback := cs.idx > 0
		mctx, mspan := trace.Start(ctx, "llm "+m.String(), trace.KindLLM, map[string]any{
			"provider": m.Provider, "model": m.Model, "fallback": isFallback,
			"max_retries": policy.MaxRetries, "attempt_timeout_ms": attemptTimeout.Milliseconds(),
		})
		streamThis := stream
		var resp *llm.Response
		attempts, err := retry.Do(mctx, policy, retry.Options{
			Classify: llm.Classify,
			Sleep:    rt.Sleep,
			Observe: func(at retry.Attempt) {
				outcome := "ok"
				if at.Err != nil {
					outcome = at.Reason
				}
				rt.Metrics.Inc("playground_llm_attempts_total", "provider", m.Provider, "outcome", outcome)
				if at.NextDelay > 0 {
					res.Retries++
					rt.Metrics.Inc("playground_retries_total", "reason", at.Reason)
					mspan.Event("retry scheduled", map[string]any{
						"after_attempt": at.Number, "reason": at.Reason, "delay_ms": at.NextDelay.Milliseconds(), "error": errString(at.Err),
					})
					emit(onEvent, EventRetry, map[string]any{
						"agent": a.Name, "model": m.String(), "attempt": at.Number, "reason": at.Reason,
						"delay_ms": at.NextDelay.Milliseconds(), "error": errString(at.Err),
					})
				}
			},
		}, func(actx context.Context, n int) error {
			res.Attempts++
			actx, cancel := llm.WithAttemptTimeout(actx, attemptTimeout)
			defer cancel()
			_, aspan := trace.Start(actx, fmt.Sprintf("attempt %d", n), trace.KindAttempt, map[string]any{"stream": streamThis})
			r := req
			r.Model = m.Model
			r.Stream = streamThis
			if streamThis {
				r.OnDelta = func(t string) { emit(onEvent, EventDelta, map[string]any{"text": t}) }
			}
			out, err := provider.Complete(actx, &r)
			if err != nil {
				var apiErr *llm.APIError
				if errors.As(err, &apiErr) {
					aspan.Set("http_status", apiErr.Status)
					if apiErr.RetryAfter > 0 {
						aspan.Set("retry_after_ms", apiErr.RetryAfter.Milliseconds())
					}
					if len(apiErr.Headers) > 0 {
						aspan.Set("mock_headers", apiErr.Headers)
					}
				}
				var se *llm.StreamError
				if errors.As(err, &se) && streamThis {
					// A broken stream is retried without streaming: the
					// "stream downgrade" pattern.
					streamThis = false
					res.StreamFallback = true
					aspan.Event("stream downgrade", map[string]any{"partial_chars": len(se.Partial)})
					emit(onEvent, EventReset, map[string]any{"reason": se.Reason})
				}
				_, reason, _ := llm.Classify(err)
				aspan.Set("error_class", reason)
				aspan.End(err)
				return err
			}
			aspan.Set("finish_reason", out.FinishReason)
			aspan.Set("tool_calls", len(out.ToolCalls))
			aspan.Set("tokens_out", out.Usage.OutputTokens)
			aspan.End(nil)
			resp = out
			return nil
		})
		call := ModelCall{Model: m, Fallback: isFallback, Attempts: attempts, Outcome: "ok"}
		if err != nil {
			call.Outcome, call.Error = "failed", err.Error()
		}
		res.ModelCalls = append(res.ModelCalls, call)
		mspan.Set("attempts", attempts)
		mspan.End(err)
		if err == nil {
			if isFallback {
				rt.Metrics.Inc("playground_fallbacks_total", "agent", a.Name, "model", m.Model)
			}
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cs.idx+1 < len(cs.models) {
			next := cs.models[cs.idx+1]
			emit(onEvent, EventFallback, map[string]any{"agent": a.Name, "from": m.String(), "to": next.String(), "error": err.Error()})
		}
	}
	cs.idx = len(cs.models) - 1
	return nil, &CallError{Agent: a.Name, Models: tried, Last: lastErr}
}

// runTools validates and executes one turn's tool calls in parallel.
func (rt *Runtime) runTools(ctx context.Context, a config.Agent, calls []llm.ToolCall, opts Options) ([]llm.Message, []ToolExecution, []string, error) {
	msgs := make([]llm.Message, len(calls))
	execs := make([]ToolExecution, len(calls))
	errs := make([]error, len(calls))
	declinedFlags := make([]bool, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		if call.ID == "" {
			call.ID = NewID("call")
			calls[i] = call
		}
		wg.Add(1)
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			msgs[i], execs[i], declinedFlags[i], errs[i] = rt.runTool(ctx, a, call, opts)
		}(i, call)
	}
	wg.Wait()
	var declined []string
	for i, d := range declinedFlags {
		if d {
			declined = append(declined, calls[i].Name)
		}
	}
	return msgs, execs, declined, errors.Join(errs...)
}

func (rt *Runtime) runTool(ctx context.Context, a config.Agent, call llm.ToolCall, opts Options) (llm.Message, ToolExecution, bool, error) {
	start := time.Now()
	ex := ToolExecution{CallID: call.ID, Tool: call.Name, Arguments: call.Arguments}
	tctx, span := trace.Start(ctx, "tool "+call.Name, trace.KindTool, map[string]any{"call_id": call.ID, "arguments": call.Arguments})
	emit(opts.OnEvent, EventToolCall, map[string]any{"agent": a.Name, "tool": call.Name, "call_id": call.ID, "arguments": call.Arguments})
	declined := false
	finish := func(result any, isErr bool, outcome string, err error) (llm.Message, ToolExecution, bool, error) {
		ex.Outcome = outcome
		ex.DurationMS = time.Since(start).Milliseconds()
		if isErr {
			ex.Error = fmt.Sprint(result)
		} else {
			ex.Result = result
		}
		payload := result
		if isErr {
			payload = map[string]any{"error": outcome, "detail": fmt.Sprint(result)}
		}
		raw, _ := json.Marshal(payload)
		span.Set("outcome", outcome)
		var spanErr error
		if isErr && outcome != "declined" {
			spanErr = errors.New(fmt.Sprint(result))
		}
		span.End(spanErr)
		rt.Metrics.Inc("playground_tool_executions_total", "tool", call.Name, "outcome", outcome)
		emit(opts.OnEvent, EventToolResult, map[string]any{"tool": call.Name, "call_id": call.ID, "outcome": outcome})
		return llm.Message{Role: "tool", ToolCallID: call.ID, ToolName: call.Name, Content: string(raw), IsError: isErr}, ex, declined, err
	}

	tool, ok := rt.Tools.Get(call.Name)
	if !ok || !contains(a.Tools, call.Name) {
		return finish(fmt.Sprintf("tool %q is not available to agent %s", call.Name, a.Name), true, "unknown_tool", nil)
	}
	args, err := tools.ParseArgs(tool, call.Arguments)
	if err != nil {
		// Never execute on malformed arguments: report back so the model can fix them.
		return finish(err.Error(), true, "invalid_arguments", nil)
	}
	if tool.RequiresApproval() {
		var approval Approval
		if opts.Approver == nil {
			approval = Approval{Approved: false, Comment: "no approver configured; side-effecting tools are declined by default", Reviewer: "policy"}
		} else {
			emit(opts.OnEvent, EventApproval, map[string]any{"tool": call.Name, "call_id": call.ID, "arguments": args})
			span.Event("awaiting human approval", nil)
			approval, err = opts.Approver(tctx, ApprovalRequest{Agent: a.Name, Tool: call.Name, CallID: call.ID, Arguments: args})
			if err != nil {
				return finish(err.Error(), true, "error", err)
			}
		}
		ex.Approval = &approval
		span.Set("approved", approval.Approved)
		if !approval.Approved {
			declined = true
			return finish(fmt.Sprintf("declined by %s: %s", approval.Reviewer, approval.Comment), true, "declined", nil)
		}
	}
	ectx, cancel := context.WithTimeout(tctx, 10*time.Second)
	defer cancel()
	result, err := tool.Execute(ectx, args)
	if err != nil {
		return finish(err.Error(), true, "error", nil)
	}
	return finish(result, false, "ok", nil)
}

func cost(cfg *config.Config, m config.ModelRef, u llm.Usage) float64 {
	p, ok := cfg.Pricing[m.Model]
	if !ok {
		return 0
	}
	return float64(u.InputTokens)/1000*p.InputPer1K + float64(u.OutputTokens)/1000*p.OutputPer1K
}

func emit(fn func(Event), typ string, data map[string]any) {
	if fn != nil {
		fn(Event{Type: typ, Data: data})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// NewID returns a short random id with a prefix.
func NewID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}
