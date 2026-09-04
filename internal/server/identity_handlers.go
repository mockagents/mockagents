package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/mockagents/mockagents/internal/tenancy"
)

// UX-01: the identity/capability contract.
//
// The GUI previously had no way to ask the server "who am I?", so it inferred
// a role by probing a privileged endpoint (GET /api/v1/tenants) and cached the
// answer in a cookie. That was wrong twice over: the probe is platform-gated,
// so viewer/editor/admin keys could not sign in at all, and a cached role
// cannot follow a server-side role change. This endpoint replaces the guess.
//
// Capabilities are DERIVED from managementRouteFloors rather than maintained
// as a second table, so a floor change cannot desynchronize the UI from the
// gate that actually runs. They are advisory — for deciding what to render —
// and never a substitute for authorization: every request is still checked by
// the middleware.

// Identity modes. Local mode is deliberately NOT reported as a synthetic
// viewer: an unauthenticated local server is an explicit product exception,
// and a UI that cannot tell the two apart would show a "signed in" state for
// a server with no access control at all.
const (
	identityModeLocal       = "local"
	identityModeMultiTenant = "multi_tenant"
)

// IdentityServer carries the server-owned facts a client cannot infer.
type IdentityServer struct {
	Version string `json:"version"`
}

// IdentityResponse is the wire contract for GET /api/v1/identity.
//
// Role is a pointer so local mode serializes as an explicit null rather than
// an empty string that a client might coerce into a role name. No secret is
// included: KeyID is an identifier, never the key material.
type IdentityResponse struct {
	Mode          string         `json:"mode"`
	Authenticated bool           `json:"authenticated"`
	TenantID      string         `json:"tenant_id,omitempty"`
	KeyID         string         `json:"key_id,omitempty"`
	Role          *string        `json:"role"`
	Capabilities  []string       `json:"capabilities"`
	Server        IdentityServer `json:"server"`
}

// Identity handles GET /api/v1/identity.
func (s *Server) Identity(w http.ResponseWriter, r *http.Request) {
	resp := IdentityResponse{Server: IdentityServer{Version: s.version()}}

	// Single-tenant mode: no auth middleware runs at all, so there is no
	// principal to report. Every route is open, so every capability is
	// available — but the caller is explicitly unauthenticated.
	if s.tenancyH == nil {
		resp.Mode = identityModeLocal
		resp.Capabilities = allCapabilitiesFrom(s.mountedRoutes)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Mode = identityModeMultiTenant
	p := tenancy.PrincipalFrom(r.Context())
	if p == nil {
		// AuthMiddleware gates this route (it is not in skipAuth), so a nil
		// principal means something upstream changed. Fail closed rather than
		// reporting an anonymous identity on a multi-tenant server.
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "missing or invalid credential",
		})
		return
	}

	role := string(p.Role)
	resp.Authenticated = true
	resp.TenantID = p.TenantID
	resp.KeyID = p.KeyID
	resp.Role = &role
	resp.Capabilities = capabilitiesFrom(s.mountedRoutes, p.Role)
	writeJSON(w, http.StatusOK, resp)
}

// version reports the server build version, falling back to the same "dev"
// default NewServer uses when the field was never populated.
func (s *Server) version() string {
	if s.handlers != nil && s.handlers.Version != "" {
		return s.handlers.Version
	}
	return "dev"
}

// capabilityForRoute derives a stable capability name from a route pattern:
// the literal path segments (parameters dropped) plus a read/write verb.
//
//	GET    /api/v1/agents                  -> agents.read
//	PUT    /api/v1/agents/{name}           -> agents.write
//	POST   /api/v1/agents/{name}/reload    -> agents.reload.write
//	POST   /api/v1/pipelines/{name}/run    -> pipelines.run.write
//	GET    /metrics                        -> metrics.read
//
// Dropping parameters is what makes the name stable across the collection and
// item routes that share a floor. Keeping literal sub-resource segments is
// what keeps genuinely different permissions apart — pipelines.run.write
// (viewer) must never collapse into pipelines.write (editor).
// TestCapabilityNames_NoFloorCollision enforces exactly that invariant.
func capabilityForRoute(pattern string) string {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		return ""
	}
	path = strings.TrimPrefix(path, "/api/v1")

	segs := make([]string, 0, 4)
	for seg := range strings.SplitSeq(path, "/") {
		if seg == "" || strings.HasPrefix(seg, "{") {
			continue
		}
		segs = append(segs, seg)
	}
	if len(segs) == 0 {
		return ""
	}

	verb := "write"
	if method == http.MethodGet {
		verb = "read"
	}
	return strings.Join(append(segs, verb), ".")
}

// capabilitiesFrom returns the sorted capability names role can exercise among
// the given mounted routes.
func capabilitiesFrom(routes map[string]tenancy.Role, role tenancy.Role) []string {
	set := make(map[string]struct{}, len(routes))
	for pattern, floor := range routes {
		// roleOpen ranks below every real role, and Role.AtLeast deliberately
		// rejects an unknown requirement — so an open route has to be admitted
		// explicitly rather than through AtLeast.
		if floor != roleOpen && !role.AtLeast(floor) {
			continue
		}
		if name := capabilityForRoute(pattern); name != "" {
			set[name] = struct{}{}
		}
	}
	return sortedKeys(set)
}

// allCapabilitiesFrom returns every capability the given routes expose, used
// for local mode where no floor is enforced.
func allCapabilitiesFrom(routes map[string]tenancy.Role) []string {
	set := make(map[string]struct{}, len(routes))
	for pattern := range routes {
		if name := capabilityForRoute(pattern); name != "" {
			set[name] = struct{}{}
		}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
