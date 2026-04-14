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
