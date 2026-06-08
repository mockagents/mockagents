package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// newAgentWriteServerP is like newAgentWriteServer but injects a fixed principal
// into every request context (simulating the auth middleware), so the write
// handlers see a multi-tenant caller.
func newAgentWriteServerP(t *testing.T, agentsDir string, p *tenancy.Principal) (*httptest.Server, *Handlers) {
	t.Helper()
	reg := engine.NewAgentRegistry()
	eng := engine.NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := &Handlers{Engine: eng, AgentsDir: agentsDir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	inject := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if p != nil {
				r = r.WithContext(tenancy.WithPrincipal(r.Context(), p))
			}
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agents", inject(h.CreateAgent))
	mux.HandleFunc("PUT /api/v1/agents/{name}", inject(h.PutAgent))
	mux.HandleFunc("DELETE /api/v1/agents/{name}", inject(h.DeleteAgent))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func yamlWithTenant(name, model, tenant string) string {
	s := "apiVersion: mockagents/v1\nkind: Agent\nmetadata:\n  name: " + name + "\n"
	if tenant != "" {
		s += "  tenant_id: " + tenant + "\n"
	}
	s += "spec:\n  protocol: openai-chat-completions\n  model: " + model +
		"\n  behavior:\n    scenarios:\n      - name: default\n        response:\n          content: hi\n"
	return s
}

// TG-02: ownership is stamped from the principal, never trusted from the body.
func TestCreateAgent_IgnoresBodyTenantID(t *testing.T) {
	dir := t.TempDir()
	srv, h := newAgentWriteServerP(t, dir, &tenancy.Principal{TenantID: "ten-a", Role: tenancy.RoleEditor})

	// Body forges tenant_id: ten-b — must be overwritten with the caller's ten-a.
	code, body := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", yamlWithTenant("forge", "fm", "ten-b"))
	if code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if h.Engine.Registry.GetOwnedForTenant("forge", "ten-a") == nil {
		t.Error("agent not owned by caller tenant ten-a")
	}
	if h.Engine.Registry.GetOwnedForTenant("forge", "ten-b") != nil {
		t.Error("agent was planted into the forged tenant ten-b")
	}
	// Persisted YAML must round-trip ten-a ownership (TG-05).
	raw, err := os.ReadFile(filepath.Join(dir, "ten-a.forge.yaml"))
	if err != nil {
		t.Fatalf("expected tenant-prefixed file: %v", err)
	}
	var def types.AgentDefinition
	if err := yaml.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	if def.Metadata.TenantID != "ten-a" {
		t.Errorf("persisted tenant_id = %q, want ten-a", def.Metadata.TenantID)
	}
}

// TG-01: a tenant cannot replace or delete another tenant's agent.
func TestWrite_TenantIsolation(t *testing.T) {
	dir := t.TempDir()
	// Tenant A creates "shared".
	srvA, hA := newAgentWriteServerP(t, dir, &tenancy.Principal{TenantID: "ten-a", Role: tenancy.RoleEditor})
	if code, b := doReq(t, "POST", srvA.URL+"/api/v1/agents", "application/yaml", yamlWithTenant("shared", "model-a", "")); code != http.StatusCreated {
		t.Fatalf("A create: %d %s", code, b)
	}

	// Tenant B (same engine) gets its own server view by sharing the registry.
	hB := &Handlers{Engine: hA.Engine, AgentsDir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	inject := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(tenancy.WithPrincipal(r.Context(), &tenancy.Principal{TenantID: "ten-b", Role: tenancy.RoleEditor})))
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/agents/{name}", inject(hB.PutAgent))
	mux.HandleFunc("DELETE /api/v1/agents/{name}", inject(hB.DeleteAgent))
	srvB := httptest.NewServer(mux)
	t.Cleanup(srvB.Close)

	// B deletes "shared" → 404 (it owns no such agent).
	if code, _ := doReq(t, "DELETE", srvB.URL+"/api/v1/agents/shared", "", ""); code != http.StatusNotFound {
		t.Errorf("B delete of A's agent: status=%d, want 404", code)
	}
	// B PUTs "shared" → creates B's OWN agent; A's is untouched.
	if code, _ := doReq(t, "PUT", srvB.URL+"/api/v1/agents/shared", "application/yaml", yamlWithTenant("shared", "model-b", "")); code != http.StatusCreated {
		t.Errorf("B put: expected 201 created for B's own")
	}
	a := hA.Engine.Registry.GetOwnedForTenant("shared", "ten-a")
	if a == nil || a.Spec.Model != "model-a" {
		t.Errorf("tenant A's agent was mutated by tenant B: %+v", a)
	}
	if hA.Engine.Registry.GetOwnedForTenant("shared", "ten-b") == nil {
		t.Error("tenant B's own agent not created")
	}
}

// FB04-03: a PUT/POST of a name that exists only GLOBALLY is a create for the
// tenant, not an update, and must not 409.
func TestWrite_GlobalShadowIsCreateNotUpdate(t *testing.T) {
	dir := t.TempDir()
	srv, h := newAgentWriteServerP(t, dir, &tenancy.Principal{TenantID: "ten-a", Role: tenancy.RoleEditor})
	// Seed a GLOBAL agent named "foo" directly.
	h.Engine.Registry.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "foo"},
		Spec:     types.AgentSpec{Protocol: "openai-chat-completions", Model: "global-foo"},
	})

	// PUT foo as ten-a → 201 created (not 200 updated).
	code, body := doReq(t, "PUT", srv.URL+"/api/v1/agents/foo", "application/yaml", yamlWithTenant("foo", "tenant-foo", ""))
	if code != http.StatusCreated || !strings.Contains(body, `"status":"created"`) {
		t.Errorf("PUT over global: status=%d body=%s, want 201 created", code, body)
	}
	// Global agent left intact.
	if g := h.Engine.Registry.GetOwnedForTenant("foo", ""); g == nil || g.Spec.Model != "global-foo" {
		t.Errorf("global foo was mutated: %+v", g)
	}
}

