package config

import (
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// decodePipelineYAML is a small helper that parses a YAML string into
// a PipelineDefinition + its yaml.Node so tests can exercise the
// line-number-aware validator. Uses the same two-pass decode as
// LoadFile so the Node and struct stay in sync.
func decodePipelineYAML(t *testing.T, src string) (*types.PipelineDefinition, *yaml.Node) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(src), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var def types.PipelineDefinition
	if err := node.Decode(&def); err != nil {
		t.Fatalf("decode pipeline: %v", err)
	}
	return &def, &node
}

func TestValidatePipeline_Valid(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: research
spec:
  topology: sequential
  agents:
    - id: writer
      ref: summary-writer
    - id: reviewer
      ref: fact-checker
`)
	if errs := ValidatePipeline(def, "", node); errs != nil {
		t.Errorf("unexpected errors: %v", errs.Error())
	}
}

func TestValidatePipeline_MissingName(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata: {}
spec:
  topology: sequential
  agents:
    - id: a
      ref: writer
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsField(errs, "metadata.name") {
		t.Errorf("no metadata.name error: %v", errs.Error())
	}
}

func TestValidatePipeline_InvalidTopology(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: star
  agents:
    - id: a
      ref: writer
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsField(errs, "spec.topology") {
		t.Errorf("no topology error: %v", errs.Error())
	}
}

func TestValidatePipeline_EmptyAgents(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: sequential
  agents: []
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsField(errs, "spec.agents") {
		t.Errorf("no agents error: %v", errs.Error())
	}
}

func TestValidatePipeline_DuplicateNodeID(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: sequential
  agents:
    - id: w
      ref: writer
    - id: w
      ref: reviewer
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsMessage(errs, "duplicate") {
		t.Errorf("no duplicate error: %v", errs.Error())
	}
}

// The duplicate-id error is reported on spec.agents[1].id. NodeAt only
// understood dot paths, so that bracket path never resolved and the error
// pointed at 0:0 instead of the offending node.
func TestValidatePipeline_DuplicateNodeIDReportsPosition(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: sequential
  agents:
    - id: w
      ref: writer
    - id: w
      ref: reviewer
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	var dup *ValidationError
	for _, e := range errs.Errors {
		if e.Field == "spec.agents[1].id" {
			dup = e
		}
	}
	if dup == nil {
		t.Fatalf("no spec.agents[1].id error: %v", errs.Error())
	}
	// Line 10 is the second node's "- id: w"; the value starts at column 11.
	if dup.Line != 10 || dup.Column != 11 {
		t.Errorf("duplicate id reported at %d:%d, want 10:11", dup.Line, dup.Column)
	}
}

func TestValidatePipeline_EdgeReferencesUnknownNode(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: graph
  agents:
    - id: a
      ref: writer
    - id: b
      ref: reviewer
  edges:
    - from: a
      to: nope
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsMessage(errs, "unknown node") {
		t.Errorf("no unknown-node error: %v", errs.Error())
	}
}

func TestValidatePipeline_SelfLoop(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: graph
  agents:
    - id: a
      ref: writer
  edges:
    - from: a
      to: a
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsMessage(errs, "self-loop") {
		t.Errorf("no self-loop error: %v", errs.Error())
	}
}

func TestValidatePipeline_EdgesUnderNonGraphTopology(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: sequential
  agents:
    - id: a
      ref: writer
    - id: b
      ref: reviewer
  edges:
    - from: a
      to: b
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsMessage(errs, "only honored under topology") {
		t.Errorf("expected topology warning: %v", errs.Error())
	}
}

func TestValidatePipeline_MissingKind(t *testing.T) {
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
metadata:
  name: x
spec:
  topology: sequential
  agents:
    - id: a
      ref: writer
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected error")
	}
	if !containsField(errs, "kind") {
		t.Errorf("no kind error: %v", errs.Error())
	}
}

// --- Edge polish: when_contains + duplicate edges ---

func TestValidatePipeline_WhitespaceOnlyWhenContains(t *testing.T) {
	// when_contains set to "   " looks like a filter but matches
	// nothing useful — almost always a typo.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
  edges:
    - from: a
      to: b
      when_contains: "   "
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil || !containsMessage(errs, "whitespace-only") {
		t.Errorf("expected whitespace-only error: %v", errs)
	}
}

func TestValidatePipeline_EmptyWhenContainsTolerated(t *testing.T) {
	// An edge with when_contains unset (or explicitly empty) is
	// an unconditional edge — legal. The validator must NOT flag
	// it as whitespace-only.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
  edges:
    - from: a
      to: b
`)
	if errs := ValidatePipeline(def, "", node); errs != nil {
		t.Errorf("unconditional edge should be valid: %v", errs.Error())
	}
}

