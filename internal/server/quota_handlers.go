package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// QuotaHandlers serves the per-tenant quota API (REF-08 slice C): a caller can
// read its own limits + usage, and a platform operator can set a tenant's
// override.
type QuotaHandlers struct {
	Enforcer *quota.Enforcer
	// Store, when set, persists overrides so they survive restarts. Nil keeps
	// overrides in-memory only.
	Store tenancy.Store
}

// GetQuota handles GET /api/v1/quota — the caller tenant's effective limits and
// current-month usage. In single-tenant mode the caller has no tenant id and
// sees the (unlimited) defaults.
func (h *QuotaHandlers) GetQuota(w http.ResponseWriter, r *http.Request) {
	if h.Enforcer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "quotas not enabled"})
		return
	}
	tenantID := callerTenantID(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"limits":    h.Enforcer.Effective(tenantID),
		"usage":     h.Enforcer.Usage(tenantID),
	})
}

// SetTenantQuota handles PUT /api/v1/tenants/{id}/quota — set a per-tenant
// override. Platform-gated (route floor): a tenant admin must not raise its own
// cap, so quota management is a cross-tenant operator action.
func (h *QuotaHandlers) SetTenantQuota(w http.ResponseWriter, r *http.Request) {
	if h.Enforcer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "quotas not enabled"})
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing tenant id"})
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var cfg quota.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if cfg.RatePerSec < 0 || cfg.RateBurst < 0 || cfg.MonthlySpendUSD < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quota values must be non-negative"})
		return
	}
	// Persist first (so a restart keeps the override); only then apply it to the
	// live enforcer, so the in-memory and on-disk views never diverge.
	if h.Store != nil {
		if err := h.Store.SetTenantQuota(r.Context(), id, cfg); err != nil {
			if errors.Is(err, tenancy.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant not found"})
				return
			}
			writeServerError(w, err)
			return
		}
	}
	h.Enforcer.SetOverride(id, cfg)
	writeJSON(w, http.StatusOK, map[string]any{"tenant_id": id, "limits": cfg})
}
