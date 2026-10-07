package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/guard"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/router"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/trace"
)

// Exec is the API a workflow definition uses to do work inside a run. Every
// helper records into the run (steps, stats, events, review items) and the
// trace, so workflows stay short and declarative.
type Exec struct {
	e        *Engine
	runID    string
	workflow string
	input    map[string]any
	opts     RunOptions
	onEvent  func(agents.Event)
	trace    *trace.Trace

	mu       sync.Mutex
	stepSeq  int
	lastName string
}

// RunID returns the run id.
func (x *Exec) RunID() string { return x.runID }

// Input returns the launch input.
func (x *Exec) Input() map[string]any { return x.input }

// String reads a string input field.
func (x *Exec) String(key, def string) string {
	if s, ok := x.input[key].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return def
}

// Bool reads a boolean input field.
func (x *Exec) Bool(key string) bool {
	b, _ := x.input[key].(bool)
	return b
}

// Config returns the live configuration (read-only).
func (x *Exec) Config() *config.Config { return x.e.Config.Current() }

// Engine exposes the engine (for Arm and metrics).
func (x *Exec) Engine() *Engine { return x.e }

func (x *Exec) setPhase(phase string) {
	x.e.Store.update(x.runID, func(r *Run) bool {
		if r.Phase == phase {
			return false
		}
		r.Phase = phase
		return true
	})
}

func (x *Exec) lastStep() string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.lastName
}

// Event appends to the run's live event log and forwards to any SSE client.
func (x *Exec) Event(typ string, data map[string]any) {
	x.e.Store.update(x.runID, func(r *Run) bool {
		r.Events = append(r.Events, RunEvent{Time: time.Now(), Type: typ, Data: maps.Clone(data)})
		if len(r.Events) > maxEvents {
			r.Events = r.Events[len(r.Events)-maxEvents:]
		}
		return true
	})
	if x.onEvent != nil {
		x.onEvent(agents.Event{Type: typ, Data: data})
	}
}

// StepHandle lets a step body annotate its step record.
type StepHandle struct {
	x  *Exec
	id string
}

func (h *StepHandle) mutate(fn func(s *Step)) {
	h.x.e.Store.update(h.x.runID, func(r *Run) bool {
		for _, s := range r.Steps {
			if s.ID == h.id {
				fn(s)
				return true
			}
		}
		return false
	})
}

// Note appends a human-readable note to the step.
func (h *StepHandle) Note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	h.mutate(func(s *Step) { s.Notes = append(s.Notes, msg) })
}

// Agent records agent-call details on the step.
func (h *StepHandle) Agent(res *agents.Result) {
	if res == nil {
		return
	}
	h.mutate(func(s *Step) {
		s.Agent = res.Agent
		s.Tier = res.Tier
		s.Model = res.Model.String()
		s.Attempts += res.Attempts
		s.Retries += res.Retries
		s.Fallback = s.Fallback || res.FallbackUsed
		s.CostUSD += res.CostUSD
	})
}

// Escalated marks the step as escalated to the LLM tier.
func (h *StepHandle) Escalated() { h.mutate(func(s *Step) { s.Escalated = true }) }

// Step runs fn as a named step. The returned value becomes the step output.
func (x *Exec) Step(ctx context.Context, name, kind string, fn func(ctx context.Context, h *StepHandle) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	x.mu.Lock()
	x.stepSeq++
	id := fmt.Sprintf("step_%02d", x.stepSeq)
	x.lastName = name
	x.mu.Unlock()
	start := time.Now()
	x.e.Store.update(x.runID, func(r *Run) bool {
		r.Steps = append(r.Steps, &Step{ID: id, Name: name, Kind: kind, Status: StepRunning, StartedAt: start})
		r.CurrentStep = name
		if !r.Waiting() {
			r.Phase = PhaseRunning
		}
		return true
	})
	sctx, span := trace.Start(ctx, "step "+name, trace.KindStep, map[string]any{"step": name, "kind": kind})
	h := &StepHandle{x: x, id: id}
	out, err := fn(sctx, h)
	span.End(err)
	end := time.Now()
	h.mutate(func(s *Step) {
		s.EndedAt = &end
		s.DurationMS = end.Sub(start).Milliseconds()
		s.Output = out
		if err != nil {
			s.Status = StepFailed
			s.Error = err.Error()
		} else {
			s.Status = StepCompleted
		}
	})
	return out, err
}

// Skip records a step that was deliberately not run (e.g. consensus
// short-circuit), so the run shows why an agent did not appear.
func (x *Exec) Skip(name, kind, reason string) {
	x.mu.Lock()
	x.stepSeq++
	id := fmt.Sprintf("step_%02d", x.stepSeq)
	x.mu.Unlock()
	now := time.Now()
	x.e.Store.update(x.runID, func(r *Run) bool {
		r.Steps = append(r.Steps, &Step{ID: id, Name: name, Kind: kind, Status: StepSkipped, StartedAt: now, EndedAt: &now, Notes: []string{reason}})
		return true
	})
}