func TestValidatePipeline_DuplicateUnconditionalEdge(t *testing.T) {
	// Two edges a → b with no guards. Second is redundant.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
  edges:
    - from: a
      to: b
    - from: a
      to: b
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil || !containsMessage(errs, "duplicate edge") {
		t.Errorf("expected duplicate-edge error: %v", errs)
	}
}

func TestValidatePipeline_DuplicateGuardedEdge(t *testing.T) {
	// Two edges a → b both guarded by "foo". Second is redundant.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
  edges:
    - from: a
      to: b
      when_contains: foo
    - from: a
      to: b
      when_contains: foo
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil || !containsMessage(errs, "guarded by \"foo\"") {
		t.Errorf("expected guarded-dup error: %v", errs)
	}
}

func TestValidatePipeline_DistinctGuardsAreValid(t *testing.T) {
	// Two edges a → b with DIFFERENT when_contains substrings are
	// legal — they're parallel guarded paths.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: x
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
  edges:
    - from: a
      to: b
      when_contains: foo
    - from: a
      to: b
      when_contains: bar
`)
	if errs := ValidatePipeline(def, "", node); errs != nil {
		t.Errorf("distinct guards should be valid: %v", errs.Error())
	}
}

// --- Graph topology checks: cycles + unreachable nodes ---

func TestValidatePipeline_GraphDiamondIsValid(t *testing.T) {
	// a → b, a → c, b → d, c → d: classic diamond, no cycle,
	// every node reachable from a.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: diamond
spec:
  topology: graph
  agents:
    - id: a
      ref: writer
    - id: b
      ref: summarizer
    - id: c
      ref: fact-checker
    - id: d
      ref: final
  edges:
    - from: a
      to: b
    - from: a
      to: c
    - from: b
      to: d
    - from: c
      to: d
`)
	if errs := ValidatePipeline(def, "", node); errs != nil {
		t.Errorf("expected valid diamond, got: %v", errs.Error())
	}
}

func TestValidatePipeline_GraphTwoNodeCycle(t *testing.T) {
	// a ↔ b: the smallest non-trivial cycle (self-loops are caught
	// earlier by the edge rules).
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: loop
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
  edges:
    - from: a
      to: b
    - from: b
      to: a
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil {
		t.Fatal("expected cycle error")
	}
	if !containsMessage(errs, "cycle") {
		t.Errorf("no cycle error: %v", errs.Error())
	}
}

func TestValidatePipeline_GraphThreeNodeCycle(t *testing.T) {
	// a → b → c → a
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: triangle
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
    - id: c
      ref: w3
  edges:
    - from: a
      to: b
    - from: b
      to: c
    - from: c
      to: a
`)
	errs := ValidatePipeline(def, "", node)
	if errs == nil || !containsMessage(errs, "cycle") {
		t.Errorf("expected cycle error, got: %v", errs)
	}
}

func TestValidatePipeline_GraphUnreachableNode(t *testing.T) {
	// a → b, then c sits off on its own island.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: island
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
    - id: c
      ref: w3
  edges:
    - from: a
      to: b
    - from: a
      to: c
`)
	// c is reachable from a here; this is valid. We need a real
	// unreachable case — remove the a → c edge.
	def, node = decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: island
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
    - id: c
      ref: w3
  edges:
    - from: a
      to: b
