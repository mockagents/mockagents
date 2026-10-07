// Package router decides which tier, and therefore which model, serves an
// agent call.
//
// The hybrid SLM/LLM policy:
//
//   - An agent pinned to a tier ("slm" / "llm") uses that tier.
//   - An agent on "auto" uses the tier its role maps to in router.policy
//     (summarize/classify/extract/generate -> slm; plan/reason/verify/judge/chat -> llm).
//   - If the chosen tier has no model configured, the other tier is used.
//   - A caller may override per call. Workflows also escalate an SLM result to
//     the LLM tier when it is low-confidence or fails a guard (see Escalate).
package router

import (
	"fmt"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
)

// Decision is the router's verdict for one call.
type Decision struct {
	Tier     string          `json:"tier"`
	Model    config.ModelRef `json:"model"`
	Reason   string          `json:"reason"`
	Override bool            `json:"override,omitempty"`
}

// Route picks the tier and model for agent a. override ("", "auto", "slm",
// "llm") is a per-call caller preference.
func Route(cfg *config.Config, a config.Agent, override string) (Decision, error) {
	if len(a.Models) == 0 {
		return Decision{}, fmt.Errorf("agent %q has no models configured", a.Name)
	}
	want, reason := "", ""
	switch {
	case override == config.TierSLM || override == config.TierLLM:
		want, reason = override, "caller override"
	case a.Tier == config.TierSLM || a.Tier == config.TierLLM:
		want, reason = a.Tier, "agent pinned to tier"
	default:
		if t, ok := cfg.Router.Policy[a.Role]; ok {
			want, reason = t, fmt.Sprintf("policy: role %q -> %s", a.Role, t)
		} else {
			want, reason = config.TierLLM, fmt.Sprintf("no policy for role %q, defaulting to llm", a.Role)
		}
	}
	if m, ok := a.Models[want]; ok {
		return Decision{Tier: want, Model: m, Reason: reason, Override: override == config.TierSLM || override == config.TierLLM}, nil
	}
	other := config.TierLLM
	if want == config.TierLLM {
		other = config.TierSLM
	}
	if m, ok := a.Models[other]; ok {
		return Decision{Tier: other, Model: m, Reason: fmt.Sprintf("%s; %s tier not configured for this agent, using %s", reason, want, other)}, nil
	}
	return Decision{}, fmt.Errorf("agent %q has no model for tier %s", a.Name, want)
}

// Escalate returns the LLM-tier decision for an agent whose SLM output was
// not good enough. ok is false when the agent has no LLM model.
func Escalate(a config.Agent, why string) (Decision, bool) {
	m, ok := a.Models[config.TierLLM]
	if !ok {
		return Decision{}, false
	}
	return Decision{Tier: config.TierLLM, Model: m, Reason: "escalated: " + why, Override: true}, true
}
