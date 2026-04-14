// Package contract exposes the public surface of an agent as a
// canonical, comparable document. Contracts are intended to be
// serialized (JSON), stored under version control, and diffed across
// revisions to catch breaking changes before they ship.
package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/mockagents/mockagents/internal/types"
)

// Contract is the canonical, diff-friendly description of an agent.
// Fields mirror the consumer-visible subset of types.AgentDefinition —
// match rules and response content are deliberately excluded because
// those are producer internals, not part of the contract.
type Contract struct {
	Name     string         `json:"name"`
	Protocol string         `json:"protocol"`
	Model    string         `json:"model,omitempty"`
	Tools    []ToolContract `json:"tools,omitempty"`
	// Scenarios records the ordered list of scenario names the agent
	// advertises; it is part of the contract because consumers may
	// assert on matched scenario names in tests.
	Scenarios []string        `json:"scenarios,omitempty"`
	Streaming *StreamContract `json:"streaming,omitempty"`
}

// ToolContract is the comparable slice of a tool definition. The
// JSON-Schema shape is kept as-is so standard diff tools still work
// against it.
type ToolContract struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// StreamContract advertises streaming support.
type StreamContract struct {
	Enabled bool `json:"enabled"`
}

// Extract builds a Contract from an AgentDefinition. The result is
// deterministic: tools and scenarios are sorted by name so lexical
// diffs are noise-free across reordering refactors.
func Extract(def *types.AgentDefinition) *Contract {
	c := &Contract{
		Name:     def.Metadata.Name,
		Protocol: def.Spec.Protocol,
		Model:    def.Spec.Model,
	}

	tools := make([]ToolContract, 0, len(def.Spec.Tools))
	for _, t := range def.Spec.Tools {
		tools = append(tools, ToolContract{
			Name:        t.Name,
			Description: t.Description,
			// Round-trip through JSON so the parameters are always a
			// canonical map[string]any tree regardless of whether the
			// agent was loaded from YAML or JSON. Without this the
			// deeper nodes can end up with yaml-native types and the
			// downstream diff helpers see different shapes for the
			// same logical schema.
			Parameters: normalizeJSON(t.Parameters),
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	c.Tools = tools

	scenarios := make([]string, 0, len(def.Spec.Behavior.Scenarios))
	for _, s := range def.Spec.Behavior.Scenarios {
		scenarios = append(scenarios, s.Name)
	}
	sort.Strings(scenarios)
	c.Scenarios = scenarios

	if def.Spec.Behavior.Streaming != nil {
		c.Streaming = &StreamContract{Enabled: def.Spec.Behavior.Streaming.Enabled}
	}
	return c
}

// Severity classifies a single change.
type Severity string

const (
	SeverityBreaking Severity = "breaking"
	SeverityAdditive Severity = "additive"
	SeverityInfo     Severity = "info"
)

// Change is one row in the diff between two contracts.
type Change struct {
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Message  string   `json:"message"`
}

// Diff compares two contracts and returns the list of detected changes.
// The returned slice is empty when the contracts are identical.
func Diff(old, new *Contract) []Change {
	var changes []Change

	if old.Protocol != new.Protocol {
		changes = append(changes, Change{
			Severity: SeverityBreaking,
			Path:     "protocol",
			Message:  fmt.Sprintf("protocol changed %q -> %q", old.Protocol, new.Protocol),
		})
	}
	if old.Model != new.Model {
		// Model name is visible to clients but switching it is not
		// necessarily breaking — flag as info.
		changes = append(changes, Change{
			Severity: SeverityInfo,
			Path:     "model",
			Message:  fmt.Sprintf("model changed %q -> %q", old.Model, new.Model),
		})
	}

	changes = append(changes, diffTools(old.Tools, new.Tools)...)
	changes = append(changes, diffScenarios(old.Scenarios, new.Scenarios)...)
	changes = append(changes, diffStreaming(old.Streaming, new.Streaming)...)

	return changes
}

// HasBreaking reports whether any breaking change is present.
func HasBreaking(changes []Change) bool {
	for _, c := range changes {
		if c.Severity == SeverityBreaking {
			return true
		}
	}
	return false
}

func diffTools(oldTools, newTools []ToolContract) []Change {
	oldByName := indexToolsByName(oldTools)
	newByName := indexToolsByName(newTools)

	var changes []Change
	// Removed or modified tools.
	for name, ot := range oldByName {
		nt, ok := newByName[name]
		if !ok {
			changes = append(changes, Change{
				Severity: SeverityBreaking,
				Path:     "tools." + name,
				Message:  fmt.Sprintf("tool %q removed", name),
			})
			continue
		}
		changes = append(changes, diffTool(name, ot, nt)...)
	}
	// Added tools are always additive.
	for name := range newByName {
		if _, ok := oldByName[name]; !ok {
			changes = append(changes, Change{
				Severity: SeverityAdditive,
				Path:     "tools." + name,
				Message:  fmt.Sprintf("tool %q added", name),
			})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

func diffTool(name string, old, new ToolContract) []Change {
	var changes []Change
	if old.Description != new.Description {
		changes = append(changes, Change{
			Severity: SeverityInfo,
			Path:     "tools." + name + ".description",
			Message:  "description changed",
		})
	}

	// Parameter-schema comparison. We inspect only the pieces that
	// affect consumers: the required array and the property set.
	oldReq := schemaRequired(old.Parameters)
	newReq := schemaRequired(new.Parameters)
	for _, field := range newReq {
		if !contains(oldReq, field) {
			changes = append(changes, Change{
				Severity: SeverityBreaking,
				Path:     "tools." + name + ".required",
				Message:  fmt.Sprintf("parameter %q newly required", field),
			})
		}
	}
	for _, field := range oldReq {
		if !contains(newReq, field) {
			changes = append(changes, Change{
				Severity: SeverityAdditive,
				Path:     "tools." + name + ".required",
				Message:  fmt.Sprintf("parameter %q no longer required", field),
			})
		}
	}

	oldProps := schemaProperties(old.Parameters)
	newProps := schemaProperties(new.Parameters)
	for key := range oldProps {
		if _, ok := newProps[key]; !ok {
			changes = append(changes, Change{
				Severity: SeverityBreaking,
				Path:     "tools." + name + ".properties." + key,
				Message:  fmt.Sprintf("property %q removed", key),
			})
		} else if !reflect.DeepEqual(oldProps[key], newProps[key]) {
			changes = append(changes, Change{
				Severity: SeverityBreaking,
				Path:     "tools." + name + ".properties." + key,
				Message:  fmt.Sprintf("property %q schema changed", key),
			})
		}
	}
	for key := range newProps {
		if _, ok := oldProps[key]; !ok {
			changes = append(changes, Change{
				Severity: SeverityAdditive,
				Path:     "tools." + name + ".properties." + key,
				Message:  fmt.Sprintf("property %q added", key),
			})
		}
	}
	return changes
}

func diffScenarios(old, new []string) []Change {
	oldSet := make(map[string]bool, len(old))
	for _, s := range old {
		oldSet[s] = true
	}
	newSet := make(map[string]bool, len(new))
	for _, s := range new {
		newSet[s] = true
	}
	var changes []Change
	for s := range oldSet {
		if !newSet[s] {
			changes = append(changes, Change{
				Severity: SeverityBreaking,
				Path:     "scenarios." + s,
				Message:  fmt.Sprintf("scenario %q removed", s),
			})
		}
	}
	for s := range newSet {
		if !oldSet[s] {
			changes = append(changes, Change{
				Severity: SeverityAdditive,
				Path:     "scenarios." + s,
				Message:  fmt.Sprintf("scenario %q added", s),
			})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

func diffStreaming(old, new *StreamContract) []Change {
	oldEnabled := old != nil && old.Enabled
	newEnabled := new != nil && new.Enabled
	if oldEnabled && !newEnabled {
		return []Change{{
			Severity: SeverityBreaking,
			Path:     "streaming",
			Message:  "streaming disabled",
		}}
	}
	if !oldEnabled && newEnabled {
		return []Change{{
			Severity: SeverityAdditive,
			Path:     "streaming",
			Message:  "streaming enabled",
		}}
	}
	return nil
}

// --- JSON-Schema helpers (best-effort) ---

func schemaRequired(schema map[string]interface{}) []string {
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func schemaProperties(schema map[string]interface{}) map[string]interface{} {
	raw, ok := schema["properties"]
	if !ok {
		return nil
	}
	props, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	return props
}

func indexToolsByName(tools []ToolContract) map[string]ToolContract {
	out := make(map[string]ToolContract, len(tools))
	for _, t := range tools {
		out[t.Name] = t
	}
	return out
}

// normalizeJSON round-trips an arbitrary value through JSON so every
// nested map becomes map[string]any and every nested array becomes
// []any. This removes ambiguity introduced by YAML decoders that may
// produce map[interface{}]interface{} for deeply nested untyped maps.
func normalizeJSON(v any) map[string]any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
