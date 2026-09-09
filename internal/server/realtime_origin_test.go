package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// TestRealtimeTenantFor_CookiePrincipalCrossOrigin is the audit H-04 guard:
// a tenant principal that could only have come from the SSO cookie is
// dropped on a cross-origin WebSocket handshake, and kept in every case a
// foreign page cannot forge.
func TestRealtimeTenantFor_CookiePrincipalCrossOrigin(t *testing.T) {
	mk := func(origin string, cookie, bearer bool) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://api.example:8080/v1/realtime", nil)
		r.Host = "api.example:8080"
		r = r.WithContext(engine.WithTenantID(r.Context(), "ten_acme"))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie {
			r.AddCookie(&http.Cookie{Name: tenancy.SessionCookieName, Value: "mas_x"})
		}
		if bearer {
			r.Header.Set("Authorization", "Bearer mak_x")
		}
		return r
	}
	cases := []struct {
		name    string
		req     *http.Request
		allowed []string
		want    string
	}{
		{"cookie + foreign origin", mk("https://evil.example", true, false), nil, ""},
		{"cookie + same origin", mk("http://api.example:8080", true, false), nil, "ten_acme"},
		{"cookie + same origin, https scheme", mk("https://api.example:8080", true, false), nil, "ten_acme"},
		{"cookie + allow-listed origin", mk("https://console.example", true, false), []string{"https://console.example"}, "ten_acme"},
		{"cookie + wildcard allowlist", mk("https://anything.example", true, false), []string{"*"}, "ten_acme"},
		{"cookie + no origin (non-browser)", mk("", true, false), nil, "ten_acme"},
		{"bearer + foreign origin", mk("https://evil.example", false, true), nil, "ten_acme"},
		{"bearer and cookie + foreign origin", mk("https://evil.example", true, true), nil, "ten_acme"},
		{"no credential at all + foreign origin", mk("https://evil.example", false, false), nil, "ten_acme"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := realtimeTenantFor(tc.req, tc.allowed); got != tc.want {
				t.Fatalf("tenant = %q, want %q", got, tc.want)
			}
		})
	}
	// No principal at all is always anonymous.
	anon := httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	if got := realtimeTenantFor(anon, nil); got != "" {
		t.Fatalf("anonymous request scoped to %q", got)
	}
}
