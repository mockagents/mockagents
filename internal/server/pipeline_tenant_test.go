package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
)

// TestPipelineWrite_RequiresPlatformInMultiTenant is the audit M-04 guard.
// Pipelines carry no TenantID: one PUT rewrites the definition every tenant
// sees and runs, so an editor floor let any tenant's editor edit shared state.
func TestPipelineWrite_RequiresPlatformInMultiTenant(t *testing.T) {
	const pattern = "PUT /api/v1/pipelines/{name}"
	const path = "/api/v1/pipelines/alpha"

	for _, role := range []tenancy.Role{tenancy.RoleEditor, tenancy.RoleAdmin} {
		p := &tenancy.Principal{TenantID: "t1", KeyID: "k", Role: role}
		if code := mountAndServe(t, true, pattern, http.MethodPut, path, p); code == http.StatusOK {
			t.Errorf("%s reached the pipeline write route; pipelines are global state", role)
		}
	}
	platform := &tenancy.Principal{TenantID: "t1", KeyID: "k", Role: tenancy.RolePlatform}
	if code := mountAndServe(t, true, pattern, http.MethodPut, path, platform); code != http.StatusOK {
		t.Errorf("platform blocked from the pipeline write route: status %d", code)
	}

	// Single-tenant mode is an unauthenticated local-dev tool: still open.
	if code := mountAndServe(t, false, pattern, http.MethodPut, path, nil); code != http.StatusOK {
		t.Errorf("single-tenant pipeline write blocked: status %d", code)
	}
}

// TestPipelineRun_StaysViewer: a run resolves each ref in the caller's own
// tenant scope, so tightening the write floor must not tighten execution.
func TestPipelineRun_StaysViewer(t *testing.T) {
	viewer := &tenancy.Principal{TenantID: "t1", KeyID: "k", Role: tenancy.RoleViewer}
	code := mountAndServe(t, true, "POST /api/v1/pipelines/{name}/run", http.MethodPost, "/api/v1/pipelines/alpha/run", viewer)
	if code != http.StatusOK {
		t.Errorf("viewer blocked from running a pipeline: status %d", code)
	}
}

// tenantScoped wraps h so every request carries the given tenant id, the way
// the auth middleware does for an authenticated principal.
func tenantScoped(h http.Handler, tenantID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(engine.WithTenantID(r.Context(), tenantID)))
	})
}

// newTenantPipelineServer builds the pipeline write surface with one agent
// owned by ownerTenant, and scopes every request to callerTenant.
func newTenantPipelineServer(t *testing.T, agentName, ownerTenant, callerTenant string, defs ...*types.PipelineDefinition) *httptest.Server {
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
	areg.Register(&types.AgentDefinition{
		APIVersion: "mockagents/v1",
		Kind:       "Agent",
		Metadata:   types.Metadata{Name: agentName, TenantID: ownerTenant},
		Spec:       types.AgentSpec{Model: agentName + "-model", Protocol: "openai-chat-completions"},
	})

	h := &PipelineHandlers{Registry: preg, AgentRegistry: areg, AgentsDir: dir}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/pipelines/{name}", h.GetPipeline)
	mux.HandleFunc("PUT /api/v1/pipelines/{name}", h.UpdatePipeline)
	out := httptest.NewServer(tenantScoped(mux, callerTenant))
	t.Cleanup(out.Close)
	return out
}

// TestValidateAgentRefs_ResolvesTenantOwnedAgent is the second half of M-04:
// ref validation used the global-only lookup, so a pipeline referencing a
// tenant-owned agent was rejected even though a run would have resolved it.
func TestValidateAgentRefs_ResolvesTenantOwnedAgent(t *testing.T) {
	def := singleRefPipeline("alpha", "support")
	srv := newTenantPipelineServer(t, "support", "acme", "acme", def)

	etag, got := getPipelineETag(t, srv, "alpha")
	got.Metadata.Description = "edited by the owning tenant"
	resp := putPipeline(t, srv, "alpha", etag, got)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readBody(resp)
		t.Fatalf("PUT status = %d, want 200; a ref to the caller's own agent must validate. body = %s", resp.StatusCode, body)
	}
}

// TestValidateAgentRefs_RejectsOtherTenantsAgent: visibility still stops at
// the tenant boundary — a caller who cannot see the agent gets the same
// unknown-ref error as before.
func TestValidateAgentRefs_RejectsOtherTenantsAgent(t *testing.T) {
	def := singleRefPipeline("alpha", "support")
	srv := newTenantPipelineServer(t, "support", "acme", "globex", def)

	etag, got := getPipelineETag(t, srv, "alpha")
	got.Metadata.Description = "edited by a stranger"
	resp := putPipeline(t, srv, "alpha", etag, got)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("PUT status = %d, want 422 for a ref the caller cannot see", resp.StatusCode)
	}
	var report ValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 {
		t.Fatal("422 carried no validation errors")
	}
	found := false
	for _, e := range report.Errors {
		if e.Field == "spec.agents[0].ref" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error on spec.agents[0].ref, got %+v", report.Errors)
	}
}

// singleRefPipeline builds a one-node sequential pipeline pointing at ref.
func singleRefPipeline(name, ref string) *types.PipelineDefinition {
	return &types.PipelineDefinition{
		APIVersion: "mockagents/v1",
		Kind:       "Pipeline",
		Metadata:   types.Metadata{Name: name},
		Spec: types.PipelineSpec{
			Topology: types.TopologySequential,
			Agents:   []types.PipelineAgent{{ID: "a", Ref: ref}},
		},
	}
}
