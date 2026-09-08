package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/adapter"
)

// newTestOpenRoutes builds the same open-route set registerRoutes builds,
// from the same sources (builtins + the default adapter registry), without
// standing up a Server.
func newTestOpenRoutes() *openRoutes {
	o := newOpenRoutes()
	for _, p := range builtinOpenRoutes {
		o.add(p)
	}
	for _, a := range adapter.DefaultRegistry(newTestEngineFromReg()).Adapters() {
		for _, rt := range a.Routes() {
			o.add(rt.Pattern)
		}
	}
	return o
}

var wildcardSeg = regexp.MustCompile(`\{[^}]+\}`)

// TestOpenRoutes_EveryAdapterRouteIsOpen is the audit H-03 guard: every
// provider surface the adapter registry mounts must be reachable without a
// MockAgents key in multi-tenant mode, because clients send their own
// provider key which the mock ignores. The former hand-written skipAuth list
// covered five of them and failed the rest closed.
func TestOpenRoutes_EveryAdapterRouteIsOpen(t *testing.T) {
	open := newTestOpenRoutes()
	seen := 0
	for _, a := range adapter.DefaultRegistry(newTestEngineFromReg()).Adapters() {
		for _, rt := range a.Routes() {
			method, path := http.MethodGet, rt.Pattern
			if i := strings.IndexByte(rt.Pattern, ' '); i >= 0 {
				method, path = rt.Pattern[:i], rt.Pattern[i+1:]
			}
			path = wildcardSeg.ReplaceAllString(path, "sample")
			req := httptest.NewRequest(method, path, nil)
			if !open.skip(req) {
				t.Errorf("adapter %s route %q: skipAuth = false, want true", a.Name(), rt.Pattern)
			}
			seen++
		}
	}
	if seen < 30 {
		t.Fatalf("only %d adapter routes enumerated — registry wiring changed?", seen)
	}
}

// TestOpenRoutes_ManagementRoutesStayGated is the other half of H-03: deriving
// the open set from the adapter registry must not sweep in the control plane.
func TestOpenRoutes_ManagementRoutesStayGated(t *testing.T) {
	open := newTestOpenRoutes()
	for pattern := range managementRouteFloors {
		method, path := pattern, pattern
		if i := strings.IndexByte(pattern, ' '); i >= 0 {
			method, path = pattern[:i], pattern[i+1:]
		}
		path = wildcardSeg.ReplaceAllString(path, "sample")
		if path == "/api/v1/health" || path == "/api/v1/ready" {
			continue // the two probe targets are open by design
		}
		if open.skip(httptest.NewRequest(method, path, nil)) {
			t.Errorf("management route %q is auth-exempt", pattern)
		}
	}
}

// TestOpenRoutes_MethodMismatchStillSkips: an open path reached with the wrong
// method must surface the router's 405, not the auth middleware's 401 — the
// path is open, whatever the verb.
func TestOpenRoutes_MethodMismatchStillSkips(t *testing.T) {
	open := newTestOpenRoutes()
	if !open.skip(httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)) {
		t.Fatal("GET on an open POST route should still skip auth")
	}
}
