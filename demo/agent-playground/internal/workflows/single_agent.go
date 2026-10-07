package workflows

import (
	"context"
	"errors"
	"fmt"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
)

// SingleAgent backs POST /api/agents/{name}/invoke: one agent call wrapped
// in a run, so direct invocations get the same trace, stats, audit review
// item, deadline and watchdog as full workflows.
type SingleAgent struct{}

// Info implements workflow.Definition.
func (SingleAgent) Info() workflow.Info {
	return workflow.Info{
		Name:     "agent",
		Title:    "Single agent call",
		Summary:  "Invoke one agent directly (used by POST /api/agents/{name}/invoke and the Chat tab).",
		Patterns: []string{"tool use", "retry/fallback", "streaming", "audit review"},
		Hidden:   true,
		InputSchema: objectSchema([]string{"agent", "input"}, map[string]any{
			"agent": prop("string", "Agent name."),
			"input": prop("string", "User message."),
			"tier":  prop("string", "Optional tier override: auto | slm | llm."),
		}),
	}
}

// Validate implements workflow.Definition.
func (SingleAgent) Validate(input map[string]any) error {
	errs := []error{
		rejectUnknown(input, "agent", "input", "tier"),
		requireString(input, "agent", 63),
		requireString(input, "input", 8000),
		optionalString(input, "tier", 4),
	}
	if t, ok := input["tier"].(string); ok && t != "" && t != "auto" && t != "slm" && t != "llm" {
		errs = append(errs, fmt.Errorf("input.tier must be auto, slm or llm"))
	}
	return errors.Join(errs...)
}

// Run implements workflow.Definition.
func (SingleAgent) Run(ctx context.Context, x *workflow.Exec) (any, error) {
	agent := x.String("agent", "")
	if _, ok := x.Config().Agent(agent); !ok {
		return nil, workflow.Fail(workflow.CodeStepFailed, "unknown agent %q", agent)
	}
	return x.Step(ctx, "invoke "+agent, "agent", func(ctx context.Context, h *workflow.StepHandle) (any, error) {
		opts := workflow.InvokeOptions{AuditTitle: "Direct call to " + agent}
		opts.Tier = x.String("tier", "")
		res, err := x.Invoke(ctx, h, agent, x.String("input", ""), opts)
		if err != nil {
			return res, err
		}
		return res, nil
	})
}