// InvokeOptions tune Exec.Invoke.
type InvokeOptions struct {
	agents.Options
	// AuditTitle labels the non-blocking audit review item ("" uses a default).
	AuditTitle string
	// NoAudit skips the audit item (used for outputs that immediately pass
	// through a blocking gate, which records them already).
	NoAudit bool
}

// Invoke calls an agent with run-level overrides applied, updates run stats,
// and records the output as a non-blocking audit review item, so every
// AI-generated output is reviewable.
func (x *Exec) Invoke(ctx context.Context, h *StepHandle, agent, input string, o InvokeOptions) (*agents.Result, error) {
	opts := o.Options
	if opts.Retry == nil && x.opts.Retry != nil {
		r := *x.opts.Retry
		opts.Retry = &r
	}
	if opts.Tier == "" && opts.Decision == nil {
		if t, ok := x.opts.Tiers[agent]; ok {
			opts.Tier = t
		}
	}
	if opts.Stream == nil && x.opts.Stream {
		t := true
		opts.Stream = &t
	}
	if opts.Approver == nil {
		opts.Approver = x.approveTool
	}
	if opts.SessionID == "" {
		// Prefixing the X-Session-Id with the run id lets the mockagents
		// interaction log be filtered per run (GET /api/v1/logs?session_prefix=<run_id>).
		opts.SessionID = x.runID + "." + agent + "." + agents.NewID("s")
	}
	userEvent := opts.OnEvent
	opts.OnEvent = func(ev agents.Event) {
		if ev.Type != agents.EventDelta {
			data := maps.Clone(ev.Data)
			if data == nil {
				data = map[string]any{}
			}
			data["agent"] = agent
			x.Event(ev.Type, data)
		} else if x.onEvent != nil {
			x.onEvent(ev)
		}
		if userEvent != nil {
			userEvent(ev)
		}
	}
	res, err := x.e.Runtime.Invoke(ctx, agent, input, opts)
	if h != nil {
		h.Agent(res)
	}
	if res != nil {
		x.e.Store.update(x.runID, func(r *Run) bool {
			r.Stats.LLMCalls++
			r.Stats.Attempts += res.Attempts
			r.Stats.Retries += res.Retries
			if res.FallbackUsed {
				r.Stats.Fallbacks++
			}
			r.Stats.ToolCalls += len(res.ToolExecutions)
			r.Stats.TokensIn += res.Usage.InputTokens
			r.Stats.TokensOut += res.Usage.OutputTokens
			r.Stats.CostUSD += res.CostUSD
			if res.Tier == config.TierSLM {
				r.Stats.SLMCalls++
			} else {
				r.Stats.LLMTierCall++
			}
			return true
		})
		x.e.Metrics.Add("playground_cost_usd_total", res.CostUSD, "agent", agent, "tier", res.Tier)
		x.e.Metrics.Add("playground_tokens_total", float64(res.Usage.InputTokens), "direction", "input", "tier", res.Tier)
		x.e.Metrics.Add("playground_tokens_total", float64(res.Usage.OutputTokens), "direction", "output", "tier", res.Tier)
	}
	if err != nil {
		return res, err
	}
	if !o.NoAudit {
		title := o.AuditTitle
		if title == "" {
			title = "Output of " + agent
		}
		x.audit(title, res)
	}
	return res, nil
}

// Escalate re-runs an agent on the LLM tier after an SLM result was not good
// enough (low confidence or a failed guard).
func (x *Exec) Escalate(ctx context.Context, h *StepHandle, agent, input, why string, o InvokeOptions) (*agents.Result, error) {
	a, ok := x.Config().Agent(agent)
	if !ok {
		return nil, fmt.Errorf("%w: %q", config.ErrUnknownAgent, agent)
	}
	d, ok := router.Escalate(a, why)
	if !ok {
		return nil, fmt.Errorf("agent %s has no LLM tier to escalate to", agent)
	}
	x.e.Metrics.Inc("playground_escalations_total", "agent", agent)
	x.e.Store.update(x.runID, func(r *Run) bool { r.Stats.Escalations++; return true })
	x.Event("escalation", map[string]any{"agent": agent, "to": d.Model.String(), "why": why})
	if h != nil {
		h.Escalated()
		h.Note("escalated to %s: %s", d.Model.String(), why)
	}
	o.Decision = &d
	return x.Invoke(ctx, h, agent, input, o)
}

