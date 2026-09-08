package config

import (
	"strings"
	"testing"
)

// chaosAgent wraps a chaos block in a minimal valid agent.
func chaosAgent(chaos string) string {
	indented := strings.ReplaceAll(strings.TrimRight(chaos, "\n"), "\n", "\n      ")
	return `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: chaos-bounds
spec:
  protocol: openai-chat-completions
  behavior:
    chaos:
      ` + indented + `
    scenarios:
      - name: default
        response:
          content: "ok"
`
}

// TestValidateChaos_Bounds is the audit M-01 guard: every numeric bound the
// JSON schema declares for the chaos block must be enforced by the Go
// validator, because nothing loads the schema at runtime. Before this,
// `status_code: 42` passed validation and panicked net/http per request.
func TestValidateChaos_Bounds(t *testing.T) {
	cases := []struct {
		name  string
		chaos string
		field string
		msg   string
	}{
		{"status_code below range", "errors:\n  status_code: 42", "spec.behavior.chaos.errors.status_code", "between 400 and 599"},
		{"status_code 2xx", "errors:\n  status_code: 200", "spec.behavior.chaos.errors.status_code", "between 400 and 599"},
		{"status_code above range", "errors:\n  status_code: 1000", "spec.behavior.chaos.errors.status_code", "between 400 and 599"},
		{"status_codes entry", "errors:\n  status_codes: [503, 42]", "spec.behavior.chaos.errors.status_codes.1", "between 400 and 599"},
		{"errors.rate", "errors:\n  rate: 1.5\n  status_code: 500", "spec.behavior.chaos.errors.rate", "[0.0, 1.0]"},
		{"timeout_ms", "errors:\n  timeout: true\n  timeout_ms: 600000", "spec.behavior.chaos.errors.timeout_ms", "between 0 and 60000"},
		{"errors.fail_first", "errors:\n  fail_first: -1", "spec.behavior.chaos.errors.fail_first", ">= 0"},
		{"rate_limit.requests", "rate_limit:\n  requests: 0\n  window_ms: 1000", "spec.behavior.chaos.rate_limit.requests", ">= 1"},
		{"rate_limit.window_ms", "rate_limit:\n  requests: 5\n  window_ms: 0", "spec.behavior.chaos.rate_limit.window_ms", ">= 1"},
		{"latency.distribution", "latency:\n  distribution: pareto\n  min_ms: 10", "spec.behavior.chaos.latency.distribution", "unknown latency distribution"},
		{"latency.min_ms negative", "latency:\n  min_ms: -5", "spec.behavior.chaos.latency.min_ms", "between 0 and 60000"},
		{"latency.max_ms over cap", "latency:\n  min_ms: 10\n  max_ms: 70000", "spec.behavior.chaos.latency.max_ms", "between 0 and 60000"},
		{"latency.max_ms < min_ms", "latency:\n  min_ms: 500\n  max_ms: 100", "spec.behavior.chaos.latency.max_ms", ">= min_ms"},
		{"latency.stddev_ms negative", "latency:\n  distribution: normal\n  mean_ms: 100\n  stddev_ms: -1", "spec.behavior.chaos.latency.stddev_ms", "between 0 and 60000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := loadAndValidate(t, chaosAgent(tc.chaos))
			assertHasError(t, errs, tc.field, tc.msg)
		})
	}
}

// TestValidateChaos_BoundaryValuesPass pins the inclusive edges so the guard
// above cannot creep and reject legitimate fixtures.
func TestValidateChaos_BoundaryValuesPass(t *testing.T) {
	valid := []string{
		"errors:\n  status_code: 400",
		"errors:\n  status_code: 599",
		"errors:\n  status_codes: [429, 503]\n  rate: 1.0",
		"errors:\n  rate: 0.0\n  fail_first: 3",
		"errors:\n  timeout: true\n  timeout_ms: 60000",
		"rate_limit:\n  requests: 1\n  window_ms: 1",
		"latency:\n  distribution: normal\n  mean_ms: 60000\n  stddev_ms: 0",
		"latency:\n  min_ms: 0\n  max_ms: 60000",
		"latency:\n  distribution: fixed\n  min_ms: 250",
	}
	for _, chaos := range valid {
		t.Run(strings.ReplaceAll(chaos, "\n", " "), func(t *testing.T) {
			if errs := loadAndValidate(t, chaosAgent(chaos)); errs != nil {
				t.Fatalf("expected no errors, got: %v", errs)
			}
		})
	}
}
