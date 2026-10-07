package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/guard"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

// ResearchBrief is the orchestrator-worker workflow:
//
//	planner (LLM) -> researcher (LLM + tools) -> summarizer (SLM)
//	  -> grounding guard + verifier (LLM judge) -> [regenerate on LLM] -> human review
type ResearchBrief struct{}

// Plan is the planner's structured output.
type Plan struct {
	Topic     string     `json:"topic"`
	Objective string     `json:"objective"`
	Steps     []PlanStep `json:"steps"`
	Risks     []string   `json:"risks"`
}

// PlanStep is one planned step.
type PlanStep struct {
	ID    int    `json:"id"`
	Agent string `json:"agent"`
	Goal  string `json:"goal"`
}

// Verdict is the verifier's structured output.
type Verdict struct {
	Verdict    string   `json:"verdict"`
	Confidence float64  `json:"confidence"`
	Issues     []string `json:"issues"`
}

// Info implements workflow.Definition.
func (ResearchBrief) Info() workflow.Info {
	return workflow.Info{
		Name:    "research-brief",
		Title:   "Research brief",
		Summary: "A planner orchestrates a tool-using researcher and an SLM summarizer. A grounding guard plus an LLM judge catch hallucinations; ungrounded output is regenerated on the LLM tier; a human approves the brief.",
		Patterns: []string{
			"orchestrator-worker", "structured output (JSON plan)", "tool use (parallel calls)",
			"SLM summarization", "deterministic grounding guard", "LLM-as-judge", "SLM->LLM escalation on guard failure",
			"human review gate with revise loop",
		},
		Agents: []string{"planner", "researcher", "summarizer", "verifier", "editor"},
		InputSchema: objectSchema([]string{"topic"}, map[string]any{
			"topic":                  prop("string", "What to research (max 120 chars). Try 'retry strategies', 'SLM vs LLM routing' or 'hallucination guards'."),
			"audience":               prop("string", "Who the brief is for (default: engineering)."),
			"simulate_hallucination": prop("boolean", "Fault toggle: the SLM summarizer fixture returns a planted hallucination so you can watch the guard catch it."),
		}),
		Examples: []workflow.Example{
			{Name: "retry-strategies", Description: "Grounded brief on retries and backoff.", Input: map[string]any{"topic": "retry strategies for LLM APIs"}},
			{Name: "slm-routing", Description: "Brief on SLM vs LLM routing economics.", Input: map[string]any{"topic": "SLM vs LLM routing"}},
			{Name: "catch-a-hallucination", Description: "The SLM hallucinates; the guard and judge catch it; the LLM regenerates.", Input: map[string]any{"topic": "retry strategies for LLM APIs", "simulate_hallucination": true}},
		},
	}
}

// Validate implements workflow.Definition.
func (ResearchBrief) Validate(input map[string]any) error {
	return errors.Join(
		rejectUnknown(input, "topic", "audience", "simulate_hallucination"),
		requireString(input, "topic", 120),
		optionalString(input, "audience", 60),
		optionalBool(input, "simulate_hallucination"),
	)
}

