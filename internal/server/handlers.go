package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// maxListLimit caps the `limit` query param on every list endpoint
// (logs/audit/costs) so a caller-controlled size can't drive an unbounded
// scan/allocation (X-LIMIT-001).
const maxListLimit = 10000

// maxListOffset caps the `offset` query param so a caller can't pass a huge
// value (e.g. ?offset=999999999) that forces SQLite to scan-and-discard an
// unbounded prefix of the table (SEC-04). Generous enough for real deep
// pagination; values above it are clamped, matching the limit clamp.
const maxListOffset = 1_000_000

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

	// agentWriteMu serializes the conflict-check → persist → register sequence
	// of the agent write API (FB-04) so concurrent POST/PUT/DELETE on the same
	// name can't interleave into a torn registry/disk state.
	agentWriteMu sync.Mutex
}

// HealthCheck is the LIVENESS probe: it answers "is this process running",
// and therefore returns 200 unconditionally. Whether the process can actually
// serve a mock is a different question, answered by GET /api/v1/ready — see
// ReadinessHandlers. Restarting a pod that is alive but has no fixtures would
// fix nothing, which is why liveness must NOT check dependencies.
//
// uptime_seconds and agents_loaded were documented in docs/api-spec.yaml long
// before they were emitted; they are added here rather than deleted from the
// spec because a consumer that followed the published contract should work.
// The pre-existing "uptime" string stays (the GUI reads it).
func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.StartTime)
	agents := 0
	if h.Engine != nil && h.Engine.Registry != nil {
		agents = h.Engine.Registry.Count()
	}
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:        "ok",
		Version:       h.Version,
		Uptime:        uptime.String(),
		UptimeSeconds: int64(uptime.Seconds()),
		AgentsLoaded:  agents,
	})
}

