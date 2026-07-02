package engine

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// AgentRegistry is a thread-safe in-memory store of loaded agent definitions.
//
// Agents are keyed by (owner tenant id, name): the outer map is the owner
// tenant ("" = global/unowned), the inner map is name → definition. This lets
// two tenants each own an agent with the same name (REF-08). Single-tenant
// deployments use only the "" bucket and behave exactly as v0.1.
//
// A parallel byModel index serves the legacy tenant-agnostic GetByModel, and a
// byModelTenant index backs the hot-path GetByModelForTenant so model lookups
// are O(1) instead of scanning the agents map on every request.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]map[string]*types.AgentDefinition // ownerTenantID -> name -> def
	// byModel is the tenant-agnostic, most-recently-registered agent per model.
	// It backs GetByModel only; the tenant-aware hot path uses byModelTenant.
	byModel map[string]*types.AgentDefinition
	// byModelTenant is the tenant-aware model index that backs the hot path
	// GetByModelForTenant (PERF-01): model -> owner-tenant -> the
	// lexicographically-smallest-named agent in that (model, owner) class. It
	// preserves both the tenant-visibility rule and the deterministic tie-break
	// (F-AR-002) while turning the per-request lookup from an O(n) agents scan
	// into two map reads. Maintained by rebuilding only the affected model
	// bucket on Register/Remove (rare).
	byModelTenant map[string]map[string]*types.AgentDefinition
	// sources records the on-disk file each registered agent was loaded from or
	// persisted to, keyed the same way (ownerTenantID -> name -> path). It lets
	// the write API overwrite/delete the EXACT backing file instead of guessing a
	// name-derived path, so an agent loaded from a differently-named file isn't
	// duplicated on update or resurrected after delete (FB04-01/02). "" means the
	// source is unknown (e.g. an agent created in memory only).
	sources map[string]map[string]string
}

// NewAgentRegistry creates an empty agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents:        make(map[string]map[string]*types.AgentDefinition),
		byModel:       make(map[string]*types.AgentDefinition),
		byModelTenant: make(map[string]map[string]*types.AgentDefinition),
		sources:       make(map[string]map[string]string),
	}
}

// rebuildModelBucket recomputes byModelTenant[model] from the agents that
// declare it: per owner tenant, the lexicographically smallest name wins
// (F-AR-002). The caller must hold the write lock. Cost is O(agents-with-model),
// paid only on Register/Remove; the hot-path lookup is then O(1).
func (r *AgentRegistry) rebuildModelBucket(model string) {
	if model == "" {
		return
	}
	owners := make(map[string]*types.AgentDefinition)
	for owner, byName := range r.agents {
		for _, def := range byName {
			if def.Spec.Model != model {
				continue
			}
			if cur, ok := owners[owner]; !ok || def.Metadata.Name < cur.Metadata.Name {
				owners[owner] = def
			}
		}
	}
	if len(owners) == 0 {
		delete(r.byModelTenant, model)
	} else {
		r.byModelTenant[model] = owners
	}
}

// Register adds or replaces an agent definition, keyed by (owner tenant, name).
// Replacing an agent with the same (tenant, name) clears its previous byModel
// entry first so stale model mappings never leak.
//
// Model collisions are last-writer-wins for byModel (F-AR-003): if two agents
// with different (tenant, name) declare the same Spec.Model, both remain in the
// agents map (and both appear in List), but byModel[model] points at whichever
// was registered last. GetByModel returns the most recent registrant; List
// still shows every agent — they intentionally disagree. The tenant-aware
// GetByModelForTenant does not consult byModel and breaks ties by name instead.
func (r *AgentRegistry) Register(def *types.AgentDefinition) {
	// Guard before locking (F-AR-004): a nil def would panic on the Name deref
	// below — under the write lock — taking down the request that triggered the
	// reload. Callers validate first, so this only fires on programmatic misuse.
	if def == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registerLocked(def)
}

// RegisterWithSource is Register plus recording the on-disk file the definition
// was loaded from or persisted to (see the sources field). Pass "" to register
// without a known source. The write API uses Source() to find the exact file to
// overwrite/delete so an agent loaded from a differently-named file is neither
// duplicated on update nor resurrected after delete (FB04-01/02).
func (r *AgentRegistry) RegisterWithSource(def *types.AgentDefinition, sourcePath string) {
	if def == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registerLocked(def)
	if sourcePath != "" {
		if r.sources == nil {
			r.sources = make(map[string]map[string]string)
		}
		owner := def.Metadata.TenantID
		if r.sources[owner] == nil {
			r.sources[owner] = make(map[string]string)
		}
		r.sources[owner][def.Metadata.Name] = sourcePath
	}
}

