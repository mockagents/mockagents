package mockagents

import "testing"

// multiTurnResult is a two-turn scenario whose tool calls are spread across
// turns — the shape the aggregate assertions exist for.
func multiTurnResult() *ScenarioResult {
	return &ScenarioResult{
		ScenarioName:   "support",
		TotalLatencyMs: 30,
		Responses: []*ChatResponse{
			{
				Content: "checking the weather",
				ToolCalls: []ToolCall{
					{ID: "t1", Name: "get_weather", Arguments: map[string]any{"city": "London"}},
				},
			},
			{
				Content:      "and your order",
				FinishReason: "stop",
				StatusCode:   200,
				ToolCalls: []ToolCall{
					{ID: "t2", Name: "search_orders", Arguments: map[string]any{"id": "ORD-1"}},
				},
			},
		},
	}
}

// TestScenarioToolCallsAggregate is the audit M-38 guard: the Go SDK bound its
// tool-call assertions to the LAST response, so a check that passed in Python
// and TypeScript could silently pass here for the wrong reason (or fail).
func TestScenarioToolCallsAggregate(t *testing.T) {
	calls := multiTurnResult().ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("ToolCalls() returned %d calls, want 2 across both turns", len(calls))
	}
	if calls[0].Name != "get_weather" || calls[1].Name != "search_orders" {
		t.Errorf("calls are out of invocation order: %v", toolCallNames(calls))
	}
}

func TestExpectScenarioCountsToolCallsAcrossTurns(t *testing.T) {
	ExpectScenario(t, multiTurnResult()).
		ToHaveToolCallCount(2).
		ToHaveToolCall("get_weather", map[string]any{"city": "London"}).
		ToHaveToolCall("search_orders", nil).
		ToHaveToolCallSequence([]string{"get_weather", "search_orders"})
}

func TestExpectScenarioToolCallCountMismatchReports(t *testing.T) {
	rec := &recordingT{}
	// 1 was the old (last-response-only) answer; the aggregate is 2.
	ExpectScenario(rec, multiTurnResult()).ToHaveToolCallCount(1)
	if len(rec.errors) != 1 {
		t.Fatalf("expected one failure, got %d", len(rec.errors))
	}
}

func TestExpectScenarioSequenceIsFullEquality(t *testing.T) {
	rec := &recordingT{}
	// A prefix is not a match: an unexpected extra call must fail.
	ExpectScenario(rec, multiTurnResult()).ToHaveToolCallSequence([]string{"get_weather"})
	if len(rec.errors) != 1 {
		t.Fatalf("expected a sequence failure for a prefix, got %d", len(rec.errors))
	}

	rec2 := &recordingT{}
	// Order matters.
	ExpectScenario(rec2, multiTurnResult()).
		ToHaveToolCallSequence([]string{"search_orders", "get_weather"})
	if len(rec2.errors) != 1 {
		t.Fatalf("expected a sequence failure for wrong order, got %d", len(rec2.errors))
	}
}

func TestExpectSingleResponseStillReadsItsOwnCalls(t *testing.T) {
	Expect(t, sampleResponse()).
		ToHaveToolCallCount(1).
		ToHaveToolCallSequence([]string{"lookup_order"})
}

func TestScenarioToolCallsSkipsNilResponses(t *testing.T) {
	result := &ScenarioResult{Responses: []*ChatResponse{nil, {ToolCalls: []ToolCall{{Name: "a"}}}}}
	if got := len(result.ToolCalls()); got != 1 {
		t.Errorf("ToolCalls() = %d calls, want 1", got)
	}
}
