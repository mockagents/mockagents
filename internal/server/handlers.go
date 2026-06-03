package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// maxListLimit caps the `limit` query param on every list endpoint
// (logs/audit/costs) so a caller-controlled size can't drive an unbounded
// scan/allocation (X-LIMIT-001).
const maxListLimit = 10000

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

// respEncoder bundles a pooled bytes.Buffer with a json.Encoder bound to it so
// writeJSON reuses both across responses instead of allocating a fresh encoder
// per response and reflect-encoding directly into the socket (PERF-04).
// json.Encoder.Encode appends a trailing newline, so the wire bytes are
// unchanged from the previous json.NewEncoder(w).Encode(v).
type respEncoder struct {
	buf *bytes.Buffer
	enc *json.Encoder
}

var respEncPool = sync.Pool{
	New: func() any {
		b := new(bytes.Buffer)
		return &respEncoder{buf: b, enc: json.NewEncoder(b)}
	},
}

// maxPooledRespBufBytes caps the buffer size retained in the pool so a single
// large response can't turn it into a permanent memory high-water mark.
const maxPooledRespBufBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	re := respEncPool.Get().(*respEncoder)
	re.buf.Reset()
	defer func() {
		if re.buf.Cap() <= maxPooledRespBufBytes {
			respEncPool.Put(re)
		}
	}()
	if err := re.enc.Encode(v); err != nil {
		// Encoding into the buffer failed before we wrote anything — best-effort
		// log; we still send the status below so the client isn't left hanging.
		slog.Error("failed to encode JSON response", "error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(re.buf.Bytes())
}

// writeError writes the canonical management-API error envelope:
// a flat {"error": message} body with the given status.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeServerError logs an internal error server-side and returns a generic
// 500 to the client, so DB/driver internals never leak over the wire
// (F-TN-006).
func writeServerError(w http.ResponseWriter, err error) {
	slog.Error("internal server error", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// parseBoundedInt reads an integer query value, rejecting non-integers and
// values below min with a 400, and clamping values above max down to max
// (max <= 0 means no upper bound). The caller guards `value != ""`.
func parseBoundedInt(w http.ResponseWriter, value, param string, min, max int) (int, bool) {
	n, err := strconv.Atoi(value)
	if err != nil || n < min {
		writeError(w, http.StatusBadRequest, "invalid "+param+" parameter")
		return 0, false
	}
	if max > 0 && n > max {
		n = max
	}
	return n, true
}

// parseTimestampParam validates an optional RFC3339 timestamp query param,
// returning the original string for store filters that take a string. An
// empty value is valid (no filter); a malformed value writes a 400 and
// returns ok=false. This gives the costs/logs list endpoints the same
// input validation the audit endpoint already does (F-CO-004 / F-LH-011) —
// without it, a bad `since`/`until` silently depends on store behavior.
// RFC3339 is short, so a valid value is inherently length-bounded.
func parseTimestampParam(w http.ResponseWriter, value, param string) (string, bool) {
	if value == "" {
		return "", true
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		writeError(w, http.StatusBadRequest, param+" must be RFC3339: "+err.Error())
		return "", false
	}
	return value, true
}
