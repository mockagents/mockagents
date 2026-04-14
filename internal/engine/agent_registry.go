package engine

import (
	"fmt"
	"sort"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// AgentRegistry is a thread-safe in-memory store of loaded agent definitions,
// keyed by metadata.name.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*types.AgentDefinition
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*types.AgentDefinition),
	}
}

// Register adds or replaces an agent definition in the registry.
func (r *AgentRegistry) Register(def *types.AgentDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[def.Metadata.Name] = def
}

// Get retrieves an agent definition by name. Returns nil if not found.
func (r *AgentRegistry) Get(name string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[name]
}

// GetByModel retrieves the first agent matching the given model name.
// Returns nil if no agent uses that model.
func (r *AgentRegistry) GetByModel(model string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, def := range r.agents {
		if def.Spec.Model == model {
			return def
		}
	}
	return nil
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
func (r *AgentRegistry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.agents[name]; !ok {
		return fmt.Errorf("agent %q not found", name)
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
