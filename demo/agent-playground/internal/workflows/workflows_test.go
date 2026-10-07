package workflows_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/metrics"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/mockctl"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/mockhost"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/retry"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflows"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

type env struct {
	eng     *workflow.Engine
	metrics *metrics.Registry
	cfg     *config.Store
}

// newEnv boots a real (embedded) mockagents server with the playground's
// fixtures and wires the full runtime against it over HTTP.
func newEnv(t *testing.T) *env {
	t.Helper()
	host, err := mockhost.Start(mockhost.Options{
		AgentsDir: "../../mockagents", PricingFile: "../../config/mock-pricing.yaml", Logger: discard,
	})
	if err != nil {
		t.Fatalf("start embedded mock: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })
	reg := tools.NewRegistry()
	cfg, err := config.Load("../../config/playground.json", reg.Names())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store := config.NewStore(cfg, reg.Names())
	m := metrics.New()
	rt := &agents.Runtime{Config: store, Providers: agents.NewProviders(host.URL(), ""), Tools: reg, Metrics: m, Logger: discard}
	eng := workflow.NewEngine(rt, store, m, discard)
	eng.Register(workflows.All()...)
	eng.Arm = mockctl.New(host.URL(), "").Arm
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
	})
	return &env{eng: eng, metrics: m, cfg: store}
}

func (e *env) run(t *testing.T, wf string, input map[string]any, opts workflow.RunOptions) *workflow.Run {
	t.Helper()
	run, err := e.eng.Start(wf, input, opts, workflow.StartOptions{})
	if err != nil {
		t.Fatalf("start %s: %v", wf, err)
	}
	return e.wait(t, run.ID, nil)
}

func (e *env) wait(t *testing.T, id string, until func(*workflow.Run) bool) *workflow.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run, err := e.eng.Wait(ctx, id, until)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if until == nil && !run.Terminal() {
		t.Fatalf("run %s still %s/%s after timeout", id, run.Status, run.Phase)
	}
	return run
}

func auto() workflow.RunOptions { return workflow.RunOptions{ReviewMode: "auto"} }

func outputMap(t *testing.T, run *workflow.Run) map[string]any {
	t.Helper()
	m, ok := run.Output.(map[string]any)
	if !ok {
		t.Fatalf("output is %T, want map", run.Output)
	}
	return m
}

func mustStatus(t *testing.T, run *workflow.Run, want workflow.Status) {
	t.Helper()
	if run.Status != want {
		raw, _ := json.MarshalIndent(run, "", "  ")
		t.Fatalf("run status = %s, want %s\n%s", run.Status, want, raw)
	}
}

func TestResearchBrief_Grounded(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "research-brief", map[string]any{"topic": "retry strategies for LLM APIs"}, auto())
	mustStatus(t, run, workflow.StatusCompleted)
	out := outputMap(t, run)
	if got := out["regenerations"]; got != float64(0) && got != 0 {
		t.Errorf("regenerations = %v, want 0", got)
	}
	summary, _ := out["summary"].(string)
	if !strings.Contains(summary, "3200") {
		t.Errorf("summary %q should contain the grounded figure 3200", summary)
	}
	if run.Stats.SLMCalls == 0 || run.Stats.LLMTierCall == 0 {
		t.Errorf("expected both SLM and LLM tier calls, got %+v", run.Stats)
	}
	if run.Stats.ToolCalls < 2 {
		t.Errorf("expected the researcher's parallel tool calls, got %d", run.Stats.ToolCalls)
	}
}

func TestResearchBrief_CatchesHallucination(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "research-brief", map[string]any{"topic": "retry strategies", "simulate_hallucination": true}, auto())
	mustStatus(t, run, workflow.StatusCompleted)
	out := outputMap(t, run)
	if got := out["regenerations"]; got != 1 && got != float64(1) {
		t.Fatalf("regenerations = %v, want 1", got)
	}
	if run.Stats.Escalations != 1 {
		t.Errorf("escalations = %d, want 1 (SLM -> LLM after guard failure)", run.Stats.Escalations)
	}
	var sawGuardFail, sawFixture bool
	for _, s := range run.Steps {
		for _, n := range s.Notes {
			if strings.Contains(n, "violation: unsupported") {
				sawGuardFail = true
			}
			if strings.Contains(n, "X-Mockagents-Hallucination") {
				sawFixture = true
			}
		}
	}
	if !sawGuardFail || !sawFixture {
		t.Errorf("guard violation noted=%v, fixture header noted=%v", sawGuardFail, sawFixture)
	}
	if e.metrics.Sum("playground_guard_violations_total") != 1 {
		t.Errorf("guard violation metric = %v", e.metrics.Sum("playground_guard_violations_total"))
	}
}

