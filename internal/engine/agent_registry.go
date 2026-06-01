package engine

import (
	"fmt"
	"sort"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// AgentRegistry is a thread-safe in-memory store of loaded agent definitions,
// keyed by metadata.name. A parallel byModel index is maintained so
// model-based lookups (the common case for OpenAI/Anthropic adapters)
// are O(1) instead of scanning the agents map on every request.
type AgentRegistry struct {
	mu      sync.RWMutex
	agents  map[string]*types.AgentDefinition
	byModel map[string]*types.AgentDefinition
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents:  make(map[string]*types.AgentDefinition),
		byModel: make(map[string]*types.AgentDefinition),
	}
}

// Register adds or replaces an agent definition in the registry. The
// byModel index is kept in sync: if an existing definition with the
// same name is being replaced, its previous model entry is cleared
// first so stale model mappings never leak.
func (r *AgentRegistry) Register(def *types.AgentDefinition) {
	// Guard before locking (F-AR-004): a nil def would panic on the
	// def.Metadata.Name deref below — under the write lock — taking down
	// the request that triggered the reload. Callers validate first, so
	// this only fires on a programmatic misuse; drop it silently.
	if def == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.agents[def.Metadata.Name]; ok && prev.Spec.Model != "" {
		// Only clear the index entry if it still points at `prev`
		// (another agent may have since claimed the same model).
		if indexed, ok := r.byModel[prev.Spec.Model]; ok && indexed == prev {
			delete(r.byModel, prev.Spec.Model)
		}
	}
	r.agents[def.Metadata.Name] = def
	if def.Spec.Model != "" {
		r.byModel[def.Spec.Model] = def
	}
}

// Get retrieves an agent definition by name. Returns nil if not found.
func (r *AgentRegistry) Get(name string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[name]
}

// GetByModel retrieves the agent matching the given model name.
// Returns nil if no agent uses that model. O(1) via the byModel index.
func (r *AgentRegistry) GetByModel(model string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byModel[model]
}

// List returns all registered agent definitions sorted by name.
func (r *AgentRegistry) List() []*types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*types.AgentDefinition, 0, len(r.agents))
	for _, def := range r.agents {
		result = append(result, def)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata.Name < result[j].Metadata.Name
	})
	return result
}

// ListNames returns all registered agent names sorted alphabetically.
func (r *AgentRegistry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Remove deletes an agent by name. Returns an error if not found.
// Also drops the corresponding byModel index entry when applicable.
func (r *AgentRegistry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	def, ok := r.agents[name]
	if !ok {
		return fmt.Errorf("agent %q not found", name)
	}
	if def.Spec.Model != "" {
		if indexed, ok := r.byModel[def.Spec.Model]; ok && indexed == def {
			delete(r.byModel, def.Spec.Model)
		}
	}
	delete(r.agents, name)
	return nil
}

// Count returns the number of registered agents.
func (r *AgentRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// --- Tenant-scoped views (v0.2 multi-tenancy isolation) ---
//
// The registry intentionally still keys agents by name (not by
// `(tenant, name)`) so v0.1 single-tenant deployments and the
// existing API are unchanged. Tenant scoping is layered on top as a
// *visibility* filter: an agent with a non-empty Metadata.TenantID
// is only returned to callers carrying that tenantID, while agents
// with an empty TenantID are "global" and visible to everyone.
//
// Constraint: agent names must be globally unique within a single
// MockAgents process. Two tenants cannot register agents with the
// same name; the second Register would replace the first regardless
// of the tenant fields. Multi-process tenancy (one MockAgents
// instance per tenant) sidesteps this entirely.

// visibleTo reports whether `def` is visible to a caller bound to
// `tenantID`. Empty `tenantID` matches only global agents (the v0.1
// behavior); non-empty `tenantID` matches global + that tenant's
// own agents.
func visibleTo(def *types.AgentDefinition, tenantID string) bool {
	if def == nil {
		return false
	}
	owner := def.Metadata.TenantID
	if owner == "" {
		return true
	}
	return owner == tenantID
}

// GetForTenant returns the named agent if it is visible to the
// given tenant, or nil otherwise. Empty tenantID returns global
// agents only — preserving the v0.1 lookup semantics.
func (r *AgentRegistry) GetForTenant(name, tenantID string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def := r.agents[name]
	if !visibleTo(def, tenantID) {
		return nil
	}
	return def
}

// GetByModelForTenant returns the agent matching the given model
// name if it is visible to the given tenant. When a tenant-scoped
// agent and a global agent share the same model name the
// tenant-scoped one wins for tenant callers; the global one wins
// for anonymous callers.
//
// This deliberately bypasses the byModel index because a
// tenant-scoped Register can overwrite the index slot belonging to
// a global agent — the index reflects "most recently registered for
// this model", which is fine for the non-tenant fast path
// (`GetByModel`) but ambiguous once tenancy enters the picture. The
// scan is O(n) but only runs in tenant-scoped lookups.
func (r *AgentRegistry) GetByModelForTenant(model, tenantID string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var fallbackGlobal *types.AgentDefinition
	for _, def := range r.agents {
		if def.Spec.Model != model {
			continue
		}
		if def.Metadata.TenantID == tenantID {
			// Exact owner match wins immediately.
			return def
		}
		if def.Metadata.TenantID == "" && fallbackGlobal == nil {
			fallbackGlobal = def
		}
	}
	return fallbackGlobal
}

// ListForTenant returns all agents visible to the given tenant
// sorted by name. Equivalent to List() when tenantID is empty.
func (r *AgentRegistry) ListForTenant(tenantID string) []*types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*types.AgentDefinition, 0, len(r.agents))
	for _, def := range r.agents {
		if visibleTo(def, tenantID) {
			result = append(result, def)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata.Name < result[j].Metadata.Name
	})
	return result
}

// ListNamesForTenant returns the names visible to the given tenant
// sorted alphabetically.
func (r *AgentRegistry) ListNamesForTenant(tenantID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.agents))
	for name, def := range r.agents {
		if visibleTo(def, tenantID) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
