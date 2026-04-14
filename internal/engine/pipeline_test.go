package engine

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

// newPipelineTestEngine builds an engine pre-populated with a set of agents.
// Each agent has a single default scenario that echoes its name followed by
// the incoming message, which makes it trivial to assert topology behavior.
func newPipelineTestEngine(t *testing.T, agentNames ...string) *Engine {
	t.Helper()
	reg := NewAgentRegistry()
	for _, name := range agentNames {
		reg.Register(&types.AgentDefinition{
			APIVersion: types.AgentAPIVersion,
			Kind:       types.AgentKind,
			Metadata:   types.Metadata{Name: name},
			Spec: types.AgentSpec{
				Protocol: "openai-chat-completions",
				Model:    name + "-model",
				Behavior: types.BehaviorConfig{
					Scenarios: []types.Scenario{
						{
							Name:     "default",
							Response: types.ScenarioResponse{Content: name + ":{{.Message}}"},
						},
					},
				},
			},
		})
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL), logger)
}

func TestPipelineSequential(t *testing.T) {
	eng := newPipelineTestEngine(t, "alpha", "beta", "gamma")
	exec := NewPipelineExecutor(eng)

	def := &types.PipelineDefinition{
		APIVersion: types.AgentAPIVersion,
		Kind:       types.PipelineKind,
		Metadata:   types.Metadata{Name: "seq-pipeline"},
		Spec: types.PipelineSpec{
			Topology: types.TopologySequential,
			Agents: []types.PipelineAgent{
				{ID: "a", Ref: "alpha"},
				{ID: "b", Ref: "beta"},
				{ID: "c", Ref: "gamma"},
			},
		},
	}

	res, err := exec.Run(def, "hello", "session-1")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Nodes) != 3 {
		t.Fatalf("expected 3 node results, got %d", len(res.Nodes))
	}
	// alpha sees "hello"; beta sees alpha's output; gamma sees beta's output.
	if got := res.Nodes[0].Response.Content; got != "alpha:hello" {
		t.Errorf("node 0 content = %q, want %q", got, "alpha:hello")
	}
	if got := res.Nodes[1].Response.Content; got != "beta:alpha:hello" {
		t.Errorf("node 1 content = %q, want %q", got, "beta:alpha:hello")
	}
	if got := res.Nodes[2].Response.Content; got != "gamma:beta:alpha:hello" {
		t.Errorf("node 2 content = %q, want %q", got, "gamma:beta:alpha:hello")
	}
	if res.FinalResponse().Content != "gamma:beta:alpha:hello" {
		t.Errorf("unexpected final response: %q", res.FinalResponse().Content)
	}
}

func TestPipelineParallel(t *testing.T) {
	eng := newPipelineTestEngine(t, "alpha", "beta", "gamma")
	exec := NewPipelineExecutor(eng)

	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "par-pipeline"},
		Spec: types.PipelineSpec{
			Topology: types.TopologyParallel,
			Agents: []types.PipelineAgent{
				{ID: "a", Ref: "alpha"},
				{ID: "b", Ref: "beta"},
				{ID: "c", Ref: "gamma"},
			},
		},
	}

	res, err := exec.Run(def, "ping", "session-2")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Nodes) != 3 {
		t.Fatalf("expected 3 node results, got %d", len(res.Nodes))
	}
	// All three nodes should have seen the same input.
	for _, n := range res.Nodes {
		if !strings.HasSuffix(n.Response.Content, ":ping") {
			t.Errorf("node %q did not receive root input: %q", n.NodeID, n.Response.Content)
		}
	}
}

func TestPipelineGraphWithConditionalEdge(t *testing.T) {
	// router emits "route-to-b" which matches one edge and not the other.
	reg := NewAgentRegistry()
	reg.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "router"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "route",
					Response: types.ScenarioResponse{Content: "route-to-b"},
				}},
			},
		},
	})
	reg.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "worker-a"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "a",
					Response: types.ScenarioResponse{Content: "a-ran"},
				}},
			},
		},
	})
	reg.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "worker-b"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "b",
					Response: types.ScenarioResponse{Content: "b-ran"},
				}},
			},
		},
	})

	eng := NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	exec := NewPipelineExecutor(eng)

	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "graph-pipeline"},
		Spec: types.PipelineSpec{
			Topology: types.TopologyGraph,
			Agents: []types.PipelineAgent{
				{ID: "r", Ref: "router"},
				{ID: "a", Ref: "worker-a"},
				{ID: "b", Ref: "worker-b"},
			},
			Edges: []types.PipelineEdge{
				{From: "r", To: "a", When: "route-to-a"},
				{From: "r", To: "b", When: "route-to-b"},
			},
		},
	}

	res, err := exec.Run(def, "go", "session-3")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// worker-a should have been skipped.
	if res.ResponseByNodeID("a") != nil {
		t.Errorf("worker-a should not have run")
	}
	if res.ResponseByNodeID("b") == nil {
		t.Fatal("worker-b should have run")
	}
	if got := res.ResponseByNodeID("b").Content; got != "b-ran" {
		t.Errorf("worker-b content = %q, want %q", got, "b-ran")
	}
}
