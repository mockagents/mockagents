package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// TenancyHandlers serves /api/v1/tenants and /api/v1/keys. They are
// only mounted when multi-tenant mode is enabled; in single-tenant mode
// these routes return 404 like any other unknown path.
type TenancyHandlers struct {
	Store    tenancy.Store
	Recorder *audit.Recorder // optional; nil = audit disabled for these routes
}

// principalOrUnauthorized returns the authenticated principal, or writes a
// 401 and returns nil. RequireRole already guarantees a principal on these
// routes, so a nil here is purely defensive.
func principalOrUnauthorized(w http.ResponseWriter, r *http.Request) *tenancy.Principal {
	p := tenancy.PrincipalFrom(r.Context())
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return nil
	}
	return p
}

// ensureOwnTenant returns the path {id} tenant only when it matches the
// caller's own tenant; otherwise it writes a 404 (deliberately hiding the
// existence of other tenants) and returns ok=false. This is the
// tenant-ownership gate for the {id}-addressed management routes — without
// it, a tenant-A admin could operate on tenant-B by path id (X-SEC-001).
func (h *TenancyHandlers) ensureOwnTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := principalOrUnauthorized(w, r)
	if p == nil {
		return "", false
	}
	pathTenant := r.PathValue("id")
	if pathTenant != p.TenantID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
		return "", false
	}
	return pathTenant, true
}

// maxJSONBodyBytes caps control-plane JSON request bodies (X-DOS-001).
// The management API only ever accepts tiny objects (a name + a role), so
// 64 KiB is far more than enough while stopping an unbounded decode.
const maxJSONBodyBytes = 64 << 10

// decodeJSONBody reads a size-capped JSON body into dst. On a too-large
// body it writes 413 and returns false; on malformed JSON it writes 400
// and returns false; on success returns true.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return false
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return false
	}
	return true
}

// ListTenants handles GET /api/v1/tenants — admin only.
func (h *TenancyHandlers) ListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.Store.ListTenants(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenants)
}

// CreateTenantRequest is the JSON body accepted by POST /api/v1/tenants.
type CreateTenantRequest struct {
	Name string `json:"name"`
}

// CreateTenant handles POST /api/v1/tenants — admin only.
func (h *TenancyHandlers) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req CreateTenantRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	tenant, err := h.Store.CreateTenant(r.Context(), req.Name)
	if err != nil {
		// A duplicate name is a 409, not a 500 or 400 (F-TN-008). Any other
		// post-validation failure is internal — don't echo the raw store
		// error (F-TN-006).
		if errors.Is(err, tenancy.ErrConflict) {
			writeError(w, http.StatusConflict, "tenant name already exists")
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventTenantCreated, tenant.ID,
		audit.MarshalDetails(map[string]any{"name": tenant.Name}))
	writeJSON(w, http.StatusCreated, tenant)
}

// DeleteTenant handles DELETE /api/v1/tenants/{id} — admin only.
func (h *TenancyHandlers) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Store.DeleteTenant(r.Context(), id); err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventTenantDeleted, id, "")
	w.WriteHeader(http.StatusNoContent)
}

// ListAPIKeys handles GET /api/v1/tenants/{id}/keys — admin or editor.
func (h *TenancyHandlers) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.ensureOwnTenant(w, r)
	if !ok {
		return
	}
	keys, err := h.Store.ListAPIKeys(r.Context(), tenantID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// CreateAPIKeyRequest is the JSON body accepted by POST /api/v1/tenants/{id}/keys.
type CreateAPIKeyRequest struct {
	Name string       `json:"name"`
	Role tenancy.Role `json:"role"`
}

// CreateAPIKey handles POST /api/v1/tenants/{id}/keys — admin only.
// Returns the plaintext key in the response body; subsequent list
// requests only show metadata.
func (h *TenancyHandlers) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.ensureOwnTenant(w, r)
	if !ok {
		return
	}
	var req CreateAPIKeyRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if !req.Role.IsValid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role (must be viewer, editor, or admin)"})
		return
	}
	result, err := h.Store.CreateAPIKey(r.Context(), tenantID, req.Name, req.Role)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventAPIKeyCreated, result.Key.ID,
		audit.MarshalDetails(map[string]any{
			"tenant_id": tenantID,
			"name":      result.Key.Name,
			"role":      string(result.Key.Role),
			"prefix":    result.Key.Prefix,
		}))
	writeJSON(w, http.StatusCreated, result)
}

