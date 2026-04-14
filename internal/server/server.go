package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/observability"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
)

const (
	DefaultPort         = 8080
	DefaultReadTimeout  = 30 * time.Second
	DefaultWriteTimeout = 60 * time.Second
	DefaultIdleTimeout  = 120 * time.Second
	DefaultMaxBodyBytes = 10 * 1024 * 1024 // 10 MB
	ShutdownTimeout     = 5 * time.Second
)

// Config holds HTTP server configuration.
type Config struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	MaxBodyBytes int64
	AgentsDir    string
	Version      string
	LogStore     *storage.SQLiteStore // Optional interaction log store.
	// TenancyStore enables multi-tenant mode when non-nil. Every
	// /api/v1/* request then requires a valid API key and the routes
	// /api/v1/tenants and /api/v1/keys are mounted for admin CRUD.
	TenancyStore tenancy.Store
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Port:         DefaultPort,
		ReadTimeout:  DefaultReadTimeout,
		WriteTimeout: DefaultWriteTimeout,
		IdleTimeout:  DefaultIdleTimeout,
		MaxBodyBytes: DefaultMaxBodyBytes,
		Version:      "dev",
	}
}

// Server wraps http.Server with the MockAgents router and lifecycle management.
type Server struct {
	httpServer  *http.Server
	engine      *engine.Engine
	handlers    *Handlers
	tenancyH    *TenancyHandlers
	logger      *slog.Logger
	config      Config
	listener    net.Listener
}

// New creates a new Server with the given engine and configuration.
func New(eng *engine.Engine, cfg Config, logger *slog.Logger) *Server {
	handlers := &Handlers{
		Engine:    eng,
		AgentsDir: cfg.AgentsDir,
		StartTime: time.Now(),
		Version:   cfg.Version,
		Logger:    logger,
	}

	mux := http.NewServeMux()
	s := &Server{
		engine:   eng,
		handlers: handlers,
		logger:   logger,
		config:   cfg,
	}
	if cfg.TenancyStore != nil {
		s.tenancyH = &TenancyHandlers{Store: cfg.TenancyStore}
	}
	s.registerRoutes(mux)

	// Build middleware chain: outermost first.
	var handler http.Handler = mux
	handler = ExtractAPIKey(handler)
	if cfg.LogStore != nil {
		handler = InteractionCapture(cfg.LogStore)(handler)
	}
	// Tenancy auth gates every /api/v1/* route when multi-tenant mode
	// is enabled. Health, the OpenAI/Anthropic LLM endpoints, and
	// /v1/models are left open so load balancers and existing SDKs
	// keep working without credentials.
	if cfg.TenancyStore != nil {
		handler = tenancy.AuthMiddleware(cfg.TenancyStore, skipAuth)(handler)
	}
	handler = MaxBodySize(cfg.MaxBodyBytes)(handler)
	handler = CORS(handler)
	handler = StructuredLogger(logger)(handler)
	handler = Recovery(logger)(handler)
	handler = RequestID(handler)
	handler = observability.HTTPMiddleware(handler)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	return s
}

// registerRoutes mounts the management API, protocol adapters, and engine endpoints.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Management API under /api/v1/
	mux.HandleFunc("GET /api/v1/health", s.handlers.HealthCheck)
	mux.HandleFunc("GET /api/v1/agents", s.handlers.ListAgents)
	mux.HandleFunc("GET /api/v1/agents/{name}", s.handlers.GetAgent)
	mux.HandleFunc("POST /api/v1/agents/{name}/reload", s.handlers.ReloadAgent)

	// Tenancy CRUD — only mounted when multi-tenant mode is enabled.
	// All routes below require the admin role at the middleware level.
	if s.tenancyH != nil {
		mux.Handle("GET /api/v1/tenants", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.ListTenants)))
		mux.Handle("POST /api/v1/tenants", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.CreateTenant)))
		mux.Handle("DELETE /api/v1/tenants/{id}", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.DeleteTenant)))
		mux.Handle("GET /api/v1/tenants/{id}/keys", tenancy.RequireRole(tenancy.RoleEditor, http.HandlerFunc(s.tenancyH.ListAPIKeys)))
		mux.Handle("POST /api/v1/tenants/{id}/keys", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.CreateAPIKey)))
		mux.Handle("DELETE /api/v1/keys/{id}", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.DeleteAPIKey)))
	}

	// OpenAI-compatible endpoints.
	openai := &adapter.OpenAIHandler{Engine: s.engine}
	mux.HandleFunc("POST /v1/chat/completions", openai.HandleChatCompletions)
	mux.HandleFunc("GET /v1/models", openai.HandleModels)

	// Anthropic-compatible endpoint.
	anthropic := &adapter.AnthropicHandler{Engine: s.engine}
	mux.HandleFunc("POST /v1/messages", anthropic.HandleMessages)

	// Log query API.
	logHandlers := &LogHandlers{Store: s.config.LogStore}
	mux.HandleFunc("GET /api/v1/logs", logHandlers.ListLogs)
	mux.HandleFunc("GET /api/v1/logs/{id}", logHandlers.GetLog)
	mux.HandleFunc("DELETE /api/v1/logs", logHandlers.DeleteLogs)

	// Generic engine endpoint (internal/testing).
	mux.HandleFunc("POST /v1/engines/process", s.handleProcessRequest)
}