// registerLocked performs the registration under the caller-held write lock.
func (r *AgentRegistry) registerLocked(def *types.AgentDefinition) {
	owner := def.Metadata.TenantID
	name := def.Metadata.Name
	bucket := r.agents[owner]
	if bucket == nil {
		bucket = make(map[string]*types.AgentDefinition)
		r.agents[owner] = bucket
	}
	var oldModel string
	if prev, ok := bucket[name]; ok {
		oldModel = prev.Spec.Model
		if prev.Spec.Model != "" {
			// Only clear the index entry if it still points at `prev`
			// (another agent may have since claimed the same model).
			if indexed, ok := r.byModel[prev.Spec.Model]; ok && indexed == prev {
				delete(r.byModel, prev.Spec.Model)
			}
		}
	}
	bucket[name] = def
	if def.Spec.Model != "" {
		// Model collisions resolve deterministically (F-AR-002), but the
		// losing agent becomes unreachable by model — surface it at load time
		// instead of leaving authors to discover the tie-break by probing
		// (round-9 R9-17: six shipped examples claim gpt-4o).
		if prev, ok := r.byModel[def.Spec.Model]; ok && prev.Metadata.Name != name {
			slog.Warn("model claimed by multiple agents; requests resolve to one deterministically",
				"model", def.Spec.Model, "agents", []string{prev.Metadata.Name, name})
		}
		r.byModel[def.Spec.Model] = def
	}
	// Keep the tenant-aware index in sync (PERF-01): rebuild the bucket for the
	// new model, and the old model too if this agent moved between models.
	r.rebuildModelBucket(def.Spec.Model)
	if oldModel != "" && oldModel != def.Spec.Model {
		r.rebuildModelBucket(oldModel)
	}
}

// Get retrieves a global (unowned) agent definition by name. Returns nil if not
// found. This is the tenant-agnostic lookup — it only sees the "" bucket; use
// GetForTenant for tenant-scoped resolution.
func (r *AgentRegistry) Get(name string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b := r.agents[""]; b != nil {
		return b[name]
	}
	return nil
}

// GetByModel retrieves the agent matching the given model name.
// Returns nil if no agent uses that model. O(1) via the byModel index.
func (r *AgentRegistry) GetByModel(model string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byModel[model]
}

// countLocked returns the total agent count across all tenant buckets. Caller
// must hold at least the read lock.
func (r *AgentRegistry) countLocked() int {
	n := 0
	for _, byName := range r.agents {
		n += len(byName)
	}
	return n
}

// List returns all registered agent definitions across every tenant, sorted by
// name then owner tenant id (so same-named agents from different tenants have a
// stable order).
func (r *AgentRegistry) List() []*types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*types.AgentDefinition, 0, r.countLocked())
	for _, byName := range r.agents {
		for _, def := range byName {
			result = append(result, def)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Metadata.Name != result[j].Metadata.Name {
			return result[i].Metadata.Name < result[j].Metadata.Name
		}
		return result[i].Metadata.TenantID < result[j].Metadata.TenantID
	})
	return result
}

