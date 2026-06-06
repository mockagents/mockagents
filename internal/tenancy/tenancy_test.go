package tenancy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// newTestStore opens a fresh SQLite store under t.TempDir() so every
// test gets an isolated database.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "tenancy.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// --- Role semantics ---

func TestRoleRankAndAtLeast(t *testing.T) {
	if !RoleAdmin.AtLeast(RoleViewer) {
		t.Error("admin should be >= viewer")
	}
	if RoleViewer.AtLeast(RoleAdmin) {
		t.Error("viewer should not be >= admin")
	}
	if RoleEditor.AtLeast(RoleAdmin) {
		t.Error("editor should not be >= admin")
	}
	if !RoleEditor.AtLeast(RoleEditor) {
		t.Error("editor should be >= editor")
	}
	if Role("bogus").AtLeast(RoleViewer) {
		t.Error("unknown role should never satisfy RequireRole")
	}
}

// --- Store CRUD ---

func TestCreateAndGetTenant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if tenant.ID == "" || tenant.Name != "acme" {
		t.Errorf("unexpected tenant: %+v", tenant)
	}
	got, err := store.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if got.Name != "acme" {
		t.Errorf("GetTenant returned %+v", got)
	}
}

func TestCreateTenantRejectsDuplicateName(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.CreateTenant(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTenant(ctx, "acme"); err == nil {
		t.Error("expected duplicate tenant name to fail")
	}
}

func TestCreateAPIKeyReturnsPlaintextOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")

	result, err := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", RoleEditor)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if result.Plaintext == "" {
		t.Fatal("plaintext missing")
	}
	if result.Key.Prefix == "" || result.Key.Role != RoleEditor {
		t.Errorf("key metadata wrong: %+v", result.Key)
	}

	// List should return metadata without the hash or plaintext.
	keys, err := store.ListAPIKeys(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
}