// FB04-01 / FB04-02: the write API tracks the agent's real backing file, so a
// DELETE removes that file (no resurrect) and a PUT overwrites it in place
// (no duplicate file), even when the filename differs from the agent name.
func TestWrite_TracksDifferentlyNamedSourceFile(t *testing.T) {
	dir := t.TempDir()
	// An agent "foo" loaded from team-foo.yaml (filename != name).
	srcPath := filepath.Join(dir, "team-foo.yaml")
	if err := os.WriteFile(srcPath, []byte(yamlWithTenant("foo", "v1", "")), 0644); err != nil {
		t.Fatal(err)
	}
	srv, h := newAgentWriteServerP(t, dir, nil) // single-tenant
	h.Engine.Registry.RegisterWithSource(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "foo"},
		Spec:     types.AgentSpec{Protocol: "openai-chat-completions", Model: "v1"},
	}, srcPath)

	// PUT replaces it: must overwrite team-foo.yaml in place, NOT create foo.yaml.
	if code, b := doReq(t, "PUT", srv.URL+"/api/v1/agents/foo", "application/yaml", yamlWithTenant("foo", "v2", "")); code != http.StatusOK {
		t.Fatalf("PUT: %d %s", code, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "foo.yaml")); !os.IsNotExist(err) {
		t.Errorf("PUT created a duplicate foo.yaml instead of overwriting the source")
	}
	raw, _ := os.ReadFile(srcPath)
	if !strings.Contains(string(raw), "v2") {
		t.Errorf("source file not overwritten with new content: %s", raw)
	}

	// DELETE must remove team-foo.yaml so the agent can't resurrect on restart.
	if code, _ := doReq(t, "DELETE", srv.URL+"/api/v1/agents/foo", "", ""); code != http.StatusOK {
		t.Fatalf("DELETE failed")
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("DELETE left the backing file %s — agent would resurrect on restart", srcPath)
	}
}

// TG-06: concurrent creates of the same name → exactly one 201, the rest 409,
// one registry entry, one non-torn file.
func TestCreateAgent_ConcurrentSameNameOneWins(t *testing.T) {
	dir := t.TempDir()
	srv, h := newAgentWriteServerP(t, dir, nil)
	const n = 16
	var wg sync.WaitGroup
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", srv.URL+"/api/v1/agents", strings.NewReader(yamlWithTenant("race-bot", "rm", "")))
			req.Header.Set("Content-Type", "application/yaml")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				codes <- 0
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(codes)
	created, conflict := 0, 0
	for c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		}
	}
	if created != 1 || conflict != n-1 {
		t.Errorf("created=%d conflict=%d, want 1 and %d", created, conflict, n-1)
	}
	if h.Engine.Registry.GetOwnedForTenant("race-bot", "") == nil {
		t.Error("race-bot not registered")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected exactly one persisted file, got %d", len(entries))
	}
}

// TG-07: a body over the 1 MiB cap is rejected with 413 and changes nothing.
func TestCreateAgent_BodyTooLarge(t *testing.T) {
	dir := t.TempDir()
	srv, h := newAgentWriteServerP(t, dir, nil)
	big := yamlWithTenant("big", "bm", "") + "# " + strings.Repeat("x", (1<<20)+1024) + "\n"
	code, _ := doReq(t, "POST", srv.URL+"/api/v1/agents", "application/yaml", big)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want 413", code)
	}
	if h.Engine.Registry.GetOwnedForTenant("big", "") != nil {
		t.Error("oversize body still registered an agent")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("oversize body wrote a file (%d entries)", len(entries))
	}
}

func TestSanitizeFilenamePart(t *testing.T) {
	cases := map[string]string{
		"ten-a":        "ten-a",
		"ten/../../x":  "ten_______x",
		"a b":          "a_b",
		"":             "tenant",
	}
	for in, want := range cases {
		if got := sanitizeFilenamePart(in); got != want {
			t.Errorf("sanitizeFilenamePart(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(sanitizeFilenamePart(in), `/\`) {
			t.Errorf("sanitizeFilenamePart(%q) leaked a path separator", in)
		}
	}
}
