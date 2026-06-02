package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// newAuditTestStore builds an isolated audit store seeded with events from
// two different tenants plus one tenant-less system event.
func newAuditTestStore(t *testing.T) *audit.SQLiteStore {
	t.Helper()
	store, err := audit.NewSQLiteStore(filepath.Join(t.TempDir(), "audit_test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := t.Context()
	seed := []*audit.Event{
		{Kind: audit.EventAPIKeyCreated, Target: "key_a1", Actor: audit.Actor{Name: "a-admin", TenantID: "ten_a", Role: "admin"}, Timestamp: time.Now().Add(-3 * time.Minute)},
		{Kind: audit.EventAPIKeyCreated, Target: "key_b1", Actor: audit.Actor{Name: "b-admin", TenantID: "ten_b", Role: "admin"}, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Kind: audit.EventAPIKeyDeleted, Target: "key_a1", Actor: audit.Actor{Name: "a-admin", TenantID: "ten_a", Role: "admin"}, Timestamp: time.Now().Add(-1 * time.Minute)},
		{Kind: audit.EventAuthDenied, Actor: audit.Actor{Name: "anonymous"}, Timestamp: time.Now()},
	}
	for i, e := range seed {
		if err := store.Append(ctx, e); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	return store
}

// TestAuditHandlers_TenantScoped is the X-SEC-002 regression guard: a
// tenant-A admin hitting GET /api/v1/audit must see only tenant-A events,
// never tenant-B's audit trail (nor the tenant-less system event).
func TestAuditHandlers_TenantScoped(t *testing.T) {
	h := &AuditHandlers{Store: newAuditTestStore(t)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/audit", h.ListEvents)

	adminA := &tenancy.Principal{TenantID: "ten_a", KeyID: "k_a", Role: tenancy.RoleAdmin}
	srv := servePrincipal(adminA, mux)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var events []*audit.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("tenant-A admin saw %d events, want 2 (own tenant only)", len(events))
	}
	for _, e := range events {
		if e.Actor.TenantID != "ten_a" {
			t.Errorf("cross-tenant leak: event from tenant %q exposed to ten_a admin", e.Actor.TenantID)
		}
	}
}

// TestAuditHandlers_SingleTenantUnfiltered confirms that with no principal
// on the context (single-tenant / local-dev mode) the endpoint returns the
// full, unfiltered view — the scoping must not break the common local case.
func TestAuditHandlers_SingleTenantUnfiltered(t *testing.T) {
	h := &AuditHandlers{Store: newAuditTestStore(t)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/audit", h.ListEvents)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req) // no servePrincipal wrapper => anonymous

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var events []*audit.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("single-tenant view returned %d events, want all 4", len(events))
	}
}

// TestAuditHandlers_InputValidation covers F-AU-002: an unknown kind and a
// malformed since are 400s, and a nil store is a 503.
func TestAuditHandlers_InputValidation(t *testing.T) {
	h := &AuditHandlers{Store: newAuditTestStore(t)}

	t.Run("unknown kind 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ListEvents(rec, httptest.NewRequest("GET", "/api/v1/audit?kind=bogus.kind", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("bad since 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ListEvents(rec, httptest.NewRequest("GET", "/api/v1/audit?since=not-a-timestamp", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("limit out of range 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ListEvents(rec, httptest.NewRequest("GET", "/api/v1/audit?limit=-1", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("nil store 503", func(t *testing.T) {
		nilH := &AuditHandlers{Store: nil}
		rec := httptest.NewRecorder()
		nilH.ListEvents(rec, httptest.NewRequest("GET", "/api/v1/audit", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})
}
