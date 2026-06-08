package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

// presetAgent builds a minimal valid agent whose chaos block uses the given
// preset (and an optional pre-set Errors to test override precedence).
func presetAgent(preset string, errs *types.ChaosErrorConfig) *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "p"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Chaos: &types.ChaosConfig{Preset: preset, Errors: errs},
				Scenarios: []types.Scenario{
					{Name: "default", Response: types.ScenarioResponse{Content: "ok"}},
				},
			},
		},
	}
}

func TestExpandChaosPreset_AllExpandAndEnable(t *testing.T) {
	for _, name := range types.ChaosPresets {
		def := presetAgent(name, nil)
		ApplyDefaults(def)
		c := def.Spec.Behavior.Chaos
		if !c.Enabled {
			t.Errorf("preset %q: chaos not enabled after expansion", name)
		}
		if c.Errors == nil && c.Latency == nil && c.RateLimit == nil {
			t.Errorf("preset %q: nothing was expanded", name)
		}
	}
}

func TestExpandChaosPreset_Specifics(t *testing.T) {
	cases := map[string]int{ // preset -> expected error status
		"server-down":   503,
		"rate-limited":  429,
		"access-denied": 403,
		"unauthorized":  401,
	}
	for name, want := range cases {
		def := presetAgent(name, nil)
		ApplyDefaults(def)
		got := def.Spec.Behavior.Chaos.Errors
		if got == nil || got.StatusCode != want {
			t.Errorf("preset %q: status = %v, want %d", name, got, want)
		}
	}

	// flaky is a fail_first fixture, not a per-request rate.
	def := presetAgent("flaky", nil)
	ApplyDefaults(def)
	if e := def.Spec.Behavior.Chaos.Errors; e == nil || e.FailFirst != 2 {
		t.Errorf("flaky: expected FailFirst=2, got %v", e)
	}

	// slow expands latency, not errors.
	def = presetAgent("slow", nil)
	ApplyDefaults(def)
	if l := def.Spec.Behavior.Chaos.Latency; l == nil || l.MaxMs == 0 {
		t.Errorf("slow: expected a latency block, got %v", l)
	}
}

func TestExpandChaosPreset_ExplicitOverrideWins(t *testing.T) {
	// preset rate-limited (429) but the author also set an explicit Errors block:
	// the explicit one must survive (preset only fills nil sections).
	def := presetAgent("rate-limited", &types.ChaosErrorConfig{Rate: 1, StatusCode: 418})
	ApplyDefaults(def)
	if got := def.Spec.Behavior.Chaos.Errors.StatusCode; got != 418 {
		t.Errorf("explicit errors clobbered by preset: status = %d, want 418", got)
	}
}

func TestExpandChaosPreset_UnknownLeftUntouched(t *testing.T) {
	def := presetAgent("bogus", nil)
	ApplyDefaults(def)
	c := def.Spec.Behavior.Chaos
	if c.Errors != nil || c.Latency != nil {
		t.Error("unknown preset should not expand any section")
	}
}

func TestValidate_UnknownChaosPreset(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: p
spec:
  protocol: openai-chat-completions
  behavior:
    chaos:
      preset: not-a-real-preset
    scenarios:
      - name: default
        response:
          content: "ok"
`)
	assertHasError(t, errs, "spec.behavior.chaos.preset", "unknown chaos preset")
}

func TestValidate_ScenarioToolCallMissingName(t *testing.T) {
	// A tool_call with no name (e.g. only raw_arguments) must be rejected even
	// though tool_calls satisfies the relaxed "carry something" rule (FB03-V1).
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: t
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: bad
        response:
          tool_calls:
            - raw_arguments: "{"
`)
	assertHasError(t, errs, "spec.behavior.scenarios.0.response.tool_calls.0.name", "tool_call name is required")
}

func TestValidate_StreamingPercentilePairs(t *testing.T) {
	base := `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: t
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
    streaming:
      enabled: true
`
	// Lone p50 (no p95) is rejected.
	errs := loadAndValidate(t, base+"      ttft_p50_ms: 300\n")
	assertHasError(t, errs, "spec.behavior.streaming.ttft_p50_ms", "requires ttft_p95_ms")

	// Lone p95 (no p50) is rejected.
	errs = loadAndValidate(t, base+"      itl_p95_ms: 80\n")
	assertHasError(t, errs, "spec.behavior.streaming.itl_p95_ms", "requires itl_p50_ms")

	// p95 < p50 is rejected.
	errs = loadAndValidate(t, base+"      ttft_p50_ms: 500\n      ttft_p95_ms: 300\n")
	assertHasError(t, errs, "spec.behavior.streaming.ttft_p95_ms", "must be >=")

	// A valid pair passes.
	if errs := loadAndValidate(t, base+"      ttft_p50_ms: 300\n      ttft_p95_ms: 1200\n"); errs != nil {
		t.Errorf("valid percentile pair rejected: %s", errs)
	}
}

func TestValidate_ValidChaosPreset(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: p
spec:
  protocol: openai-chat-completions
  behavior:
    chaos:
      preset: rate-limited
    scenarios:
      - name: default
        response:
          content: "ok"
`)
	if errs != nil {
		t.Errorf("valid preset rejected: %s", errs)
	}
}
