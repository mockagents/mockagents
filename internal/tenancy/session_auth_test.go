package tenancy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAuthMiddleware_SessionCookie verifies the dual-auth path: a request with
// no API key but a valid session cookie authenticates, and the resolved
// Principal carries the session's tenant + role (REF-08 slice D).
func TestAuthMiddleware_SessionCookie(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	ten, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	u, err := store.CreateUser(ctx, "alice@acme.com", ten.ID, RoleEditor)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.CreateSession(ctx, u.ID, ten.ID, u.Role, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var got *Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware(store, nil)(next)

	// 1. Session cookie authenticates with no API key.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session auth = %d, want 200", rec.Code)
	}
	if got == nil || got.TenantID != ten.ID || got.Role != RoleEditor || got.KeyID != u.ID {
		t.Fatalf("principal = %+v", got)
	}

	// 2. An invalid session token is 401.
	bad := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	bad.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "mas_bogus"})
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusUnauthorized {
		t.Errorf("invalid session = %d, want 401", badRec.Code)
	}

	// 3. No credentials at all is 401.
	none := httptest.NewRecorder()
	h.ServeHTTP(none, httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil))
	if none.Code != http.StatusUnauthorized {
		t.Errorf("no credentials = %d, want 401", none.Code)
	}

	// 4. After logout the session no longer authenticates.
	if err := store.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	gone := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	gone.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	goneRec := httptest.NewRecorder()
	h.ServeHTTP(goneRec, gone)
	if goneRec.Code != http.StatusUnauthorized {
		t.Errorf("revoked session = %d, want 401", goneRec.Code)
	}
}
