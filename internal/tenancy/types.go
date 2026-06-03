// Package tenancy implements multi-tenant auth + RBAC for MockAgents.
//
// The package is opt-in: when multi-tenant mode is disabled (the default)
// nothing in this package runs and MockAgents behaves as a single-tenant
// dev tool. When enabled, every control-plane request must carry a valid
// API key and the tenant + role attached to that key gates access.
//
// This slice intentionally scopes only the management API surface
// (/api/v1/*). Tenant-scoped data isolation for the existing
// /v1/chat/completions and /v1/messages endpoints is a deliberate
// follow-up — that rewires the engine and deserves its own session.
package tenancy

import (
	"errors"
	"time"
)

// Role is one of a small fixed set of permission levels. Roles compare
// via rank() so middleware can express "admin or higher".
type Role string

const (
	RoleViewer Role = "viewer"
	RoleEditor Role = "editor"
	RoleAdmin  Role = "admin"
	// RolePlatform is the cross-tenant operator role: it sits above admin
	// and is the only role permitted to manage the tenant collection
	// (list/create/delete tenants). A per-tenant admin is confined to its
	// own tenant's keys. Platform keys are minted ONLY by the bootstrap
	// path (cmd/mockagents) — the management API refuses to assign this role
	// (see IsAssignableViaAPI) so a tenant admin cannot self-escalate.
	RolePlatform Role = "platform"
)

// rank returns the ordering of a role for comparison. Higher is more
// privileged. Unknown roles rank -1 so they always fail RequireAtLeast.
func (r Role) rank() int {
	switch r {
	case RoleViewer:
		return 1
	case RoleEditor:
		return 2
	case RoleAdmin:
		return 3
	case RolePlatform:
		return 4
	}
	return -1
}

// AtLeast reports whether r is at least as privileged as required.
func (r Role) AtLeast(required Role) bool {
	return r.rank() >= required.rank() && required.rank() > 0
}

// IsValid reports whether r is one of the known roles.
func (r Role) IsValid() bool { return r.rank() > 0 }

// IsAssignableViaAPI reports whether r may be assigned to a key through the
// management API. It is every valid role EXCEPT RolePlatform: the platform
// role is bootstrap-only so a tenant admin cannot create or promote a key to
// cross-tenant privilege (X-TN-001).
func (r Role) IsAssignableViaAPI() bool { return r.IsValid() && r != RolePlatform }

// Tenant is the top-level isolation boundary. A tenant owns API keys and
// (in future slices) agents, pipelines, and interaction logs.
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// APIKey represents a single issued credential. Only the prefix and
// hash are persisted; the plaintext is returned exactly once at creation
// time so the caller can hand it to its users.
type APIKey struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used,omitempty"`
}

// NewAPIKeyResult is what POST /api/v1/keys returns on success. The
// plaintext key is exposed only here; after this response the server
// holds only the bcrypt hash.
type NewAPIKeyResult struct {
	Key       APIKey `json:"key"`
	Plaintext string `json:"plaintext"`
}

// Principal is the authenticated caller derived from a valid API key.
// Attached to the request context by the auth middleware so downstream
// handlers can make RBAC decisions without re-hitting the store.
type Principal struct {
	TenantID string
	KeyID    string
	Role     Role
}

// ErrNotFound is returned by Store methods when a lookup misses.
var ErrNotFound = errors.New("tenancy: not found")

// ErrConflict is returned when a create would violate a uniqueness
// constraint (e.g. a duplicate tenant name). Handlers turn this into a 409
// so a duplicate is not conflated with a bad request (400) or a DB failure
// (500). See F-TN-008.
var ErrConflict = errors.New("tenancy: already exists")

// ErrInvalidKey is returned when an API key doesn't exist or the hash
// doesn't match. Middleware turns this into a 401.
var ErrInvalidKey = errors.New("tenancy: invalid api key")