// UpdateAPIKeyRoleRequest is the JSON body accepted by PATCH /api/v1/keys/{id}.
type UpdateAPIKeyRoleRequest struct {
	Role tenancy.Role `json:"role"`
}

// UpdateAPIKeyRole handles PATCH /api/v1/keys/{id} — admin only.
// Promotes or demotes an existing key and records the transition to
// the audit log so every privilege escalation leaves a trail.
func (h *TenancyHandlers) UpdateAPIKeyRole(w http.ResponseWriter, r *http.Request) {
	p := principalOrUnauthorized(w, r)
	if p == nil {
		return
	}
	id := r.PathValue("id")
	var req UpdateAPIKeyRoleRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if !req.Role.IsValid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role (must be viewer, editor, or admin)"})
		return
	}
	prev, next, err := h.Store.UpdateAPIKeyRole(r.Context(), p.TenantID, id, req.Role)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventAPIKeyRoleChanged, id,
		audit.MarshalDetails(map[string]any{
			"from": string(prev),
			"to":   string(next),
		}))
	writeJSON(w, http.StatusOK, map[string]any{
		"id":   id,
		"role": string(next),
	})
}

// RotateAPIKey handles POST /api/v1/keys/{id}/rotate — admin only.
// Returns a NewAPIKeyResult with the fresh plaintext secret; the
// previous prefix is recorded in the audit trail so operators can
// correlate a rotation with a specific compromised credential.
func (h *TenancyHandlers) RotateAPIKey(w http.ResponseWriter, r *http.Request) {
	p := principalOrUnauthorized(w, r)
	if p == nil {
		return
	}
	id := r.PathValue("id")
	result, oldPrefix, err := h.Store.RotateAPIKey(r.Context(), p.TenantID, id)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventAPIKeyRotated, result.Key.ID,
		audit.MarshalDetails(map[string]any{
			"tenant_id":  result.Key.TenantID,
			"name":       result.Key.Name,
			"role":       string(result.Key.Role),
			"old_prefix": oldPrefix,
			"new_prefix": result.Key.Prefix,
		}))
	writeJSON(w, http.StatusOK, result)
}

// BulkRotateResult is the wire shape returned by BulkRotateTenantKeys.
// Thin wrapper that adds the `count` aggregate so callers can
// confirm how many keys were touched without counting the array
// themselves.
type BulkRotateResult struct {
	Count   int                        `json:"count"`
	Results []*tenancy.NewAPIKeyResult `json:"results"`
}

// BulkRotateTenantKeys handles POST /api/v1/tenants/{id}/keys/rotate
// — admin only. Rotates every key in the tenant inside a single
// database transaction, emits one `api_key.rotated` audit event per
// key with `bulk: true` so operators can correlate the batch, and
// returns the fresh plaintexts as an array.
//
// This is the operator's emergency response to a suspected
// tenant-wide credential compromise: one click, every active
// credential replaced, every old secret dead. The alternative
// (admin clicks "Rotate" on every key one by one) leaves a
// window where half the keys are still the compromised values.
func (h *TenancyHandlers) BulkRotateTenantKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.ensureOwnTenant(w, r)
	if !ok {
		return
	}
	// ?except=self lets the caller preserve their own key so they
	// don't lock themselves out of the very admin console they're
	// using to respond to the compromise. The handler reads the
	// caller's Principal from the context — the middleware already
	// authenticated them, so the key id is trustworthy.
	var excludeIDs []string
	if r.URL.Query().Get("except") == "self" {
		if principal := tenancy.PrincipalFrom(r.Context()); principal != nil && principal.KeyID != "" {
			excludeIDs = append(excludeIDs, principal.KeyID)
		}
	}
	results, oldPrefixes, err := h.Store.BulkRotateTenantKeys(r.Context(), tenantID, excludeIDs...)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	// The store returns results and oldPrefixes as parallel slices indexed
	// in lockstep below. Guard their lengths so a contract violation surfaces
	// as a clean 500 instead of an index-out-of-range panic mid-loop — which
	// would also leave a partial audit trail (F-TN-009).
	if len(oldPrefixes) != len(results) {
		writeServerError(w, fmt.Errorf("bulk rotate: store returned %d results but %d old prefixes",
			len(results), len(oldPrefixes)))
		return
	}
	// One audit entry per rotated key. Using the same event kind
	// as individual rotations keeps the audit filter surface
	// simple; the `bulk: true` detail distinguishes the two
	// call sites for operators who want to grep for batches.
	for i, res := range results {
		h.Recorder.RecordHTTP(r, audit.EventAPIKeyRotated, res.Key.ID,
			audit.MarshalDetails(map[string]any{
				"tenant_id":  res.Key.TenantID,
				"name":       res.Key.Name,
				"role":       string(res.Key.Role),
				"old_prefix": oldPrefixes[i],
				"new_prefix": res.Key.Prefix,
				"bulk":       true,
			}))
	}
	writeJSON(w, http.StatusOK, BulkRotateResult{
		Count:   len(results),
		Results: results,
	})
}

