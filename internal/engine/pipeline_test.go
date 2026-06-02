package engine

import (
	"context"
	"errors"
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
				{From: "r", To: "a", WhenContains: "route-to-a"},
				{From: "r", To: "b", WhenContains: "route-to-b"},
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

// TestPipelineGraphWhenContainsIsSubstring locks the F-PL-010 contract:
// edge.WhenContains is a SUBSTRING guard, not exact equality. The router
// echoes "router:another"; the edge guard "no" appears inside that string
// (a-no-ther) but is not equal to it, so substring semantics fire the edge
// while equality semantics would prune it. A second edge guarded by a
// string that is absent must be pruned. This pins the behavior so a future
// "tighten to equality" change is caught.
func TestPipelineGraphWhenContainsIsSubstring(t *testing.T) {
	eng := newPipelineTestEngine(t, "router", "worker-a", "worker-b")
	exec := NewPipelineExecutor(eng)

	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "substring-guard"},
		Spec: types.PipelineSpec{
			Topology: types.TopologyGraph,
			Agents: []types.PipelineAgent{
				{ID: "r", Ref: "router"},
				{ID: "a", Ref: "worker-a"},
				{ID: "b", Ref: "worker-b"},
			},
			Edges: []types.PipelineEdge{
				// "router:another" contains "no" but does not equal it.
				{From: "r", To: "a", WhenContains: "no"},
				// "router:another" does not contain "zzz".
				{From: "r", To: "b", WhenContains: "zzz"},
			},
		},
	}

	res, err := exec.Run(def, "another", "session-substr")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ResponseByNodeID("a") == nil {
		t.Error("worker-a should have run: substring guard \"no\" matches \"router:another\"")
	}
	if res.ResponseByNodeID("b") != nil {
		t.Error("worker-b should have been pruned: \"zzz\" is not a substring of \"router:another\"")
	}
}

// TestPipelineGraphMultiRoot covers F-PL-004: a graph with more than one
// zero-incoming root must visit every node, not just the first root's
// reachable subgraph. The old executor walked a single start node, so r2
// and its descendant b were silently dropped from res.Nodes.
func TestPipelineGraphMultiRoot(t *testing.T) {
	eng := newPipelineTestEngine(t, "alpha", "beta", "gamma", "delta")
	exec := NewPipelineExecutor(eng)

	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "multiroot"},
		Spec: types.PipelineSpec{
			Topology: types.TopologyGraph,
			Agents: []types.PipelineAgent{
				{ID: "r1", Ref: "alpha"},
				{ID: "r2", Ref: "beta"},
				{ID: "a", Ref: "gamma"},
				{ID: "b", Ref: "delta"},
			},
			Edges: []types.PipelineEdge{
				{From: "r1", To: "a"},
				{From: "r2", To: "b"},
			},
		},
	}

	res, err := exec.Run(def, "hello", "session-mr")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Nodes) != 4 {
		t.Fatalf("expected all 4 nodes to run, got %d: %+v", len(res.Nodes), res.Nodes)
	}
	for _, id := range []string{"r1", "r2", "a", "b"} {
		if res.ResponseByNodeID(id) == nil {
			t.Errorf("node %q was dropped (not executed)", id)
		}
	}
	// Roots see the pipeline input; descendants see their root's output.
	if got := res.ResponseByNodeID("a").Content; got != "gamma:alpha:hello" {
		t.Errorf("node a content = %q, want %q", got, "gamma:alpha:hello")
	}
	if got := res.ResponseByNodeID("b").Content; got != "delta:beta:hello" {
		t.Errorf("node b content = %q, want %q", got, "delta:beta:hello")
	}
}