// ListNames returns the distinct agent names across every tenant, sorted
// alphabetically. A name owned by more than one tenant appears once.
func (r *AgentRegistry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := make(map[string]struct{})
	for _, byName := range r.agents {
		for name := range byName {
			set[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Remove deletes every agent named `name`, in any tenant bucket, and fixes the
// model indexes. Returns an error if no agent by that name exists. (In practice
// the file watcher — the only caller — operates on a single agents directory
// where names are unique; the across-buckets sweep preserves the historic
// "remove by name" behavior.) Use RemoveForTenant for tenant-precise removal.
func (r *AgentRegistry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	models := make(map[string]struct{})
	found := false
	for owner, byName := range r.agents {
		def, ok := byName[name]
		if !ok {
			continue
		}
		found = true
		if def.Spec.Model != "" {
			if indexed, ok := r.byModel[def.Spec.Model]; ok && indexed == def {
				delete(r.byModel, def.Spec.Model)
			}
			models[def.Spec.Model] = struct{}{}
		}
		delete(byName, name)
		if len(byName) == 0 {
			delete(r.agents, owner)
		}
		if sb := r.sources[owner]; sb != nil {
			delete(sb, name)
			if len(sb) == 0 {
				delete(r.sources, owner)
			}
		}
	}
	if !found {
		return fmt.Errorf("agent %q not found", name)
	}
	for m := range models {
		r.rebuildModelBucket(m)
	}
	return nil
}

// RemoveForTenant deletes the agent named `name` owned by `tenantID` ("" =
// global). Returns an error if that specific (tenant, name) pair is absent.
func (r *AgentRegistry) RemoveForTenant(name, tenantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	byName := r.agents[tenantID]
	def, ok := byName[name]
	if !ok {
		return fmt.Errorf("agent %q not found for tenant %q", name, tenantID)
	}
	if def.Spec.Model != "" {
		if indexed, ok := r.byModel[def.Spec.Model]; ok && indexed == def {
			delete(r.byModel, def.Spec.Model)
		}
	}
	delete(byName, name)
	if len(byName) == 0 {
		delete(r.agents, tenantID)
	}
	if sb := r.sources[tenantID]; sb != nil {
		delete(sb, name)
		if len(sb) == 0 {
			delete(r.sources, tenantID)
		}
	}
	r.rebuildModelBucket(def.Spec.Model)
	return nil
}

// Source returns the on-disk file backing the (tenantID, name) agent, or "" if
// the source is unknown (created in memory only, or never registered with one).
func (r *AgentRegistry) Source(name, tenantID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b := r.sources[tenantID]; b != nil {
		return b[name]
	}
	return ""
}

// GetOwnedForTenant returns the agent named `name` owned by tenantID ITSELF,
// with NO global fallback — unlike GetForTenant. The write API uses it so
// create-vs-replace, the 409 conflict check, the HTTP status, and the audit
// kind reflect the caller's own bucket rather than a shadowed global (FB04-03).
func (r *AgentRegistry) GetOwnedForTenant(name, tenantID string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b := r.agents[tenantID]; b != nil {
		return b[name]
	}
	return nil
}

// Count returns the total number of registered agents across all tenants.
func (r *AgentRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.countLocked()
}

// --- Tenant-scoped views (multi-tenancy isolation) ---
//
// Agents are keyed by (owner tenant, name). An agent with a non-empty
// Metadata.TenantID is owned by that tenant and visible only to it; an agent
// with an empty TenantID is "global" and visible to everyone as a shared
// fallback. Two tenants may each own an agent with the same name; when a
// tenant owns a name that also exists globally, the tenant's agent shadows the
// global one for that tenant (REF-08). Single-tenant deployments (every agent
// under "") are unchanged.

// GetForTenant returns the named agent visible to the given tenant: the
// tenant's own agent if present, else the global agent of that name, else nil.
// Empty tenantID resolves global agents only — the v0.1 lookup semantics.
func (r *AgentRegistry) GetForTenant(name, tenantID string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b := r.agents[tenantID]; b != nil {
		if def := b[name]; def != nil {
			return def
		}
	}
	if tenantID != "" {
		if g := r.agents[""]; g != nil {
			if def := g[name]; def != nil {
				return def
			}
		}
	}
	return nil
}

// GetByModelForTenant returns the agent matching the given model name visible
// to the given tenant. A tenant-scoped agent wins over a global one for tenant
// callers; the global one wins for anonymous callers.
//
// This is the adapter hot path (every model-based request), so it reads the
// byModelTenant index in O(1) rather than scanning the agents map (PERF-01).
// The index can't reuse the plain byModel slot — a tenant-scoped Register can
// overwrite the slot a global agent holds, and byModel is "most recently
// registered" rather than tenant-aware — so byModelTenant keeps a separate
// per-(model, owner) winner.
//
// Tie-break (F-AR-002): if several agents in the same visibility class (owner
// or global) share a model, the lexicographically smallest name wins, so the
// answer is stable across requests rather than depending on map-iteration order.
func (r *AgentRegistry) GetByModelForTenant(model, tenantID string) *types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	owners := r.byModelTenant[model]
	if owners == nil {
		return nil
	}
	// Caller's own tenant wins (for anonymous callers tenantID == "" is the
	// global class); otherwise fall back to the global agent.
	if ownerMatch := owners[tenantID]; ownerMatch != nil {
		return ownerMatch
	}
	return owners[""]
}

// ListForTenant returns all agents visible to the given tenant — the tenant's
// own agents plus globals, with a tenant agent shadowing a global of the same
// name — sorted by name. Equivalent to the global list when tenantID is empty.
func (r *AgentRegistry) ListForTenant(tenantID string) []*types.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	merged := r.mergedForTenantLocked(tenantID)
	result := make([]*types.AgentDefinition, 0, len(merged))
	for _, def := range merged {
		result = append(result, def)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata.Name < result[j].Metadata.Name
	})
	return result
}

// ListNamesForTenant returns the names visible to the given tenant, sorted.
func (r *AgentRegistry) ListNamesForTenant(tenantID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	merged := r.mergedForTenantLocked(tenantID)
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// mergedForTenantLocked returns name → def visible to tenantID: globals first,
// then the tenant's own agents shadowing any global of the same name. Caller
// must hold at least the read lock.
func (r *AgentRegistry) mergedForTenantLocked(tenantID string) map[string]*types.AgentDefinition {
	merged := make(map[string]*types.AgentDefinition)
	for name, def := range r.agents[""] {
		merged[name] = def
	}
	if tenantID != "" {
		for name, def := range r.agents[tenantID] {
			merged[name] = def
		}
	}
	return merged
}
