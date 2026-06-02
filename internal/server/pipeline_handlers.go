package server

import (
	"net/http"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// PipelineHandlers serves GET /api/v1/pipelines and
// GET /api/v1/pipelines/{name}. The registry lives on the engine side
// so these handlers are a thin read-only façade over it — they hold
// no state of their own.
type PipelineHandlers struct {
	Registry *engine.PipelineRegistry
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
// directly from this shape.
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
	writeJSON(w, http.StatusOK, def)
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
