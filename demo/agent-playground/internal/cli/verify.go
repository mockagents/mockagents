package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/app"
)

// Check is one verification result.
type Check struct {
	Area   string        `json:"area"`
	Name   string        `json:"name"`
	Passed bool          `json:"passed"`
	Detail string        `json:"detail"`
	Took   time.Duration `json:"took_ns"`
}

// Report is the outcome of Verify.
type Report struct {
	Checks []Check `json:"checks"`
	Passed int     `json:"passed"`
	Failed int     `json:"failed"`
}

// OK reports whether every check passed.
func (r *Report) OK() bool { return r.Failed == 0 && r.Passed > 0 }

func verifyCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	g := addGlobals(fs)
	embedded := fs.Bool("embedded", false, "boot a private playground + embedded mock in-process and verify that")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	base := g.server
	if *embedded {
		a, err := app.New(app.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if err != nil {
			return err
		}
		defer a.Close()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = a.Run(ctx, ln); close(done) }()
		defer func() { cancel(); <-done }()
		base = "http://" + ln.Addr().String()
		fmt.Fprintf(out, "verifying an embedded playground at %s (mock %s)\n\n", base, a.MockURL)
	} else {
		fmt.Fprintf(out, "verifying the playground at %s\n\n", base)
	}
	rep := Verify(context.Background(), NewClient(base, g.token), func(c Check) {
		mark := "PASS"
		if !c.Passed {
			mark = "FAIL"
		}
		fmt.Fprintf(out, "[%s] %-14s %-58s %6dms  %s\n", mark, c.Area, c.Name, c.Took.Milliseconds(), strings.Join(strings.Fields(c.Detail), " "))
	})
	if g.asJSON {
		printJSON(out, rep)
	}
	fmt.Fprintf(out, "\n%d passed, %d failed\n", rep.Passed, rep.Failed)
	if !rep.OK() {
		return errors.New("verification failed")
	}
	return nil
}