// HealthResponse is the body GET /api/v1/health returns.
//
// Always "ok": this is the LIVENESS probe, so reaching the handler at all is
// the answer. Whether the process can serve a mock is ReadinessResponse's
// question.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	// Uptime is the human-readable form (time.Duration.String); UptimeSeconds
	// is the machine-readable one. Both are kept because the GUI reads the
	// first and the published contract promised the second.
	Uptime        string `json:"uptime"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	AgentsLoaded  int    `json:"agents_loaded"`
}

// ReloadResponse is the body POST /api/v1/agents/{name}/reload returns.
//
// One agent, named — not a bulk count. The spec described a bulk reload
// ({reloaded_count, errors}) that this route has never performed; the checker
// found that once the shape had a type to compare against.
type ReloadResponse struct {
	Status string `json:"status"`
	Agent  string `json:"agent"`
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
	// EffectiveRevision is the revision of the RUNNING definition — the same
	// value GET /agents/{name} publishes as X-Mockagents-Revision-Effective.
	// Listing it lets a caller see at a glance which definitions have moved,
	// without a request per agent. Empty when it could not be computed, which
	// is not the same as unchanged.
	//
	// Deliberately NOT the ETag. The ETag combines this with a hash of the
	// backing file, so computing it here would mean reading every agent's file
	// on every listing — and a caller that mistook one for the other would
	// send a precondition that always fails. Fetch the agent for its ETag.
	EffectiveRevision string `json:"effective_revision,omitempty"`
	// Persistence answers "does this survive a restart", which a count of
	// agents cannot. One of:
	//
	//	file    — backed by a file that is present now
	//	runtime — no backing file; created at runtime and lost on restart
	//	missing — a backing file is tracked but is not there any more, so the
	//	          definition is serving from memory only
	//
	// "missing" is deliberately distinct from "runtime": one is a choice, the
	// other is a surprise waiting for the next restart.
	Persistence string `json:"persistence"`
	// File is the base name of the backing file, when there is one. Only the
	// base name: the absolute path is server-side detail a client has no use
	// for and should not be handed.
	File string `json:"file,omitempty"`
}

// Persistence values for AgentSummary. Stable wire strings.
const (
	agentPersistedToFile = "file"
	agentRuntimeOnly     = "runtime"
	agentFileMissing     = "missing"
)

// summarizePersistence classifies an agent's durability from its tracked source
// path. The stat is deliberate: a tracked path whose file has been deleted out
// of band still serves from memory, and reporting it as persisted would promise
// a restart it will not survive. A stat per agent is cheap next to the read that
// computing a source hash would need — and the source hash is not what these
// two columns are for.
func summarizePersistence(source string) (kind, file string) {
	if source == "" {
		return agentRuntimeOnly, ""
	}
	if _, err := os.Stat(source); err != nil {
		return agentFileMissing, filepath.Base(source)
	}
	return agentPersistedToFile, filepath.Base(source)
}

// ListAgents returns a JSON array of agent summaries scoped to the
// caller's tenant. In single-tenant mode the caller has no tenant id
// and the listing returns global agents only — identical to v0.1.
func (h *Handlers) ListAgents(w http.ResponseWriter, r *http.Request) {
	tenantID := callerTenantID(r)
	agents := h.Engine.Registry.ListForTenant(tenantID)
	summaries := make([]AgentSummary, 0, len(agents))
	for _, a := range agents {
		summary := AgentSummary{
			Name:          a.Metadata.Name,
			Description:   a.Metadata.Description,
			Model:         a.Spec.Model,
			Protocol:      a.Spec.Protocol,
			ScenarioCount: len(a.Spec.Behavior.Scenarios),
			ToolCount:     len(a.Spec.Tools),
			Tags:          a.Metadata.Tags,
		}
		source := h.Engine.Registry.Source(a.Metadata.Name, tenantID)
		summary.Persistence, summary.File = summarizePersistence(source)
		// Only the effective revision, so this stays a marshal per agent with
		// no file read. Drift against the file on disk is a per-agent question
		// and stays on GET /agents/{name}, which already answers it.
		if rev, err := revisionFor(a, ""); err == nil {
			summary.EffectiveRevision = rev.Effective
		}
		summaries = append(summaries, summary)
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
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:           fmt.Sprintf("agent %q not found", name),
			AvailableAgents: h.Engine.Registry.ListNamesForTenant(tenantID),
		})
		return
	}
	// UX-03: publish the revision an editor echoes back as If-Match. The body
	// is the COMPLETE definition (not the reduced summary type), so what a
	// client reads here is what it can safely round-trip.
	if rev, err := revisionFor(agent, h.Engine.Registry.Source(name, tenantID)); err == nil {
		setRevisionHeaders(w, rev)
	}

	// An editor wants the document in the format the product is authored in.
	// Content negotiation is additive: JSON stays the default, so no existing
	// client changes behaviour.
	//
	// The YAML served is the CANONICAL form of the definition the engine is
	// serving — not the bytes of the source file. That is deliberate: it is
	// exactly what a conditional PUT will store, so what an editor shows is
	// what it will write. The cost is that comments and formatting from a
	// hand-authored file are not represented here, and a save replaces them;
	// a UI must say so rather than implying round-trip fidelity.
	if wantsYAML(r) {
		out, err := yaml.Marshal(agent)
		if err != nil {
			writeServerError(w, fmt.Errorf("marshaling agent: %w", err))
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
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
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
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
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error:   "validation failed",
				Details: errList.Error(),
			})
			return
		}

		h.Engine.Registry.RegisterWithSource(result.Definition, result.FilePath)
		h.Logger.Info("agent reloaded",
			"agent", name,
			"file", filepath.Base(result.FilePath),
		)
		h.Recorder.RecordHTTP(r, audit.EventAgentReloaded, name,
			audit.MarshalDetails(map[string]any{
				"file": filepath.Base(result.FilePath),
			}))
		writeJSON(w, http.StatusOK, ReloadResponse{Status: "reloaded", Agent: name})
		return
	}

	if !found {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no definition file found for agent %q in %s", name, h.AgentsDir))
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

// ErrorResponse is the canonical management-API error envelope.
//
// A fixed struct encodes without the map allocation the literal incurred on
// every 4xx/5xx, which adds up under chaos error storms (PERF-16) — and, since
// it is exported, tools/driftcheck can hold the OpenAPI `Error` schema to it.
// It was that check that showed most call sites were still building the map by
// hand, getting neither guarantee.
//
// The optional fields are the two shapes the management API actually adds to an
// error, kept here rather than as separate envelopes so there is ONE error
// shape to document and one to parse.
//
// NOT the shape the provider endpoints return under a quota rejection: those
// deliberately mimic the upstream `{"error": {"type", "message"}}` object so an
// SDK surfaces them correctly. See providerError.
type ErrorResponse struct {
	Error string `json:"error"`
	// AvailableAgents lists what the caller COULD have asked for, on a 404 for
	// an unknown agent. A tenant-scoped list: it never names another tenant's.
	AvailableAgents []string `json:"available_agents,omitempty"`
	// Details carries validator output when a write or reload failed schema
	// validation.
	Details string `json:"details,omitempty"`
}

// writeError writes the canonical management-API error envelope:
// a flat {"error": message} body with the given status.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
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
