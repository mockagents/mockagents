package mockagents

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
)

// InProcessOptions configures NewInProcessClient.
type InProcessOptions struct {
	// AgentsDir is the directory to load YAML agent definitions from.
	// Required.
	AgentsDir string
	// Logger, when nil, defaults to a discarding slog handler. Override
	// to surface engine logs in test output.
	Logger *slog.Logger
	// SessionTTL overrides the in-memory session store TTL. Zero uses
	// the engine default.
	SessionTTL time.Duration
}

// InProcessClient is a Client whose requests hit an in-memory
// mockagents engine served via httptest.Server. No subprocess is
// spawned and no free port is negotiated, so downstream Go users can
// spin it up thousands of times in a single test run with sub-
// millisecond startup.
//
// The embedded Client is the ordinary HTTP client, but the in-process
// server mounts only a subset of the real routes:
//
//   - POST /v1/chat/completions and GET /v1/models (OpenAI)
//   - POST /v1/messages (Anthropic)
//   - GET /api/v1/health
//
// So Chat, Message, ChatStream, MessageStream, IterStream and Health work
// unchanged, while the management API (ListAgents, GetAgent, ReloadAgent,
// RotateMyAPIKey), the Gemini and Responses endpoints, MCP, auth/tenancy
// and the server-wide chaos policy are NOT available: those calls return a
// 404 *HTTPError. Use NewServer (the real binary) when a test needs them.
// Close releases the underlying resources.
type InProcessClient struct {
	*Client
	server *httptest.Server
}

// NewInProcessClient loads the agents under opts.AgentsDir, builds an
// engine + registry in the current process, and wires them into an
// httptest.Server mounting the OpenAI + Anthropic adapter handlers.
// The returned client is ready for Chat / Message / IterStream calls.
//
// Failures during agent loading are returned as errors. Individual
// parse failures are tolerated (matches the CLI's start behavior) as
// long as at least one agent loaded cleanly.
func NewInProcessClient(opts InProcessOptions) (*InProcessClient, error) {
	if opts.AgentsDir == "" {
		return nil, fmt.Errorf("mockagents: InProcessOptions.AgentsDir is required")
	}
	if _, err := os.Stat(opts.AgentsDir); err != nil {
		return nil, fmt.Errorf("mockagents: agents dir %q: %w", opts.AgentsDir, err)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	registry := engine.NewAgentRegistry()
	results, errs := config.LoadDir(opts.AgentsDir)
	validator := &config.Validator{}
	var loaded int
	for _, r := range results {
		if r == nil || r.Definition == nil {
			continue
		}
		// Same order as `mockagents start`: fill defaults first (this is
		// where a chaos.preset expands into concrete faults and streaming
		// gets its chunk defaults), then validate the effective definition.
		// Skipping either made in-process agents behave differently from
		// the server, e.g. a `preset: rate-limited` agent never returned 429.
		config.ApplyDefaults(r.Definition)
		if verr := validator.Validate(r.Definition, r.FilePath, r.Node); verr != nil {
			logger.Warn("mockagents: skipping invalid agent", "file", r.FilePath, "errors", verr.Error())
			errs = append(errs, verr)
			continue
		}
		registry.Register(r.Definition)
		loaded++
	}
	if loaded == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("mockagents: no valid agents in %q (first error: %w)", opts.AgentsDir, errs[0])
		}
		return nil, fmt.Errorf("mockagents: no agent definitions found in %q", opts.AgentsDir)
	}

	ttl := opts.SessionTTL
	if ttl == 0 {
		ttl = state.DefaultSessionTTL
	}
	store := state.NewMemoryStore(ttl)
	eng := engine.NewEngine(registry, store, logger)

	mux := http.NewServeMux()
	oa := &adapter.OpenAIHandler{Engine: eng}
	an := &adapter.AnthropicHandler{Engine: eng}
	mux.HandleFunc("POST /v1/chat/completions", oa.HandleChatCompletions)
	mux.HandleFunc("GET /v1/models", oa.HandleModels)
	mux.HandleFunc("POST /v1/messages", an.HandleMessages)
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","mode":"in-process"}`))
	})

	srv := httptest.NewServer(mux)
	return &InProcessClient{
		Client: NewClient(ClientOptions{BaseURL: srv.URL}),
		server: srv,
	}, nil
}

// Close tears down the httptest.Server.
func (c *InProcessClient) Close() {
	if c == nil || c.server == nil {
		return
	}
	c.server.Close()
	c.server = nil
}

// BaseURL returns the URL the in-process server is listening on so
// callers can point a third-party SDK (e.g. openai-go) at the same
// mock without touching the embedded Client.
func (c *InProcessClient) BaseURL() string {
	if c == nil || c.server == nil {
		return ""
	}
	return c.server.URL
}
