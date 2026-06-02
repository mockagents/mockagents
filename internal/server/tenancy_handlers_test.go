package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// newRotateTestStore opens an isolated tenancy store under t.TempDir()
// and returns it plus a cleanup hook.
func newRotateTestStore(t *testing.T) *tenancy.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := tenancy.NewSQLiteStore(filepath.Join(dir, "tenancy.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// servePrincipal wraps mux so every request carries p, mirroring what
// tenancy.AuthMiddleware injects in production. Wrapping the mux (not the
// inner handler) keeps path-value parsing intact.
func servePrincipal(p *tenancy.Principal, mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(tenancy.WithPrincipal(r.Context(), p))
		mux.ServeHTTP(w, r)
	})
}

func TestTenancyHandlers_RotateAPIKey(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateAPIKey(ctx, tenant.ID, "ci", tenancy.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}

	// The handler records audit events but the in-memory audit
	// recorder with a nil store is a no-op — that's the same shape
	// server.New uses when AuditStore is unset.
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/keys/{id}/rotate", h.RotateAPIKey)
	admin := &tenancy.Principal{TenantID: tenant.ID, KeyID: "k_admin", Role: tenancy.RoleAdmin}
	srv := httptest.NewServer(servePrincipal(admin, mux))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/keys/"+created.Key.ID+"/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out tenancy.NewAPIKeyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Key.ID != created.Key.ID {
		t.Errorf("id changed: %q", out.Key.ID)
	}
	if out.Plaintext == "" || out.Plaintext == created.Plaintext {
		t.Errorf("plaintext = %q (old %q)", out.Plaintext, created.Plaintext)
	}
	// The old plaintext must no longer resolve, and the new one must.
	if _, err := store.Resolve(ctx, created.Plaintext); err == nil {
		t.Error("old plaintext still resolves")
	}
	if _, err := store.Resolve(ctx, out.Plaintext); err != nil {
		t.Errorf("new plaintext fails to resolve: %v", err)
	}
}

