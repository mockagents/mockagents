package engine

import (
	"sort"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// PipelineRegistry is a thread-safe in-memory store of pipeline definitions
// keyed by metadata.name. It also records the on-disk source file each
// pipeline was loaded from (when known) so the GUI editor can write an edited
// definition back to the file it came from rather than guessing (REF-07).
type PipelineRegistry struct {
	mu        sync.RWMutex
	pipelines map[string]*types.PipelineDefinition
	sources   map[string]string // name -> source file path (best-effort)
}

// NewPipelineRegistry creates an empty pipeline registry.
func NewPipelineRegistry() *PipelineRegistry {
	return &PipelineRegistry{
		pipelines: make(map[string]*types.PipelineDefinition),
		sources:   make(map[string]string),
	}
}

// Register adds or replaces a pipeline definition. A nil def or one with
// an empty Metadata.Name is ignored (F-PR-002): the nil case would panic
// on the Name deref under the write lock, and an empty name would key the
// pipeline under "" where it shadows any other unnamed pipeline and can
// never be looked up. Callers validate first, so this only guards
// programmatic misuse. Register leaves any previously recorded source path
// for the name intact; use RegisterWithSource to set it.
func (r *PipelineRegistry) Register(def *types.PipelineDefinition) {
	if def == nil || def.Metadata.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pipelines[def.Metadata.Name] = def
}

// RegisterWithSource adds or replaces a pipeline definition and records the
// on-disk file it was loaded from (or written to). An empty sourcePath clears
// any recorded source for the name.
func (r *PipelineRegistry) RegisterWithSource(def *types.PipelineDefinition, sourcePath string) {
	if def == nil || def.Metadata.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pipelines[def.Metadata.Name] = def
	if sourcePath == "" {
		delete(r.sources, def.Metadata.Name)
		return
	}
	r.sources[def.Metadata.Name] = sourcePath
}

// Source returns the on-disk file a pipeline was loaded from, or "" when the
// name is unknown or has no recorded source.
func (r *PipelineRegistry) Source(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sources[name]
}

// GetPipeline retrieves a pipeline by name. Returns nil if not found.
// Implements runner.PipelineRegistry.
func (r *PipelineRegistry) GetPipeline(name string) *types.PipelineDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pipelines[name]
}

// List returns all pipelines sorted by name. It allocates a fresh slice
// and sorts on every call (F-PR-003) — deliberately not cached, because
// the only callers are admin/management surfaces (the GUI pipeline catalog
// and the management API), not the request hot path. If a per-request
// caller ever needs this, cache a sorted snapshot and invalidate on
// Register/Remove instead.
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