func (x *Exec) audit(title string, res *agents.Result) {
	ctx := map[string]any{
		"tier": res.Tier, "model": res.Model.String(), "attempts": res.Attempts,
		"fallback_used": res.FallbackUsed, "cost_usd": res.CostUSD,
	}
	if len(res.MockHeaders) > 0 {
		ctx["mock_headers"] = res.MockHeaders
	}
	if len(res.ToolExecutions) > 0 {
		ctx["tool_executions"] = res.ToolExecutions
	}
	it := x.e.Reviews.Create(ReviewItem{
		RunID: x.runID, Workflow: x.workflow, Kind: ReviewAudit, Blocking: false,
		Title: title, Agent: res.Agent, Content: res.Output, Context: ctx,
		AllowedActions: []string{ActionApprove, ActionFlag},
	})
	x.e.Store.update(x.runID, func(r *Run) bool { r.ReviewIDs = append(r.ReviewIDs, it.ID); return true })
}

func (x *Exec) reviewSettings() (mode string, timeout time.Duration, onTimeout string, maxRevisions int) {
	rv := x.Config().Defaults.Review
	mode, onTimeout, maxRevisions = rv.Mode, rv.OnTimeout, rv.MaxRevisions
	timeout = time.Duration(rv.TimeoutMS) * time.Millisecond
	if x.opts.ReviewMode != "" {
		mode = x.opts.ReviewMode
	}
	if x.opts.ReviewTimeoutMS > 0 {
		timeout = time.Duration(x.opts.ReviewTimeoutMS) * time.Millisecond
	}
	return
}

// waitDecision blocks on a blocking review item, bounded by the review
// timeout and the run deadline, and applies the on_timeout policy.
func (x *Exec) waitDecision(ctx context.Context, it ReviewItem, phase string) (Decision, error) {
	_, timeout, onTimeout, _ := x.reviewSettings()
	if dl, ok := ctx.Deadline(); ok {
		// Never wait past the run deadline: leave a second for the
		// workflow to record the outcome.
		if remain := time.Until(dl) - time.Second; remain < timeout {
			timeout = max(remain, 0)
		}
	}
	x.setPhase(phase)
	x.Event("review_requested", map[string]any{"review_id": it.ID, "kind": it.Kind, "title": it.Title, "timeout_ms": timeout.Milliseconds()})
	_, span := trace.Start(ctx, "review "+it.Title, trace.KindReview, map[string]any{"review_id": it.ID, "kind": it.Kind, "timeout_ms": timeout.Milliseconds()})
	d, err := x.e.Reviews.Wait(ctx, it.ID, timeout)
	if errors.Is(err, ErrReviewTimeout) {
		if onTimeout == "approve" {
			d, err = decisionOf(x.e.Reviews.Decide(it.ID, Decision{Action: ActionApprove, Reviewer: "timeout-policy", Comment: "no decision before review timeout; on_timeout=approve", Auto: true}))
		} else {
			x.e.Reviews.Expire(it.ID, "no decision before the review timeout")
			err = &RunError{Code: CodeReviewTimeout, Message: fmt.Sprintf("review %s (%s) received no decision within %s", it.ID, it.Title, timeout)}
		}
	}
	if err == nil {
		span.Set("action", d.Action)
		span.Set("reviewer", d.Reviewer)
		x.e.Metrics.Inc("playground_reviews_total", "kind", it.Kind, "action", d.Action)
		x.Event("review_decided", map[string]any{"review_id": it.ID, "action": d.Action, "reviewer": d.Reviewer})
	}
	span.End(err)
	x.setPhase(PhaseRunning)
	return d, err
}

// helper: Decide returns (ReviewItem, error); adapt to Decision.
func decisionOf(it ReviewItem, err error) (Decision, error) {
	if err != nil {
		return Decision{}, err
	}
	if it.Decision == nil {
		return Decision{}, ErrReviewNotPending
	}
	return *it.Decision, nil
}

// Gate is a blocking human-review checkpoint on an AI output.
func (x *Exec) Gate(ctx context.Context, title, agent, content string, details map[string]any, allowRevise bool) (Decision, ReviewItem, error) {
	allowed := []string{ActionApprove, ActionReject}
	if allowRevise {
		allowed = append(allowed, ActionRevise)
	}
	_, timeout, _, _ := x.reviewSettings()
	deadline := time.Now().Add(timeout)
	it := x.e.Reviews.Create(ReviewItem{
		RunID: x.runID, Workflow: x.workflow, Kind: ReviewGate, Blocking: true,
		Title: title, Agent: agent, Content: content, Context: details,
		AllowedActions: allowed, Deadline: &deadline,
	})
	x.e.Store.update(x.runID, func(r *Run) bool { r.ReviewIDs = append(r.ReviewIDs, it.ID); return true })
	d, err := x.waitDecisionAdapter(ctx, it, PhaseAwaitingReview)
	return d, it, err
}