// RotateMyAPIKey handles POST /api/v1/keys/me/rotate — any
// authenticated role. Reads the caller's key id from the request
// context (set by the auth middleware) and regenerates the secret
// in place. Unlike RotateAPIKey this does not require an admin
// role because the caller can only rotate *their own* credential:
// worst-case they've burned their own access and need to re-login,
// which is exactly what they asked for. The audit event records
// the old + new prefix just like the admin path.
//
// Returns 401 when the request is unauthenticated (single-tenant
// mode does not install the auth middleware, so RotateMyAPIKey is
// admin-only-callable via the other route there).
func (h *TenancyHandlers) RotateMyAPIKey(w http.ResponseWriter, r *http.Request) {
	principal := tenancy.PrincipalFrom(r.Context())
	if principal == nil || principal.KeyID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "self-rotation requires an authenticated request",
		})
		return
	}
	result, oldPrefix, err := h.Store.RotateAPIKey(r.Context(), principal.TenantID, principal.KeyID)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			// The principal was authenticated but the underlying
			// key is gone — race with DeleteAPIKey. Return 404 to
			// mirror the admin path.
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventAPIKeyRotated, result.Key.ID,
		audit.MarshalDetails(map[string]any{
			"tenant_id":  result.Key.TenantID,
			"name":       result.Key.Name,
			"role":       string(result.Key.Role),
			"old_prefix": oldPrefix,
			"new_prefix": result.Key.Prefix,
			"self":       true,
		}))
	writeJSON(w, http.StatusOK, result)
}

// BurnMyAPIKey handles POST /api/v1/keys/me/burn — any
// authenticated role. Rotates the caller's key in place exactly
// like RotateMyAPIKey, but deliberately discards the new plaintext
// instead of returning it. Responds with 204 No Content.
//
// Use case: operators who suspect the current browser session has
// been compromised (e.g. somebody shoulder-surfed the cookie jar)
// want to kill the session entirely — they do NOT want the new
// plaintext anywhere near the compromised browser. Recovery goes
// through an out-of-band channel (a different machine with an
// admin credential minting a fresh key, or the CLI bootstrap
// flow).
//
// The underlying secret is still generated and hashed via
// RotateAPIKey so the caller's old plaintext is definitively
// dead, the auth cache is flushed, and the audit trail records
// the rotation — it just doesn't travel back over this HTTP
// response. The audit event detail carries `burn: true` so
// operators can correlate post-incident.
func (h *TenancyHandlers) BurnMyAPIKey(w http.ResponseWriter, r *http.Request) {
	principal := tenancy.PrincipalFrom(r.Context())
	if principal == nil || principal.KeyID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "burn requires an authenticated request",
		})
		return
	}
	result, oldPrefix, err := h.Store.RotateAPIKey(r.Context(), principal.TenantID, principal.KeyID)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventAPIKeyRotated, result.Key.ID,
		audit.MarshalDetails(map[string]any{
			"tenant_id":  result.Key.TenantID,
			"name":       result.Key.Name,
			"role":       string(result.Key.Role),
			"old_prefix": oldPrefix,
			"new_prefix": result.Key.Prefix,
			"self":       true,
			"burn":       true,
		}))
	// Zeroing the local reference is defense-in-depth — the
	// plaintext is already out of scope as soon as we return, but
	// explicit zero-then-nil makes the intent obvious for anyone
	// reading the code later. The Go runtime may still keep a
	// copy in the bcrypt scratch buffer, but that's the existing
	// threat model (the regular RotateAPIKey path has the same
	// concern).
	result.Plaintext = ""
	result = nil
	w.WriteHeader(http.StatusNoContent)
}

// DeleteAPIKey handles DELETE /api/v1/keys/{id} — admin only.
func (h *TenancyHandlers) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	p := principalOrUnauthorized(w, r)
	if p == nil {
		return
	}
	id := r.PathValue("id")
	if err := h.Store.DeleteAPIKey(r.Context(), p.TenantID, id); err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
			return
		}
		writeServerError(w, err)
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventAPIKeyDeleted, id, "")
	w.WriteHeader(http.StatusNoContent)
}
