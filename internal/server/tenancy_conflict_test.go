package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/tenancy"
)

func newTestRecorder() *audit.Recorder {
	return audit.NewRecorder(nil, func(*http.Request) audit.Actor { return audit.Actor{Name: "test"} })
}

// TestStore_CreateTenant_DuplicateIsConflict is the store-level half of
// F-TN-008: a duplicate name must surface as ErrConflict, not a generic
// driver error, so the handler can map it to 409.
func TestStore_CreateTenant_DuplicateIsConflict(t *testing.T) {
	store := newRotateTestStore(t)
	ctx := context.Background()
	if _, err := store.CreateTenant(ctx, "acme"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := store.CreateTenant(ctx, "acme")
	if err == nil {
		t.Fatal("expected an error creating a duplicate tenant name")
	}
	if !errors.Is(err, tenancy.ErrConflict) {
		t.Errorf("duplicate create error = %v, want tenancy.ErrConflict", err)
	}
}

// TestTenancyHandlers_CreateTenant_DuplicateReturns409 is the handler-level
// half of F-TN-008.
func TestTenancyHandlers_CreateTenant_DuplicateReturns409(t *testing.T) {
	store := newRotateTestStore(t)
	if _, err := store.CreateTenant(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	h := &TenancyHandlers{Store: store, Recorder: newTestRecorder()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants", h.CreateTenant)
	admin := &tenancy.Principal{TenantID: "ten_x", KeyID: "k_admin", Role: tenancy.RoleAdmin}
	srv := httptest.NewServer(servePrincipal(admin, mux))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants", "application/json",
		strings.NewReader(`{"name":"acme"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate tenant create status = %d, want 409", resp.StatusCode)
	}
}

// mismatchedBulkStore returns parallel slices of different lengths from
// BulkRotateTenantKeys to exercise the F-TN-009 length guard. All other
// Store methods are inherited from the embedded nil interface and must not
// be called by the handler under test.
type mismatchedBulkStore struct {
	tenancy.Store
}

func (mismatchedBulkStore) BulkRotateTenantKeys(_ context.Context, _ string, _ ...string) ([]*tenancy.NewAPIKeyResult, []string, error) {
	// 2 results but only 1 old prefix — indexing oldPrefixes[1] would panic
	// without the guard.
	results := []*tenancy.NewAPIKeyResult{
		{Key: tenancy.APIKey{ID: "k1"}},
		{Key: tenancy.APIKey{ID: "k2"}},
	}
	return results, []string{"pfx1"}, nil
}

// TestTenancyHandlers_BulkRotate_LengthMismatchIs500 is the F-TN-009
// regression guard: a store that returns mismatched parallel slices must
// yield a clean 500, not an index-out-of-range panic mid-loop.
func TestTenancyHandlers_BulkRotate_LengthMismatchIs500(t *testing.T) {
	h := &TenancyHandlers{Store: mismatchedBulkStore{}, Recorder: newTestRecorder()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/{id}/keys/rotate", h.BulkRotateTenantKeys)
	admin := &tenancy.Principal{TenantID: "ten_x", KeyID: "k_admin", Role: tenancy.RoleAdmin}
	srv := httptest.NewServer(servePrincipal(admin, mux))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/tenants/ten_x/keys/rotate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("length-mismatch status = %d, want 500", resp.StatusCode)
	}
}
