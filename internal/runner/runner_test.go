package runner

import (
	"io"
	"log/slog"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

func newEngineWithAgent(t *testing.T, def *types.AgentDefinition) *engine.Engine {
	t.Helper()
	reg := engine.NewAgentRegistry()
	reg.Register(def)
	return engine.NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunSuiteAgentTargetPassing(t *testing.T) {
	agent := &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "support"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Tools: []types.ToolDefinition{{
				Name:       "lookup_order",
				Parameters: types.JSONSchemaObject{"type": "object"},
			}},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:  "order",
					Match: &types.MatchRule{ContentContains: "order"},
					Response: types.ScenarioResponse{
						Content: "checking shipment status",
						ToolCalls: []types.ToolCallSpec{{
							Name:      "lookup_order",
							Arguments: map[string]any{"id": "ORD-1"},
						}},
					},
				}},
			},
		},
	}
	eng := newEngineWithAgent(t, agent)
	r := New(eng, nil)

	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "support-suite"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Agent: "support"},
			Cases: []types.TestCase{{
				Name: "order-status",
				Steps: []types.TestStep{{
					Role: "user", Content: "what is my order status?",
				}},
				Assertions: []types.TestAssertion{
					{Type: types.AssertResponseContains, Value: "shipment"},
					{Type: types.AssertScenarioMatched, Value: "order"},
					{Type: types.AssertToolCall, Tool: "lookup_order",
						Args: map[string]any{"id": "ORD-1"}},
					{Type: types.AssertLatencyMsLT, MaxMs: 5000},
				},
			}},
		},
	}

	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Failed != 0 {
		t.Fatalf("expected 0 failures, got %d: %+v", res.Failed, res.Cases[0].Failures)
	}
	if res.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", res.Passed)
	}
}

func TestRunSuiteAssertionFailures(t *testing.T) {
	agent := &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "plain"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "default",
					Response: types.ScenarioResponse{Content: "hello"},
				}},
			},
		},
	}
	eng := newEngineWithAgent(t, agent)
	r := New(eng, nil)

	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "bad-suite"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Agent: "plain"},
			Cases: []types.TestCase{{
				Name:  "will-fail",
				Steps: []types.TestStep{{Role: "user", Content: "hi"}},
				Assertions: []types.TestAssertion{
					{Type: types.AssertResponseContains, Value: "goodbye"},
					{Type: types.AssertToolCall, Tool: "nonexistent"},
				},
			}},
		},
	}

	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("expected 1 failure, got %d", res.Failed)
	}
	if len(res.Cases[0].Failures) != 2 {
		t.Errorf("expected 2 assertion failures, got %d: %v",
			len(res.Cases[0].Failures), res.Cases[0].Failures)
	}
}

func TestRunSuitePipelineTarget(t *testing.T) {
	reg := engine.NewAgentRegistry()
	reg.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "summarizer"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "summarize",
					Response: types.ScenarioResponse{Content: "summary ready"},
				}},
			},
		},
	})
	reg.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "researcher"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "research",
					Response: types.ScenarioResponse{Content: "research done"},
				}},
			},
		},
	})
	eng := engine.NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	pipeReg := engine.NewPipelineRegistry()
	pipeReg.Register(&types.PipelineDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.PipelineKind,
		Metadata: types.Metadata{Name: "research-flow"},
		Spec: types.PipelineSpec{
			Topology: types.TopologySequential,
			Agents: []types.PipelineAgent{
				{ID: "r", Ref: "researcher"},
				{ID: "s", Ref: "summarizer"},
			},
		},
	})

	r := New(eng, pipeReg)
	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "pipe-suite"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Pipeline: "research-flow"},
			Cases: []types.TestCase{{
				Name:  "summary",
				Steps: []types.TestStep{{Role: "user", Content: "topic"}},
				Assertions: []types.TestAssertion{
					{Type: types.AssertResponseContains, Value: "summary"},
					{Type: types.AssertResponseContains, Value: "research", NodeID: "r"},
				},
			}},
		},
	}

	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Failed != 0 {
		t.Fatalf("expected 0 failures, got %d: %+v", res.Failed, res.Cases[0].Failures)
	}
}

func intPtr(n int) *int { return &n }

// multiToolAgent emits two ordered tool calls (alpha then beta) so the
// trajectory assertions have something to check.
func multiToolAgent() *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "planner"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Tools: []types.ToolDefinition{
				{Name: "alpha", Parameters: types.JSONSchemaObject{"type": "object"}},
				{Name: "beta", Parameters: types.JSONSchemaObject{"type": "object"}},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name: "plan",
					Response: types.ScenarioResponse{
						Content: "planning",
						ToolCalls: []types.ToolCallSpec{
							{Name: "alpha", Arguments: map[string]any{"step": 1}},
							{Name: "beta", Arguments: map[string]any{"step": 2}},
						},
					},
				}},
			},
		},
	}
}

