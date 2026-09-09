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
	// toolCalls is what the trajectory assertions read. For a single response
	// it is that response's calls; for a scenario it is the aggregate across
	// every turn, which is what the Python and TypeScript SDKs (and the YAML
	// TestSuite assertions) compare against.
	toolCalls []ToolCall
}

// Expect returns an Expectation bound to a ChatResponse.
func Expect(t testing.TB, response *ChatResponse) *Expectation {
	t.Helper()
	if response == nil {
		t.Fatalf("mockagents: Expect called with nil response")
	}
	return &Expectation{
		t:         t,
		response:  response,
		latency:   response.LatencyMs,
		toolCalls: response.ToolCalls,
	}
}

// ExpectScenario returns an Expectation over a ScenarioResult: outcome checks
// (content, finish reason, status) read the LAST response, while trajectory
// checks (tool calls) read every turn. Latency is the scenario total.
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
		t:         t,
		response:  last,
		latency:   result.TotalLatencyMs,
		prefix:    fmt.Sprintf("scenario %q: ", result.ScenarioName),
		toolCalls: result.ToolCalls(),
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

// ToHaveToolCallCount asserts the total number of tool calls across the whole
// trajectory. This is the `tool_call_count` assertion of `kind: TestSuite`
// YAML, so a check written here transfers to a YAML suite unchanged.
func (e *Expectation) ToHaveToolCallCount(count int) *Expectation {
	e.t.Helper()
	if len(e.toolCalls) != count {
		e.t.Errorf("%sexpected %d tool calls, got %d %v",
			e.prefix, count, len(e.toolCalls), toolCallNames(e.toolCalls))
	}
	return e
}

// ToHaveToolCall asserts a tool call with the given name happened anywhere in
// the trajectory. When args is non-nil, every key in args must deep-equal the
// matching key on the actual tool call.
func (e *Expectation) ToHaveToolCall(name string, args map[string]any) *Expectation {
	e.t.Helper()
	for _, tc := range e.toolCalls {
		if tc.Name != name {
			continue
		}
		if args == nil || argsMatch(tc.Arguments, args) {
			return e
		}
	}
	e.t.Errorf("%sexpected tool call %q with args %v, got %v",
		e.prefix, name, args, toolCallSummary(e.toolCalls))
	return e
}

// ToHaveToolCallSequence asserts the exact ordered sequence of tool-call names
// across the whole trajectory, compared for FULL equality rather than as a
// subsequence — an unexpected extra call fails it.
//
// This is the `tool_call_sequence` assertion of `kind: TestSuite` YAML and the
// counterpart of the Python and TypeScript SDKs' matcher of the same name; the
// Go SDK previously had no equivalent (audit M-38).
func (e *Expectation) ToHaveToolCallSequence(names []string) *Expectation {
	e.t.Helper()
	got := toolCallNames(e.toolCalls)
	if len(got) != len(names) {
		e.t.Errorf("%sexpected tool call sequence %v, got %v", e.prefix, names, got)
		return e
	}
	for i := range names {
		if got[i] != names[i] {
			e.t.Errorf("%sexpected tool call sequence %v, got %v", e.prefix, names, got)
			return e
		}
	}
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

// toolCallNames extracts the ordered names, for sequence comparison and for
// failure messages that say what actually happened.
func toolCallNames(calls []ToolCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Name)
	}
	return out
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
