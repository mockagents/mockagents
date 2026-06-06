package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/oidcauth"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// fakeAuth is a stand-in Authenticator so the callback can be tested without a
// live IdP. Exchange returns the configured claims (or error).
type fakeAuth struct {
	claims      *oidcauth.Claims
	err         error
	gotState    string
	gotVerifier string
	gotCode     string
}

func (f *fakeAuth) AuthCodeURL(state, verifier string) string {
	f.gotState, f.gotVerifier = state, verifier
	return "https://idp.example/authorize?state=" + url.QueryEscape(state)
}

func (f *fakeAuth) Exchange(_ context.Context, code, _ string) (*oidcauth.Claims, error) {
	f.gotCode = code
	if f.err != nil {
		return nil, f.err
	}
	return f.claims, nil
}

func newSSO(t *testing.T, auth oidcauth.Authenticator) (*SSOHandlers, tenancy.Store, string) {
	t.Helper()
	store, err := tenancy.NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ten, err := store.CreateTenant(context.Background(), "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	h := &SSOHandlers{
		Auth:        auth,
		Store:       store,
		DomainMap:   map[string]string{"acme.com": ten.ID},
		DefaultRole: tenancy.RoleEditor,
		SessionTTL:  time.Hour,
	}
	return h, store, ten.ID
}

// doLogin runs Login and returns the flow cookies + the state the handler sent.
func doLogin(t *testing.T, h *SSOHandlers) ([]*http.Cookie, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login code = %d, want 302", rec.Code)
	}
	a, _ := h.Auth.(*fakeAuth)
	return rec.Result().Cookies(), a.gotState
}

func TestSSO_LoginSetsFlowCookies(t *testing.T) {
	auth := &fakeAuth{}
	h, _, _ := newSSO(t, auth)
	cookies, state := doLogin(t, h)

	var hasState, hasVerifier bool
	for _, c := range cookies {
		switch c.Name {
		case oidcStateCookie:
			hasState = c.Value == state && c.HttpOnly
		case oidcVerifierCookie:
			hasVerifier = c.Value == auth.gotVerifier && c.HttpOnly
		}
	}
	if !hasState || !hasVerifier {
		t.Errorf("login should set HttpOnly state+verifier cookies (state=%v verifier=%v)", hasState, hasVerifier)
	}
	if auth.gotState == "" || auth.gotVerifier == "" {
		t.Error("AuthCodeURL not called with state + PKCE verifier")
	}
}

func TestSSO_CallbackHappyPath_JITProvisions(t *testing.T) {
	// Mixed-case email exercises normalization to a lowercase domain.
	auth := &fakeAuth{claims: &oidcauth.Claims{Email: "Alice@ACME.com"}}
	h, store, tenID := newSSO(t, auth)
	cookies, state := doLogin(t, h)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+url.QueryEscape(state), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}

	var sessTok string
	for _, c := range rec.Result().Cookies() {
		if c.Name == tenancy.SessionCookieName {
			sessTok = c.Value
		}
	}
	if sessTok == "" {
		t.Fatal("callback set no session cookie")
	}
	// The session resolves to the mapped tenant + default role.
	p, err := store.ResolveSession(context.Background(), sessTok)
	if err != nil || p.TenantID != tenID || p.Role != tenancy.RoleEditor {
		t.Fatalf("ResolveSession = %+v err=%v", p, err)
	}
	// The user was JIT-provisioned under the normalized email.
	u, err := store.GetUserByEmail(context.Background(), "alice@acme.com")
	if err != nil || u.TenantID != tenID {
		t.Fatalf("GetUserByEmail = %+v err=%v", u, err)
	}

	// A second login for the same email reuses the user (no duplicate error).
	cookies2, state2 := doLogin(t, h)
	req2 := httptest.NewRequest(http.MethodGet, "/auth/callback?code=def&state="+url.QueryEscape(state2), nil)
	for _, c := range cookies2 {
		req2.AddCookie(c)
	}
	rec2 := httptest.NewRecorder()
	h.Callback(rec2, req2)
	if rec2.Code != http.StatusFound {
		t.Fatalf("second callback = %d, want 302; body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestSSO_CallbackStateMismatchRejected(t *testing.T) {
	auth := &fakeAuth{claims: &oidcauth.Claims{Email: "alice@acme.com"}}
	h, _, _ := newSSO(t, auth)
	cookies, _ := doLogin(t, h)

	// Wrong state query param vs the cookie → CSRF defense → 400.
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state=forged", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("state mismatch = %d, want 400", rec.Code)
	}
	if auth.gotCode != "" {
		t.Error("code exchange must not run when state mismatches")
	}
}

func TestSSO_CallbackUnmappedDomainForbidden(t *testing.T) {
	auth := &fakeAuth{claims: &oidcauth.Claims{Email: "bob@evil.com"}}
	h, _, _ := newSSO(t, auth)
	cookies, state := doLogin(t, h)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+url.QueryEscape(state), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unmapped domain = %d, want 403", rec.Code)
	}
}

func TestSSO_CallbackExchangeErrorUnauthorized(t *testing.T) {
	auth := &fakeAuth{err: errors.New("bad code")}
	h, _, _ := newSSO(t, auth)
	cookies, state := doLogin(t, h)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+url.QueryEscape(state), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("exchange error = %d, want 401", rec.Code)
	}
}

func TestSSO_CallbackMissingFlowCookies(t *testing.T) {
	auth := &fakeAuth{claims: &oidcauth.Claims{Email: "alice@acme.com"}}
	h, _, _ := newSSO(t, auth)
	// No flow cookies at all → 400 (can't validate state).
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state=x", nil)
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no flow cookies = %d, want 400", rec.Code)
	}
}

func TestSSO_LogoutRevokesSession(t *testing.T) {
	auth := &fakeAuth{}
	h, store, tenID := newSSO(t, auth)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "x@acme.com", tenID, tenancy.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, tenID, u.Role, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: tenancy.SessionCookieName, Value: tok})
	rec := httptest.NewRecorder()
	h.Logout(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200", rec.Code)
	}
	if _, err := store.ResolveSession(ctx, tok); !errors.Is(err, tenancy.ErrInvalidSession) {
		t.Errorf("session should be revoked after logout, got %v", err)
	}
}
