package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/tenancy"
)

// ---------------------------------------------------------------------------
// capability derivation
// ---------------------------------------------------------------------------

func TestCapabilityForRoute(t *testing.T) {
	cases := map[string]string{
		"GET /api/v1/agents":                    "agents.read",
		"GET /api/v1/agents/{name}":             "agents.read",
		"POST /api/v1/agents":                   "agents.write",
		"PUT /api/v1/agents/{name}":             "agents.write",
		"DELETE /api/v1/agents/{name}":          "agents.write",
		"POST /api/v1/agents/{name}/reload":     "agents.reload.write",
		"GET /api/v1/tenants":                   "tenants.read",
		"GET /api/v1/tenants/{id}/keys":         "tenants.keys.read",
		"POST /api/v1/tenants/{id}/keys":        "tenants.keys.write",
		"POST /api/v1/tenants/{id}/keys/rotate": "tenants.keys.rotate.write",
		"PUT /api/v1/tenants/{id}/quota":        "tenants.quota.write",
		"POST /api/v1/keys/me/rotate":           "keys.me.rotate.write",
		"GET /api/v1/logs/stream/metrics":       "logs.stream.metrics.read",
		"DELETE /api/v1/logs":                   "logs.write",
		"POST /api/v1/pipelines/{name}/run":     "pipelines.run.write",
		"PUT /api/v1/pipelines/{name}":          "pipelines.write",
		"POST /api/v1/config/validate":          "config.validate.write",
		"GET /metrics":                          "metrics.read",
	}
	for pattern, want := range cases {
		if got := capabilityForRoute(pattern); got != want {
			t.Errorf("capabilityForRoute(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// TestCapabilityNames_NoFloorCollision is the safety invariant behind deriving
// capabilities from the route table: two routes may share a capability name
// only if they share a floor. Without this, a viewer-gated route could donate
// its name to an editor-gated one (pipelines.run.write vs pipelines.write) and
// the UI would enable an action the server refuses.
func TestCapabilityNames_NoFloorCollision(t *testing.T) {
	seen := make(map[string]struct {
		floor   tenancy.Role
		pattern string
	})
	for pattern, floor := range managementRouteFloors {
		name := capabilityForRoute(pattern)
		if name == "" {
			t.Errorf("route %q derives an empty capability", pattern)
			continue
		}
		if prev, ok := seen[name]; ok && prev.floor != floor {
			t.Errorf("capability %q is shared by routes at different floors: %q (%q) and %q (%q)",
				name, prev.pattern, prev.floor, pattern, floor)
			continue
		}
		seen[name] = struct {
			floor   tenancy.Role
			pattern string
		}{floor, pattern}
	}
}

// TestCapabilitiesFrom_RoleLadder pins what each role may do. Higher roles are
// supersets of lower ones, and no role below platform sees tenant management.
func TestCapabilitiesFrom_RoleLadder(t *testing.T) {
	viewer := capabilitiesFrom(managementRouteFloors, tenancy.RoleViewer)
	editor := capabilitiesFrom(managementRouteFloors, tenancy.RoleEditor)
	admin := capabilitiesFrom(managementRouteFloors, tenancy.RoleAdmin)
	platform := capabilitiesFrom(managementRouteFloors, tenancy.RolePlatform)

	// Open routes reach every role.
	for _, role := range [][]string{viewer, editor, admin, platform} {
		if !slices.Contains(role, "agents.read") {
			t.Error("every authenticated role must have agents.read")
		}
		if !slices.Contains(role, "logs.read") {
			t.Error("every authenticated role must have logs.read")
		}
	}

	// Viewer may execute pipelines and read costs, but not author.
	if !slices.Contains(viewer, "pipelines.run.write") {
		t.Error("viewer must be able to run pipelines (route floor is viewer)")
	}
	if slices.Contains(viewer, "agents.write") {
		t.Error("viewer must NOT have agents.write")
	}
	if slices.Contains(viewer, "pipelines.write") {
		t.Error("viewer must NOT have pipelines.write")
	}
	if slices.Contains(viewer, "audit.read") {
		t.Error("viewer must NOT have audit.read")
	}

	// Editor authors but does not audit or administer keys.
	if !slices.Contains(editor, "agents.write") {
		t.Error("editor must have agents.write")
	}
	if slices.Contains(editor, "audit.read") {
		t.Error("editor must NOT have audit.read")
	}
	if slices.Contains(editor, "tenants.keys.write") {
		t.Error("editor must NOT mint keys")
	}

	// Admin audits and administers keys, but never manages tenants (X-TN-001).
	if !slices.Contains(admin, "audit.read") {
		t.Error("admin must have audit.read")
	}
	if !slices.Contains(admin, "tenants.keys.write") {
		t.Error("admin must be able to mint keys")
	}
	if slices.Contains(admin, "tenants.read") {
		t.Error("admin must NOT list tenants — that is platform-only")
	}
	if slices.Contains(admin, "tenants.quota.write") {
		t.Error("admin must NOT set quotas — a tenant admin cannot raise its own cap")
	}

	// Platform is the cross-tenant operator.
	if !slices.Contains(platform, "tenants.read") || !slices.Contains(platform, "tenants.write") {
		t.Error("platform must manage the tenant collection")
	}

	// Monotonicity: each role is a superset of the one below it.
	for _, pair := range []struct {
		lowName, highName string
		low, high         []string
	}{
		{"viewer", "editor", viewer, editor},
		{"editor", "admin", editor, admin},
		{"admin", "platform", admin, platform},
	} {
		for _, c := range pair.low {
			if !slices.Contains(pair.high, c) {
				t.Errorf("%s has %q but %s does not; roles must be monotonic",
					pair.lowName, c, pair.highName)
			}
		}
	}
}

func TestAllCapabilitiesFrom_CoversEveryRoute(t *testing.T) {
	all := allCapabilitiesFrom(managementRouteFloors)
	for pattern := range managementRouteFloors {
		name := capabilityForRoute(pattern)
		if !slices.Contains(all, name) {
			t.Errorf("allCapabilities is missing %q (from route %q)", name, pattern)
		}
	}
	if !slices.IsSorted(all) {
		t.Error("allCapabilities must be sorted for a stable wire response")
	}
}

// ---------------------------------------------------------------------------
// handler
// ---------------------------------------------------------------------------

func decodeIdentity(t *testing.T, rec *httptest.ResponseRecorder) IdentityResponse {
	t.Helper()
	var got IdentityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding identity response: %v (body %q)", err, rec.Body.String())
	}
	return got
}

// Local (single-tenant) mode must report itself as local and explicitly
// unauthenticated — never as a synthetic viewer (epic §8.1).
func TestIdentity_LocalMode(t *testing.T) {
	s := &Server{handlers: &Handlers{Version: "1.2.3"}, mountedRoutes: managementRouteFloors}

	rec := httptest.NewRecorder()
	s.Identity(rec, httptest.NewRequest(http.MethodGet, "/api/v1/identity", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeIdentity(t, rec)
	if got.Mode != identityModeLocal {
		t.Errorf("mode = %q, want %q", got.Mode, identityModeLocal)
	}
	if got.Authenticated {
		t.Error("local mode must report authenticated=false")
	}
	if got.Role != nil {
		t.Errorf("local mode must report a null role, got %q", *got.Role)
	}
	if got.Server.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", got.Server.Version)
	}
	// Every route is open locally, so every capability is available.
	if len(got.Capabilities) != len(allCapabilitiesFrom(managementRouteFloors)) {
		t.Errorf("local mode capabilities = %d, want all %d",
			len(got.Capabilities), len(allCapabilitiesFrom(managementRouteFloors)))
	}
}

// A null role must survive JSON round-tripping as an explicit null, so a
// client cannot mistake it for the empty string.
func TestIdentity_LocalMode_RoleSerializesAsNull(t *testing.T) {
	s := &Server{handlers: &Handlers{Version: "dev"}, mountedRoutes: managementRouteFloors}
	rec := httptest.NewRecorder()
	s.Identity(rec, httptest.NewRequest(http.MethodGet, "/api/v1/identity", nil))

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(raw["role"]) != "null" {
		t.Errorf("role = %s, want null", raw["role"])
	}
}

func TestIdentity_MultiTenant_EveryRole(t *testing.T) {
	for _, role := range tenancy.AllRoles() {
		t.Run(string(role), func(t *testing.T) {
			s := &Server{handlers: &Handlers{Version: "dev"}, tenancyH: &TenancyHandlers{}, mountedRoutes: managementRouteFloors}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/identity", nil)
			req = req.WithContext(tenancy.WithPrincipal(req.Context(), &tenancy.Principal{
				TenantID: "tenant-a",
				KeyID:    "key-1",
				Role:     role,
			}))
			rec := httptest.NewRecorder()
			s.Identity(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			got := decodeIdentity(t, rec)
			if got.Mode != identityModeMultiTenant {
				t.Errorf("mode = %q, want %q", got.Mode, identityModeMultiTenant)
			}
			if !got.Authenticated {
				t.Error("authenticated must be true")
			}
			if got.Role == nil || *got.Role != string(role) {
				t.Errorf("role = %v, want %q", got.Role, role)
			}
			if got.TenantID != "tenant-a" {
				t.Errorf("tenant_id = %q, want tenant-a", got.TenantID)
			}
			if got.KeyID != "key-1" {
				t.Errorf("key_id = %q, want key-1", got.KeyID)
			}

			want := capabilitiesFrom(managementRouteFloors, role)
			if !slices.Equal(got.Capabilities, want) {
				t.Errorf("capabilities = %v, want %v", got.Capabilities, want)
			}
		})
	}
}

// The response must never carry key material — only the key's identifier.
func TestIdentity_LeaksNoSecret(t *testing.T) {
	s := &Server{handlers: &Handlers{Version: "dev"}, tenancyH: &TenancyHandlers{}, mountedRoutes: managementRouteFloors}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/identity", nil)
	req.Header.Set("Authorization", "Bearer super-secret-key-value")
	req = req.WithContext(tenancy.WithPrincipal(req.Context(), &tenancy.Principal{
		TenantID: "t", KeyID: "k", Role: tenancy.RoleAdmin,
	}))
	rec := httptest.NewRecorder()
	s.Identity(rec, req)

	if body := rec.Body.String(); strings.Contains(body, "super-secret-key-value") {
		t.Errorf("identity response leaked the bearer credential: %s", body)
	}
}

// Multi-tenant mode with no principal must fail closed rather than reporting
// an anonymous identity.
func TestIdentity_MultiTenant_NoPrincipalIsUnauthorized(t *testing.T) {
	s := &Server{handlers: &Handlers{Version: "dev"}, tenancyH: &TenancyHandlers{}, mountedRoutes: managementRouteFloors}

	rec := httptest.NewRecorder()
	s.Identity(rec, httptest.NewRequest(http.MethodGet, "/api/v1/identity", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// Role downgrade is observed immediately: capabilities are computed per
// request, never cached against the key.
func TestIdentity_ReflectsRoleDowngrade(t *testing.T) {
	s := &Server{handlers: &Handlers{Version: "dev"}, tenancyH: &TenancyHandlers{}, mountedRoutes: managementRouteFloors}

	call := func(role tenancy.Role) IdentityResponse {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/identity", nil)
		req = req.WithContext(tenancy.WithPrincipal(req.Context(), &tenancy.Principal{
			TenantID: "t", KeyID: "k", Role: role,
		}))
		rec := httptest.NewRecorder()
		s.Identity(rec, req)
		return decodeIdentity(t, rec)
	}

	before := call(tenancy.RoleAdmin)
	after := call(tenancy.RoleViewer)

	if !slices.Contains(before.Capabilities, "audit.read") {
		t.Fatal("admin should start with audit.read")
	}
	if slices.Contains(after.Capabilities, "audit.read") {
		t.Error("a downgraded principal must immediately lose audit.read")
	}
	if after.Role == nil || *after.Role != "viewer" {
		t.Error("role must reflect the downgraded principal")
	}
}