func TestSupportTriage_RefundApprovedByHuman(t *testing.T) {
	e := newEnv(t)
	run, err := e.eng.Start("support-triage", map[string]any{"ticket": "I was charged twice for order ORD-1001. Please refund the duplicate charge."}, workflow.RunOptions{ReviewMode: "manual"}, workflow.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	run = e.wait(t, run.ID, func(r *workflow.Run) bool { return r.Phase == workflow.PhaseAwaitingApproval })
	if run.Status != workflow.StatusInProgress || run.Phase != workflow.PhaseAwaitingApproval {
		t.Fatalf("expected awaiting_approval, got %s/%s", run.Status, run.Phase)
	}
	pending := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Kind: workflow.ReviewToolApproval})
	if len(pending) != 1 {
		t.Fatalf("pending tool approvals = %d, want 1", len(pending))
	}
	if _, err := e.eng.Reviews.Decide(pending[0].ID, workflow.Decision{Action: "approve", Reviewer: "test"}); err != nil {
		t.Fatal(err)
	}
	run = e.wait(t, run.ID, func(r *workflow.Run) bool { return r.Phase == workflow.PhaseAwaitingReview })
	gates := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Kind: workflow.ReviewGate})
	if len(gates) != 1 {
		t.Fatalf("pending gates = %d, want 1", len(gates))
	}
	if !strings.Contains(gates[0].Content, "refunded 49.99") {
		t.Errorf("gate content %q should be the approved-refund reply", gates[0].Content)
	}
	if _, err := e.eng.Reviews.Decide(gates[0].ID, workflow.Decision{Action: "approve", Reviewer: "test"}); err != nil {
		t.Fatal(err)
	}
	run = e.wait(t, run.ID, nil)
	mustStatus(t, run, workflow.StatusCompleted)
	out := outputMap(t, run)
	if out["routed_to"] != "billing-specialist" {
		t.Errorf("routed_to = %v", out["routed_to"])
	}
}

func TestSupportTriage_RefundDeclined(t *testing.T) {
	e := newEnv(t)
	run, err := e.eng.Start("support-triage", map[string]any{"ticket": "Please refund the duplicate charge on ORD-1001."}, workflow.RunOptions{ReviewMode: "manual"}, workflow.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	run = e.wait(t, run.ID, func(r *workflow.Run) bool { return r.Phase == workflow.PhaseAwaitingApproval })
	pending := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Kind: workflow.ReviewToolApproval})
	if len(pending) != 1 {
		t.Fatalf("pending approvals = %d", len(pending))
	}
	if _, err := e.eng.Reviews.Decide(pending[0].ID, workflow.Decision{Action: "reject", Comment: "needs supervisor", Reviewer: "test"}); err != nil {
		t.Fatal(err)
	}
	run = e.wait(t, run.ID, func(r *workflow.Run) bool { return r.Phase == workflow.PhaseAwaitingReview })
	gates := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Kind: workflow.ReviewGate})
	if len(gates) != 1 || !strings.Contains(gates[0].Content, "billing specialist will review") {
		t.Fatalf("expected the declined-refund reply, got %+v", gates)
	}
	_, _ = e.eng.Reviews.Decide(gates[0].ID, workflow.Decision{Action: "approve"})
	mustStatus(t, e.wait(t, run.ID, nil), workflow.StatusCompleted)
}

func TestSupportTriage_EscalatesUnsureSLM(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "support-triage", map[string]any{"ticket": "My last invoice looks wrong and since then the app crashes on login."}, auto())
	mustStatus(t, run, workflow.StatusCompleted)
	out := outputMap(t, run)
	if out["escalated"] != true || out["routed_to"] != "tech-specialist" {
		t.Fatalf("escalated=%v routed_to=%v, want true / tech-specialist", out["escalated"], out["routed_to"])
	}
}

