package workflows

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/retry"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

// ResilienceDrill injects one fault per case through mockagents chaos and
// semantic-error fixtures, then checks that the agent runtime recovered the
// way it should. The drill itself always terminates: completed when every
// case behaved as expected, failed (drill_failed) otherwise.
type ResilienceDrill struct{}

// DrillCase is one fault scenario.
type DrillCase struct {
	Name     string `json:"name"`
	Agent    string `json:"agent"`
	Fault    string `json:"fault"`
	Pattern  string `json:"pattern"`
	Expected string `json:"expected"`
	// stateful cases need their mockagents counters re-armed first.
	mockAgent string
	stateful  bool
	expectErr bool
	check     func(res *agents.Result, err error, armed bool) (bool, string)
}

// DrillRow is one case's observed outcome.
type DrillRow struct {
	Case          string `json:"case"`
	Fault         string `json:"fault"`
	Pattern       string `json:"pattern"`
	Expected      string `json:"expected"`
	Observed      string `json:"observed"`
	Passed        bool   `json:"passed"`
	Attempts      int    `json:"attempts"`
	Retries       int    `json:"retries"`
	Fallback      bool   `json:"fallback_used"`
	Model         string `json:"model,omitempty"`
	LatencyMS     int64  `json:"latency_ms"`
	Output        string `json:"output,omitempty"`
	Error         string `json:"error,omitempty"`
	FailedCleanly bool   `json:"failed_cleanly,omitempty"`
}

func drillCases() []DrillCase {
	return []DrillCase{
		{Name: "flaky", Agent: "drill-flaky", mockAgent: "pg-drill-flaky", stateful: true,
			Fault: "503 on the first 2 requests", Pattern: "retry with exponential backoff + jitter",
			Expected: "succeeds on the primary model after retries, no fallback",
			check: func(r *agents.Result, err error, armed bool) (bool, string) {
				if err != nil {
					return false, "failed: " + err.Error()
				}
				if armed && r.Retries < 2 {
					return false, fmt.Sprintf("expected >= 2 retries, saw %d", r.Retries)
				}
				return !r.FallbackUsed, fmt.Sprintf("recovered after %d retries", r.Retries)
			}},
		{Name: "rate-limited", Agent: "drill-ratelimited", mockAgent: "pg-drill-ratelimited",
			Fault: "429 + Retry-After on every request", Pattern: "honour Retry-After, then fall back",
			Expected: "retries exhausted, answered by the fallback model",
			check: func(r *agents.Result, err error, _ bool) (bool, string) {
				if err != nil {
					return false, "failed: " + err.Error()
				}
				return r.FallbackUsed && r.Retries >= 1, fmt.Sprintf("%d retries honouring Retry-After, then fallback to %s", r.Retries, r.Model)
			}},
		{Name: "timeout", Agent: "drill-slow", mockAgent: "pg-drill-slow",
			Fault: "2.5 s latency vs 800 ms per-attempt timeout", Pattern: "per-attempt timeout + fallback",
			Expected: "attempts time out, answered by the fallback model",
			check: func(r *agents.Result, err error, _ bool) (bool, string) {
				if err != nil {
					return false, "failed: " + err.Error()
				}
				return r.FallbackUsed, fmt.Sprintf("%d attempt(s) timed out, fallback to %s", r.Attempts-1, r.Model)
			}},
		{Name: "connection-reset", Agent: "drill-connreset", mockAgent: "pg-drill-connreset", stateful: true,
			Fault: "TCP reset on the first request", Pattern: "transport errors are retryable",
			Expected: "succeeds after a retry",
			check: func(r *agents.Result, err error, armed bool) (bool, string) {
				if err != nil {
					return false, "failed: " + err.Error()
				}
				if armed && r.Retries < 1 {
					return false, "expected a retry after the reset"
				}
				return true, fmt.Sprintf("recovered after %d retry", r.Retries)
			}},
		{Name: "truncated", Agent: "drill-truncated", mockAgent: "pg-drill-truncated",
			Fault: "finish_reason=length (cut-off answer)", Pattern: "continuation request",
			Expected: "answer completed by a continuation",
			check: func(r *agents.Result, err error, _ bool) (bool, string) {
				if err != nil {
					return false, "failed: " + err.Error()
				}
				return r.Continuations >= 1 && r.FinishReason != "length", fmt.Sprintf("%d continuation(s), final finish_reason=%s", r.Continuations, r.FinishReason)
			}},
		{Name: "bad-tool-args", Agent: "drill-badargs", mockAgent: "pg-drill-badargs",
			Fault: "malformed JSON tool arguments", Pattern: "argument validation + model self-correction",
			Expected: "invalid call rejected without executing, corrected call executed",
			check: func(r *agents.Result, err error, _ bool) (bool, string) {
				if err != nil {
					return false, "failed: " + err.Error()
				}
				var outcomes []string
				for _, te := range r.ToolExecutions {
					outcomes = append(outcomes, te.Outcome)
				}
				ok := slices.Contains(outcomes, "invalid_arguments") && slices.Contains(outcomes, "ok")
				return ok, "tool outcomes: " + strings.Join(outcomes, " -> ")
			}},
		{Name: "stream-cut", Agent: "drill-streamcut", mockAgent: "pg-drill-streamcut",
			Fault: "SSE stream cut after 3 chunks", Pattern: "stream integrity check + non-streaming retry",
			Expected: "broken stream detected, completed without streaming",
			check: func(r *agents.Result, err error, _ bool) (bool, string) {
				if err != nil {
					return false, "failed: " + err.Error()
				}
				return r.StreamFallback, fmt.Sprintf("stream downgrade=%v after %d attempt(s)", r.StreamFallback, r.Attempts)
			}},
		{Name: "outage", Agent: "drill-overloaded", mockAgent: "pg-drill-overloaded", expectErr: true,
			Fault: "503 on every request, no fallback", Pattern: "bounded failure (no infinite retry)",
			Expected: "fails cleanly after max_retries; the drill continues (isolation)",
			check: func(r *agents.Result, err error, _ bool) (bool, string) {
				var ex *retry.ExhaustedError
				if errors.As(err, &ex) {
					return true, fmt.Sprintf("failed cleanly after %d attempt(s): %s", ex.Attempts, ex.Reason)
				}
				if err != nil {
					return false, "failed, but not by exhausting retries: " + err.Error()
				}
				return false, "unexpectedly succeeded"
			}},
	}
}