func TestResolveValidatesPlaintext(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")
	result, _ := store.CreateAPIKey(ctx, tenant.ID, "ci", RoleAdmin)

	principal, err := store.Resolve(ctx, result.Plaintext)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if principal.TenantID != tenant.ID {
		t.Errorf("tenant = %q, want %q", principal.TenantID, tenant.ID)
	}
	if principal.Role != RoleAdmin {
		t.Errorf("role = %q, want admin", principal.Role)
	}

	// Wrong key must fail with ErrInvalidKey.
	if _, err := store.Resolve(ctx, "mak_00000000_bogus"); err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestDeleteTenantCascadesKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")
	_, _ = store.CreateAPIKey(ctx, tenant.ID, "k1", RoleViewer)

	if err := store.DeleteTenant(ctx, tenant.ID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	keys, err := store.ListAPIKeys(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys after tenant delete, got %d", len(keys))
	}
}

// --- Middleware ---

func mustCreateKey(t *testing.T, store *SQLiteStore, role Role) string {
	t.Helper()
	ctx := context.Background()
	tenant, err := store.CreateTenantByName(ctx, role)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CreateAPIKey(ctx, tenant.ID, "test", role)
	if err != nil {
		t.Fatal(err)
	}
	return result.Plaintext
}

// CreateTenantByName is a convenience helper kept local to tests: it
// upserts a tenant whose name is a deterministic function of the role
// so tests that want independent tenants get them automatically.
func (s *SQLiteStore) CreateTenantByName(ctx context.Context, role Role) (*Tenant, error) {
	name := "test-" + string(role)
	if existing, err := s.GetTenantByName(ctx, name); err == nil {
		return existing, nil
	}
	return s.CreateTenant(ctx, name)
}

func TestAuthMiddlewareRejectsMissingKey(t *testing.T) {
	store := newTestStore(t)
	handler := AuthMiddleware(store, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareAcceptsBearerAndXApiKey(t *testing.T) {
	store := newTestStore(t)
	key := mustCreateKey(t, store, RoleAdmin)

	handler := AuthMiddleware(store, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p == nil {
			t.Error("principal missing from context")
			return
		}
		if p.Role != RoleAdmin {
			t.Errorf("role = %q, want admin", p.Role)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Bearer
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("bearer auth failed: %d %s", rec.Code, rec.Body.String())
	}

	// X-Api-Key
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("X-Api-Key", key)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("X-Api-Key auth failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddlewareSkipPredicate(t *testing.T) {
	store := newTestStore(t)
	skip := func(r *http.Request) bool { return r.URL.Path == "/health" }
	handler := AuthMiddleware(store, skip)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("skip predicate failed: status %d", rec.Code)
	}
}

func TestRequireRoleForbidsInsufficient(t *testing.T) {
	store := newTestStore(t)
	key := mustCreateKey(t, store, RoleViewer)

	inner := RequireRole(RoleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler := AuthMiddleware(store, nil)(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestRequireRolePassesExactMatch(t *testing.T) {
	store := newTestStore(t)
	key := mustCreateKey(t, store, RoleEditor)

	inner := RequireRole(RoleEditor, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler := AuthMiddleware(store, nil)(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestDenialHook_AuthMiddleware verifies that both the missing-key
// and invalid-key rejection paths in AuthMiddleware fire the denial
// hook with the correct status and a human-readable reason.
func TestDenialHook_AuthMiddleware(t *testing.T) {
	store := newTestStore(t)

	type denial struct {
		path   string
		status int
		reason string
	}
	var got []denial
	SetDenialHook(func(r *http.Request, status int, reason string) {
		got = append(got, denial{path: r.URL.Path, status: status, reason: reason})
	})
	t.Cleanup(func() { SetDenialHook(nil) })

	handler := AuthMiddleware(store, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Missing credentials.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	// Invalid credentials.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/keys/abc", nil)
	req.Header.Set("Authorization", "Bearer mak_not_a_real_key_99")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if len(got) != 2 {
		t.Fatalf("expected 2 denial events, got %d: %+v", len(got), got)
	}
	if got[0].status != http.StatusUnauthorized || got[0].reason != "missing credentials" {
		t.Errorf("first denial wrong: %+v", got[0])
	}
	if got[1].status != http.StatusUnauthorized || got[1].reason != "invalid credential" {
		t.Errorf("second denial wrong: %+v", got[1])
	}
}

// TestDenialHook_RequireRole verifies that the 403 path through
// RequireRole fires the denial hook with the caller's actual role
// embedded in the reason.
func TestDenialHook_RequireRole(t *testing.T) {
	store := newTestStore(t)
	key := mustCreateKey(t, store, RoleViewer)

	var got int
	var gotReason string
	SetDenialHook(func(r *http.Request, status int, reason string) {
		got = status
		gotReason = reason
	})
	t.Cleanup(func() { SetDenialHook(nil) })

	inner := RequireRole(RoleAdmin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler := AuthMiddleware(store, nil)(inner)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/42", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got != http.StatusForbidden {
		t.Errorf("denial status = %d, want 403", got)
	}
	if gotReason == "" || !contains(gotReason, "viewer") || !contains(gotReason, "admin") {
		t.Errorf("denial reason missing role transition: %q", gotReason)
	}
}

// TestUpdateAPIKeyRole_RoundTrip exercises the new
// UpdateAPIKeyRole store method: promote viewer -> admin, verify the
// previous role comes back, and confirm subsequent Resolve reflects
// the new permission.
func TestUpdateAPIKeyRole_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	result, err := store.CreateAPIKey(ctx, tenant.ID, "promoter", RoleViewer)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	prev, next, err := store.UpdateAPIKeyRole(ctx, result.Key.TenantID, result.Key.ID, RoleAdmin)
	if err != nil {
		t.Fatalf("UpdateAPIKeyRole: %v", err)
	}
	if prev != RoleViewer || next != RoleAdmin {
		t.Errorf("transition = %q -> %q, want viewer -> admin", prev, next)
	}

	principal, err := store.Resolve(ctx, result.Plaintext)
	if err != nil {
		t.Fatalf("Resolve after update: %v", err)
	}
	if principal.Role != RoleAdmin {
		t.Errorf("principal.Role = %q after promotion, want admin", principal.Role)
	}

	if _, _, err := store.UpdateAPIKeyRole(ctx, "ten_any", "no-such-id", RoleEditor); err == nil {
		t.Error("expected ErrNotFound for unknown id")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
