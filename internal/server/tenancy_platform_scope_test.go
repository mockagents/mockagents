package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/tenancy"
)

// platformScopeFixture stands up the key-management routes with a store
// holding the bootstrap tenant (where the platform key lives) and one
// customer tenant, and returns a server that authenticates every request as
// the given principal.
func platformScopeFixture(t *testing.T, caller func(bootstrap, acme *tenancy.Tenant) *tenancy.Principal) (*httptest.Server, *tenancy.Tenant, *tenancy.SQLiteStore) {
	t.Helper()
	store := newRotateTestStore(t)
	bootstrap, err := store.CreateTenant(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	acme, err := store.CreateTenant(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	h := &TenancyHandlers{Store: store, Recorder: newTestRecorder()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tenants/{id}/keys", h.ListAPIKeys)
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys", h.CreateAPIKey)
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys/rotate", h.BulkRotateTenantKeys)
	mux.HandleFunc("PATCH /api/v1/keys/{id}", h.UpdateAPIKeyRole)
	mux.HandleFunc("POST /api/v1/keys/{id}/rotate", h.RotateAPIKey)
	mux.HandleFunc("DELETE /api/v1/keys/{id}", h.DeleteAPIKey)
	srv := httptest.NewServer(servePrincipal(caller(bootstrap, acme), mux))
	t.Cleanup(srv.Close)
	return srv, acme, store
}

func TestTenantAdminCannotMutatePlatformKeyInOwnTenant(t *testing.T) {
	srv, _, store := platformScopeFixture(t, func(bootstrap, _ *tenancy.Tenant) *tenancy.Principal {
		return &tenancy.Principal{TenantID: bootstrap.ID, KeyID: "k_admin", Role: tenancy.RoleAdmin}
	})
	bootstrap, err := store.ListTenants(context.Background())
	if err != nil || len(bootstrap) < 1 {
		t.Fatalf("list tenants: %v", err)
	}
	var defaultTenant *tenancy.Tenant
	for _, tenant := range bootstrap {
		if tenant.Name == "default" {
			defaultTenant = tenant
		}
	}
	if defaultTenant == nil {
		t.Fatal("default tenant missing")
	}
	platform, err := store.CreateAPIKeyWithPlaintext(context.Background(), defaultTenant.ID, "platform", tenancy.RolePlatform, "mak_12345678_abcdefghijklmnopqrstuvwxyz012345")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/keys/" + platform.ID + "/rotate", ""},
		{http.MethodPatch, "/api/v1/keys/" + platform.ID, `{"role":"viewer"}`},
		{http.MethodDelete, "/api/v1/keys/" + platform.ID, ""},
		{http.MethodPost, "/api/v1/tenants/" + defaultTenant.ID + "/keys/rotate", ""},
	}
	for _, tc := range cases {
		resp := do(t, tc.method, srv.URL+tc.path, tc.body)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
	if _, err := store.Resolve(context.Background(), "mak_12345678_abcdefghijklmnopqrstuvwxyz012345"); err != nil {
		t.Fatalf("denied mutation changed platform credential: %v", err)
	}
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestPlatform_CanOnboardATenant is the audit H-02 regression: the platform
// operator creates tenants, but its key lives in the bootstrap tenant, so the
// own-tenant gate used to 404 the very next call — minting the new tenant's
// first admin key. A fresh tenant had no way to obtain a credential.
func TestPlatform_CanOnboardATenant(t *testing.T) {
	srv, acme, _ := platformScopeFixture(t, func(bootstrap, _ *tenancy.Tenant) *tenancy.Principal {
		return &tenancy.Principal{TenantID: bootstrap.ID, KeyID: "k_platform", Role: tenancy.RolePlatform}
	})

	// Mint the tenant's first admin key.
	resp := do(t, http.MethodPost, srv.URL+"/api/v1/tenants/"+acme.ID+"/keys", `{"name":"acme-admin","role":"admin"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("platform create key in new tenant: status = %d, want 201", resp.StatusCode)
	}
	var minted tenancy.NewAPIKeyResult
	if err := json.NewDecoder(resp.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}
	if minted.Key.TenantID != acme.ID {
		t.Fatalf("minted key tenant = %q, want %q", minted.Key.TenantID, acme.ID)
	}

	// And see it in that tenant's listing.
	resp = do(t, http.MethodGet, srv.URL+"/api/v1/tenants/"+acme.ID+"/keys", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("platform list keys: status = %d, want 200", resp.StatusCode)
	}
	var listed []tenancy.APIKey
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != minted.Key.ID {
		t.Fatalf("listed keys = %+v, want the one just minted", listed)
	}

	// The flat key routes need the tenant named explicitly (they carry none
	// in the path). Without it the platform is scoped to its own tenant and
	// the key is invisible; with it the key can be administered.
	resp = do(t, http.MethodPatch, srv.URL+"/api/v1/keys/"+minted.Key.ID, `{"role":"editor"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("patch without ?tenant: status = %d, want 404", resp.StatusCode)
	}
	resp = do(t, http.MethodPatch, srv.URL+"/api/v1/keys/"+minted.Key.ID+"?tenant="+acme.ID, `{"role":"editor"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch with ?tenant: status = %d, want 200", resp.StatusCode)
	}
	resp = do(t, http.MethodPost, srv.URL+"/api/v1/keys/"+minted.Key.ID+"/rotate?tenant="+acme.ID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate with ?tenant: status = %d, want 200", resp.StatusCode)
	}
	resp = do(t, http.MethodDelete, srv.URL+"/api/v1/keys/"+minted.Key.ID+"?tenant="+acme.ID, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete with ?tenant: status = %d, want 204", resp.StatusCode)
	}

	// An unknown tenant is still a 404 — the bypass is for tenants that exist.
	resp = do(t, http.MethodPost, srv.URL+"/api/v1/tenants/ten_nope/keys", `{"name":"x","role":"admin"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("platform create key in unknown tenant: status = %d, want 404", resp.StatusCode)
	}
}

// TestTenantAdmin_CannotReachAnotherTenant pins X-SEC-001 alongside the new
// platform bypass: a per-tenant admin still sees a 404 for another tenant's
// path id, and the ?tenant= parameter is inert for it.
func TestTenantAdmin_CannotReachAnotherTenant(t *testing.T) {
	srv, acme, store := platformScopeFixture(t, func(bootstrap, _ *tenancy.Tenant) *tenancy.Principal {
		return &tenancy.Principal{TenantID: bootstrap.ID, KeyID: "k_admin", Role: tenancy.RoleAdmin}
	})
	victim, err := store.CreateAPIKey(context.Background(), acme.ID, "acme-ci", tenancy.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ method, url, body string }{
		{http.MethodPost, "/api/v1/tenants/" + acme.ID + "/keys", `{"name":"x","role":"admin"}`},
		{http.MethodGet, "/api/v1/tenants/" + acme.ID + "/keys", ""},
		{http.MethodPatch, "/api/v1/keys/" + victim.Key.ID + "?tenant=" + acme.ID, `{"role":"admin"}`},
		{http.MethodPost, "/api/v1/keys/" + victim.Key.ID + "/rotate?tenant=" + acme.ID, ""},
		{http.MethodDelete, "/api/v1/keys/" + victim.Key.ID + "?tenant=" + acme.ID, ""},
	}
	for _, c := range cases {
		resp := do(t, c.method, srv.URL+c.url, c.body)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (tenant admin must not cross tenants)", c.method, c.url, resp.StatusCode)
		}
	}
	// And the victim key is untouched.
	keys, err := store.ListAPIKeys(context.Background(), acme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Role != tenancy.RoleViewer || keys[0].Prefix != victim.Key.Prefix {
		t.Fatalf("victim key changed: %+v", keys)
	}
}
