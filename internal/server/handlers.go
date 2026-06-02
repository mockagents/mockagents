package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// callerTenantID returns the tenant id of the authenticated principal
// on the request, or the empty string in single-tenant mode.
// Centralized here so every control-plane handler scopes the same
// way and the engine package stays free of any tenancy import.
func callerTenantID(r *http.Request) string {
	if p := tenancy.PrincipalFrom(r.Context()); p != nil {
		return p.TenantID
	}
	return ""
}

// Handlers holds the dependencies for HTTP handler functions.
type Handlers struct {
	Engine    *engine.Engine
	AgentsDir string
	StartTime time.Time
	Version   string
	Logger    *slog.Logger
	Recorder  *audit.Recorder // optional; nil = audit disabled
}

// HealthCheck returns server health status.
func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": h.Version,
		"uptime":  time.Since(h.StartTime).String(),
	})
}

// AgentSummary is the JSON response for listing agents.
type AgentSummary struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Model         string   `json:"model"`
	Protocol      string   `json:"protocol"`
	ScenarioCount int      `json:"scenario_count"`
	ToolCount     int      `json:"tool_count"`
	Tags          []string `json:"tags,omitempty"`
}

// ListAgents returns a JSON array of agent summaries scoped to the
// caller's tenant. In single-tenant mode the caller has no tenant id
// and the listing returns global agents only — identical to v0.1.
func (h *Handlers) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents := h.Engine.Registry.ListForTenant(callerTenantID(r))
	summaries := make([]AgentSummary, 0, len(agents))
	for _, a := range agents {
		summaries = append(summaries, AgentSummary{
			Name:          a.Metadata.Name,
			Description:   a.Metadata.Description,
			Model:         a.Spec.Model,
			Protocol:      a.Spec.Protocol,
			ScenarioCount: len(a.Spec.Behavior.Scenarios),
			ToolCount:     len(a.Spec.Tools),
			Tags:          a.Metadata.Tags,
		})
	}
	writeJSON(w, http.StatusOK, summaries)
}

// GetAgent returns the full definition of a single agent visible to
// the caller's tenant. A 404 is returned when the agent exists but
// belongs to a different tenant — leaking "you are not allowed" via
// 403 would expose the existence of foreign agent names.
func (h *Handlers) GetAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tenantID := callerTenantID(r)
	agent := h.Engine.Registry.GetForTenant(name, tenantID)
	if agent == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":            fmt.Sprintf("agent %q not found", name),
			"available_agents": h.Engine.Registry.ListNamesForTenant(tenantID),
		})
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

// ReloadAgent re-reads an agent's YAML from disk, validates, and replaces in-memory.
// Tenant-scoped: a caller from tenant A cannot reload tenant B's
// agents, even if they know the name.
func (h *Handlers) ReloadAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tenantID := callerTenantID(r)

	existing := h.Engine.Registry.GetForTenant(name, tenantID)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": fmt.Sprintf("agent %q not found", name),
		})
		return
	}

	// Scan agents directory for the file matching this agent.
	results, loadErrs := config.LoadDir(h.AgentsDir)
	if len(loadErrs) > 0 {
		h.Logger.Warn("errors loading agents during reload", "errors", fmt.Sprintf("%v", loadErrs))
	}

	var found bool
	validator := &config.Validator{}
	for _, result := range results {
		config.ApplyDefaults(result.Definition)
		// Match by name AND tenant (F-HD-002): if two tenants own
		// same-named agents, reload only the file belonging to the same
		// tenant as the agent the caller is authorized for — never
		// register another tenant's definition over it.
		if result.Definition.Metadata.Name != name ||
			result.Definition.Metadata.TenantID != existing.Metadata.TenantID {
			continue
		}
		found = true

		if errList := validator.Validate(result.Definition, result.FilePath, result.Node); errList != nil {
			h.Logger.Error("validation failed during reload",
				"agent", name,
				"file", result.FilePath,
				"errors", errList.Error(),
			)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "validation failed",
				"details": errList.Error(),
			})
			return
		}

		h.Engine.Registry.Register(result.Definition)
		h.Logger.Info("agent reloaded",
			"agent", name,
			"file", filepath.Base(result.FilePath),
		)
		h.Recorder.RecordHTTP(r, audit.EventAgentReloaded, name,
			audit.MarshalDetails(map[string]any{
				"file": filepath.Base(result.FilePath),
			}))
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "reloaded",
			"agent":  name,
		})
		return
	}

	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": fmt.Sprintf("no definition file found for agent %q in %s", name, h.AgentsDir),
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Already started writing; best effort log.
		slog.Error("failed to write JSON response", "error", err)
	}
}
