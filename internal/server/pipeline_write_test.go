package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// newPipelineWriteServer builds a PipelineHandlers wired for the write path:
// a pipeline registry seeded from on-disk YAML (so each pipeline has a real
// source file), an agent registry holding the named agents (so ref checks
// pass), and a temp agents dir. Returns the server, both registries, and the
// agents dir.
func newPipelineWriteServer(t *testing.T, agents []string, defs ...*types.PipelineDefinition) (*httptest.Server, *engine.PipelineRegistry, string) {
	t.Helper()
	dir := t.TempDir()

	preg := engine.NewPipelineRegistry()
	for _, d := range defs {
		p := filepath.Join(dir, d.Metadata.Name+".yaml")
		b, err := yaml.Marshal(d)
		if err != nil {
			t.Fatalf("marshal %s: %v", d.Metadata.Name, err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
		preg.RegisterWithSource(d, p)
	}

	areg := engine.NewAgentRegistry()
	for _, name := range agents {
		areg.Register(&types.AgentDefinition{
			APIVersion: "mockagents/v1",
			Kind:       "Agent",
			Metadata:   types.Metadata{Name: name},
			Spec:       types.AgentSpec{Model: name + "-model", Protocol: "openai-chat-completions"},
		})
	}

	h := &PipelineHandlers{Registry: preg, AgentRegistry: areg, AgentsDir: dir}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/pipelines/{name}", h.GetPipeline)
	mux.HandleFunc("PUT /api/v1/pipelines/{name}", h.UpdatePipeline)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, preg, dir
}

func getPipelineETag(t *testing.T, srv *httptest.Server, name string) (string, types.PipelineDefinition) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/v1/pipelines/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", name, resp.StatusCode)
	}
	var def types.PipelineDefinition
	if err := json.NewDecoder(resp.Body).Decode(&def); err != nil {
		t.Fatal(err)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("GET returned no ETag")
	}
	return etag, def
}

func putPipeline(t *testing.T, srv *httptest.Server, name, ifMatch string, def any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(def)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/pipelines/"+name, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestUpdatePipeline_HappyPath(t *testing.T) {
	srv, preg, dir := newPipelineWriteServer(t, []string{"agent-a", "agent-b"},
		samplePipeline("alpha", "sequential", 2))

	etag, def := getPipelineETag(t, srv, "alpha")
	def.Metadata.Description = "edited description"

	resp := putPipeline(t, srv, "alpha", etag, def)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readBody(resp)
		t.Fatalf("PUT status = %d, body = %s", resp.StatusCode, body)
	}
	newETag := resp.Header.Get("ETag")
	if newETag == "" || newETag == etag {
		t.Errorf("expected a changed ETag, got %q (was %q)", newETag, etag)
	}

	// Registry reflects the edit.
	if got := preg.GetPipeline("alpha"); got == nil || got.Metadata.Description != "edited description" {
		t.Errorf("registry not updated: %+v", got)
	}
	// File on disk reflects the edit and re-validates.
	onDisk, err := os.ReadFile(filepath.Join(dir, "alpha.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(onDisk, []byte("edited description")) {
		t.Errorf("file not updated:\n%s", onDisk)
	}
	// A subsequent GET reports the new ETag (file hash is stable post-write).
	again, _ := getPipelineETag(t, srv, "alpha")
	if again != newETag {
		t.Errorf("post-write GET ETag %q != PUT ETag %q", again, newETag)
	}
}

func TestUpdatePipeline_MissingIfMatch(t *testing.T) {
	srv, _, _ := newPipelineWriteServer(t, []string{"agent-a"},
		samplePipeline("alpha", "sequential", 1))
	_, def := getPipelineETag(t, srv, "alpha")

	resp := putPipeline(t, srv, "alpha", "", def) // no If-Match
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 428", resp.StatusCode)
	}
}

func TestUpdatePipeline_StaleIfMatch(t *testing.T) {
	srv, _, _ := newPipelineWriteServer(t, []string{"agent-a"},
		samplePipeline("alpha", "sequential", 1))
	_, def := getPipelineETag(t, srv, "alpha")

	resp := putPipeline(t, srv, "alpha", `"deadbeef"`, def) // wrong version
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412", resp.StatusCode)
	}
}

func TestUpdatePipeline_UnknownAgentRef422(t *testing.T) {
	srv, _, dir := newPipelineWriteServer(t, []string{"agent-a"},
		samplePipeline("alpha", "sequential", 1))
	etag, def := getPipelineETag(t, srv, "alpha")
	def.Spec.Agents[0].Ref = "ghost-agent" // not in the agent registry

	resp := putPipeline(t, srv, "alpha", etag, def)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	var vr validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		t.Fatal(err)
	}
	if vr.OK || len(vr.Errors) == 0 {
		t.Errorf("expected validation errors, got %+v", vr)
	}
	// The bad edit must NOT have been written.
	onDisk, _ := os.ReadFile(filepath.Join(dir, "alpha.yaml"))
	if bytes.Contains(onDisk, []byte("ghost-agent")) {
		t.Error("invalid pipeline was persisted")
	}
}

func TestUpdatePipeline_NameMismatch400(t *testing.T) {
	srv, _, _ := newPipelineWriteServer(t, []string{"agent-a"},
		samplePipeline("alpha", "sequential", 1))
	etag, def := getPipelineETag(t, srv, "alpha")
	def.Metadata.Name = "beta" // disagrees with the path

	resp := putPipeline(t, srv, "alpha", etag, def)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUpdatePipeline_NotFound404(t *testing.T) {
	srv, _, _ := newPipelineWriteServer(t, []string{"agent-a"},
		samplePipeline("alpha", "sequential", 1))
	resp := putPipeline(t, srv, "missing", `"x"`, samplePipeline("missing", "sequential", 1))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSafePipelineName(t *testing.T) {
	cases := map[string]bool{
		"research-pipeline": true,
		"alpha":             true,
		"":                  false,
		".":                 false,
		"..":                false,
		"a/b":               false,
		`a\b`:               false,
		"../etc/passwd":     false,
	}
	for name, want := range cases {
		if got := safePipelineName(name); got != want {
			t.Errorf("safePipelineName(%q) = %v, want %v", name, got, want)
		}
	}
}

func readBody(resp *http.Response) (string, error) {
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}