// Verify runs the end-to-end feature checklist against a playground.
// progress (optional) is called after each check.
func Verify(ctx context.Context, c *Client, progress func(Check)) *Report {
	rep := &Report{}
	check := func(area, name string, fn func() (string, error)) {
		start := time.Now()
		detail, err := fn()
		ch := Check{Area: area, Name: name, Passed: err == nil, Detail: detail, Took: time.Since(start)}
		if err != nil {
			ch.Detail = err.Error()
			rep.Failed++
		} else {
			rep.Passed++
		}
		rep.Checks = append(rep.Checks, ch)
		if progress != nil {
			progress(ch)
		}
	}
	auto := map[string]any{"review_mode": "auto"}
	runAuto := func(wf string, input map[string]any) (*Run, error) {
		run, err := c.StartRun(ctx, wf, input, auto, 90*time.Second)
		if err != nil {
			return nil, err
		}
		if !run.Terminal() {
			return c.WaitRun(ctx, run.ID, nil, 90*time.Second)
		}
		return run, nil
	}
	expect := func(run *Run, status string) error {
		if run.Status != status {
			msg := ""
			if run.Error != nil {
				msg = run.Error.Code + ": " + run.Error.Message
			}
			return fmt.Errorf("run %s is %s, want %s %s", run.ID, run.Status, status, msg)
		}
		return nil
	}
	out := func(run *Run) map[string]any {
		m, _ := run.Output.(map[string]any)
		return m
	}

	// --- Platform ---------------------------------------------------------
	check("platform", "health and readiness (mock reachable)", func() (string, error) {
		var h, r map[string]any
		if err := c.Get(ctx, "/healthz", nil, &h); err != nil {
			return "", err
		}
		if err := c.Get(ctx, "/readyz", nil, &r); err != nil {
			return "", err
		}
		return fmt.Sprintf("status=%v", r["status"]), nil
	})
	check("platform", "OpenAPI document served (YAML + JSON)", func() (string, error) {
		var doc map[string]any
		if err := c.Get(ctx, "/openapi.json", nil, &doc); err != nil {
			return "", err
		}
		paths, _ := doc["paths"].(map[string]any)
		if doc["openapi"] == nil || len(paths) < 20 {
			return "", fmt.Errorf("unexpected OpenAPI document (paths=%d)", len(paths))
		}
		return fmt.Sprintf("openapi %v, %d paths", doc["openapi"], len(paths)), nil
	})
	check("mockagents", "mock status: every fixture loaded", func() (string, error) {
		var st struct {
			Mode   string `json:"mode"`
			Agents []any  `json:"agents"`
		}
		if err := c.Get(ctx, "/api/mock/status", nil, &st); err != nil {
			return "", err
		}
		if len(st.Agents) < 25 {
			return "", fmt.Errorf("mock has %d agents, want >= 25", len(st.Agents))
		}
		return fmt.Sprintf("%d mock agents (%s)", len(st.Agents), st.Mode), nil
	})

	// --- Agents -----------------------------------------------------------
	var agentList struct {
		Agents []struct {
			Name string `json:"name"`
		} `json:"agents"`
	}
	check("agents", "agent catalog", func() (string, error) {
		if err := c.Get(ctx, "/api/agents", nil, &agentList); err != nil {
			return "", err
		}
		if len(agentList.Agents) < 20 {
			return "", fmt.Errorf("only %d agents", len(agentList.Agents))
		}
		return fmt.Sprintf("%d agents", len(agentList.Agents)), nil
	})
	inputs := map[string]string{
		"planner": "Task: plan a research brief\nTopic: retry strategies", "researcher": "Task: research\nTopic: retry strategies",
		"summarizer": "Task: summarize\nTopic: retry strategies\nFindings:\n...", "verifier": "Task: verify grounding\nGuard flags: none",
		"classifier": "Task: classify support ticket\nTicket: I was charged twice", "billing-specialist": "Task: resolve billing ticket\nTicket: duplicate charge on ORD-1001",
		"tech-specialist": "Task: resolve technical ticket\nTicket: 504 crash", "reply-writer": "Task: draft reply\nTicket: hello",
		"editor": "Task: revise\nReviewer feedback: shorter\nDraft:\nA long draft.", "proposer-a": "Question: which database for storage?",
		"proposer-b": "Question: which database for storage?", "critic": "Question: should we rewrite it?", "arbiter": "Question: should we rewrite it?",
		"assistant": "hello", "backup": "status?",
	}
	expectFail := map[string]bool{"drill-overloaded": true}
	for _, a := range agentList.Agents {
		name := a.Name
		check("agents", "invoke "+name, func() (string, error) {
			in := inputs[name]
			if in == "" {
				in = "Task: resilience drill\nCase: verify\nReport the status."
			}
			var res struct {
				Status string `json:"status"`
				Result struct {
					Output   string `json:"output"`
					Tier     string `json:"tier"`
					Attempts int    `json:"attempts"`
					Fallback bool   `json:"fallback_used"`
				} `json:"result"`
				Error *struct{ Code, Message string } `json:"error"`
			}
			_, err := c.Post(ctx, "/api/agents/"+name+"/invoke", nil, map[string]any{"input": in, "options": auto}, &res)
			if err != nil {
				return "", err
			}
			if expectFail[name] {
				if res.Status != "failed" {
					return "", fmt.Errorf("expected a clean failure, got %s", res.Status)
				}
				return "failed cleanly as designed: " + truncate(res.Error.Message, 60), nil
			}
			if res.Status != "completed" || res.Result.Output == "" {
				return "", fmt.Errorf("status=%s output=%q err=%v", res.Status, res.Result.Output, res.Error)
			}
			return fmt.Sprintf("%s tier, %d attempt(s): %s", res.Result.Tier, res.Result.Attempts, truncate(res.Result.Output, 50)), nil
		})
	}
	check("streaming", "SSE streaming invoke (assistant)", func() (string, error) {
		deltas, done := 0, false
		err := c.Stream(ctx, "/api/agents/assistant/invoke", map[string]any{"input": "what can you do?", "stream": true}, func(ev string, _ json.RawMessage) bool {
			if ev == "delta" {
				deltas++
			}
			if ev == "done" {
				done = true
				return false
			}
			return true
		})
		if err != nil {
			return "", err
		}
		if deltas < 3 || !done {
			return "", fmt.Errorf("deltas=%d done=%v", deltas, done)
		}
		return fmt.Sprintf("%d deltas then done", deltas), nil
	})

	// --- Tools ------------------------------------------------------------
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"search_kb", map[string]any{"query": "retry backoff"}, "KB-101"},
		{"calculator", map[string]any{"expression": "200 * 2^4"}, "3200"},
		{"lookup_order", map[string]any{"order_id": "ORD-1002"}, "Team plan"},
	} {
		tc := tc
		check("tools", "execute "+tc.tool, func() (string, error) {
			var res map[string]any
			if _, err := c.Post(ctx, "/api/tools/"+tc.tool+"/execute", nil, map[string]any{"arguments": tc.args}, &res); err != nil {
				return "", err
			}
			raw, _ := json.Marshal(res["result"])
			if !strings.Contains(string(raw), tc.want) {
				return "", fmt.Errorf("result %s lacks %q", raw, tc.want)
			}
			return truncate(string(raw), 70), nil
		})
	}
	check("tools", "issue_refund is approval-gated (403 when called directly)", func() (string, error) {
		_, err := c.Post(ctx, "/api/tools/issue_refund/execute", nil, map[string]any{"arguments": map[string]any{"order_id": "ORD-1001", "amount": 1, "reason": "x"}}, nil)
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 403 {
			return "403 approval_required", nil
		}
		return "", fmt.Errorf("expected 403, got %v", err)
	})

	// --- Workflows (auto review) --------------------------------------------
	check("orchestrator", "research-brief: plan -> tools -> SLM summary -> guard -> judge", func() (string, error) {
		run, err := runAuto("research-brief", map[string]any{"topic": "retry strategies for LLM APIs"})
		if err != nil {
			return "", err
		}
		if err := expect(run, "completed"); err != nil {
			return "", err
		}
		if run.Stats.SLMCalls == 0 || run.Stats.LLMTierCall == 0 || run.Stats.ToolCalls < 2 {
			return "", fmt.Errorf("stats %+v", run.Stats)
		}
		return fmt.Sprintf("%d steps, slm=%d llm=%d tools=%d", len(run.Steps), run.Stats.SLMCalls, run.Stats.LLMTierCall, run.Stats.ToolCalls), nil
	})
	check("hallucination", "planted hallucination caught, regenerated on LLM tier", func() (string, error) {
		run, err := runAuto("research-brief", map[string]any{"topic": "retry strategies", "simulate_hallucination": true})
		if err != nil {
			return "", err
		}
		if err := expect(run, "completed"); err != nil {
			return "", err
		}
		if out(run)["regenerations"] != float64(1) || run.Stats.Escalations != 1 {
			return "", fmt.Errorf("regenerations=%v escalations=%d", out(run)["regenerations"], run.Stats.Escalations)
		}
		return "guard flagged 97/100/2024 + KB-999; escalated summary grounded", nil
	})
	check("routing", "support-triage: SLM stays on SLM for a general ticket", func() (string, error) {
		run, err := runAuto("support-triage", map[string]any{"ticket": "How do I change my notification settings?"})
		if err != nil {
			return "", err
		}
		if err := expect(run, "completed"); err != nil {
			return "", err
		}
		if run.Stats.LLMTierCall != 0 {
			return "", fmt.Errorf("expected no LLM-tier calls, got %d", run.Stats.LLMTierCall)
		}
		return fmt.Sprintf("routed_to=%v, %d SLM calls, 0 LLM calls", out(run)["routed_to"], run.Stats.SLMCalls), nil
	})
	check("routing", "support-triage: unsure SLM escalates to LLM", func() (string, error) {
		run, err := runAuto("support-triage", map[string]any{"ticket": "My last invoice looks wrong and since then the app crashes on login."})
		if err != nil {
			return "", err
		}
		if err := expect(run, "completed"); err != nil {
			return "", err
		}
		if out(run)["escalated"] != true || out(run)["routed_to"] != "tech-specialist" {
			return "", fmt.Errorf("escalated=%v routed_to=%v", out(run)["escalated"], out(run)["routed_to"])
		}
		return "SLM 0.48 < 0.6 -> LLM: technical (0.86) -> tech-specialist", nil
	})
	for _, dc := range []struct{ q, method string }{
		{"Which database should the single-node edition use for storage?", "consensus"},
		{"Should we rewrite the legacy billing module this quarter?", "arbitration"},
		{"Should we launch the new pricing page on Monday?", "arbitration"},
	} {
		dc := dc
		check("arbitration", "decision-review: "+dc.method+" ("+truncate(dc.q, 30)+")", func() (string, error) {
			run, err := runAuto("decision-review", map[string]any{"question": dc.q})
			if err != nil {
				return "", err
			}
			if err := expect(run, "completed"); err != nil {
				return "", err
			}
			o := out(run)
			if o["method"] != dc.method {
				return "", fmt.Errorf("method=%v", o["method"])
			}
			return fmt.Sprintf("method=%v needs_human=%v", o["method"], o["needs_human_decision"]), nil
		})
	}
	check("resilience", "resilience-drill: every fault recovered or failed cleanly", func() (string, error) {
		run, err := runAuto("resilience-drill", map[string]any{})
		if err != nil {
			return "", err
		}
		if err := expect(run, "completed"); err != nil {
			return "", err
		}
		if run.Stats.Retries < 4 || run.Stats.Fallbacks < 2 {
			return "", fmt.Errorf("retries=%d fallbacks=%d", run.Stats.Retries, run.Stats.Fallbacks)
		}
		return fmt.Sprintf("8 cases, retries=%d fallbacks=%d", run.Stats.Retries, run.Stats.Fallbacks), nil
	})
	check("resilience", "fail_hard drill ends in the terminal failed state", func() (string, error) {
		run, err := runAuto("resilience-drill", map[string]any{"cases": []any{"outage"}, "fail_hard": true})
		if err != nil {
			return "", err
		}
		if err := expect(run, "failed"); err != nil {
			return "", err
		}
		return "failed: " + run.Error.Code, nil
	})
	check("retry", "retry policy above 5 retries is rejected (400)", func() (string, error) {
		_, err := c.StartRun(ctx, "decision-review", map[string]any{"question": "q"}, map[string]any{"retry": map[string]any{"max_retries": 6}}, 0)
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 400 {
			return "400 " + ae.Code, nil
		}
		return "", fmt.Errorf("expected 400, got %v", err)
	})

	// --- Human review (manual) ----------------------------------------------
	check("human-review", "approval-gated refund + revise + approve via API", func() (string, error) {
		run, err := c.StartRun(ctx, "support-triage", map[string]any{"ticket": "I was charged twice for order ORD-1001. Please refund the duplicate charge."},
			map[string]any{"review_mode": "manual"}, 30*time.Second)
		if err != nil {
			return "", err
		}
		if run.Phase != "awaiting_approval" {
			return "", fmt.Errorf("expected awaiting_approval, got %s/%s", run.Status, run.Phase)
		}
		steps := []string{}
		pending, err := c.PendingReviews(ctx, run.ID)
		if err != nil || len(pending) != 1 || pending[0].Kind != "tool_approval" {
			return "", fmt.Errorf("pending=%v err=%v", pending, err)
		}
		if _, err := c.Decide(ctx, pending[0].ID, "approve", "duplicate confirmed", "verify"); err != nil {
			return "", err
		}
		steps = append(steps, "refund approved")
		run, err = c.WaitRun(ctx, run.ID, (*Run).Waiting, 30*time.Second)
		if err != nil {
			return "", err
		}
		pending, _ = c.PendingReviews(ctx, run.ID)
		if len(pending) != 1 || pending[0].Kind != "output_gate" {
			return "", fmt.Errorf("expected an output gate, got %v", pending)
		}
		if _, err := c.Decide(ctx, pending[0].ID, "revise", "add an apology for the delay", "verify"); err != nil {
			return "", err
		}
		steps = append(steps, "revision requested")
		var gate Review
		run, err = c.WaitRun(ctx, run.ID, func(r *Run) bool {
			p, _ := c.PendingReviews(ctx, r.ID)
			if len(p) == 1 && p[0].ID != pending[0].ID {
				gate = p[0]
				return true
			}
			return false
		}, 30*time.Second)
		if err != nil {
			return "", err
		}
		if !strings.Contains(gate.Content, "Revised per reviewer feedback") {
			return "", fmt.Errorf("revised draft missing feedback: %q", gate.Content)
		}
		if _, err := c.Decide(ctx, gate.ID, "approve", "", "verify"); err != nil {
			return "", err
		}
		steps = append(steps, "revision approved")
		run, err = c.WaitRun(ctx, run.ID, nil, 30*time.Second)
		if err != nil {
			return "", err
		}
		if err := expect(run, "completed"); err != nil {
			return "", err
		}
		return strings.Join(steps, " -> ") + " -> completed", nil
	})
	check("human-review", "every AI output recorded as an audit review item", func() (string, error) {
		var res struct {
			Count int `json:"count"`
		}
		if err := c.Get(ctx, "/api/reviews", url.Values{"kind": {"output_audit"}}, &res); err != nil {
			return "", err
		}
		if res.Count < 20 {
			return "", fmt.Errorf("only %d audit items", res.Count)
		}
		return fmt.Sprintf("%d audit items", res.Count), nil
	})
	check("no-stuck", "unanswered review times out -> failed (review_timeout)", func() (string, error) {
		run, err := c.StartRun(ctx, "support-triage", map[string]any{"ticket": "How do I change my notification settings?"},
			map[string]any{"review_mode": "manual", "review_timeout_ms": 1000}, 0)
		if err != nil {
			return "", err
		}
		run, err = c.WaitRun(ctx, run.ID, nil, 30*time.Second)
		if err != nil {
			return "", err
		}
		if err := expect(run, "failed"); err != nil {
			return "", err
		}
		if run.Error.Code != "review_timeout" {
			return "", fmt.Errorf("code=%s", run.Error.Code)
		}
		return "review_timeout after 1s", nil
	})
	check("no-stuck", "cancel moves a waiting run to failed (cancelled)", func() (string, error) {
		run, err := c.StartRun(ctx, "support-triage", map[string]any{"ticket": "How do I change my notification settings?"}, map[string]any{"review_mode": "manual"}, 30*time.Second)
		if err != nil {
			return "", err
		}
		var res Run
		if _, err := c.Post(ctx, "/api/runs/"+run.ID+"/cancel", nil, map[string]any{"reason": "verify"}, &res); err != nil {
			return "", err
		}
		if res.Status != "failed" || res.Error.Code != "cancelled" {
			return "", fmt.Errorf("status=%s", res.Status)
		}
		return "cancelled", nil
	})

	// --- Configuration ------------------------------------------------------
	check("config", "runtime agent reconfiguration (summarizer -> llm tier)", func() (string, error) {
		defer func() { _, _ = c.Post(ctx, "/api/config/reset", nil, nil, nil) }()
		if _, err := c.Do(ctx, "PATCH", "/api/agents/summarizer", nil, map[string]any{"tier": "llm"}, nil); err != nil {
			return "", err
		}
		var res struct {
			Result struct {
				Tier  string         `json:"tier"`
				Model map[string]any `json:"model"`
			} `json:"result"`
		}
		if _, err := c.Post(ctx, "/api/agents/summarizer/invoke", nil, map[string]any{"input": "Task: summarize\nTopic: retry strategies\nFindings:\n..."}, &res); err != nil {
			return "", err
		}
		if res.Result.Tier != "llm" {
			return "", fmt.Errorf("tier=%s", res.Result.Tier)
		}
		return fmt.Sprintf("now served by %v", res.Result.Model["model"]), nil
	})
	check("config", "invalid config change rejected, previous config kept", func() (string, error) {
		_, err := c.Do(ctx, "PATCH", "/api/config", nil, map[string]any{"defaults": map[string]any{"retry": map[string]any{"max_retries": 9}}}, nil)
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != 400 {
			return "", fmt.Errorf("expected 400, got %v", err)
		}
		var cfg struct {
			Config struct {
				Defaults struct {
					Retry struct {
						MaxRetries int `json:"max_retries"`
					} `json:"retry"`
				} `json:"defaults"`
			} `json:"config"`
		}
		if err := c.Get(ctx, "/api/config", nil, &cfg); err != nil {
			return "", err
		}
		if cfg.Config.Defaults.Retry.MaxRetries > 5 {
			return "", fmt.Errorf("invalid value was applied")
		}
		return "400 invalid_config; max_retries still " + fmt.Sprint(cfg.Config.Defaults.Retry.MaxRetries), nil
	})

	// --- mockagents surfaces --------------------------------------------------
	check("mockagents", "native pipeline executed by mockagents", func() (string, error) {
		var res struct {
			Nodes []any `json:"nodes"`
		}
		if _, err := c.Post(ctx, "/api/mock/pipelines/pg-native-brief/run", nil, map[string]any{"input": "Task: plan a research brief\nTopic: retry strategies"}, &res); err != nil {
			return "", err
		}
		if len(res.Nodes) != 3 {
			return "", fmt.Errorf("nodes=%d", len(res.Nodes))
		}
		return "3 nodes (plan -> research -> summarize)", nil
	})
	check("mockagents", "interaction logs + cost rollup from the mock", func() (string, error) {
		var logs []map[string]any
		if err := c.Get(ctx, "/api/mock/logs", url.Values{"limit": {"200"}}, &logs); err != nil {
			return "", err
		}
		if len(logs) == 0 {
			return "", fmt.Errorf("the mock recorded no interactions")
		}
		var costs map[string]any
		if err := c.Get(ctx, "/api/mock/costs", nil, &costs); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d logged interactions; cost rollup keys=%d", len(logs), len(costs)), nil
	})

	// --- Final invariant --------------------------------------------------------
	check("no-stuck", "no run left in_progress; only valid states", func() (string, error) {
		deadline := time.Now().Add(20 * time.Second)
		for {
			var res struct {
				Runs   []Run          `json:"runs"`
				Counts map[string]int `json:"counts"`
			}
			if err := c.Get(ctx, "/api/runs", url.Values{"limit": {"500"}}, &res); err != nil {
				return "", err
			}
			for _, r := range res.Runs {
				if r.Status != "in_progress" && r.Status != "completed" && r.Status != "failed" {
					return "", fmt.Errorf("run %s has invalid status %q", r.ID, r.Status)
				}
			}
			if res.Counts["in_progress"] == 0 {
				return fmt.Sprintf("%d runs: completed=%d failed=%d in_progress=0", len(res.Runs), res.Counts["completed"], res.Counts["failed"]), nil
			}
			if time.Now().After(deadline) {
				return "", fmt.Errorf("%d run(s) still in_progress", res.Counts["in_progress"])
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
	check("observability", "Prometheus metrics exported", func() (string, error) {
		status, err := c.Do(ctx, "GET", "/metrics", nil, nil, nil)
		if err != nil || status != 200 {
			return "", fmt.Errorf("status=%d err=%v", status, err)
		}
		return "200 text/plain", nil
	})
	return rep
}
