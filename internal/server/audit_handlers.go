package server

import (
	"net/http"
	"time"

	"github.com/mockagents/mockagents/internal/audit"
)

// AuditHandlers serves GET /api/v1/audit. The route is mounted
// unconditionally when an audit store is configured; in multi-tenant
// mode the auth middleware ensures the caller carries at least a
// viewer role, and the route binding below further gates the list to
// admin role to keep the who-did-what surface private.
type AuditHandlers struct {
	Store audit.Store
}

// ListEvents handles GET /api/v1/audit.
//
// Query parameters:
//
//	kind    one of tenant.created|tenant.deleted|api_key.created|api_key.deleted|api_key.role_changed|agent.reloaded|auth.denied
//	actor   exact-match actor name (e.g. "bootstrap-admin")
//	since   RFC3339 timestamp; returns events with timestamp >= since
//	limit   max rows to return (default 100, max 1000)
func (h *AuditHandlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil { // defensive, mirrors costs/pipelines (F-AU-001)
		writeError(w, http.StatusServiceUnavailable, "audit logging is not enabled")
		return
	}
	q := audit.Query{
		Kind:  audit.EventKind(r.URL.Query().Get("kind")),
		Actor: r.URL.Query().Get("actor"),
	}
	if q.Kind != "" && !q.Kind.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "unknown kind; try one of: tenant.created, tenant.deleted, api_key.created, api_key.deleted, api_key.role_changed, agent.reloaded, auth.denied",
		})
		return
	}
	if since := r.URL.Query().Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "since must be RFC3339: " + err.Error(),
			})
			return
		}
		q.Since = t
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		n, ok := parseBoundedInt(w, limit, "limit", 0, maxListLimit)
		if !ok {
			return
		}
		q.Limit = n
	}

	events, err := h.Store.List(r.Context(), q)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
