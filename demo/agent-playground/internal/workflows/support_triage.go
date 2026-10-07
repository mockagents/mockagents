package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/agents"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/guard"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

// SupportTriage is the router workflow:
//
//	classifier (SLM, Gemini) --low confidence--> classifier (LLM)
//	  -> route to billing | technical | general specialist (tools; refunds need approval)
//	  -> human review of the customer reply
type SupportTriage struct{}

// Classification is the classifier's structured output.
type Classification struct {
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Urgency    string  `json:"urgency"`
	Reason     string  `json:"reason,omitempty"`
}

var specialists = map[string]string{
	"billing":   "billing-specialist",
	"technical": "tech-specialist",
	"general":   "reply-writer",
}

// Info implements workflow.Definition.
func (SupportTriage) Info() workflow.Info {
	return workflow.Info{
		Name:    "support-triage",
		Title:   "Support triage",
		Summary: "An SLM classifier routes each ticket to a specialist agent and escalates to the LLM tier when it is unsure. The billing specialist's refund tool needs human approval, and a human reviews the reply.",
		Patterns: []string{
			"router / dispatcher", "SLM-first, LLM-on-doubt escalation", "confidence thresholds",
			"multi-turn tool loop", "approval-gated side-effecting tool", "idempotent tools",
			"human feedback injection on decline", "human review gate",
		},
		Agents: []string{"classifier", "billing-specialist", "tech-specialist", "reply-writer", "editor"},
		InputSchema: objectSchema([]string{"ticket"}, map[string]any{
			"ticket":   prop("string", "The customer's message (max 2000 chars)."),
			"customer": prop("string", "Optional customer name."),
		}),
		Examples: []workflow.Example{
			{Name: "duplicate-charge", Description: "Billing: lookup_order, then an approval-gated issue_refund.", Input: map[string]any{"ticket": "I was charged twice for order ORD-1001. Please refund the duplicate charge.", "customer": "Dana Whitfield"}},
			{Name: "app-crash", Description: "Technical: the specialist searches the knowledge base.", Input: map[string]any{"ticket": "The export page crashes with a 504 error every time I try it."}},
			{Name: "ambiguous", Description: "Mixed signals: the SLM is unsure (0.48) and the LLM decides.", Input: map[string]any{"ticket": "My last invoice looks wrong and since then the app crashes on login."}},
			{Name: "general", Description: "General question, handled by the SLM reply writer.", Input: map[string]any{"ticket": "How do I change my notification settings?"}},
		},
	}
}

// Validate implements workflow.Definition.
func (SupportTriage) Validate(input map[string]any) error {
	return errors.Join(
		rejectUnknown(input, "ticket", "customer"),
		requireString(input, "ticket", 2000),
		optionalString(input, "customer", 80),
	)
}

// Run implements workflow.Definition.
func (SupportTriage) Run(ctx context.Context, x *workflow.Exec) (any, error) {
	ticket := guard.SanitizeLine(x.String("ticket", ""), 2000)
	cfg := x.Config()
	threshold := cfg.Router.EscalationThreshold

	// 1. Classify: SLM first, escalate on low confidence.
	var cls Classification
	escalated := false
	_, err := x.Step(ctx, "classify", "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
		prompt := fmt.Sprintf("Task: classify support ticket\nTicket: %s\nRespond with JSON {category, confidence, urgency}.", ticket)
		res, err := x.Invoke(ctx, h, "classifier", prompt, workflow.InvokeOptions{AuditTitle: "Ticket classification"})
		if err != nil {
			return nil, err
		}
		parseErr := guard.ExtractJSON(res.Output, &cls)
		if res.Tier == config.TierSLM && (parseErr != nil || cls.Confidence < threshold) {
			why := fmt.Sprintf("SLM confidence %.2f < threshold %.2f", cls.Confidence, threshold)
			if parseErr != nil {
				why = "SLM output was not valid JSON"
			}
			res, err = x.Escalate(ctx, h, "classifier", prompt, why, workflow.InvokeOptions{AuditTitle: "Ticket classification (escalated)"})
			if err != nil {
				return nil, err
			}
			escalated = true
			cls = Classification{}
			parseErr = guard.ExtractJSON(res.Output, &cls)
		}
		if parseErr != nil {
			return nil, workflow.Fail(workflow.CodeStepFailed, "classifier returned no JSON classification: %v", parseErr)
		}
		cls.Category = strings.ToLower(strings.TrimSpace(cls.Category))
		return cls, nil
	})
	if err != nil {
		return nil, err
	}

	// 2. Route.
	specialist, ok := specialists[cls.Category]
	if !ok {
		specialist = specialists["general"]
	}
	_, _ = x.Step(ctx, "route", "logic", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
		h.Note("category %q (confidence %.2f, urgency %s) -> %s", cls.Category, cls.Confidence, cls.Urgency, specialist)
		return map[string]any{"category": cls.Category, "specialist": specialist, "escalated": escalated}, nil
	})

	// 3. Resolve with the specialist (tool loop; refunds wait for approval).
	var resolution *agents.Result
	_, err = x.Step(ctx, "resolve: "+specialist, "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
		prompt := fmt.Sprintf("Task: resolve %s ticket\nTicket: %s\nClassification: %s (%.2f, urgency %s)", cls.Category, ticket, cls.Category, cls.Confidence, cls.Urgency)
		if c := guard.SanitizeLine(x.String("customer", ""), 80); c != "" {
			prompt += "\nCustomer: " + c
		}
		res, err := x.Invoke(ctx, h, specialist, prompt, workflow.InvokeOptions{AuditTitle: "Draft reply from " + specialist})
		if err != nil {
			return nil, err
		}
		resolution = res
		for _, te := range res.ToolExecutions {
			note := fmt.Sprintf("tool %s -> %s", te.Tool, te.Outcome)
			if te.Approval != nil {
				note += fmt.Sprintf(" (approved=%v by %s)", te.Approval.Approved, te.Approval.Reviewer)
			}
			h.Note("%s", note)
		}
		return map[string]any{"reply": res.Output, "tool_executions": res.ToolExecutions}, nil
	})
	if err != nil {
		return nil, err
	}

	// 4. Human review of the customer-facing reply.
	reply, decisions, err := x.ReviewLoop(ctx, "Customer reply ("+cls.Category+")", specialist, resolution.Output, map[string]any{
		"ticket": ticket, "classification": cls, "escalated": escalated, "tool_executions": resolution.ToolExecutions,
	})
	return map[string]any{
		"classification": cls, "escalated": escalated, "routed_to": specialist,
		"reply": reply, "tool_executions": resolution.ToolExecutions, "reviews": decisions,
	}, err
}