// handleProcessRequest is a generic engine endpoint for testing.
func (s *Server) handleProcessRequest(w http.ResponseWriter, r *http.Request) {
	var req engine.InboundRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	resp, err := s.engine.ProcessRequest(&req)
	if err != nil {
		status := http.StatusInternalServerError
		if err == engine.ErrAgentNotFound || isNotFound(err) {
			status = http.StatusNotFound
		} else if err == engine.ErrEmptyMessage {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	// Stream if requested.
	if req.Stream {
		s.handleStreamResponse(w, r, &req, resp)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleStreamResponse writes the response as an SSE stream in the
// protocol-appropriate format.
func (s *Server) handleStreamResponse(
	w http.ResponseWriter,
	r *http.Request,
	req *engine.InboundRequest,
	resp *engine.Response,
) {
	// Resolve streaming config from the agent definition.
	agent := s.engine.Registry.Get(resp.AgentName)
	if agent == nil {
		agent = s.engine.Registry.GetByModel(resp.Model)
	}

	var streamCfg *types.StreamingConfig
	if agent != nil {
		streamCfg = agent.Spec.Behavior.Streaming
	}

	// Determine protocol from agent spec.
	protocol := "openai"
	if agent != nil && strings.Contains(agent.Spec.Protocol, "anthropic") {
		protocol = "anthropic"
	}

	var streamErr error
	switch protocol {
	case "anthropic":
		streamErr = streaming.StreamAnthropic(r.Context(), w, resp, streamCfg)
	default:
		streamErr = streaming.StreamOpenAI(r.Context(), w, resp, streamCfg)
	}

	if streamErr != nil {
		s.logger.Error("streaming error",
			"agent", resp.AgentName,
			"error", streamErr,
		)
	}
}

// Listen binds the server socket synchronously. Call it from the main
// goroutine before spawning a goroutine for Serve so tests can safely
// observe ListenAddr without racing against the serve goroutine's
// listener initialization. Calling Listen twice returns an error.
func (s *Server) Listen() error {
	if s.listener != nil {
		return fmt.Errorf("server already listening on %s", s.listener.Addr())
	}
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.httpServer.Addr, err)
	}
	s.listener = ln
	return nil
}

// Serve runs the HTTP server on the already-bound listener. Blocks
// until the server is shut down. Must be preceded by a successful
// Listen call.
func (s *Server) Serve() error {
	if s.listener == nil {
		return fmt.Errorf("server not listening; call Listen first")
	}
	addr := s.listener.Addr().(*net.TCPAddr)
	s.logger.Info("MockAgents server started",
		"addr", fmt.Sprintf("http://localhost:%d", addr.Port),
		"agents", s.engine.Registry.Count(),
	)
	if err := s.httpServer.Serve(s.listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// ListenAndServe is a convenience that binds and serves in a single
// call. Note: callers that want to discover the actual listen address
// (for example tests using port 0) must use Listen + go Serve instead,
// because with the combined form the listener is written from the
// serve goroutine and ListenAddr racy.
func (s *Server) ListenAndServe() error {
	if err := s.Listen(); err != nil {
		return err
	}
	return s.Serve()
}

// ListenAddr returns the actual address the server is listening on.
// Only valid after Listen (or ListenAndServe) has been called.
func (s *Server) ListenAddr() string {
	if s.listener == nil {
		return s.httpServer.Addr
	}
	return s.listener.Addr().String()
}

// Shutdown gracefully shuts down the server, waiting up to ShutdownTimeout
// for in-flight requests to complete.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	s.logger.Info("shutting down server", "timeout", ShutdownTimeout)
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the server's listen address. Useful after ListenAndServe
// when port 0 is used for testing.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is empty")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func isNotFound(err error) bool {
	return errors.Is(err, engine.ErrAgentNotFound) ||
		strings.Contains(err.Error(), engine.ErrAgentNotFound.Error())
}

// skipAuth lists paths that remain unauthenticated when multi-tenant
// mode is enabled. Health probes need to work without credentials so
// load balancers don't start failing closed; the LLM endpoints are
// open by design because clients send their own provider API keys
// that MockAgents deliberately ignores.
func skipAuth(r *http.Request) bool {
	path := r.URL.Path
	if path == "/api/v1/health" {
		return true
	}
	if strings.HasPrefix(path, "/v1/chat/completions") ||
		strings.HasPrefix(path, "/v1/messages") ||
		strings.HasPrefix(path, "/v1/models") ||
		strings.HasPrefix(path, "/v1/engines/") {
		return true
	}
	return false
}
