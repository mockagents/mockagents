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
