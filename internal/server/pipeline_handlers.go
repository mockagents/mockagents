package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// maxPipelineBodyBytes caps the JSON a caller may PUT to the pipeline write
// endpoint. Pipeline definitions are small; 1 MiB stops an unbounded ReadAll
// from OOMing the process (mirrors the validate handler's cap).
const maxPipelineBodyBytes = 1 << 20

// PipelineRunRequest is the wire contract for executing a registered pipeline.
// SessionID is optional; when omitted the HTTP request id becomes the run's
// session id so independent calls cannot accidentally share agent turn state.
type PipelineRunRequest struct {
	Input     string `json:"input"`
	SessionID string `json:"session_id,omitempty"`
}

// pipelineRunError preserves completed nodes when a later node fails. This is
// particularly useful for parallel pipelines and trajectory assertions: the
// caller can see what ran instead of receiving an opaque error only.
type pipelineRunError struct {
	Error string `json:"error"`
	// Code classifies the failure so a caller can react without parsing Error.
	// A pipeline that references an agent nobody loaded needs a different
	// response from a scenario that failed to match — the first is fixed by
	// loading a definition, the second by editing one — and telling them apart
	// by substring is the kind of coupling that breaks on a reworded message.
	Code   string                 `json:"code,omitempty"`
	Result *engine.PipelineResult `json:"result,omitempty"`
}

// Run failure codes. Stable wire values: a client may switch on them.
const (
	// runErrMissingDependency: a node names an agent that is not loaded for
	// this caller. Note that the run DID start — refs resolve at node-execution
	// time, so nodes before the missing one have already run and advanced their
	// session state.
	runErrMissingDependency = "missing_dependency"
	// runErrInvalidPipeline: the definition itself cannot be executed (unknown
	// topology, a cycle). No node ran.
	runErrInvalidPipeline = "invalid_pipeline"
	// runErrNodeFailed: a node executed and failed. The default.
	runErrNodeFailed = "node_failed"
)

// pipelineRunErrorCode classifies a run failure. errors.Is sees through
// errors.Join, so a parallel run whose failures include a missing agent is
// still reported as a missing dependency.
func pipelineRunErrorCode(err error) string {
	switch {
	case errors.Is(err, engine.ErrAgentNotFound):
		return runErrMissingDependency
	case errors.Is(err, engine.ErrUnknownTopology), errors.Is(err, engine.ErrPipelineCycle):
		return runErrInvalidPipeline
	default:
		return runErrNodeFailed
	}
}

// PipelineHandlers serves the /api/v1/pipelines endpoints. Reads (list +
// detail) are a thin façade over the registry; the write path (PUT, REF-07)
// validates an edited definition, persists it atomically to its source file,
// re-registers it, and audits the change.
type PipelineHandlers struct {
	Registry *engine.PipelineRegistry
	// Executor runs registered definitions through the same Engine used by the
	// provider adapters. Nil means execution is not configured (read/edit
	// endpoints can still be used independently in focused tests).
	Executor *engine.PipelineExecutor
	// AgentRegistry resolves pipeline agent refs during write validation.
	// Nil skips the cross-document ref check.
	AgentRegistry *engine.AgentRegistry
	// AgentsDir is where a newly-named pipeline (no known source file) is
	// written. Empty disables writing pipelines that have no source.
	AgentsDir string
	// Recorder records pipeline.saved events. Nil = audit disabled.
	Recorder *audit.Recorder
	Logger   *slog.Logger
	// writeMu serializes the read-version → write → re-register sequence so
	// two concurrent saves can't interleave.
	writeMu sync.Mutex
}