func TestRunSuite_ToolCallCountAndSequence(t *testing.T) {
	r := New(newEngineWithAgent(t, multiToolAgent()), nil)
	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "trajectory"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Agent: "planner"},
			Cases: []types.TestCase{
				{
					Name:  "pass",
					Steps: []types.TestStep{{Role: "user", Content: "go"}},
					Assertions: []types.TestAssertion{
						{Type: types.AssertToolCallCount, Count: intPtr(2)},
						{Type: types.AssertToolCallSequence, Sequence: []string{"alpha", "beta"}},
					},
				},
				{
					Name:  "fail",
					Steps: []types.TestStep{{Role: "user", Content: "go"}},
					Assertions: []types.TestAssertion{
						{Type: types.AssertToolCallCount, Count: intPtr(1)},              // got 2
						{Type: types.AssertToolCallSequence, Sequence: []string{"beta"}}, // wrong order/len
						{Type: types.AssertToolCallCount},                                // missing count
					},
				},
			},
		},
	}
	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Passed != 1 || res.Failed != 1 {
		t.Fatalf("expected 1 pass / 1 fail, got %d / %d", res.Passed, res.Failed)
	}
	if got := len(res.Cases[1].Failures); got != 3 {
		t.Errorf("expected 3 assertion failures in the fail case, got %d: %v", got, res.Cases[1].Failures)
	}
}

func TestRunSuite_NodeSequence(t *testing.T) {
	reg := engine.NewAgentRegistry()
	for _, name := range []string{"researcher", "summarizer"} {
		reg.Register(&types.AgentDefinition{
			APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
			Metadata: types.Metadata{Name: name},
			Spec: types.AgentSpec{
				Protocol: "openai-chat-completions",
				Behavior: types.BehaviorConfig{
					Scenarios: []types.Scenario{{Name: "s", Response: types.ScenarioResponse{Content: name + " done"}}},
				},
			},
		})
	}
	eng := engine.NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	pipeReg := engine.NewPipelineRegistry()
	pipeReg.Register(&types.PipelineDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.PipelineKind,
		Metadata: types.Metadata{Name: "flow"},
		Spec: types.PipelineSpec{
			Topology: types.TopologySequential,
			Agents:   []types.PipelineAgent{{ID: "r", Ref: "researcher"}, {ID: "s", Ref: "summarizer"}},
		},
	})
	r := New(eng, pipeReg)

	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "node-seq"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Pipeline: "flow"},
			Cases: []types.TestCase{
				{
					Name:       "right-order",
					Steps:      []types.TestStep{{Role: "user", Content: "x"}},
					Assertions: []types.TestAssertion{{Type: types.AssertNodeSequence, Sequence: []string{"r", "s"}}},
				},
				{
					Name:       "wrong-order",
					Steps:      []types.TestStep{{Role: "user", Content: "x"}},
					Assertions: []types.TestAssertion{{Type: types.AssertNodeSequence, Sequence: []string{"s", "r"}}},
				},
			},
		},
	}
	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Passed != 1 || res.Failed != 1 {
		t.Fatalf("expected 1 pass / 1 fail, got %d / %d", res.Passed, res.Failed)
	}
}

func TestRunSuite_NodeSequenceRequiresPipeline(t *testing.T) {
	r := New(newEngineWithAgent(t, multiToolAgent()), nil)
	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "misuse"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Agent: "planner"},
			Cases: []types.TestCase{{
				Name:       "agent-target",
				Steps:      []types.TestStep{{Role: "user", Content: "go"}},
				Assertions: []types.TestAssertion{{Type: types.AssertNodeSequence, Sequence: []string{"r"}}},
			}},
		},
	}
	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("node_sequence on an agent target should fail, got %d failures", res.Failed)
	}
}

func TestEvaluateAssertion_CheapBehavioral(t *testing.T) {
	withTools := &engine.Response{
		Content:   "I'll look that up.",
		ToolCalls: []types.ToolCallSpec{{Name: "search"}},
	}
	noTools := &engine.Response{Content: "Order #4131 ships Tuesday."}
	refused := &engine.Response{Refusal: "I can't help with that request."}

	cases := []struct {
		name      string
		assertion types.TestAssertion
		resp      *engine.Response
		wantPass  bool
	}{
		{"no_tool_call passes when clean", types.TestAssertion{Type: types.AssertNoToolCall}, noTools, true},
		{"no_tool_call fails on a call", types.TestAssertion{Type: types.AssertNoToolCall}, withTools, false},
		{"refusal passes when refused", types.TestAssertion{Type: types.AssertRefusal}, refused, true},
		{"refusal fails when not refused", types.TestAssertion{Type: types.AssertRefusal}, noTools, false},
		{"refusal value substring passes", types.TestAssertion{Type: types.AssertRefusal, Value: "can't help"}, refused, true},
		{"refusal value substring fails", types.TestAssertion{Type: types.AssertRefusal, Value: "policy"}, refused, false},
		{"response_matches passes", types.TestAssertion{Type: types.AssertResponseMatches, Value: `#\d+ ships`}, noTools, true},
		{"response_matches fails", types.TestAssertion{Type: types.AssertResponseMatches, Value: `^cancelled`}, noTools, false},
		{"response_matches bad regex fails", types.TestAssertion{Type: types.AssertResponseMatches, Value: `[`}, noTools, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ec := &evalContext{final: c.resp, toolCalls: c.resp.ToolCalls}
			msg := evaluateAssertion(c.assertion, ec)
			if c.wantPass && msg != "" {
				t.Errorf("expected pass, got failure: %s", msg)
			}
			if !c.wantPass && msg == "" {
				t.Errorf("expected failure, got pass")
			}
		})
	}
}

