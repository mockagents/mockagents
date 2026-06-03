package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockagents/mockagents/internal/tenancy"
)

// mountAndServe mounts pattern via mountManaged on a server in the requested
// tenancy mode, then serves one request carrying principal p (nil =
// anonymous) and returns the response status.
func mountAndServe(t *testing.T, multiTenant bool, pattern, method, reqPath string, p *tenancy.Principal) int {
	t.Helper()
	var s Server
	if multiTenant {
		s.tenancyH = &TenancyHandlers{} // non-nil flips multi-tenant mode
	}
	mux := http.NewServeMux()
	s.mountManaged(mux, pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(method, reqPath, nil)
	if p != nil {
		req = req.WithContext(tenancy.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

// TestMountManaged_AppliesFloorInMultiTenant is the X-AUTHZ-001 behavioral
// guard: in multi-tenant mode mountManaged enforces the table's role floor;
// below-floor callers are rejected and at/above-floor callers pass.
func TestMountManaged_AppliesFloorInMultiTenant(t *testing.T) {
	viewer := &tenancy.Principal{TenantID: "t1", KeyID: "k", Role: tenancy.RoleViewer}
	admin := &tenancy.Principal{TenantID: "t1", KeyID: "k", Role: tenancy.RoleAdmin}

	// admin-floor route rejects a viewer...
	if code := mountAndServe(t, true, "GET /api/v1/audit", "GET", "/api/v1/audit", viewer); code == http.StatusOK {
		t.Error("viewer reached an admin-floor route (GET /api/v1/audit)")
	}
	// ...and admits an admin.
	if code := mountAndServe(t, true, "GET /api/v1/audit", "GET", "/api/v1/audit", admin); code != http.StatusOK {
		t.Errorf("admin blocked from admin-floor route: status %d", code)
	}
	// viewer-floor route (previously ungated, F-CO-005) admits a viewer.
	if code := mountAndServe(t, true, "GET /api/v1/costs", "GET", "/api/v1/costs", viewer); code != http.StatusOK {
		t.Errorf("viewer blocked from viewer-floor route (costs): status %d", code)
	}
	// roleOpen route admits any authenticated caller.
	if code := mountAndServe(t, true, "GET /api/v1/agents", "GET", "/api/v1/agents", viewer); code != http.StatusOK {
		t.Errorf("viewer blocked from open route (agents): status %d", code)
	}
}

// TestMountManaged_PlatformFloorRejectsAdmin is the X-TN-001 guard: the
// tenant-collection routes require the platform role, so a per-tenant admin is
// rejected while a platform principal passes.
func TestMountManaged_PlatformFloorRejectsAdmin(t *testing.T) {
	admin := &tenancy.Principal{TenantID: "t1", KeyID: "k", Role: tenancy.RoleAdmin}
	platform := &tenancy.Principal{TenantID: "t1", KeyID: "k", Role: tenancy.RolePlatform}

	if code := mountAndServe(t, true, "DELETE /api/v1/tenants/{id}", "DELETE", "/api/v1/tenants/t2", admin); code == http.StatusOK {
		t.Error("a per-tenant admin reached the platform-floor tenant-delete route")
	}
	if code := mountAndServe(t, true, "DELETE /api/v1/tenants/{id}", "DELETE", "/api/v1/tenants/t2", platform); code != http.StatusOK {
		t.Errorf("platform principal blocked from a platform-floor route: status %d", code)
	}
	if code := mountAndServe(t, true, "GET /api/v1/tenants", "GET", "/api/v1/tenants", admin); code == http.StatusOK {
		t.Error("a per-tenant admin reached the platform-floor tenant-list route")
	}
}

// TestMountManaged_OpenInSingleTenant confirms single-tenant (local-dev) mode
// applies no floor — even an admin-floor route is reachable anonymously.
func TestMountManaged_OpenInSingleTenant(t *testing.T) {
	if code := mountAndServe(t, false, "GET /api/v1/audit", "GET", "/api/v1/audit", nil); code != http.StatusOK {
		t.Errorf("single-tenant admin-floor route not open: status %d", code)
	}
}

// TestMountManaged_PanicsOnUnknownRoute is the anti-drift guard: a route with
// no floor entry cannot be mounted, so an ungated route can't slip in the way
// ReloadAgent once did (F-HD-001).
func TestMountManaged_PanicsOnUnknownRoute(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic mounting a route with no floor entry")
		}
	}()
	var s Server
	s.mountManaged(http.NewServeMux(), "GET /api/v1/no-such-route", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
}

// TestManagementRouteFloors_FlaggedRoutes pins the floors the review called
// out (F-CO-005, F-PL-001, F-HD-001) plus the existing sensitive routes, so a
// future edit that loosens one fails here.
func TestManagementRouteFloors_FlaggedRoutes(t *testing.T) {
	want := map[string]tenancy.Role{
		"GET /api/v1/costs":                 tenancy.RoleViewer, // F-CO-005
		"GET /api/v1/pipelines":             tenancy.RoleViewer, // F-PL-001
		"GET /api/v1/pipelines/{name}":      tenancy.RoleViewer, // F-PL-001
		"POST /api/v1/agents/{name}/reload": tenancy.RoleEditor, // F-HD-001
		"GET /api/v1/audit":                 tenancy.RoleAdmin,
		"POST /api/v1/config/validate":      tenancy.RoleEditor,
		"DELETE /api/v1/keys/{id}":          tenancy.RoleAdmin,
		// X-TN-001: tenant-collection routes are platform-only.
		"GET /api/v1/tenants":         tenancy.RolePlatform,
		"POST /api/v1/tenants":        tenancy.RolePlatform,
		"DELETE /api/v1/tenants/{id}": tenancy.RolePlatform,
	}
	for pat, w := range want {
		if got, ok := managementRouteFloors[pat]; !ok {
			t.Errorf("route %q missing from managementRouteFloors", pat)
		} else if got != w {
			t.Errorf("floor[%q] = %q, want %q", pat, got, w)
		}
	}
}

// TestManagementRouteFloors_AllValid asserts every floor is roleOpen or a
// real role, so a typo'd value can't silently mean "open".
func TestManagementRouteFloors_AllValid(t *testing.T) {
	valid := map[tenancy.Role]bool{
		roleOpen:             true,
		tenancy.RoleViewer:   true,
		tenancy.RoleEditor:   true,
		tenancy.RoleAdmin:    true,
		tenancy.RolePlatform: true,
	}
	for pat, floor := range managementRouteFloors {
		if !valid[floor] {
			t.Errorf("route %q has invalid floor %q", pat, floor)
		}
	}
}
