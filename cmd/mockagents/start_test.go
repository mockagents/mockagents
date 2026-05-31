package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
)

// quietLogger discards output so the registerPipelines warnings don't
// clutter test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func validPipeline(name string) *types.PipelineDefinition {
	return &types.PipelineDefinition{
		APIVersion: types.AgentAPIVersion,
		Kind:       types.PipelineKind,
		Metadata:   types.Metadata{Name: name},
		Spec: types.PipelineSpec{
			Topology: types.TopologyGraph,
			Agents: []types.PipelineAgent{
				{ID: "a", Ref: "agent-a"},
				{ID: "b", Ref: "agent-b"},
			},
			Edges: []types.PipelineEdge{
				{From: "a", To: "b"},
			},
		},
	}
}

// cyclicPipeline is structurally valid except for a graph cycle
// (a → b → a), which only the graph-pass cycle detector in
// config.ValidatePipeline catches — never the bare registry Register.
func cyclicPipeline(name string) *types.PipelineDefinition {
	def := validPipeline(name)
	def.Spec.Edges = []types.PipelineEdge{
		{From: "a", To: "b"},
		{From: "b", To: "a"},
	}
	return def
}

// TestRegisterPipelines_SkipsCyclic is the X-07 regression test: a
// cyclic pipeline definition must be rejected (logged + skipped) at
// server start, not just by `mockagents validate`. Before the fix,
// start.go registered every pipeline unconditionally, so the cyclic
// one would have been registered and only blown up later in the
// executor.
func TestRegisterPipelines_SkipsCyclic(t *testing.T) {
	pipelines := []*config.PipelineLoadResult{
		{Definition: validPipeline("good"), FilePath: "good.yaml"},
		{Definition: cyclicPipeline("bad-cycle"), FilePath: "bad.yaml"},
	}

	reg := registerPipelines(pipelines, quietLogger())

	if got := reg.Count(); got != 1 {
		t.Fatalf("expected 1 valid pipeline registered, got %d", got)
	}
	if reg.GetPipeline("good") == nil {
		t.Errorf("valid pipeline %q should be registered", "good")
	}
	if reg.GetPipeline("bad-cycle") != nil {
		t.Errorf("cyclic pipeline %q must be skipped, but it was registered", "bad-cycle")
	}
}

// TestRegisterPipelines_SkipsNilAndAllValid covers the nil-guard and
// the happy path: nil entries are skipped, and a directory of only
// valid pipelines registers them all.
func TestRegisterPipelines_SkipsNilAndAllValid(t *testing.T) {
	pipelines := []*config.PipelineLoadResult{
		nil,
		{Definition: nil, FilePath: "empty.yaml"},
		{Definition: validPipeline("one"), FilePath: "one.yaml"},
		{Definition: validPipeline("two"), FilePath: "two.yaml"},
	}

	reg := registerPipelines(pipelines, quietLogger())

	if got := reg.Count(); got != 2 {
		t.Fatalf("expected 2 valid pipelines registered, got %d", got)
	}
}