// TestRunSuite_MultiTurnTrajectory proves the runner replays every user step as
// a turn in one session and aggregates the trajectory: tool_call_sequence /
// tool_call_count span all turns, while outcome assertions (response_contains,
// scenario_matched) read the final turn.
func TestRunSuite_MultiTurnTrajectory(t *testing.T) {
	agent := &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "travel"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Tools: []types.ToolDefinition{
				{Name: "search_flights", Parameters: types.JSONSchemaObject{"type": "object"}},
				{Name: "book_flight", Parameters: types.JSONSchemaObject{"type": "object"}},
			},
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "search",
						Match: &types.MatchRule{ContentContains: "search"},
						Response: types.ScenarioResponse{
							Content:   "here are some flights",
							ToolCalls: []types.ToolCallSpec{{Name: "search_flights"}},
						},
					},
					{
						Name:  "book",
						Match: &types.MatchRule{ContentContains: "book"},
						Response: types.ScenarioResponse{
							Content:   "your flight is booked",
							ToolCalls: []types.ToolCallSpec{{Name: "book_flight"}},
						},
					},
				},
			},
		},
	}
	eng := newEngineWithAgent(t, agent)
	r := New(eng, nil)

	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "travel-suite"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Agent: "travel"},
			Cases: []types.TestCase{{
				Name: "search-then-book",
				Steps: []types.TestStep{
					{Role: "user", Content: "search for flights to NYC"},
					{Role: "user", Content: "book the first one"},
				},
				Assertions: []types.TestAssertion{
					// Trajectory: spans BOTH turns — the whole point of multi-turn.
					{Type: types.AssertToolCallSequence, Sequence: []string{"search_flights", "book_flight"}},
					{Type: types.AssertToolCallCount, Count: intPtr(2)},
					// Outcome: reads the final turn only.
					{Type: types.AssertResponseContains, Value: "booked"},
					{Type: types.AssertScenarioMatched, Value: "book"},
				},
			}},
		},
	}

	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Failed != 0 {
		t.Fatalf("expected 0 failures, got %d: %+v", res.Failed, res.Cases[0].Failures)
	}
}

// TestRunSuite_MultiTurnSessionTurnCount proves the session turn count advances
// across replayed steps: a turn_number-scoped scenario only fires on its turn.
func TestRunSuite_MultiTurnSessionTurnCount(t *testing.T) {
	turn2 := 2
	agent := &types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "counter"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:     "second-turn",
						Match:    &types.MatchRule{TurnNumber: &turn2},
						Response: types.ScenarioResponse{Content: "this is turn two"},
					},
					{
						Name:     "default",
						Match:    &types.MatchRule{ContentContains: "ping"},
						Response: types.ScenarioResponse{Content: "this is some other turn"},
					},
				},
			},
		},
	}
	eng := newEngineWithAgent(t, agent)
	r := New(eng, nil)

	suite := &types.TestSuiteDefinition{
		Metadata: types.Metadata{Name: "counter-suite"},
		Spec: types.TestSuiteSpec{
			Target: types.TestTarget{Agent: "counter"},
			Cases: []types.TestCase{{
				Name: "turn-aware",
				Steps: []types.TestStep{
					{Role: "user", Content: "ping"},
					{Role: "user", Content: "ping"},
				},
				// The final (2nd) turn must have matched the turn_number:2 scenario,
				// which only happens if the session turn count advanced across steps.
				Assertions: []types.TestAssertion{
					{Type: types.AssertScenarioMatched, Value: "second-turn"},
					{Type: types.AssertResponseContains, Value: "turn two"},
				},
			}},
		},
	}

	res, err := r.RunSuite(suite)
	if err != nil {
		t.Fatalf("RunSuite error: %v", err)
	}
	if res.Failed != 0 {
		t.Fatalf("expected 0 failures, got %d: %+v", res.Failed, res.Cases[0].Failures)
	}
}