func TestTenancyHandlers_RotateAPIKey_NotFound(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/keys/{id}/rotate", h.RotateAPIKey)
	admin := &tenancy.Principal{TenantID: "ten_x", KeyID: "k_admin", Role: tenancy.RoleAdmin}
	srv := httptest.NewServer(servePrincipal(admin, mux))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/keys/key_bogus/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestTenancyHandlers_RotateMyAPIKey covers the self-rotation path.
// The handler reads the Principal from the request context (set by
// the auth middleware), so the test injects one manually via
// tenancy.WithPrincipal and asserts the round-trip: the old
// plaintext stops resolving, the new one resolves to the same key
// id, and the response body carries the fresh secret.
func TestTenancyHandlers_RotateMyAPIKey(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateAPIKey(ctx, tenant.ID, "self-rot", tenancy.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	// Wrap the handler with a tiny middleware that injects the
	// principal — mirrors what tenancy.AuthMiddleware does in
	// production without requiring the full auth chain.
	inject := func(next http.Handler) http.Handler {
		principal := &tenancy.Principal{
			TenantID: tenant.ID,
			KeyID:    created.Key.ID,
			Role:     tenancy.RoleViewer,
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := tenancy.WithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	srv := httptest.NewServer(inject(http.HandlerFunc(h.RotateMyAPIKey)))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out tenancy.NewAPIKeyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Key.ID != created.Key.ID {
		t.Errorf("id changed: %q", out.Key.ID)
	}
	if out.Plaintext == "" || out.Plaintext == created.Plaintext {
		t.Errorf("plaintext unchanged: %q", out.Plaintext)
	}
	// Old plaintext must no longer resolve.
	if _, err := store.Resolve(ctx, created.Plaintext); err == nil {
		t.Error("old plaintext still resolves")
	}
	// New plaintext must resolve to the same key id + role.
	p, err := store.Resolve(ctx, out.Plaintext)
	if err != nil {
		t.Fatalf("new plaintext does not resolve: %v", err)
	}
	if p.KeyID != created.Key.ID {
		t.Errorf("key id changed on principal: %q", p.KeyID)
	}
	if p.Role != tenancy.RoleViewer {
		t.Errorf("role changed: %q", p.Role)
	}
}

// TestTenancyHandlers_RotateMyAPIKey_Unauthenticated covers the
// defensive 401 path: without a Principal on the context, the
// handler must refuse rather than blow up or read a nil key id.
func TestTenancyHandlers_BulkRotateTenantKeys(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", tenancy.RoleEditor)
	b, _ := store.CreateAPIKey(ctx, tenant.ID, "viewer-bot", tenancy.RoleViewer)

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys/rotate", h.BulkRotateTenantKeys)
	admin := &tenancy.Principal{TenantID: tenant.ID, KeyID: "k_admin", Role: tenancy.RoleAdmin}
	srv := httptest.NewServer(servePrincipal(admin, mux))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants/"+tenant.ID+"/keys/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out BulkRotateResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
	if len(out.Results) != 2 {
		t.Fatalf("results = %d", len(out.Results))
	}
	// Both old plaintexts must be dead; both new plaintexts must
	// resolve via the store.
	if _, err := store.Resolve(ctx, a.Plaintext); err == nil {
		t.Error("old a plaintext still resolves")
	}
	if _, err := store.Resolve(ctx, b.Plaintext); err == nil {
		t.Error("old b plaintext still resolves")
	}
	for _, r := range out.Results {
		if _, err := store.Resolve(ctx, r.Plaintext); err != nil {
			t.Errorf("new plaintext for %q fails to resolve: %v", r.Key.Name, err)
		}
	}
}

func TestTenancyHandlers_BulkRotateTenantKeys_ExceptSelf(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")
	admin, _ := store.CreateAPIKey(ctx, tenant.ID, "admin-self", tenancy.RoleAdmin)
	other, _ := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", tenancy.RoleEditor)

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	// Inject the admin as the caller's Principal so ?except=self
	// can resolve their key id.
	inject := func(next http.Handler) http.Handler {
		principal := &tenancy.Principal{
			TenantID: tenant.ID,
			KeyID:    admin.Key.ID,
			Role:     tenancy.RoleAdmin,
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := tenancy.WithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants/{id}/keys/rotate", inject(http.HandlerFunc(h.BulkRotateTenantKeys)))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants/"+tenant.ID+"/keys/rotate?except=self",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out BulkRotateResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	// Only the other key should be rotated, not the admin's own.
	if out.Count != 1 {
		t.Errorf("count = %d, want 1", out.Count)
	}
	// Admin's old plaintext must still resolve (excluded).
	if _, err := store.Resolve(ctx, admin.Plaintext); err != nil {
		t.Errorf("admin key should still resolve: %v", err)
	}
	// Other's old plaintext must be dead.
	if _, err := store.Resolve(ctx, other.Plaintext); err == nil {
		t.Error("other key should have been rotated")
	}
}

func TestTenancyHandlers_BulkRotateTenantKeys_UnknownTenant(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys/rotate", h.BulkRotateTenantKeys)
	// Authenticated as a *different* tenant: addressing ten_bogus must 404
	// (ownership gate), not leak whether that tenant exists.
	admin := &tenancy.Principal{TenantID: "ten_real", KeyID: "k_admin", Role: tenancy.RoleAdmin}
	srv := httptest.NewServer(servePrincipal(admin, mux))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants/ten_bogus/keys/rotate",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTenancyHandlers_RotateMyAPIKey_Unauthenticated(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	srv := httptest.NewServer(http.HandlerFunc(h.RotateMyAPIKey))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// TestTenancyHandlers_BurnMyAPIKey exercises the rotate-and-burn
// path: the handler must rotate the caller's key (so the old
// plaintext dies) AND return 204 with an empty body (so the new
// plaintext never travels back over the wire). This is the
// "confirmed compromise" emergency response.
func TestTenancyHandlers_BurnMyAPIKey(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateAPIKey(ctx, tenant.ID, "self-burn", tenancy.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	inject := func(next http.Handler) http.Handler {
		principal := &tenancy.Principal{
			TenantID: tenant.ID,
			KeyID:    created.Key.ID,
			Role:     tenancy.RoleEditor,
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := tenancy.WithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	srv := httptest.NewServer(inject(http.HandlerFunc(h.BurnMyAPIKey)))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("body should be empty, got %q", string(body))
	}

	// Critical assertion: the server side DID rotate. The old
	// plaintext must no longer resolve, and a fresh call to the
	// store Resolve path with the old plaintext must fail.
	if _, err := store.Resolve(ctx, created.Plaintext); err == nil {
		t.Error("burn did not invalidate the old plaintext")
	}
	// The new key's row still exists (burn ≠ delete) — we prove
	// this by listing the tenant's keys and confirming the
	// single key id is preserved.
	keys, err := store.ListAPIKeys(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("tenant has %d keys after burn, want 1", len(keys))
	}
	if keys[0].ID != created.Key.ID {
		t.Errorf("key id changed: %q -> %q", created.Key.ID, keys[0].ID)
	}
	if keys[0].Prefix == created.Key.Prefix {
		t.Error("prefix unchanged — rotation did not happen")
	}
}

func TestTenancyHandlers_BurnMyAPIKey_Unauthenticated(t *testing.T) {
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	srv := httptest.NewServer(http.HandlerFunc(h.BurnMyAPIKey))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// TestTenancyHandlers_CrossTenantIDOR_Returns404 is the X-SEC-001
// regression test: a tenant-A admin must not be able to read or mutate
// tenant-B's API keys via the {id}/{tenantID} path params. Every
// cross-tenant attempt returns 404 (hiding existence) and leaves B's
// credential fully intact. This test must FAIL against the pre-fix code.
func TestTenancyHandlers_CrossTenantIDOR_Returns404(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	tenantA, _ := store.CreateTenant(ctx, "tenant-a")
	tenantB, _ := store.CreateTenant(ctx, "tenant-b")
	// The victim: a live key owned by tenant B.
	bKey, err := store.CreateAPIKey(ctx, tenantB.ID, "b-secret", tenancy.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}

	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	// Authenticated as tenant A's admin.
	attacker := &tenancy.Principal{TenantID: tenantA.ID, KeyID: "k_a_admin", Role: tenancy.RoleAdmin}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/keys/{id}/rotate", h.RotateAPIKey)
	mux.HandleFunc("DELETE /api/v1/keys/{id}", h.DeleteAPIKey)
	mux.HandleFunc("PATCH /api/v1/keys/{id}", h.UpdateAPIKeyRole)
	mux.HandleFunc("GET /api/v1/tenants/{id}/keys", h.ListAPIKeys)
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys", h.CreateAPIKey)
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys/rotate", h.BulkRotateTenantKeys)
	srv := httptest.NewServer(servePrincipal(attacker, mux))
	defer srv.Close()

	do := func(method, path, body string) int {
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rdr)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	cases := []struct {
		name, method, path, body string
	}{
		{"rotate B key", "POST", "/api/v1/keys/" + bKey.Key.ID + "/rotate", ""},
		{"delete B key", "DELETE", "/api/v1/keys/" + bKey.Key.ID, ""},
		{"demote B key", "PATCH", "/api/v1/keys/" + bKey.Key.ID, `{"role":"viewer"}`},
		{"list B keys", "GET", "/api/v1/tenants/" + tenantB.ID + "/keys", ""},
		{"mint key in B", "POST", "/api/v1/tenants/" + tenantB.ID + "/keys", `{"name":"x","role":"admin"}`},
		{"bulk-rotate B", "POST", "/api/v1/tenants/" + tenantB.ID + "/keys/rotate", ""},
	}
	for _, c := range cases {
		if got := do(c.method, c.path, c.body); got != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", c.name, got)
		}
	}

	// B's credential must be completely untouched by all of the above.
	p, err := store.Resolve(ctx, bKey.Plaintext)
	if err != nil {
		t.Fatalf("B's key was affected by a cross-tenant op (no longer resolves): %v", err)
	}
	if p.Role != tenancy.RoleEditor {
		t.Errorf("B's key role changed cross-tenant: %q", p.Role)
	}
	keys, _ := store.ListAPIKeys(ctx, tenantB.ID)
	if len(keys) != 1 {
		t.Errorf("tenant B key count = %d, want 1 (no cross-tenant create/delete)", len(keys))
	}
}

func TestTenancyHandlers_OversizedBody(t *testing.T) {
	// X-DOS-001: a control-plane JSON body over the cap is rejected 413.
	store := newRotateTestStore(t)
	recorder := audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
	h := &TenancyHandlers{Store: store, Recorder: recorder}

	srv := httptest.NewServer(http.HandlerFunc(h.CreateTenant))
	defer srv.Close()

	big := `{"name":"` + strings.Repeat("a", maxJSONBodyBytes+1024) + `"}`
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}
