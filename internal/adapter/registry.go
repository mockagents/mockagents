package adapter

import (
	"net/http"

	"github.com/mockagents/mockagents/internal/engine"
)

// Route is a single HTTP route an adapter mounts: a net/http ServeMux
// pattern ("METHOD /path") paired with its handler.
type Route struct {
	Pattern string
	Handler http.HandlerFunc
}

// Adapter is a protocol surface — OpenAI, Anthropic, or a future
// provider — that mounts a fixed set of wire-compatible routes against
// the engine. The server mounts every adapter the Registry holds
// through one boundary instead of hardwiring each provider's routes, so
// adding a provider is "implement Adapter + register it" with no edits
// to the server's route wiring (REF-05).
type Adapter interface {
	// Name is a short identifier used in logs and diagnostics.
	Name() string
	// Routes returns the routes this adapter serves. The server calls
	// it once at mount time; the patterns use net/http ServeMux syntax.
	Routes() []Route
}

// Registry is the ordered set of protocol adapters the server mounts.
// The zero value is usable; Register appends in mount order.
type Registry struct {
	adapters []Adapter
}

// NewRegistry returns a Registry seeded with the given adapters, in order.
func NewRegistry(adapters ...Adapter) *Registry {
	return &Registry{adapters: append([]Adapter(nil), adapters...)}
}

// Register appends an adapter to the registry.
func (r *Registry) Register(a Adapter) {
	r.adapters = append(r.adapters, a)
}

// Adapters returns the registered adapters in mount order.
func (r *Registry) Adapters() []Adapter {
	return r.adapters
}

// DefaultRegistry returns the built-in protocol adapters (OpenAI +
// Anthropic) bound to eng. This is the single registration point for
// wire protocols: a new provider is added here and implements Adapter —
// the server mounts whatever the registry returns, so no route wiring in
// the server package changes (REF-05).
func DefaultRegistry(eng *engine.Engine) *Registry {
	return NewRegistry(
		&OpenAIHandler{Engine: eng},
		&AnthropicHandler{Engine: eng},
	)
}
