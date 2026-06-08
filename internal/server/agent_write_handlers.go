package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// maxAgentBodyBytes caps the YAML/JSON an agent write may submit (X-DOS-001),
// matching the validate endpoint. Agent definitions are small; 1 MiB stops an
// unbounded ReadAll / YAML alias-bomb from OOMing the process.
const maxAgentBodyBytes = 1 << 20

// agentNameRe mirrors the validator's metadata.name rule. A name that matches
// is inherently a safe single path segment (no "/", "\", ".", ".."), so it is
// safe to use directly in a persisted filename.
var agentNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// safeAgentName reports whether name is a valid, path-safe agent name. Applied
// to the attacker-controlled {name} path segment on PUT/DELETE before it is
// ever used to build a filesystem path.
func safeAgentName(name string) bool {
	return len(name) <= 63 && agentNameRe.MatchString(name)
}

// CreateAgent serves POST /api/v1/agents. It accepts a YAML or JSON Agent
// definition, validates it with the same rules the CLI uses, registers it in
// the engine so it serves immediately (no restart), and persists it to the
// agents directory when one is configured. The new agent is owned by the
// caller's tenant — a body-supplied tenant_id is ignored so a tenant cannot
// forge ownership. Returns 409 if an agent of that name already exists for the
// caller's tenant (use PUT to replace).
func (h *Handlers) CreateAgent(w http.ResponseWriter, r *http.Request) {
	def, canonical, ok := h.decodeAgent(w, r, "")
	if !ok {
		return
	}
	name := def.Metadata.Name
	tenantID := callerTenantID(r)

	h.agentWriteMu.Lock()
	defer h.agentWriteMu.Unlock()

	// Conflict check is against the caller's OWN bucket (no global fallback), so
	// a tenant can create a name that also exists globally (FB04-03).
	if h.Engine.Registry.GetOwnedForTenant(name, tenantID) != nil {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("agent %q already exists (use PUT /api/v1/agents/%s to replace)", name, name))
		return
	}

	file, persisted, err := h.persistAndRegister(def, name, tenantID, canonical)
	if err != nil {
		writeServerError(w, err)
		return
	}
	h.recordAgentEvent(r, audit.EventAgentCreated, name, file)
	h.Logger.Info("agent created", "agent", name, "persisted", persisted)

	writeJSON(w, http.StatusCreated, agentWriteResponse{
		Status: "created", Agent: name, Persisted: persisted, File: file,
	})
}

// PutAgent serves PUT /api/v1/agents/{name}: create-or-replace. The path name
// is authoritative and the body's metadata.name must agree. Replacing an
// existing agent is live immediately; the response status distinguishes a
// created from an updated agent.
func (h *Handlers) PutAgent(w http.ResponseWriter, r *http.Request) {
	pathName := r.PathValue("name")
	if !safeAgentName(pathName) {
		writeError(w, http.StatusBadRequest, "invalid agent name in path")
		return
	}
	def, canonical, ok := h.decodeAgent(w, r, pathName)
	if !ok {
		return
	}
	name := def.Metadata.Name
	tenantID := callerTenantID(r)

	h.agentWriteMu.Lock()
	defer h.agentWriteMu.Unlock()

	// created-vs-updated reflects the caller's OWN bucket — a global agent of the
	// same name must not make a tenant's create look like an update (FB04-03).
	existed := h.Engine.Registry.GetOwnedForTenant(name, tenantID) != nil

	file, persisted, err := h.persistAndRegister(def, name, tenantID, canonical)
	if err != nil {
		writeServerError(w, err)
		return
	}

	status := "created"
	event := audit.EventAgentCreated
	code := http.StatusCreated
	if existed {
		status, event, code = "updated", audit.EventAgentUpdated, http.StatusOK
	}
	h.recordAgentEvent(r, event, name, file)
	h.Logger.Info("agent "+status, "agent", name, "persisted", persisted)

	writeJSON(w, code, agentWriteResponse{
		Status: status, Agent: name, Persisted: persisted, File: file,
	})
}