// RunPipeline handles POST /api/v1/pipelines/{name}/run.
func (h *PipelineHandlers) RunPipeline(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil || h.Executor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "pipeline execution not configured"})
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing pipeline name"})
		return
	}
	def := h.Registry.GetPipeline(name)
	if def == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxPipelineBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req PipelineRunRequest
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if err := ensureJSONEOF(dec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Input) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "input is required"})
		return
	}
	if req.SessionID == "" {
		req.SessionID = requestIDFromContext(r.Context())
		if req.SessionID == "" { // bare handler tests or custom embedding
			req.SessionID = generateRequestID()
		}
	}

	result, err := h.Executor.RunContext(r.Context(), def, req.Input, req.SessionID)
	if err != nil {
		// The definition exists, but one or more nodes could not execute. 422
		// distinguishes a valid HTTP request from a runnable pipeline and keeps
		// any completed node trajectory available to the caller.
		writeJSON(w, http.StatusUnprocessableEntity, pipelineRunError{
			Error:  err.Error(),
			Code:   pipelineRunErrorCode(err),
			Result: result,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ensureJSONEOF rejects concatenated JSON values while allowing trailing
// whitespace. Decoder.Decode alone would silently accept the first value.
func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain exactly one JSON object")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// PipelineSummary is the row shape returned by ListPipelines. It
// mirrors AgentSummary so the GUI can reuse its card layout.
type PipelineSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Topology    string `json:"topology"`
	AgentCount  int    `json:"agent_count"`
	EdgeCount   int    `json:"edge_count"`
}

// ListPipelines handles GET /api/v1/pipelines.
func (h *PipelineHandlers) ListPipelines(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil {
		writeJSON(w, http.StatusOK, []PipelineSummary{})
		return
	}
	defs := h.Registry.List()
	out := make([]PipelineSummary, 0, len(defs))
	for _, def := range defs {
		out = append(out, summarizePipeline(def))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetPipeline handles GET /api/v1/pipelines/{name}. Returns the full
// PipelineDefinition — the GUI DAG viewer consumes nodes + edges
// directly from this shape — and sets an ETag the editor echoes back as
// If-Match on PUT for optimistic concurrency.
func (h *PipelineHandlers) GetPipeline(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing pipeline name"})
		return
	}
	if h.Registry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline registry not configured"})
		return
	}
	def := h.Registry.GetPipeline(name)
	if def == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}
	if ver, err := h.pipelineVersion(name); err == nil && ver != "" {
		w.Header().Set("ETag", `"`+ver+`"`)
	}
	writeJSON(w, http.StatusOK, def)
}

// UpdatePipeline handles PUT /api/v1/pipelines/{name}: validate an edited
// definition, then persist it atomically to its source file and re-register it
// so the change is live immediately (REF-07).
func (h *PipelineHandlers) UpdatePipeline(w http.ResponseWriter, r *http.Request) {
	if h.Registry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline registry not configured"})
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing pipeline name"})
		return
	}
	// PUT updates an existing pipeline; creation is a separate (future) flow.
	if h.Registry.GetPipeline(name) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxPipelineBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reading body: " + err.Error()})
		return
	}

	var def types.PipelineDefinition
	if err := json.Unmarshal(body, &def); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// The path name is authoritative: the body must agree, and the name must be
	// a single safe path segment (the structural kebab-case rule is re-checked
	// by ValidateBytes below; this guards the filesystem path directly).
	if def.Metadata.Name != name {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("metadata.name %q does not match path %q", def.Metadata.Name, name),
		})
		return
	}
	if !safePipelineName(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid pipeline name"})
		return
	}

	// Marshal to YAML and run it through the same validator the CLI/editor use,
	// plus the cross-document agent-ref check. Never write on invalid input.
	yamlBytes, err := yaml.Marshal(&def)
	if err != nil {
		writeServerError(w, fmt.Errorf("marshaling pipeline: %w", err))
		return
	}
	report := config.ValidateBytes(yamlBytes)
	refErrs := h.validateAgentRefs(&def)
	if len(report.Errors) > 0 || len(refErrs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, validateResponse{
			OK:     false,
			Kind:   report.Kind,
			Errors: append(report.Errors, refErrs...),
		})
		return
	}

	// Serialize the read-version → write → re-register sequence.
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	// Optimistic concurrency: If-Match must equal the current version.
	current, err := h.pipelineVersion(name)
	if err != nil {
		writeServerError(w, fmt.Errorf("computing pipeline version: %w", err))
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	if ifMatch == "" {
		writeJSON(w, http.StatusPreconditionRequired, map[string]string{
			"error": "If-Match header required (echo the ETag from GET)",
		})
		return
	}
	if ifMatch != current {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{
			"error": "pipeline changed since it was loaded; reload and re-apply",
		})
		return
	}

	target, err := h.resolveTargetFile(name)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if err := atomicWriteFile(target, yamlBytes); err != nil {
		writeServerError(w, fmt.Errorf("writing pipeline: %w", err))
		return
	}

	// Re-register so the edit is live immediately, retaining the source path.
	h.Registry.RegisterWithSource(&def, target)

	if h.Recorder != nil {
		h.Recorder.RecordHTTP(r, audit.EventPipelineSaved, name,
			audit.MarshalDetails(map[string]any{"file": filepath.Base(target)}))
	}
	if h.Logger != nil {
		h.Logger.Info("pipeline saved", "name", name, "file", filepath.Base(target))
	}

	// We wrote yamlBytes to target, so the new file hash is hashHex(yamlBytes).
	w.Header().Set("ETag", `"`+hashHex(yamlBytes)+`"`)
	writeJSON(w, http.StatusOK, &def)
}

