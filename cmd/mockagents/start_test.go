package main

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/mockagents/mockagents/internal/vector"
)

func TestPrintStartBanner(t *testing.T) {
	var buf bytes.Buffer
	printStartBanner(&buf, "0.0.0.0", 8080) // 0.0.0.0 must display as localhost
	out := buf.String()
	for _, want := range []string{
		"export OPENAI_BASE_URL=http://localhost:8080/v1",
		"export OPENAI_API_KEY=mock",
		"ANTHROPIC_BASE_URL=http://localhost:8080",
		"http://localhost:8080/v1beta/models/<model>:generateContent",
		"http://localhost:8080/api/v1/health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("banner missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "0.0.0.0") {
		t.Error("banner should not advertise the unroutable 0.0.0.0 bind address")
	}
}

func TestPrintStartBanner_HostDisplay(t *testing.T) {
	cases := []struct{ host, want string }{
		{"", "http://localhost:9090"},
		{"0.0.0.0", "http://localhost:9090"},
		{"::", "http://localhost:9090"},
		{"127.0.0.1", "http://127.0.0.1:9090"},     // explicit host echoed verbatim
		{"example.com", "http://example.com:9090"}, // hostname echoed verbatim
		{"::1", "http://[::1]:9090"},               // IPv6 literal bracketed
	}
	for _, c := range cases {
		var buf bytes.Buffer
		printStartBanner(&buf, c.host, 9090)
		if !strings.Contains(buf.String(), "OPENAI_BASE_URL="+c.want+"/v1") {
			t.Errorf("host %q: banner base URL = ...; want %q\n%s", c.host, c.want, buf.String())
		}
	}
}

// quietLogger discards output so the registerPipelines warnings don't
// clutter test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRegisterVectorCollectionsSeedsTenantScopedStore(t *testing.T) {
	results := []*config.VectorCollectionLoadResult{{Definition: &types.VectorCollectionDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.VectorCollectionKind,
		Metadata: types.Metadata{Name: "docs", TenantID: "tenant-a"},
		Spec: types.VectorCollectionSpec{Dimension: 2, Metric: "cosine", Faults: types.VectorFaults{PartialResults: &types.VectorPartialResultsFault{MaxResults: 1}}, Points: []types.VectorPoint{
			{ID: "refund", Vector: []float64{1, 0}, Metadata: map[string]any{"team": "support"}},
			{ID: "billing", Vector: []float64{0, 1}, Metadata: map[string]any{"team": "finance"}},
		}},
	}}}
	store, count := registerVectorCollections(results, quietLogger())
	if count != 1 {
		t.Fatalf("loaded %d collections, want 1", count)
	}
	result, err := store.QueryWithInfo(vector.ScopedCollectionName("tenant-a", "docs"), vector.Query{Vector: []float64{1, 0}, TopK: 2})
	matches := result.Matches
	if err != nil || len(matches) != 1 || matches[0].ExternalID != "refund" {
		t.Fatalf("query = %#v, %v", matches, err)
	}
	if !result.Partial {
		t.Fatal("startup fixture did not configure partial results")
	}
	if _, err := store.Collection("docs"); err == nil {
		t.Fatal("tenant fixture leaked into the anonymous namespace")
	}
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

func TestParseGlobalChaos(t *testing.T) {
	seed, rate, err := parseGlobalChaos("42", "0.25", false)
	if err != nil || seed != 42 || rate == nil || *rate != 0.25 {
		t.Fatalf("seed=%d rate=%v err=%v", seed, rate, err)
	}
	_, rate, err = parseGlobalChaos("42", "invalid", true)
	if err != nil || rate == nil || *rate != 0 {
		t.Fatalf("off should force zero: rate=%v err=%v", rate, err)
	}
	for _, tc := range []struct{ seed, rate string }{{"bad", ""}, {"", "-0.1"}, {"", "1.1"}} {
		if _, _, err := parseGlobalChaos(tc.seed, tc.rate, false); err == nil {
			t.Fatalf("expected error for seed=%q rate=%q", tc.seed, tc.rate)
		}
	}
}
