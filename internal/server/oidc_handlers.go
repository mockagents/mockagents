package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/oidcauth"
	"github.com/mockagents/mockagents/internal/tenancy"
)

const (
	oidcStateCookie    = "oidc_state"
	oidcVerifierCookie = "oidc_verifier"
	oidcFlowMaxAge     = 600 // 10 min — the login round-trip window
)

// SSOHandlers implements the OIDC relying-party endpoints (REF-08 slice D):
// GET /auth/login starts the flow, GET /auth/callback completes it (mapping the
// email domain to a tenant, JIT-provisioning a user, and minting a session),
// and POST /auth/logout revokes the session.
type SSOHandlers struct {
	Auth        oidcauth.Authenticator
	Store       tenancy.Store
	DomainMap   map[string]string // email domain -> tenant id
	DefaultRole tenancy.Role
	SessionTTL  time.Duration
	// Secure marks cookies Secure (HTTPS-only). Set when the deployment URL is
	// https; left off for local http development.
	Secure bool
}

// Login generates a state + PKCE verifier, stores them in short-lived cookies,
// and redirects to the IdP.
func (h *SSOHandlers) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randToken()
	if err != nil {
		writeServerError(w, err)
		return
	}
	verifier := oidcauth.GenerateVerifier()
	http.SetCookie(w, h.flowCookie(oidcStateCookie, state))
	http.SetCookie(w, h.flowCookie(oidcVerifierCookie, verifier))
	http.Redirect(w, r, h.Auth.AuthCodeURL(state, verifier), http.StatusFound)
}

// Callback validates state (CSRF), exchanges the code, maps the verified email
// to a tenant, JIT-provisions the user, mints a session, and sets the session
// cookie.
func (h *SSOHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	// Always clear the flow cookies, success or failure.
	http.SetCookie(w, h.expireCookie(oidcStateCookie, "/auth"))
	http.SetCookie(w, h.expireCookie(oidcVerifierCookie, "/auth"))

	stateCookie, sErr := r.Cookie(oidcStateCookie)
	verifierCookie, vErr := r.Cookie(oidcVerifierCookie)
	if sErr != nil || vErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing login state; restart the flow"})
		return
	}
	// State must match the cookie (CSRF defense).
	if q := r.URL.Query().Get("state"); q == "" || q != stateCookie.Value {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state mismatch"})
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing authorization code"})
		return
	}

	claims, err := h.Auth.Exchange(r.Context(), code, verifierCookie.Value)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "identity provider returned no email"})
		return
	}

	tenantID, ok := h.DomainMap[emailDomain(email)]
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "email domain is not mapped to a tenant"})
		return
	}

	// JIT-provision the user on first login (email-domain → tenant policy).
	user, err := h.Store.GetUserByEmail(r.Context(), email)
	if errors.Is(err, tenancy.ErrNotFound) {
		user, err = h.Store.CreateUser(r.Context(), email, tenantID, h.DefaultRole)
	}
	if err != nil {
		writeServerError(w, err)
		return
	}

	token, sess, err := h.Store.CreateSession(r.Context(), user.ID, user.TenantID, user.Role, h.SessionTTL)
	if err != nil {
		writeServerError(w, err)
		return
	}
	http.SetCookie(w, h.sessionCookie(token, sess.ExpiresAt))
	http.Redirect(w, r, "/", http.StatusFound)
}

// Logout revokes the caller's session and clears the cookie.
func (h *SSOHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(tenancy.SessionCookieName); err == nil && c.Value != "" {
		_ = h.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, h.expireCookie(tenancy.SessionCookieName, "/"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// --- cookie helpers ---

func (h *SSOHandlers) flowCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth",
		MaxAge:   oidcFlowMaxAge,
		HttpOnly: true,
		Secure:   h.Secure,
		// Lax so the cookie rides the top-level GET redirect back from the IdP.
		SameSite: http.SameSiteLaxMode,
	}
}

func (h *SSOHandlers) sessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     tenancy.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (h *SSOHandlers) expireCookie(name, path string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// randToken returns a URL-safe random token for the OIDC state parameter.
func randToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// emailDomain returns the lowercase domain part of an email, or "" if malformed.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return email[at+1:]
}