func (x *Exec) waitDecisionAdapter(ctx context.Context, it ReviewItem, phase string) (Decision, error) {
	mode, _, _, _ := x.reviewSettings()
	if mode == config.ReviewAuto {
		return decisionOf(x.e.Reviews.Decide(it.ID, Decision{Action: ActionApprove, Reviewer: "auto-policy", Comment: "review_mode=auto", Auto: true}))
	}
	return x.waitDecision(ctx, it, phase)
}

// approveTool is the agents.Approver for side-effecting tools.
func (x *Exec) approveTool(ctx context.Context, req agents.ApprovalRequest) (agents.Approval, error) {
	_, timeout, _, _ := x.reviewSettings()
	deadline := time.Now().Add(timeout)
	it := x.e.Reviews.Create(ReviewItem{
		RunID: x.runID, Workflow: x.workflow, Kind: ReviewToolApproval, Blocking: true,
		Title:   fmt.Sprintf("Approve %s requested by %s", req.Tool, req.Agent),
		Agent:   req.Agent,
		Content: fmt.Sprintf("%s(%s)", req.Tool, compactJSON(req.Arguments)),
		Context: map[string]any{"tool": req.Tool, "call_id": req.CallID, "arguments": req.Arguments},
		// Revise makes no sense for a tool call: approve or reject.
		AllowedActions: []string{ActionApprove, ActionReject},
		Deadline:       &deadline,
	})
	x.e.Store.update(x.runID, func(r *Run) bool { r.ReviewIDs = append(r.ReviewIDs, it.ID); return true })
	d, err := x.waitDecisionAdapter(ctx, it, PhaseAwaitingApproval)
	if err != nil {
		var re *RunError
		if errors.As(err, &re) && re.Code == CodeReviewTimeout {
			// An unanswered tool approval declines the tool (fail safe) but
			// lets the agent finish its answer.
			return agents.Approval{Approved: false, Comment: "approval timed out", Reviewer: "timeout-policy"}, nil
		}
		return agents.Approval{}, err
	}
	return agents.Approval{Approved: d.Action == ActionApprove, Comment: d.Comment, Reviewer: d.Reviewer}, nil
}

// ReviewLoop puts a draft through a blocking gate. The reviewer can approve
// (done), reject (the run fails with review_rejected) or ask for a revision.
// A revision sends the draft and feedback to the editor agent and gates the
// result again, up to max_revisions times.
func (x *Exec) ReviewLoop(ctx context.Context, title, agent, draft string, details map[string]any) (string, []Decision, error) {
	_, _, _, maxRev := x.reviewSettings()
	var history []Decision
	for rev := 0; ; rev++ {
		var d Decision
		_, err := x.Step(ctx, fmt.Sprintf("human review: %s", title), "review", func(ctx context.Context, h *StepHandle) (any, error) {
			var it ReviewItem
			var err error
			d, it, err = x.Gate(ctx, title, agent, draft, details, rev < maxRev)
			if err != nil {
				return nil, err
			}
			h.Note("%s by %s (review %s)", d.Action, d.Reviewer, it.ID)
			return map[string]any{"review_id": it.ID, "action": d.Action, "comment": d.Comment, "reviewer": d.Reviewer}, nil
		})
		if err != nil {
			return draft, history, err
		}
		history = append(history, d)
		switch d.Action {
		case ActionApprove:
			return draft, history, nil
		case ActionReject:
			return draft, history, &RunError{Code: CodeReviewRejected, Message: fmt.Sprintf("reviewer %s rejected %q: %s", d.Reviewer, title, d.Comment)}
		case ActionRevise:
			feedback := guard.SanitizeLine(d.Comment, 500)
			if feedback == "" {
				feedback = "improve clarity"
			}
			out, err := x.Step(ctx, fmt.Sprintf("revise #%d", rev+1), "agent", func(ctx context.Context, h *StepHandle) (any, error) {
				res, err := x.Invoke(ctx, h, "editor", "Task: revise\nReviewer feedback: "+feedback+"\nDraft:\n"+draft, InvokeOptions{AuditTitle: "Revision of " + title})
				if err != nil {
					return nil, err
				}
				return res.Output, nil
			})
			if err != nil {
				return draft, history, err
			}
			draft, _ = out.(string)
		}
	}
}

// Parallel runs fns concurrently and returns the first error (all are
// waited for; each should honour ctx).
func Parallel(ctx context.Context, fns ...func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func(i int, fn func(context.Context) error) {
			defer wg.Done()
			errs[i] = fn(ctx)
			if errs[i] != nil {
				cancel()
			}
		}(i, fn)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return errors.Join(errs...)
}

func compactJSON(v any) string {
	parts := []string{}
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		}
	}
	return strings.Join(parts, ", ")
}
