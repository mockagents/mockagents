package mockagents

import (
	"fmt"
	"reflect"
	"testing"
)

// Expectation is a fluent matcher over a single ChatResponse. Failing
// matchers call t.Errorf and keep going, matching the Go testing idiom:
// collect as many failures as possible per test run.
type Expectation struct {
	t        testing.TB
	response *ChatResponse
	latency  float64 // override for scenario-level latency
	prefix   string  // message prefix, e.g. "scenario "
}

// Expect returns an Expectation bound to a ChatResponse.
func Expect(t testing.TB, response *ChatResponse) *Expectation {
	t.Helper()
	if response == nil {
		t.Fatalf("mockagents: Expect called with nil response")
	}
	return &Expectation{t: t, response: response, latency: response.LatencyMs}
}

// ExpectScenario returns an Expectation bound to the last response of a
// ScenarioResult, using the scenario's total latency for latency checks.
func ExpectScenario(t testing.TB, result *ScenarioResult) *Expectation {
	t.Helper()
	if result == nil {
		t.Fatalf("mockagents: ExpectScenario called with nil result")
	}
	last := result.Last()
	if last == nil {
		t.Fatalf("mockagents: scenario %q produced no responses", result.ScenarioName)
	}
	return &Expectation{
		t:        t,
		response: last,
		latency:  result.TotalLatencyMs,
		prefix:   fmt.Sprintf("scenario %q: ", result.ScenarioName),
	}
}

// ToHaveContentContaining asserts the response content includes substring.
func (e *Expectation) ToHaveContentContaining(substring string) *Expectation {
	e.t.Helper()
	if !contains(e.response.Content, substring) {
		e.t.Errorf("%sexpected response to contain %q, got %q",
			e.prefix, substring, truncate(e.response.Content, 120))
	}
	return e
}

// ToHaveFinishReason asserts the finish_reason / stop_reason.
func (e *Expectation) ToHaveFinishReason(reason string) *Expectation {
	e.t.Helper()
	if e.response.FinishReason != reason {
		e.t.Errorf("%sexpected finish_reason=%q, got %q",
			e.prefix, reason, e.response.FinishReason)
	}
	return e
}

// ToHaveStatusCode asserts the HTTP status code.
func (e *Expectation) ToHaveStatusCode(code int) *Expectation {
	e.t.Helper()
	if e.response.StatusCode != code {
		e.t.Errorf("%sexpected status_code=%d, got %d",
			e.prefix, code, e.response.StatusCode)
	}
	return e
}

// ToHaveLatencyLessThanMs asserts the response (or scenario) latency is
// strictly less than ms.
func (e *Expectation) ToHaveLatencyLessThanMs(ms float64) *Expectation {
	e.t.Helper()
	if e.latency >= ms {
		e.t.Errorf("%sexpected latency<%.1fms, got %.1fms",
			e.prefix, ms, e.latency)
	}
	return e
}

// ToHaveToolCallCount asserts the number of tool calls in the response.
func (e *Expectation) ToHaveToolCallCount(count int) *Expectation {
	e.t.Helper()
	if len(e.response.ToolCalls) != count {
		e.t.Errorf("%sexpected %d tool calls, got %d",
			e.prefix, count, len(e.response.ToolCalls))
	}
	return e
}

// ToHaveToolCall asserts the response contains a tool call with the given
// name. When args is non-nil, every key in args must deep-equal the
// matching key on the actual tool call.
func (e *Expectation) ToHaveToolCall(name string, args map[string]any) *Expectation {
	e.t.Helper()
	for _, tc := range e.response.ToolCalls {
		if tc.Name != name {
			continue
		}
		if args == nil || argsMatch(tc.Arguments, args) {
			return e
		}
	}
	e.t.Errorf("%sexpected tool call %q with args %v, got %v",
		e.prefix, name, args, toolCallSummary(e.response.ToolCalls))
	return e
}

// --- helpers ---

func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

// indexOf is a tiny substring search that avoids importing strings just
// for Contains. Written as a linear scan because responses are short.
func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func argsMatch(actual, expected map[string]any) bool {
	for k, v := range expected {
		got, ok := actual[k]
		if !ok || !reflect.DeepEqual(got, v) {
			return false
		}
	}
	return true
}

func toolCallSummary(calls []ToolCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, fmt.Sprintf("%s(%v)", c.Name, c.Arguments))
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