func drillCaseNames() []string {
	var out []string
	for _, c := range drillCases() {
		out = append(out, c.Name)
	}
	return out
}

// Info implements workflow.Definition.
func (ResilienceDrill) Info() workflow.Info {
	items := map[string]any{"type": "string", "enum": drillCaseNames()}
	return workflow.Info{
		Name:    "resilience-drill",
		Title:   "Resilience drill",
		Summary: "Injects 429s, 503s, timeouts, connection resets, truncated answers, malformed tool arguments and broken streams through mockagents, then checks that each was recovered (or failed cleanly) as designed.",
		Patterns: []string{
			"retry with exponential backoff + jitter", "Retry-After", "per-attempt timeouts", "fallback chain",
			"transport-error retry", "continuation on truncation", "tool-argument validation", "stream downgrade",
			"bounded failure + isolation", "no stuck runs",
		},
		Agents: []string{"drill-flaky", "drill-ratelimited", "drill-slow", "drill-connreset", "drill-truncated", "drill-badargs", "drill-streamcut", "drill-overloaded", "backup"},
		InputSchema: objectSchema(nil, map[string]any{
			"cases":     map[string]any{"type": "array", "items": items, "description": "Subset of cases to run (default: all)."},
			"fail_hard": prop("boolean", "Let the outage case fail the whole run (shows the terminal failed state)."),
		}),
		Examples: []workflow.Example{
			{Name: "full-drill", Description: "Every fault, every recovery pattern.", Input: map[string]any{}},
			{Name: "retries-only", Description: "Just the retry and fallback cases.", Input: map[string]any{"cases": []any{"flaky", "rate-limited", "timeout"}}},
			{Name: "hard-failure", Description: "The outage case fails the run and ends it in the failed state.", Input: map[string]any{"cases": []any{"flaky", "outage"}, "fail_hard": true}},
		},
	}
}