// Run implements workflow.Definition.
func (ResearchBrief) Run(ctx context.Context, x *workflow.Exec) (any, error) {
	topic := guard.SanitizeLine(x.String("topic", ""), 120)
	audience := guard.SanitizeLine(x.String("audience", "engineering"), 60)
	cfg := x.Config()

	// 1. Plan (LLM tier, structured output).
	var plan Plan
	_, err := x.Step(ctx, "plan", "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
		res, err := x.Invoke(ctx, h, "planner",
			fmt.Sprintf("Task: plan a research brief\nTopic: %s\nAudience: %s\nRespond with a JSON plan.", topic, audience),
			workflow.InvokeOptions{AuditTitle: "Research plan"})
		if err != nil {
			return nil, err
		}
		if err := guard.ExtractJSON(res.Output, &plan); err != nil || len(plan.Steps) == 0 {
			// Graceful degradation: a malformed plan should not kill the run.
			h.Note("planner returned no usable JSON plan (%v); using the default plan", err)
			plan = Plan{Topic: topic, Objective: "Produce a grounded brief.", Steps: []PlanStep{
				{1, "researcher", "Gather evidence"}, {2, "summarizer", "Summarize"}, {3, "verifier", "Verify"}}}
		}
		return plan, nil
	})
	if err != nil {
		return nil, err
	}

	// 2. Research (LLM tier, tool loop with parallel tool calls).
	var findings string
	var evidence []string
	var sources []string
	_, err = x.Step(ctx, "research", "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
		var goals []string
		for _, s := range plan.Steps {
			goals = append(goals, "- "+guard.SanitizeLine(s.Goal, 200))
		}
		res, err := x.Invoke(ctx, h, "researcher",
			fmt.Sprintf("Task: research\nTopic: %s\nPlan:\n%s\nUse the available tools, then report findings with [KB-nnn] citations.", topic, strings.Join(goals, "\n")),
			workflow.InvokeOptions{AuditTitle: "Research findings"})
		if err != nil {
			return nil, err
		}
		findings = res.Output
		evidence = append(evidence, "Topic: "+topic)
		for _, te := range res.ToolExecutions {
			if te.Outcome != "ok" {
				continue
			}
			raw, _ := json.Marshal(te.Result)
			evidence = append(evidence, string(raw))
			if te.Tool == "search_kb" {
				sources = append(sources, kbIDs(te.Result)...)
			}
		}
		h.Note("%d tool call(s); evidence sources: %s", len(res.ToolExecutions), strings.Join(sources, ", "))
		return map[string]any{"findings": findings, "tool_calls": len(res.ToolExecutions), "sources": sources}, nil
	})
	if err != nil {
		return nil, err
	}

	// 3. Summarize on the SLM tier.
	var summary string
	_, err = x.Step(ctx, "summarize", "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
		prompt := fmt.Sprintf("Task: summarize\nTopic: %s\nFindings:\n%s", topic, findings)
		if x.Bool("simulate_hallucination") {
			prompt += "\n[simulate-hallucination]"
			h.Note("fault toggle on: asking the mock for its planted hallucination")
		}
		res, err := x.Invoke(ctx, h, "summarizer", prompt, workflow.InvokeOptions{AuditTitle: "Summary draft"})
		if err != nil {
			return nil, err
		}
		if fixture := res.MockHeaders["X-Mockagents-Hallucination"]; fixture != "" {
			h.Note("mock fixture metadata: X-Mockagents-Hallucination=%s (ground truth for testing; the guard does not read it)", fixture)
		}
		summary = res.Output
		return summary, nil
	})
	if err != nil {
		return nil, err
	}

	// 4. Guard + judge, with bounded regeneration on the LLM tier.
	known := tools.KnownCitations()
	var report guard.GroundingReport
	var verdict Verdict
	regenerations := 0
	for {
		_, err = x.Step(ctx, "grounding guard", "guard", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
			report = guard.CheckGrounding(summary, evidence, known, true)
			if !report.Grounded {
				x.Engine().Metrics.Inc("playground_guard_violations_total", "workflow", "research-brief")
				h.Note("violation: %s", report.Flags())
			}
			return report, nil
		})
		if err != nil {
			return nil, err
		}
		_, err = x.Step(ctx, "verify (LLM judge)", "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
			res, err := x.Invoke(ctx, h, "verifier",
				fmt.Sprintf("Task: verify grounding\nTopic: %s\nGuard flags: %s\nSummary: %s\nEvidence sources: %s",
					topic, report.Flags(), guard.SanitizeLine(summary, 2000), strings.Join(sources, ", ")),
				workflow.InvokeOptions{AuditTitle: "Grounding verdict"})
			if err != nil {
				return nil, err
			}
			if err := guard.ExtractJSON(res.Output, &verdict); err != nil {
				// Fail closed: an unparseable verdict is not a pass.
				verdict = Verdict{Verdict: "unknown", Issues: []string{"verifier returned no JSON verdict"}}
			}
			return verdict, nil
		})
		if err != nil {
			return nil, err
		}
		if report.Grounded && verdict.Verdict == "grounded" {
			break
		}
		if regenerations >= cfg.Workflows.MaxGroundingRegenerations {
			return map[string]any{"summary": summary, "grounding": report, "verifier": verdict},
				workflow.Fail(workflow.CodeGuardFailed, "summary is still ungrounded after %d regeneration(s): %s", regenerations, report.Flags())
		}
		regenerations++
		_, err = x.Step(ctx, fmt.Sprintf("regenerate summary #%d (strict grounding)", regenerations), "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
			why := "guard flags: " + report.Flags()
			prompt := fmt.Sprintf("Task: summarize\nTopic: %s\nStrict grounding: the previous summary contained unsupported content (%s). Use only figures that appear in the findings.\nFindings:\n%s",
				topic, report.Flags(), findings)
			opts := workflow.InvokeOptions{AuditTitle: fmt.Sprintf("Regenerated summary #%d", regenerations)}
			var res *agents.Result
			var err error
			if cfg.Router.EscalateOnGuardFailure {
				res, err = x.Escalate(ctx, h, "summarizer", prompt, why, opts)
			} else {
				res, err = x.Invoke(ctx, h, "summarizer", prompt, opts)
			}
			if err != nil {
				return nil, err
			}
			summary = res.Output
			return summary, nil
		})
		if err != nil {
			return nil, err
		}
	}

	// 5. Human review (blocking) with a bounded revise loop.
	final, decisions, err := x.ReviewLoop(ctx, "Research brief: "+topic, "summarizer", summary, map[string]any{
		"plan": plan, "findings": findings, "grounding": report, "verifier": verdict, "regenerations": regenerations,
	})
	out := map[string]any{
		"topic": topic, "audience": audience, "plan": plan, "findings": findings,
		"summary": final, "citations": report.Citations, "grounding": report, "verifier": verdict,
		"regenerations": regenerations, "reviews": decisions,
	}
	return out, err
}

// kbIDs pulls article ids out of a search_kb result.
func kbIDs(result any) []string {
	m, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	var ids []string
	if rs, ok := m["results"].([]map[string]any); ok {
		for _, r := range rs {
			if id, ok := r["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
