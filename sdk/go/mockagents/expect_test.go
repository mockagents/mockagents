package mockagents

import (
	"testing"
)

// recordingT is a testing.TB stub that captures Errorf calls without
// actually failing the enclosing test. Used to assert that Expect
// matchers emit the right failure messages.
type recordingT struct {
	testing.TB
	errors []string
}

func (r *recordingT) Errorf(format string, args ...any) {
	r.errors = append(r.errors, format)
}

func (r *recordingT) Helper() {}

func (r *recordingT) Fatalf(format string, args ...any) {
	// For the nil-response guard; we just record it.
	r.errors = append(r.errors, "FATAL:"+format)
}

func sampleResponse() *ChatResponse {
	return &ChatResponse{
		Content: "hello world",
		Model:   "gpt-4o",
		ToolCalls: []ToolCall{
			{ID: "t1", Name: "lookup_order", Arguments: map[string]any{"id": "ORD-1", "region": "us"}},
		},
		FinishReason: "stop",
		StatusCode:   200,
		LatencyMs:    42,
	}
}

func TestExpectChainPasses(t *testing.T) {
	Expect(t, sampleResponse()).
		ToHaveContentContaining("hello").
		ToHaveFinishReason("stop").
		ToHaveStatusCode(200).
		ToHaveLatencyLessThanMs(100).
		ToHaveToolCallCount(1).
		ToHaveToolCall("lookup_order", map[string]any{"id": "ORD-1"})
}

func TestExpectContentMismatch(t *testing.T) {
	rec := &recordingT{}
	Expect(rec, sampleResponse()).ToHaveContentContaining("goodbye")
	if len(rec.errors) == 0 {
		t.Error("expected an error for missing content")
	}
}

func TestExpectToolCallArgsMismatch(t *testing.T) {
	rec := &recordingT{}
	Expect(rec, sampleResponse()).ToHaveToolCall("lookup_order", map[string]any{"id": "ORD-9"})
	if len(rec.errors) == 0 {
		t.Error("expected an error for mismatched args")
	}
}

func TestExpectToolCallAbsent(t *testing.T) {
	rec := &recordingT{}
	resp := sampleResponse()
	resp.ToolCalls = nil
	Expect(rec, resp).ToHaveToolCall("lookup_order", nil)
	if len(rec.errors) == 0 {
		t.Error("expected an error for absent tool call")
	}
}

func TestExpectLatencyExceeded(t *testing.T) {
	rec := &recordingT{}
	resp := sampleResponse()
	resp.LatencyMs = 500
	Expect(rec, resp).ToHaveLatencyLessThanMs(100)
	if len(rec.errors) == 0 {
		t.Error("expected latency error")
	}
}

func TestExpectScenarioUsesLastResponse(t *testing.T) {
	result := &ScenarioResult{
		ScenarioName:   "multi",
		TotalLatencyMs: 10,
		Responses: []*ChatResponse{
			{Content: "first"},
			{Content: "second", FinishReason: "stop", StatusCode: 200},
		},
	}
	ExpectScenario(t, result).
		ToHaveContentContaining("second").
		ToHaveFinishReason("stop").
		ToHaveLatencyLessThanMs(100)
}

func TestExpectScenarioLatencyUsesTotal(t *testing.T) {
	rec := &recordingT{}
	result := &ScenarioResult{
		ScenarioName:   "slow",
		TotalLatencyMs: 500,
		Responses:      []*ChatResponse{{Content: "", LatencyMs: 1}},
	}
	ExpectScenario(rec, result).ToHaveLatencyLessThanMs(100)
	if len(rec.errors) == 0 {
		t.Error("expected scenario latency failure")
	}
}