// Validate implements workflow.Definition.
func (ResilienceDrill) Validate(input map[string]any) error {
	errs := []error{rejectUnknown(input, "cases", "fail_hard"), optionalBool(input, "fail_hard")}
	if v, ok := input["cases"]; ok && v != nil {
		list, ok := v.([]any)
		if !ok {
			errs = append(errs, fmt.Errorf("input.cases must be an array of case names"))
		} else {
			names := drillCaseNames()
			for _, c := range list {
				s, ok := c.(string)
				if !ok || !slices.Contains(names, s) {
					errs = append(errs, fmt.Errorf("input.cases: unknown case %v (known: %s)", c, strings.Join(names, ", ")))
				}
			}
		}
	}
	return errors.Join(errs...)
}

// Run implements workflow.Definition.
func (ResilienceDrill) Run(ctx context.Context, x *workflow.Exec) (any, error) {
	selected := map[string]bool{}
	if list, ok := x.Input()["cases"].([]any); ok && len(list) > 0 {
		for _, c := range list {
			selected[c.(string)] = true
		}
	}
	var cases []DrillCase
	for _, c := range drillCases() {
		if len(selected) == 0 || selected[c.Name] {
			cases = append(cases, c)
		}
	}
	failHard := x.Bool("fail_hard")

	// Re-arm stateful faults (mockagents' fail_first counters are per agent
	// and per server lifetime) by reloading those agents via the management API.
	armed := false
	var stateful []string
	for _, c := range cases {
		if c.stateful {
			stateful = append(stateful, c.mockAgent)
		}
	}
	if len(stateful) > 0 {
		_, _ = x.Step(ctx, "arm stateful faults", "logic", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
			if x.Engine().Arm == nil {
				h.Note("no mock management API configured; fail_first counters may already be spent")
				return map[string]any{"armed": false}, nil
			}
			if err := x.Engine().Arm(ctx, stateful); err != nil {
				h.Note("could not re-arm (%v); stateful cases may succeed on the first attempt", err)
				return map[string]any{"armed": false, "error": err.Error()}, nil
			}
			armed = true
			h.Note("reloaded %s via POST /api/v1/agents/{name}/reload (resets fail_first)", strings.Join(stateful, ", "))
			return map[string]any{"armed": true, "agents": stateful}, nil
		})
	}

	var rows []DrillRow
	allPassed := true
	for _, c := range cases {
		var row DrillRow
		_, stepErr := x.Step(ctx, "case: "+c.Name, "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
			res, err := x.Invoke(ctx, h, c.Agent, fmt.Sprintf("Task: resilience drill\nCase: %s\nReport the status.", c.Name),
				workflow.InvokeOptions{AuditTitle: "Drill output: " + c.Name})
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			passed, observed := c.check(res, err, armed)
			row = DrillRow{Case: c.Name, Fault: c.Fault, Pattern: c.Pattern, Expected: c.Expected, Observed: observed, Passed: passed}
			if res != nil {
				row.Attempts, row.Retries, row.Fallback, row.Model, row.LatencyMS, row.Output =
					res.Attempts, res.Retries, res.FallbackUsed, res.Model.String(), res.LatencyMS, res.Output
			}
			if err != nil {
				row.Error = err.Error()
				row.FailedCleanly = c.expectErr && passed
			}
			h.Note("%s: %s", map[bool]string{true: "PASS", false: "FAIL"}[passed], observed)
			if err != nil && (!c.expectErr || failHard) {
				return row, err
			}
			if err != nil {
				// Expected failure, isolated: record it and keep the drill going.
				h.Note("failure isolated: the drill continues (continue_on_error)")
			}
			return row, nil
		})
		rows = append(rows, row)
		if !row.Passed {
			allPassed = false
		}
		if stepErr != nil {
			if ctx.Err() != nil {
				return map[string]any{"cases": rows}, ctx.Err()
			}
			return map[string]any{"cases": rows, "passed": false},
				workflow.Fail(workflow.CodeAgentFailed, "case %q failed and fail_hard=true ended the run: %v", c.Name, stepErr)
		}
	}
	out := map[string]any{"cases": rows, "passed": allPassed, "armed": armed}
	if !allPassed {
		return out, workflow.Fail(workflow.CodeDrillFailed, "one or more drill cases did not behave as expected")
	}
	return out, nil
}
