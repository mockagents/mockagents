package server

import (
	"encoding/json"
	"errors"
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

// ListTenants handles GET /api/v1/tenants — admin only.
func (h *TenancyHandlers) ListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.Store.ListTenants(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	tenant, err := h.Store.CreateTenant(r.Context(), req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventTenantDeleted, id, "")
	w.WriteHeader(http.StatusNoContent)
}

// ListAPIKeys handles GET /api/v1/tenants/{id}/keys — admin or editor.
func (h *TenancyHandlers) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")
	keys, err := h.Store.ListAPIKeys(r.Context(), tenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// CreateAPIKeyRequest is the JSON body accepted by POST /api/v1/tenants/{id}/keys.
type CreateAPIKeyRequest struct {
	Name string        `json:"name"`
	Role tenancy.Role  `json:"role"`
}

// CreateAPIKey handles POST /api/v1/tenants/{id}/keys — admin only.
// Returns the plaintext key in the response body; subsequent list
// requests only show metadata.
func (h *TenancyHandlers) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")
	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

// DeleteAPIKey handles DELETE /api/v1/keys/{id} — admin only.
func (h *TenancyHandlers) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Store.DeleteAPIKey(r.Context(), id); err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Recorder.RecordHTTP(r, audit.EventAPIKeyDeleted, id, "")
	w.WriteHeader(http.StatusNoContent)
}