// TestPipelineGraphCycleErrors covers F-PL-005: a fully cyclic graph has no
// zero-incoming node. The old executor fell back to Agents[0] as the start
// and let `visited` truncate the walk, running a partial subset and
// reporting success. It must now error.
func TestPipelineGraphCycleErrors(t *testing.T) {
	eng := newPipelineTestEngine(t, "alpha", "beta")
	exec := NewPipelineExecutor(eng)

	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "cyclic"},
		Spec: types.PipelineSpec{
			Topology: types.TopologyGraph,
			Agents: []types.PipelineAgent{
				{ID: "a", Ref: "alpha"},
				{ID: "b", Ref: "beta"},
			},
			Edges: []types.PipelineEdge{
				{From: "a", To: "b"},
				{From: "b", To: "a"},
			},
		},
	}

	res, err := exec.Run(def, "hello", "session-cyc")
	if !errors.Is(err, ErrPipelineCycle) {
		t.Fatalf("expected ErrPipelineCycle, got err=%v", err)
	}
	if len(res.Nodes) != 0 {
		t.Errorf("cycle must abort before executing nodes, ran %d", len(res.Nodes))
	}
}

// TestPipelineGraphCycleWithFeederErrors covers F-PL-003/F-PL-005 together:
// a graph that *does* have a valid root (s) but also contains a downstream
// cycle (a<->b). The old code started at the root and silently stopped when
// `visited` blocked the back-edge, hiding the cycle. It must now error.
func TestPipelineGraphCycleWithFeederErrors(t *testing.T) {
	eng := newPipelineTestEngine(t, "src", "alpha", "beta")
	exec := NewPipelineExecutor(eng)

	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "feeder-cycle"},
		Spec: types.PipelineSpec{
			Topology: types.TopologyGraph,
			Agents: []types.PipelineAgent{
				{ID: "s", Ref: "src"},
				{ID: "a", Ref: "alpha"},
				{ID: "b", Ref: "beta"},
			},
			Edges: []types.PipelineEdge{
				{From: "s", To: "a"},
				{From: "a", To: "b"},
				{From: "b", To: "a"},
			},
		},
	}

	_, err := exec.Run(def, "hello", "session-fc")
	if !errors.Is(err, ErrPipelineCycle) {
		t.Fatalf("expected ErrPipelineCycle, got err=%v", err)
	}
}

// TestPipelineParallel_PartialResultsAndJoinedErrors covers F-PL-006: a
// parallel run returns every node that succeeded alongside a combined error
// of all the failures (not just the first one).
func TestPipelineParallel_PartialResultsAndJoinedErrors(t *testing.T) {
	eng := newPipelineTestEngine(t, "good")
	exec := NewPipelineExecutor(eng)
	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "par"},
		Spec: types.PipelineSpec{
			Topology: types.TopologyParallel,
			Agents: []types.PipelineAgent{
				{ID: "ok", Ref: "good"},
				{ID: "bad1", Ref: ""}, // missing ref -> error
				{ID: "bad2", Ref: ""}, // missing ref -> error
			},
		},
	}

	res, err := exec.Run(def, "hi", "s")
	if err == nil {
		t.Fatal("expected an error from the failing nodes")
	}
	// Partial result: the successful node is still present despite the errors.
	if res.ResponseByNodeID("ok") == nil {
		t.Error("successful node 'ok' should be in the result alongside the error")
	}
	// Joined errors: BOTH failures are surfaced, not just the first.
	if msg := err.Error(); !strings.Contains(msg, "bad1") || !strings.Contains(msg, "bad2") {
		t.Errorf("joined error should mention both bad1 and bad2, got: %v", msg)
	}
}

// TestPipelineRunContext_HonorsCancellation covers F-PL-009: a cancelled
// context aborts the run via the per-node engine call (ProcessRequestContext).
func TestPipelineRunContext_HonorsCancellation(t *testing.T) {
	eng := newPipelineTestEngine(t, "a", "b")
	exec := NewPipelineExecutor(eng)
	def := &types.PipelineDefinition{
		Metadata: types.Metadata{Name: "seq"},
		Spec: types.PipelineSpec{
			Topology: types.TopologySequential,
			Agents:   []types.PipelineAgent{{ID: "1", Ref: "a"}, {ID: "2", Ref: "b"}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the run starts

	if _, err := exec.RunContext(ctx, def, "hi", "s"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