func TestSupportTriage_GeneralStaysOnSLM(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "support-triage", map[string]any{"ticket": "How do I change my notification settings?"}, auto())
	mustStatus(t, run, workflow.StatusCompleted)
	out := outputMap(t, run)
	if out["escalated"] != false || out["routed_to"] != "reply-writer" {
		t.Fatalf("escalated=%v routed_to=%v", out["escalated"], out["routed_to"])
	}
	if run.Stats.LLMTierCall != 0 {
		t.Errorf("general ticket should use only the SLM tier, got %+v", run.Stats)
	}
}

func TestDecisionReview_Paths(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		question   string
		method     string
		needsHuman bool
	}{
		{"Which database should the single-node edition use for storage?", "consensus", false},
		{"Should we rewrite the legacy billing module this quarter?", "arbitration", false},
		{"Should we launch the new pricing page on Monday?", "arbitration", true},
	}
	for _, c := range cases {
		run := e.run(t, "decision-review", map[string]any{"question": c.question}, auto())
		mustStatus(t, run, workflow.StatusCompleted)
		out := outputMap(t, run)
		if out["method"] != c.method || out["needs_human_decision"] != c.needsHuman {
			t.Errorf("%q: method=%v needs_human=%v, want %s/%v", c.question, out["method"], out["needs_human_decision"], c.method, c.needsHuman)
		}
		if c.method == "consensus" {
			skipped := 0
			for _, s := range run.Steps {
				if s.Status == workflow.StepSkipped {
					skipped++
				}
			}
			if skipped != 2 {
				t.Errorf("consensus should skip critic+arbiter, skipped=%d", skipped)
			}
		}
	}
}

func TestResilienceDrill_AllCasesRecover(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "resilience-drill", map[string]any{}, auto())
	out := outputMap(t, run)
	raw, _ := json.MarshalIndent(out["cases"], "", "  ")
	mustStatus(t, run, workflow.StatusCompleted)
	if out["armed"] != true {
		t.Errorf("stateful faults should have been re-armed via the management API")
	}
	if e.metrics.Sum("playground_retries_total") < 4 {
		t.Errorf("expected retries across the drill, got %v\n%s", e.metrics.Sum("playground_retries_total"), raw)
	}
	if e.metrics.Sum("playground_fallbacks_total") < 2 {
		t.Errorf("expected fallbacks (rate-limited + timeout), got %v", e.metrics.Sum("playground_fallbacks_total"))
	}
	// Running it twice proves re-arming works (fail_first counters reset).
	run2 := e.run(t, "resilience-drill", map[string]any{"cases": []any{"flaky"}}, auto())
	mustStatus(t, run2, workflow.StatusCompleted)
	if run2.Stats.Retries < 2 {
		t.Errorf("second drill should retry again after re-arming, retries=%d", run2.Stats.Retries)
	}
}

func TestResilienceDrill_FailHardEndsFailed(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "resilience-drill", map[string]any{"cases": []any{"outage"}, "fail_hard": true}, auto())
	mustStatus(t, run, workflow.StatusFailed)
	if run.Error == nil || run.Error.Code != workflow.CodeAgentFailed {
		t.Fatalf("error = %+v, want agent_failed", run.Error)
	}
}

func TestReview_RejectFailsRun(t *testing.T) {
	e := newEnv(t)
	run, _ := e.eng.Start("support-triage", map[string]any{"ticket": "How do I change my notification settings?"}, workflow.RunOptions{ReviewMode: "manual"}, workflow.StartOptions{})
	run = e.wait(t, run.ID, func(r *workflow.Run) bool { return r.Phase == workflow.PhaseAwaitingReview })
	gate := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Kind: workflow.ReviewGate})[0]
	if _, err := e.eng.Reviews.Decide(gate.ID, workflow.Decision{Action: "reject", Comment: "tone is off"}); err != nil {
		t.Fatal(err)
	}
	run = e.wait(t, run.ID, nil)
	mustStatus(t, run, workflow.StatusFailed)
	if run.Error.Code != workflow.CodeReviewRejected {
		t.Fatalf("code = %s", run.Error.Code)
	}
}