// validateAgentRefs checks every pipeline agent ref names a known agent,
// returning per-ref validation errors (the same shape the validator emits).
func (h *PipelineHandlers) validateAgentRefs(def *types.PipelineDefinition) []*config.ValidationError {
	if h.AgentRegistry == nil {
		return nil
	}
	var errs []*config.ValidationError
	for i, node := range def.Spec.Agents {
		if node.Ref == "" {
			continue // the per-document validator already flags empty refs
		}
		if h.AgentRegistry.Get(node.Ref) == nil {
			errs = append(errs, &config.ValidationError{
				Field:      fmt.Sprintf("spec.agents[%d].ref", i),
				Message:    fmt.Sprintf("pipeline references unknown agent %q", node.Ref),
				Suggestion: fmt.Sprintf("Add an agent with metadata.name: %s, or fix the ref.", node.Ref),
			})
		}
	}
	return errs
}

// pipelineVersion returns an opaque content hash used as an ETag for optimistic
// concurrency. It hashes the source file bytes when a source path is known (so
// an out-of-band edit on disk is detected), otherwise the canonical marshaled
// definition.
func (h *PipelineHandlers) pipelineVersion(name string) (string, error) {
	if src := h.Registry.Source(name); src != "" {
		if data, err := os.ReadFile(src); err == nil {
			return hashHex(data), nil
		}
		// File vanished out-of-band: fall through to the in-memory def hash.
	}
	def := h.Registry.GetPipeline(name)
	if def == nil {
		return "", nil
	}
	data, err := yaml.Marshal(def)
	if err != nil {
		return "", err
	}
	return hashHex(data), nil
}

// resolveTargetFile returns the file to write the pipeline to: its recorded
// source when known, otherwise <AgentsDir>/<name>.yaml. The synthesized path
// is confined to AgentsDir as defense in depth.
func (h *PipelineHandlers) resolveTargetFile(name string) (string, error) {
	if src := h.Registry.Source(name); src != "" {
		return src, nil
	}
	if h.AgentsDir == "" {
		return "", fmt.Errorf("no agents directory configured; cannot persist a new pipeline file")
	}
	target := filepath.Join(h.AgentsDir, name+".yaml")
	absDir, err := filepath.Abs(h.AgentsDir)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if absTarget != absDir && !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("resolved pipeline path escapes the agents directory")
	}
	return target, nil
}

// atomicWriteFile writes data to path via a temp file in the same directory
// plus a rename, so a crash mid-write can't truncate a live config.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pipeline-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has consumed it
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// safePipelineName rejects names that aren't a single safe path segment.
func safePipelineName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`) && !strings.Contains(name, "..")
}

func summarizePipeline(def *types.PipelineDefinition) PipelineSummary {
	return PipelineSummary{
		Name:        def.Metadata.Name,
		Description: def.Metadata.Description,
		Topology:    def.Spec.Topology,
		AgentCount:  len(def.Spec.Agents),
		EdgeCount:   len(def.Spec.Edges),
	}
}
