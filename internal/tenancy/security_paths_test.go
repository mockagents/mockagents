package tenancy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestStore_CrossTenantKeyOps_ReturnNotFound covers F-ST-010: the X-SEC-001
// key-scoping fix — rotate/delete/update of a key in another tenant must
// return ErrNotFound and leave the key untouched.
func TestStore_CrossTenantKeyOps_ReturnNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenA, err := s.CreateTenant(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	tenB, err := s.CreateTenant(ctx, "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := s.CreateAPIKey(ctx, tenB.ID, "b-key", RoleEditor)
	if err != nil {
		t.Fatal(err)
	}
	idB := keyB.Key.ID

	// Tenant A must not be able to touch tenant B's key.
	if _, _, err := s.RotateAPIKey(ctx, tenA.ID, idB); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant RotateAPIKey err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteAPIKey(ctx, tenA.ID, idB); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant DeleteAPIKey err = %v, want ErrNotFound", err)
	}
	if _, _, err := s.UpdateAPIKeyRole(ctx, tenA.ID, idB, RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant UpdateAPIKeyRole err = %v, want ErrNotFound", err)
	}

	// Key B is unchanged: still present, still editor.
	keysB, err := s.ListAPIKeys(ctx, tenB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keysB) != 1 || keysB[0].Role != RoleEditor {
		t.Fatalf("key B mutated by cross-tenant ops: %+v", keysB)
	}
	// ...and its OWN tenant can still operate on it.
	if _, _, err := s.RotateAPIKey(ctx, tenB.ID, idB); err != nil {
		t.Errorf("owner RotateAPIKey failed: %v", err)
	}
}

// TestBulkRotate_RollsBackOnFailure covers F-ST-009: BulkRotateTenantKeys is
// all-or-nothing — a mid-transaction failure must leave every key unrotated. A
// context timeout far shorter than N bcrypt hashes forces the failure without
// being able to complete (so it can never false-pass by finishing in time).
func TestBulkRotate_RollsBackOnFailure(t *testing.T) {
	s := newTestStore(t)
	bg := context.Background()
	ten, err := s.CreateTenant(bg, "acme")
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	orig := make([]string, 0, n)
	for i := range n {
		k, err := s.CreateAPIKey(bg, ten.ID, fmt.Sprintf("k%d", i), RoleViewer)
		if err != nil {
			t.Fatal(err)
		}
		orig = append(orig, k.Key.Prefix)
	}

	// 40ms cannot cover 8 bcrypt hashes (~10ms+ each), so the rotation is
	// guaranteed to fail mid-transaction (or before it starts) — never to
	// complete. Either way zero keys should change.
	ctx, cancel := context.WithTimeout(bg, 40*time.Millisecond)
	defer cancel()
	if _, _, err := s.BulkRotateTenantKeys(ctx, ten.ID); err == nil {
		t.Fatal("expected BulkRotateTenantKeys to fail under the short timeout")
	}

	keys, err := s.ListAPIKeys(bg, ten.ID)
	if err != nil {
		t.Fatal(err)
	}
	have := make(map[string]bool, len(keys))
	for _, k := range keys {
		have[k.Prefix] = true
	}
	for _, p := range orig {
		if !have[p] {
			t.Errorf("prefix %q changed — bulk rotate did not roll back", p)
		}
	}
}

// errorStore embeds the Store interface (nil) and overrides only Resolve to
// return a non-sentinel error, exercising AuthMiddleware's fail-closed branch.
type errorStore struct{ Store }

func (errorStore) Resolve(context.Context, string) (*Principal, error) {
	return nil, errors.New("auth db is on fire")
}

// TestAuthMiddleware_FailsClosedOnStoreError covers F-MW-008: a raw (non
// ErrInvalidKey) error from the store must yield 500 and NOT reach the next
// handler — the auth boundary fails closed.
func TestAuthMiddleware_FailsClosedOnStoreError(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware(errorStore{}, nil)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer mak_aaaaaaaa_secretvalue")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (fail closed)", rec.Code)
	}
	if nextCalled {
		t.Error("next handler was reached despite a store error — middleware failed OPEN")
	}
}

// TestAuthMiddleware_SkipRoutePrincipal covers F-MW-009: on a skip-auth route a
// valid key still attaches the principal, while an invalid key proceeds
// anonymously (best-effort, never blocking the skip route).
func TestAuthMiddleware_SkipRoutePrincipal(t *testing.T) {
	s := newTestStore(t)
	ten, err := s.CreateTenant(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAPIKey(context.Background(), ten.ID, "k", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}

	var got *Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware(s, func(*http.Request) bool { return true })(next) // every route skipped

	// Valid key → principal attached.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+key.Plaintext)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil || got.TenantID != ten.ID {
		t.Errorf("skip route did not attach principal for a valid key: %+v", got)
	}

	// Invalid key → proceeds anonymously (no principal, still 200).
	got = nil
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req2.Header.Set("Authorization", "Bearer mak_zzzzzzzz_nosuchkey")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("skip route blocked an invalid key: status %d", rec2.Code)
	}
	if got != nil {
		t.Errorf("invalid key on a skip route attached a principal: %+v", got)
	}
}
