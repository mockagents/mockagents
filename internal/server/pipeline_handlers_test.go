package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

func newPipelineServer(t *testing.T, defs ...*types.PipelineDefinition) (*httptest.Server, *engine.PipelineRegistry) {
	t.Helper()
	reg := engine.NewPipelineRegistry()
	for _, d := range defs {
		reg.Register(d)
	}
	h := &PipelineHandlers{Registry: reg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/pipelines", h.ListPipelines)
	mux.HandleFunc("GET /api/v1/pipelines/{name}", h.GetPipeline)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, reg
}

func samplePipeline(name, topology string, n int) *types.PipelineDefinition {
	def := &types.PipelineDefinition{
		APIVersion: "mockagents/v1",
		Kind:       "Pipeline",
		Metadata:   types.Metadata{Name: name, Description: "sample"},
		Spec: types.PipelineSpec{
			Topology: topology,
		},
	}
	for i := 0; i < n; i++ {
		def.Spec.Agents = append(def.Spec.Agents, types.PipelineAgent{
			ID:  string(rune('a' + i)),
			Ref: "agent-" + string(rune('a'+i)),
		})
	}
	return def
}

func TestPipelineHandlers_List(t *testing.T) {
	srv, _ := newPipelineServer(t,
		samplePipeline("alpha", "sequential", 3),
		samplePipeline("beta", "parallel", 2),
	)
	resp, err := http.Get(srv.URL + "/api/v1/pipelines")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out []PipelineSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("count = %d", len(out))
	}
	// Sorted by name ascending.
	if out[0].Name != "alpha" || out[1].Name != "beta" {
		t.Errorf("order = %v", out)
	}
	if out[0].Topology != "sequential" || out[0].AgentCount != 3 {
		t.Errorf("out[0] = %+v", out[0])
	}
}

func TestPipelineHandlers_ListEmptyRegistry(t *testing.T) {
	srv, _ := newPipelineServer(t)
	resp, err := http.Get(srv.URL + "/api/v1/pipelines")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "[]" && string(body) != "[]\n" {
		t.Errorf("body = %q", body)
	}
}

func TestPipelineHandlers_GetByName(t *testing.T) {
	srv, _ := newPipelineServer(t,
		samplePipeline("alpha", "sequential", 2),
	)
	resp, err := http.Get(srv.URL + "/api/v1/pipelines/alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out types.PipelineDefinition
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Metadata.Name != "alpha" || out.Spec.Topology != "sequential" {
		t.Errorf("out = %+v", out)
	}
	if len(out.Spec.Agents) != 2 {
		t.Errorf("agents = %d", len(out.Spec.Agents))
	}
}

func TestPipelineHandlers_NotFound(t *testing.T) {
	srv, _ := newPipelineServer(t,
		samplePipeline("alpha", "sequential", 1),
	)
	resp, err := http.Get(srv.URL + "/api/v1/pipelines/missing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestPipelineHandlers_NilRegistryListReturnsEmpty(t *testing.T) {
	h := &PipelineHandlers{}
	srv := httptest.NewServer(http.HandlerFunc(h.ListPipelines))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func pipelineExecutionHandler(t *testing.T, def *types.PipelineDefinition, agents ...string) http.Handler {
	t.Helper()
	agentReg := engine.NewAgentRegistry()
	for _, name := range agents {
		agentReg.Register(&types.AgentDefinition{
			APIVersion: types.AgentAPIVersion,
			Kind:       types.AgentKind,
			Metadata:   types.Metadata{Name: name},
			Spec: types.AgentSpec{
				Protocol: "openai-chat-completions",
				Model:    name,
				Behavior: types.BehaviorConfig{Scenarios: []types.Scenario{{
					Name: "default", Response: types.ScenarioResponse{Content: name + ":{{.Message}}"},
				}}},
			},
		})
	}
	eng := engine.NewEngine(agentReg, state.NewMemoryStore(state.DefaultSessionTTL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	pipelineReg := engine.NewPipelineRegistry()
	pipelineReg.Register(def)
	h := &PipelineHandlers{Registry: pipelineReg, Executor: engine.NewPipelineExecutor(eng)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/pipelines/{name}/run", h.RunPipeline)
	return RequestContext(mux)
}

func TestPipelineHandlers_RunPipeline(t *testing.T) {
	def := samplePipeline("alpha", types.TopologySequential, 2)
	h := pipelineExecutionHandler(t, def, "agent-a", "agent-b")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/alpha/run",
		bytes.NewBufferString(`{"input":"hello","session_id":"test-run"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got engine.PipelineResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 2 || got.Nodes[0].NodeID != "a" || got.Nodes[1].NodeID != "b" {
		t.Fatalf("node trajectory = %+v", got.Nodes)
	}
	if got.FinalResponse() == nil || got.FinalResponse().Content != "agent-b:agent-a:hello" {
		t.Fatalf("final response = %+v", got.FinalResponse())
	}
}

func TestPipelineHandlers_RunPipelinePreservesPartialResult(t *testing.T) {
	def := samplePipeline("alpha", types.TopologySequential, 2)
	h := pipelineExecutionHandler(t, def, "agent-a") // second ref is deliberately absent
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/alpha/run", bytes.NewBufferString(`{"input":"hello"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got pipelineRunError
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Result == nil || len(got.Result.Nodes) != 2 || got.Result.Nodes[0].Response == nil {
		t.Fatalf("partial result = %+v", got.Result)
	}
	if got.Error == "" {
		t.Fatal("expected execution error")
	}
}

func TestPipelineHandlers_RunPipelineRejectsInvalidBodies(t *testing.T) {
	def := samplePipeline("alpha", types.TopologySequential, 1)
	h := pipelineExecutionHandler(t, def, "agent-a")
	for _, body := range []string{
		`{"input":""}`,
		`{"input":"hello","unexpected":true}`,
		`{"input":"hello"} {"input":"again"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/alpha/run", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, response = %s", body, rec.Code, rec.Body.String())
		}
	}
}