func TestReview_ReviseLoop(t *testing.T) {
	e := newEnv(t)
	run, _ := e.eng.Start("support-triage", map[string]any{"ticket": "How do I change my notification settings?"}, workflow.RunOptions{ReviewMode: "manual"}, workflow.StartOptions{})
	run = e.wait(t, run.ID, func(r *workflow.Run) bool { return r.Phase == workflow.PhaseAwaitingReview })
	gate := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Kind: workflow.ReviewGate})[0]
	if _, err := e.eng.Reviews.Decide(gate.ID, workflow.Decision{Action: "revise", Comment: "mention the settings page"}); err != nil {
		t.Fatal(err)
	}
	var next workflow.ReviewItem
	e.wait(t, run.ID, func(r *workflow.Run) bool {
		p := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewPending, Kind: workflow.ReviewGate})
		if len(p) == 1 {
			next = p[0]
			return true
		}
		return false
	})
	if !strings.Contains(next.Content, "Revised per reviewer feedback: mention the settings page") {
		t.Fatalf("revised draft = %q", next.Content)
	}
	_, _ = e.eng.Reviews.Decide(next.ID, workflow.Decision{Action: "approve"})
	run = e.wait(t, run.ID, nil)
	mustStatus(t, run, workflow.StatusCompleted)
}

func TestReview_TimeoutFailsRun(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "support-triage", map[string]any{"ticket": "How do I change my notification settings?"},
		workflow.RunOptions{ReviewMode: "manual", ReviewTimeoutMS: 1000})
	mustStatus(t, run, workflow.StatusFailed)
	if run.Error.Code != workflow.CodeReviewTimeout {
		t.Fatalf("code = %s", run.Error.Code)
	}
	expired := e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Status: workflow.ReviewExpired})
	if len(expired) != 1 {
		t.Fatalf("expired gates = %d, want 1", len(expired))
	}
}

func TestCancel(t *testing.T) {
	e := newEnv(t)
	run, _ := e.eng.Start("support-triage", map[string]any{"ticket": "How do I change my notification settings?"}, workflow.RunOptions{ReviewMode: "manual"}, workflow.StartOptions{})
	e.wait(t, run.ID, func(r *workflow.Run) bool { return r.Phase == workflow.PhaseAwaitingReview })
	if ok, err := e.eng.Cancel(run.ID, "test"); !ok || err != nil {
		t.Fatalf("cancel = %v, %v", ok, err)
	}
	run = e.wait(t, run.ID, nil)
	mustStatus(t, run, workflow.StatusFailed)
	if run.Error.Code != workflow.CodeCancelled {
		t.Fatalf("code = %s", run.Error.Code)
	}
	if ok, _ := e.eng.Cancel(run.ID, "again"); ok {
		t.Fatal("cancelling a finished run must be a no-op")
	}
}

func TestDirectAgentInvoke(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "agent", map[string]any{"agent": "assistant", "input": "hello"}, workflow.RunOptions{Stream: true})
	mustStatus(t, run, workflow.StatusCompleted)
	if len(e.eng.Reviews.List(workflow.Filter{RunID: run.ID, Kind: workflow.ReviewAudit})) != 1 {
		t.Fatal("every AI output should produce an audit review item")
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t)
	bad := []struct {
		wf    string
		input map[string]any
		opts  workflow.RunOptions
	}{
		{"research-brief", map[string]any{}, workflow.RunOptions{}},
		{"research-brief", map[string]any{"topic": "x", "bogus": 1}, workflow.RunOptions{}},
		{"resilience-drill", map[string]any{"cases": []any{"nope"}}, workflow.RunOptions{}},
		{"decision-review", map[string]any{"question": "q"}, workflow.RunOptions{Retry: &retry.Policy{MaxRetries: 6}}},
	}
	for _, b := range bad {
		if _, err := e.eng.Start(b.wf, b.input, b.opts, workflow.StartOptions{}); err == nil {
			t.Errorf("%s %v: expected a validation error", b.wf, b.input)
		}
	}
	if _, err := e.eng.Start("nope", nil, workflow.RunOptions{}, workflow.StartOptions{}); err == nil {
		t.Error("unknown workflow should fail")
	}
}