`)
	// c has in-degree 0, so it's a source; still reachable
	// (trivially from itself). This is NOT an error.
	if errs := ValidatePipeline(def, "", node); errs != nil {
		t.Errorf("c is a source, should be reachable: %v", errs.Error())
	}

	// Real unreachable: two disconnected components with explicit
	// incoming edges that point at dangling nodes. The node d has
	// in-degree 1 but its only predecessor is an orphan that's
	// never reached from a source.
	def, node = decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: island
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
    - id: c
      ref: w3
    - id: d
      ref: w4
  edges:
    - from: a
      to: b
    - from: c
      to: d
    - from: d
      to: c
`)
	errs := ValidatePipeline(def, "", node)
	// c ↔ d is a cycle, so cycle detection fires and reachability
	// is skipped. The error list contains a cycle message.
	if errs == nil || !containsMessage(errs, "cycle") {
		t.Errorf("expected cycle error (c↔d), got: %v", errs)
	}
}

func TestValidatePipeline_GraphIsolatedChain(t *testing.T) {
	// Two disconnected chains: a → b, c → d. Both have in-degree-
	// zero sources (a and c), so every node is reachable from
	// some source. Valid.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: two-chains
spec:
  topology: graph
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
    - id: c
      ref: w3
    - id: d
      ref: w4
  edges:
    - from: a
      to: b
    - from: c
      to: d
`)
	if errs := ValidatePipeline(def, "", node); errs != nil {
		t.Errorf("two independent chains should be valid: %v", errs.Error())
	}
}

func TestValidatePipeline_GraphUnreachableSink(t *testing.T) {
	// a → b → c is the only chain. d has one inbound edge from e,
	// and e has one inbound edge from d → d ↔ e is a cycle. But
	// we want the pure "unreachable without cycles" case: d sits
	// at the end of an edge whose source has no inbound edge,
	// which means d IS reachable from e (its source).
	//
	// The genuinely unreachable case without a cycle is hard to
	// construct because any node with a predecessor chain
	// eventually hits a source (in-degree 0 node). The only way
	// to get "unreachable" without a cycle is... you can't. The
	// reachability check is effectively a post-cycle-detection
	// pass that catches the case where the cycle-detection short
	// circuit left a node hanging in a cycle-containing graph.
	//
	// So we exercise the "valid graph with all nodes reachable"
	// path instead — the happy counterpart of the test above.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: fanout
spec:
  topology: graph
  agents:
    - id: root
      ref: w1
    - id: left
      ref: w2
    - id: right
      ref: w3
  edges:
    - from: root
      to: left
    - from: root
      to: right
`)
	if errs := ValidatePipeline(def, "", node); errs != nil {
		t.Errorf("fan-out should be valid: %v", errs.Error())
	}
}

func TestValidatePipeline_GraphChecksSkipNonGraphTopology(t *testing.T) {
	// A sequential pipeline with edges that would form a cycle is
	// already caught by the "edges only under graph" rule. The
	// graph checks should NOT add a second cycle error on top.
	def, node := decodePipelineYAML(t, `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: seq
spec:
  topology: sequential
  agents:
    - id: a
      ref: w1
    - id: b
      ref: w2
  edges:
    - from: a
      to: b
    - from: b
      to: a
`)
	errs := ValidatePipeline(def, "", node)
	// We expect exactly the "edges only honored under graph"
	// error — NOT a cycle error on top.
	if errs == nil {
		t.Fatal("expected error about edges under non-graph topology")
	}
	if containsMessage(errs, "cycle") {
		t.Errorf("cycle check should skip non-graph topology: %v", errs.Error())
	}
}

func containsField(errs *ValidationErrorList, field string) bool {
	for _, e := range errs.Errors {
		if e.Field == field {
			return true
		}
	}
	return false
}

func containsMessage(errs *ValidationErrorList, substr string) bool {
	for _, e := range errs.Errors {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}
