package server

import (
	"net/http"

	"github.com/mockagents/mockagents/internal/tenancy"
)

// roleOpen marks a management route that carries no role floor. Such a route
// is reachable by any authenticated caller in multi-tenant mode — the
// AuthMiddleware still requires a valid API key for every non-skipAuth
// route, so "open" means "any role", not "anonymous" — and by anyone in
// single-tenant mode. roleOpen is deliberately distinct from a route being
// ABSENT from managementRouteFloors, which is a programming error.
const roleOpen tenancy.Role = ""

// managementRouteFloors is the single source of truth for the minimum role
// required to call each /api/v1 management route when multi-tenant mode is
// enabled (X-AUTHZ-001). Previously each route picked its own gate inline at
// registration, which is exactly how ReloadAgent once shipped with no gate
// at all (F-HD-001); centralizing the policy makes that class of drift
// impossible — mountManaged panics on a route with no entry here, so a new
// route cannot be mounted without a deliberate floor decision.
//
// Floors are enforced only in multi-tenant mode. Single-tenant mode is an
// unauthenticated local-dev tool, so every route is open there.
var managementRouteFloors = map[string]tenancy.Role{
	// Agent catalog (read) + hot reload (write).
	"GET /api/v1/health":                roleOpen,
	"GET /api/v1/agents":                roleOpen,
	"GET /api/v1/agents/{name}":         roleOpen,
	"POST /api/v1/agents/{name}/reload": tenancy.RoleEditor, // F-HD-001: write → editor

	// Tenant collection management is a cross-tenant operation, so it
	// requires the platform role — a per-tenant admin must not list, create,
	// or delete other tenants (X-TN-001). The per-key routes below stay at
	// admin/editor and are additionally scoped to the caller's own tenant by
	// ensureOwnTenant.
	"GET /api/v1/tenants":         tenancy.RolePlatform,
	"POST /api/v1/tenants":        tenancy.RolePlatform,
	"DELETE /api/v1/tenants/{id}": tenancy.RolePlatform,
	"GET /api/v1/tenants/{id}/keys":         tenancy.RoleEditor,
	"POST /api/v1/tenants/{id}/keys":        tenancy.RoleAdmin,
	"POST /api/v1/tenants/{id}/keys/rotate": tenancy.RoleAdmin,
	"PATCH /api/v1/keys/{id}":               tenancy.RoleAdmin,
	"POST /api/v1/keys/{id}/rotate":         tenancy.RoleAdmin,
	"POST /api/v1/keys/me/rotate":           tenancy.RoleViewer, // self-service: own key only
	"POST /api/v1/keys/me/burn":             tenancy.RoleViewer, // self-service: own key only
	"DELETE /api/v1/keys/{id}":              tenancy.RoleAdmin,

	// Audit read API: who-did-what stays private to operators.
	"GET /api/v1/audit": tenancy.RoleAdmin,

	// Interaction-log query + live feed. The aggregate metrics endpoint is
	// admin-only so viewers can't fingerprint the operator's browser tabs.
	"GET /api/v1/logs":                roleOpen,
	"GET /api/v1/logs/{id}":           roleOpen,
	"DELETE /api/v1/logs":             roleOpen,
	"GET /api/v1/logs/stream":         roleOpen,
	"GET /api/v1/logs/stream/metrics": tenancy.RoleAdmin,

	// Cost aggregate (read) — previously ungated even in multi-tenant mode.
	"GET /api/v1/costs": tenancy.RoleViewer, // F-CO-005

	// Pipeline topology (read) — previously ungated even in multi-tenant mode.
	"GET /api/v1/pipelines":        tenancy.RoleViewer, // F-PL-001
	"GET /api/v1/pipelines/{name}": tenancy.RoleViewer, // F-PL-001
	// Pipeline edit (write: persists YAML to disk) → editor (REF-07).
	"PUT /api/v1/pipelines/{name}": tenancy.RoleEditor,

	// Config validation (write-ish: sprays YAML at the parser) → editor.
	"POST /api/v1/config/validate": tenancy.RoleEditor,
}

// mountManaged registers a management-API route on mux, applying the role
// floor from managementRouteFloors when multi-tenant mode is enabled. It is
// the single chokepoint for /api/v1 authorization (X-AUTHZ-001).
//
// A pattern with no floor entry panics: every management route must make a
// deliberate floor decision, so an ungated route can never slip in again.
// Registration happens at startup, so a missing entry fails loudly and
// immediately (and is asserted by TestManagementRouteFloors_Coverage).
func (s *Server) mountManaged(mux *http.ServeMux, pattern string, h http.Handler) {
	floor, ok := managementRouteFloors[pattern]
	if !ok {
		panic("server: no role floor declared for management route " + pattern)
	}
	if s.tenancyH != nil && floor != roleOpen {
		h = tenancy.RequireRole(floor, h)
	}
	mux.Handle(pattern, h)
}
