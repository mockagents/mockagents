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
	oai := &OpenAIHandler{Engine: eng}
	emb := &EmbeddingsHandler{}
	// Responses + Conversations share one store: a client creates a conversation
	// via /v1/conversations, then drives a multi-turn loop by passing its id on
	// each /v1/responses call (NF-02; the post-Assistants stateful stack).
	conversations := newConversationStore()
	resp := NewResponsesHandler(eng, conversations)
	anthropic := &AnthropicHandler{Engine: eng}
	// Files + Batch share one in-memory file store: a client uploads a request
	// JSONL via /v1/files, the batch processor reads it and writes its
	// output/error files back through the same store (A-08).
	files := newFileStore()
	batches := NewBatchesHandler(files, map[string]http.HandlerFunc{
		"/v1/chat/completions": oai.HandleChatCompletions,
		"/v1/embeddings":       emb.HandleEmbeddings,
		"/v1/responses":        resp.HandleResponses,
	})
	// Anthropic Message Batches replay each inline request through the live
	// /v1/messages handler (A-08; the inline, file-free sibling of the OpenAI
	// Batch API).
	anthropicBatches := NewAnthropicBatchesHandler(anthropic.HandleMessages)
	return NewRegistry(
		oai,
		resp,
		emb,
		&ModerationsHandler{},
		anthropic,
		&GeminiHandler{Engine: eng},
		// Azure OpenAI URL surface, delegating to the OpenAI handlers above.
		&AzureHandler{Chat: oai, Embeddings: emb},
		// OpenAI Files + Batch API (A-08).
		NewFilesHandler(files),
		batches,
		// Anthropic Message Batches (A-08).
		anthropicBatches,
		// OpenAI Conversations API (NF-02; stateful companion to Responses).
		NewConversationsHandler(conversations),
		// OpenAI Realtime API over WebSocket (NF-01).
		&RealtimeHandler{Engine: eng},
	)
}
