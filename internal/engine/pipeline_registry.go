package engine

import (
	"sort"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// PipelineRegistry is a thread-safe in-memory store of pipeline definitions
// keyed by metadata.name.
type PipelineRegistry struct {
	mu        sync.RWMutex
	pipelines map[string]*types.PipelineDefinition
}

// NewPipelineRegistry creates an empty pipeline registry.
func NewPipelineRegistry() *PipelineRegistry {
	return &PipelineRegistry{pipelines: make(map[string]*types.PipelineDefinition)}
}

// Register adds or replaces a pipeline definition. A nil def or one with
// an empty Metadata.Name is ignored (F-PR-002): the nil case would panic
// on the Name deref under the write lock, and an empty name would key the
// pipeline under "" where it shadows any other unnamed pipeline and can
// never be looked up. Callers validate first, so this only guards
// programmatic misuse.
func (r *PipelineRegistry) Register(def *types.PipelineDefinition) {
	if def == nil || def.Metadata.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pipelines[def.Metadata.Name] = def
}

// GetPipeline retrieves a pipeline by name. Returns nil if not found.
// Implements runner.PipelineRegistry.
func (r *PipelineRegistry) GetPipeline(name string) *types.PipelineDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pipelines[name]
}

// List returns all pipelines sorted by name.
func (r *PipelineRegistry) List() []*types.PipelineDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*types.PipelineDefinition, 0, len(r.pipelines))
	for _, p := range r.pipelines {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

// Count returns the number of registered pipelines.
func (r *PipelineRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.pipelines)
}
