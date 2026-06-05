// Package audit implements an append-only audit log for MockAgents
// control-plane mutations. Every tenant, API key, or agent reload
// event is persisted to a dedicated SQLite table so operators can
// answer "who changed what, when" without having to grep server logs.
//
// Audit is independent of the tenancy feature flag: when multi-tenant
// mode is off the recorder still runs, but the Actor field reports
// "anonymous" since there is no authenticated principal.
package audit

import (
	"errors"
	"time"
)

// EventKind enumerates the mutation types the audit log can record.
// Kept deliberately small so the set is easy to enumerate on a query
// dashboard; extend only when adding a genuinely new control-plane
// resource.
type EventKind string

const (
	EventTenantCreated      EventKind = "tenant.created"
	EventTenantDeleted      EventKind = "tenant.deleted"
	EventAPIKeyCreated      EventKind = "api_key.created"
	EventAPIKeyDeleted      EventKind = "api_key.deleted"
	EventAPIKeyRoleChanged  EventKind = "api_key.role_changed"
	// EventAPIKeyRotated fires when an operator regenerates an
	// existing key's secret in place. The key id, name, role, and
	// tenant stay the same; only the plaintext (and therefore the
	// hash + prefix) change. Target is the key id; Details carries
	// the old prefix so operators can match the rotation to a
	// specific compromised credential.
	EventAPIKeyRotated EventKind = "api_key.rotated"
	EventAgentReloaded EventKind = "agent.reloaded"
	// EventPipelineSaved fires when an operator writes an edited Pipeline
	// definition back to disk via PUT /api/v1/pipelines/{name} (the GUI
	// editor, REF-07). Target is the pipeline name; Details carries the
	// target file so operators can correlate the change with a working-tree
	// edit.
	EventPipelineSaved EventKind = "pipeline.saved"
	// EventAuthDenied fires on every 401 (missing/invalid credentials)
	// and 403 (valid credential, insufficient role) at the control
	// plane. The Target carries the HTTP method + path of the denied
	// request; Details carries status_code and reason. Anonymous
	// denials still produce an entry so failed-auth spikes are
	// visible to operators even when no principal is present.
	EventAuthDenied EventKind = "auth.denied"
)

// Valid reports whether k is one of the known event kinds. Used by
// the query endpoint to reject bogus ?kind= filters with a 400 rather
// than silently returning an empty list.
func (k EventKind) Valid() bool {
	switch k {
	case EventTenantCreated, EventTenantDeleted,
		EventAPIKeyCreated, EventAPIKeyDeleted, EventAPIKeyRoleChanged,
		EventAPIKeyRotated, EventAgentReloaded, EventPipelineSaved, EventAuthDenied:
		return true
	}
	return false
}

// Actor describes who performed an action. When multi-tenant mode is
// off or the request is unauthenticated (the single-tenant default),
// TenantID/KeyID are empty strings and Name is "anonymous".
type Actor struct {
	Name     string `json:"name"`
	TenantID string `json:"tenant_id,omitempty"`
	KeyID    string `json:"key_id,omitempty"`
	Role     string `json:"role,omitempty"`
	RemoteIP string `json:"remote_ip,omitempty"`
}

// Event is one row in the audit log.
//
// Target is a free-form identifier for the resource the action acted
// on (tenant id, api key id, agent name). Details is an optional
// JSON-encoded blob of extra context — for example, the key name and
// role on api_key.created so operators can see what permissions were
// just minted without having to correlate with the tenancy store.
type Event struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Kind      EventKind `json:"kind"`
	Actor     Actor     `json:"actor"`
	Target    string    `json:"target"`
	Details   string    `json:"details,omitempty"`
}

// Query filters a ListEvents call. All fields are optional; leaving a
// field zero-valued removes that filter. Limit defaults to 100 when
// zero and is capped server-side at 1000.
type Query struct {
	Kind  EventKind
	Actor string
	Since time.Time
	Limit int
	// ActorTenant, when non-empty, restricts results to events whose actor
	// belongs to that tenant. The multi-tenant audit read endpoint sets it
	// to the caller's own tenant so a tenant admin cannot read another
	// tenant's audit trail (X-SEC-002). Empty means no tenant filter
	// (single-tenant / local-dev mode sees everything).
	ActorTenant string
}

// ErrNotFound is returned by GetEvent when an id doesn't exist.
var ErrNotFound = errors.New("audit: event not found")