// DeleteAgent serves DELETE /api/v1/agents/{name}: removes the agent from the
// engine (it stops serving immediately) and deletes its persisted file when
// one exists. Tenant-scoped: a caller can only delete an agent owned by its
// own tenant. Returns 404 if no such agent is visible to the caller.
func (h *Handlers) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !safeAgentName(name) {
		writeError(w, http.StatusBadRequest, "invalid agent name in path")
		return
	}
	tenantID := callerTenantID(r)

	h.agentWriteMu.Lock()
	defer h.agentWriteMu.Unlock()

	// 404 against the caller's own bucket (a tenant cannot delete a global agent).
	if h.Engine.Registry.GetOwnedForTenant(name, tenantID) == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", name))
		return
	}

	// Remove the persisted file FIRST, using the agent's tracked source path so we
	// delete the EXACT backing file (not a guessed <name>.yaml that may not exist
	// or may be a different agent's). If removal fails for any reason other than
	// "already gone", do NOT unregister — keep the registry consistent with disk
	// and report the failure, so a "deleted" response is never a lie (FB04-01/02).
	var file string
	target := h.Engine.Registry.Source(name, tenantID)
	if target == "" {
		if t, perr := h.agentFilePath(name, tenantID); perr == nil {
			target = t
		}
	}
	if target != "" {
		if err := os.Remove(target); err == nil {
			file = filepath.Base(target)
		} else if !os.IsNotExist(err) {
			writeServerError(w, fmt.Errorf("removing persisted agent file: %w", err))
			return
		}
	}

	// File is gone (or there was none) — now drop it from the registry. The
	// existence check above holds the write lock, so this cannot fail.
	_ = h.Engine.Registry.RemoveForTenant(name, tenantID)
	h.recordAgentEvent(r, audit.EventAgentDeleted, name, file)
	h.Logger.Info("agent deleted", "agent", name)

	writeJSON(w, http.StatusOK, agentWriteResponse{Status: "deleted", Agent: name, File: file})
}

// agentWriteResponse is the JSON envelope returned by the agent write routes.
type agentWriteResponse struct {
	Status    string `json:"status"`
	Agent     string `json:"agent"`
	Persisted bool   `json:"persisted"`
	File      string `json:"file,omitempty"`
}

// decodeAgent reads the request body (bounded), parses it as a YAML or JSON
// Agent definition, forces the caller's tenant as the owner, applies defaults,
// and runs the full validator. On any problem it writes the appropriate error
// response and returns ok=false. On success it returns the typed definition and
// its canonical YAML serialization (what gets persisted/registered).
//
// wantName, when non-empty (PUT), requires the body's metadata.name to match
// the path segment.
func (h *Handlers) decodeAgent(w http.ResponseWriter, r *http.Request, wantName string) (*types.AgentDefinition, []byte, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return nil, nil, false
		}
		writeError(w, http.StatusBadRequest, "reading body: "+err.Error())
		return nil, nil, false
	}

	// yaml.v3 parses JSON too (JSON is a YAML subset), so a single decode path
	// accepts both framings.
	var def types.AgentDefinition
	if err := yaml.Unmarshal(body, &def); err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent document: "+err.Error())
		return nil, nil, false
	}
	if strings.TrimSpace(def.Metadata.Name) == "" {
		writeError(w, http.StatusBadRequest, "metadata.name is required")
		return nil, nil, false
	}
	if wantName != "" && def.Metadata.Name != wantName {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("metadata.name %q does not match path %q", def.Metadata.Name, wantName))
		return nil, nil, false
	}
	if !safeAgentName(def.Metadata.Name) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("invalid metadata.name %q: must be lowercase kebab-case", def.Metadata.Name))
		return nil, nil, false
	}

	// Ownership is set by the server from the authenticated principal — never
	// trusted from the body — so a tenant can't plant an agent in another
	// tenant (or the global namespace).
	def.Metadata.TenantID = callerTenantID(r)
	config.ApplyDefaults(&def)

	// Marshal to canonical YAML and run the same validator the CLI/editor use.
	// Never register or persist on invalid input.
	canonical, err := yaml.Marshal(&def)
	if err != nil {
		writeServerError(w, fmt.Errorf("marshaling agent: %w", err))
		return nil, nil, false
	}
	report := config.ValidateBytes(canonical)
	if report.Kind != "" && report.Kind != "Agent" {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("expected an Agent document, got %q", report.Kind))
		return nil, nil, false
	}
	if len(report.Errors) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, validateResponse{
			OK: false, Kind: report.Kind, Errors: report.Errors,
		})
		return nil, nil, false
	}
	return &def, canonical, true
}

