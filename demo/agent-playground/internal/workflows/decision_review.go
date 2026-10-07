package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/guard"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

// DecisionReview is the multi-agent arbitration workflow:
//
//	proposer A (OpenAI) || proposer B (Anthropic)
//	  agree    -> consensus short-circuit (critic and arbiter skipped)
//	  disagree -> critic -> arbiter (judge) -> low confidence? escalate to a human
//	  -> human review gate
type DecisionReview struct{}

// Proposal is a proposer's structured output.
type Proposal struct {
	Decision   string   `json:"decision"`
	Confidence float64  `json:"confidence"`
	Rationale  string   `json:"rationale"`
	Risks      []string `json:"risks"`
}

// Arbitration is the arbiter's structured output.
type Arbitration struct {
	Decision   string  `json:"decision"`
	Chosen     string  `json:"chosen"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

// Info implements workflow.Definition.
func (DecisionReview) Info() workflow.Info {
	return workflow.Info{
		Name:    "decision-review",
		Title:   "Decision review",
		Summary: "Two independent proposers on different providers answer the same question. If they agree, consensus short-circuits; if not, a critic and an arbiter decide, and a low-confidence verdict is escalated to a human.",
		Patterns: []string{
			"parallel fan-out", "cross-model consistency (hallucination reduction)", "consensus short-circuit",
			"debate / critic", "LLM-as-judge arbitration", "confidence-based human escalation", "human review gate",
		},
		Agents: []string{"proposer-a", "proposer-b", "critic", "arbiter", "editor"},
		InputSchema: objectSchema([]string{"question"}, map[string]any{
			"question": prop("string", "The decision to make (max 300 chars). Try mentioning 'database', 'rewrite' or 'launch'."),
			"context":  prop("string", "Optional background for the proposers."),
		}),
		Examples: []workflow.Example{
			{Name: "consensus", Description: "Both proposers agree; critic and arbiter are skipped.", Input: map[string]any{"question": "Which database should the single-node edition use for storage?"}},
			{Name: "arbitrated", Description: "Proposers disagree; the arbiter decides with confidence 0.78.", Input: map[string]any{"question": "Should we rewrite the legacy billing module this quarter?"}},
			{Name: "needs-human", Description: "The arbiter is unsure (0.48), so the decision is flagged for a human.", Input: map[string]any{"question": "Should we launch the new pricing page on Monday?"}},
		},
	}
}

// Validate implements workflow.Definition.
func (DecisionReview) Validate(input map[string]any) error {
	return errors.Join(
		rejectUnknown(input, "question", "context"),
		requireString(input, "question", 300),
		optionalString(input, "context", 1000),
	)
}

// Run implements workflow.Definition.
func (DecisionReview) Run(ctx context.Context, x *workflow.Exec) (any, error) {
	question := guard.SanitizeLine(x.String("question", ""), 300)
	background := guard.SanitizeLine(x.String("context", "none provided"), 1000)
	threshold := x.Config().Workflows.ArbitrationThreshold

	// 1. Independent proposals in parallel, on two different providers.
	var propA, propB Proposal
	propose := func(agent string, dst *Proposal) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := x.Step(ctx, "propose: "+agent, "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
				res, err := x.Invoke(ctx, h, agent,
					fmt.Sprintf("Task: propose a decision\nQuestion: %s\nContext: %s\nRespond with JSON {decision, confidence, rationale, risks}.", question, background),
					workflow.InvokeOptions{AuditTitle: "Proposal from " + agent})
				if err != nil {
					return nil, err
				}
				if err := guard.ExtractJSON(res.Output, dst); err != nil || dst.Decision == "" {
					return nil, workflow.Fail(workflow.CodeStepFailed, "%s returned no JSON proposal", agent)
				}
				return *dst, nil
			})
			return err
		}
	}
	if err := workflow.Parallel(ctx, propose("proposer-a", &propA), propose("proposer-b", &propB)); err != nil {
		return nil, err
	}

	agree := normalizeDecision(propA.Decision) == normalizeDecision(propB.Decision)
	var critique string
	var verdict Arbitration
	method := "consensus"
	if agree {
		verdict = Arbitration{Decision: propA.Decision, Chosen: "both", Confidence: (propA.Confidence + propB.Confidence) / 2,
			Rationale: "Independent proposers on different providers reached the same decision."}
		reason := fmt.Sprintf("consensus: both proposers chose %q; no arbitration needed", propA.Decision)
		x.Skip("critique", "agent", reason)
		x.Skip("arbitrate", "agent", reason)
	} else {
		method = "arbitration"
		pa, _ := json.Marshal(propA)
		pb, _ := json.Marshal(propB)
		_, err := x.Step(ctx, "critique", "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
			res, err := x.Invoke(ctx, h, "critic",
				fmt.Sprintf("Task: critique proposals\nQuestion: %s\nProposal A: %s\nProposal B: %s", question, pa, pb),
				workflow.InvokeOptions{AuditTitle: "Critique"})
			if err != nil {
				return nil, err
			}
			critique = res.Output
			return critique, nil
		})
		if err != nil {
			return nil, err
		}
		_, err = x.Step(ctx, "arbitrate", "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
			res, err := x.Invoke(ctx, h, "arbiter",
				fmt.Sprintf("Task: arbitrate\nQuestion: %s\nProposal A: %s\nProposal B: %s\nCritique: %s\nRespond with JSON {decision, chosen, confidence, rationale}.",
					question, pa, pb, guard.SanitizeLine(critique, 2000)),
				workflow.InvokeOptions{AuditTitle: "Arbitration verdict"})
			if err != nil {
				return nil, err
			}
			if err := guard.ExtractJSON(res.Output, &verdict); err != nil || verdict.Decision == "" {
				return nil, workflow.Fail(workflow.CodeStepFailed, "arbiter returned no JSON verdict")
			}
			if verdict.Confidence < threshold {
				h.Note("arbiter confidence %.2f < %.2f: escalating to a human decision", verdict.Confidence, threshold)
			}
			return verdict, nil
		})
		if err != nil {
			return nil, err
		}
	}
	needsHuman := verdict.Confidence < threshold
	if needsHuman {
		x.Event("human_escalation", map[string]any{"reason": "low arbiter confidence", "confidence": verdict.Confidence})
	}

	// 2. Human review gate. A low-confidence verdict is clearly flagged.
	title := "Decision: " + question
	if needsHuman {
		title = "[NEEDS HUMAN DECISION] " + title
	}
	draft := fmt.Sprintf("Decision: %s (method: %s, confidence %.2f)\nRationale: %s", verdict.Decision, method, verdict.Confidence, verdict.Rationale)
	final, decisions, err := x.ReviewLoop(ctx, title, "arbiter", draft, map[string]any{
		"proposal_a": propA, "proposal_b": propB, "agreement": agree, "critique": critique,
		"verdict": verdict, "needs_human_decision": needsHuman,
	})
	return map[string]any{
		"question": question, "proposals": map[string]any{"a": propA, "b": propB}, "agreement": agree,
		"method": method, "critique": critique, "verdict": verdict, "needs_human_decision": needsHuman,
		"decision": final, "reviews": decisions,
	}, err
}

func normalizeDecision(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, "_", "-")))
}
