// Package tools holds the playground's executable tools. Real provider APIs
// (and mockagents, faithfully) only *request* tool calls. Executing them is
// the client's job, so these tools run inside the playground. That includes
// argument validation, approval gating for side effects, and idempotency.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/llm"
)

// Tool is one executable capability an agent can request.
type Tool interface {
	Spec() llm.ToolSpec
	// RequiresApproval marks a side-effecting tool: the agent runtime asks a
	// human before executing it.
	RequiresApproval() bool
	Execute(ctx context.Context, args map[string]any) (any, error)
}

// Info is the API/CLI view of a tool.
type Info struct {
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	Parameters       map[string]any `json:"parameters"`
	RequiresApproval bool           `json:"requires_approval"`
	SideEffects      bool           `json:"side_effects"`
}

// Registry is a name -> Tool map.
type Registry struct{ tools map[string]Tool }

// NewRegistry returns the default tool set with fresh state.
func NewRegistry() *Registry {
	r := &Registry{tools: map[string]Tool{}}
	orders := newOrderBook()
	for _, t := range []Tool{
		&searchKB{},
		&calculator{},
		&lookupOrder{orders: orders},
		&issueRefund{orders: orders},
	} {
		r.tools[t.Spec().Name] = t
	}
	return r
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns the declarations for the named tools (unknown names skipped).
func (r *Registry) Specs(names []string) []llm.ToolSpec {
	var out []llm.ToolSpec
	for _, n := range names {
		if t, ok := r.tools[n]; ok {
			out = append(out, t.Spec())
		}
	}
	return out
}

// List returns every tool, sorted by name.
func (r *Registry) List() []Info {
	out := make([]Info, 0, len(r.tools))
	for _, t := range r.tools {
		s := t.Spec()
		out = append(out, Info{Name: s.Name, Description: s.Description, Parameters: s.Parameters,
			RequiresApproval: t.RequiresApproval(), SideEffects: t.RequiresApproval()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns every tool name.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// ArgumentError is a model-produced argument payload that fails validation.
// The runtime returns it to the model as an error tool result, so the model
// can correct itself, instead of executing anything.
type ArgumentError struct {
	Tool   string
	Detail string
}

func (e *ArgumentError) Error() string {
	return fmt.Sprintf("invalid arguments for %s: %s", e.Tool, e.Detail)
}

// ParseArgs decodes and validates raw tool-call arguments against the tool's
// JSON schema subset (object, required, primitive property types).
func ParseArgs(t Tool, raw string) (map[string]any, error) {
	spec := t.Spec()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, &ArgumentError{Tool: spec.Name, Detail: "arguments are not a valid JSON object: " + err.Error()}
	}
	if args == nil {
		return nil, &ArgumentError{Tool: spec.Name, Detail: "arguments must be a JSON object"}
	}
	props, _ := spec.Parameters["properties"].(map[string]any)
	if req, ok := spec.Parameters["required"].([]string); ok {
		for _, name := range req {
			if _, present := args[name]; !present {
				return nil, &ArgumentError{Tool: spec.Name, Detail: fmt.Sprintf("missing required argument %q", name)}
			}
		}
	}
	for name, v := range args {
		p, ok := props[name].(map[string]any)
		if !ok {
			continue // extra arguments are tolerated, like most providers do
		}
		want, _ := p["type"].(string)
		if !typeMatches(want, v) {
			return nil, &ArgumentError{Tool: spec.Name, Detail: fmt.Sprintf("argument %q must be of type %s", name, want)}
		}
	}
	return args, nil
}

func typeMatches(want string, v any) bool {
	switch want {
	case "", "any":
		return true
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return true
}

func schema(required []string, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

// ErrNotFound is returned for unknown records.
var ErrNotFound = errors.New("not found")