// agentFilePath returns the on-disk path an agent persists to, or "" when no
// AgentsDir is configured (in-memory-only mode). The path is confined to
// AgentsDir as defense in depth. In multi-tenant mode the filename is prefixed
// with the (sanitized) tenant id so two tenants' same-named agents don't
// collide on disk; the YAML itself carries metadata.tenant_id so ownership
// round-trips on restart.
func (h *Handlers) agentFilePath(name, tenantID string) (string, error) {
	if h.AgentsDir == "" {
		return "", nil
	}
	fname := name
	if tenantID != "" {
		fname = sanitizeFilenamePart(tenantID) + "." + name
	}
	target := filepath.Join(h.AgentsDir, fname+".yaml")

	absDir, err := filepath.Abs(h.AgentsDir)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if absTarget != absDir && !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("resolved agent path escapes the agents directory")
	}
	return target, nil
}

// persistAndRegister writes the canonical YAML to the agent's backing file (when
// AgentsDir is configured) and registers the definition in the engine. It
// overwrites the agent's EXISTING source file when one is known (so an agent
// loaded from a differently-named file is updated in place, not duplicated), and
// records the written path as the agent's source so a later update/delete targets
// the same file (FB04-01/02). Returns the base filename and whether a file was
// written. The caller holds agentWriteMu.
func (h *Handlers) persistAndRegister(def *types.AgentDefinition, name, tenantID string, canonical []byte) (string, bool, error) {
	target, err := h.resolveAgentTarget(name, tenantID)
	if err != nil {
		return "", false, err
	}
	if target == "" {
		// No AgentsDir: in-memory only, no source to track.
		h.Engine.Registry.Register(def)
		return "", false, nil
	}
	if err := atomicWriteFile(target, canonical); err != nil {
		return "", false, fmt.Errorf("writing agent file: %w", err)
	}
	h.Engine.Registry.RegisterWithSource(def, target)
	return filepath.Base(target), true, nil
}

// resolveAgentTarget returns the file to persist the (name, tenant) agent to: the
// agent's existing backing file when one is known (overwrite in place), otherwise
// the name-derived path. Returns "" when no AgentsDir is configured.
func (h *Handlers) resolveAgentTarget(name, tenantID string) (string, error) {
	if h.AgentsDir == "" {
		return "", nil
	}
	if src := h.Engine.Registry.Source(name, tenantID); src != "" {
		return src, nil
	}
	return h.agentFilePath(name, tenantID)
}

// recordAgentEvent records an audit event for an agent mutation when auditing
// is enabled (Recorder may be nil in single-tenant local-dev mode).
func (h *Handlers) recordAgentEvent(r *http.Request, kind audit.EventKind, name, file string) {
	if h.Recorder == nil {
		return
	}
	details := map[string]any{}
	if file != "" {
		details["file"] = file
	}
	h.Recorder.RecordHTTP(r, kind, name, audit.MarshalDetails(details))
}

// sanitizeFilenamePart reduces an arbitrary id to a safe filename fragment so a
// tenant id can never inject path separators or traversal into a persisted
// agent filename.
func sanitizeFilenamePart(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "tenant"
	}
	return b.String()
}
