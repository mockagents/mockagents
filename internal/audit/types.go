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
	EventTenantCreated EventKind = "tenant.created"
	EventTenantDeleted EventKind = "tenant.deleted"
	EventAPIKeyCreated EventKind = "api_key.created"
	EventAPIKeyDeleted EventKind = "api_key.deleted"
	EventAgentReloaded EventKind = "agent.reloaded"
)

// Valid reports whether k is one of the known event kinds. Used by
// the query endpoint to reject bogus ?kind= filters with a 400 rather
// than silently returning an empty list.
func (k EventKind) Valid() bool {
	switch k {
	case EventTenantCreated, EventTenantDeleted,
		EventAPIKeyCreated, EventAPIKeyDeleted,
		EventAgentReloaded:
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
}

// ErrNotFound is returned by GetEvent when an id doesn't exist.
var ErrNotFound = errors.New("audit: event not found")
