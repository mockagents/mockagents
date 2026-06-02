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
	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/observability"
	pricingpkg "github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
)

const (
	DefaultHost         = "127.0.0.1"
	DefaultPort         = 8080
	DefaultReadTimeout  = 30 * time.Second
	DefaultWriteTimeout = 60 * time.Second
	DefaultIdleTimeout  = 120 * time.Second
	DefaultMaxBodyBytes = 10 * 1024 * 1024 // 10 MB
	ShutdownTimeout     = 5 * time.Second
)

// Config holds HTTP server configuration.
type Config struct {
	Host         string
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
	// AuditStore enables the audit log. When non-nil every
	// control-plane write produces an audit event and the
	// /api/v1/audit read endpoint is mounted.
	AuditStore audit.Store
	// Prices is the per-model cost table used by /api/v1/logs and
	// /api/v1/costs. Nil disables cost annotation (fields are zero).
	Prices *pricingpkg.Table
	// Pipelines is the pipeline definition registry. Non-nil enables
	// the /api/v1/pipelines management endpoints so the GUI can
	// render a DAG viewer. Nil leaves the routes unmounted.
	Pipelines *engine.PipelineRegistry
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Host:         DefaultHost,
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
	httpServer     *http.Server
	engine         *engine.Engine
	handlers       *Handlers
	tenancyH       *TenancyHandlers
	auditH         *AuditHandlers
	recorder       *audit.Recorder
	logWorker      *LogWorker
	logBroadcaster *LogBroadcaster
	logger         *slog.Logger
	config         Config
	listener       net.Listener
}

// New creates a new Server with the given engine and configuration.
func New(eng *engine.Engine, cfg Config, logger *slog.Logger) *Server {
	// The recorder is always constructed so handlers can call it
	// unconditionally. A nil store makes it a no-op.
	recorder := audit.NewRecorder(cfg.AuditStore, principalToActor)

	// Route every 401/403 at the control plane into the audit log.
	// The hook is a package-level variable on purpose: it lets the
	// tenancy middleware stay oblivious of the audit package (no
	// import cycle) while keeping existing signatures untouched.
	tenancy.SetDenialHook(func(r *http.Request, status int, reason string) {
		recorder.RecordHTTP(r, audit.EventAuthDenied,
			r.Method+" "+r.URL.Path,
			audit.MarshalDetails(map[string]any{
				"status_code": status,
				"reason":      reason,
			}))
	})

	handlers := &Handlers{
		Engine:    eng,
		AgentsDir: cfg.AgentsDir,
		StartTime: time.Now(),
		Version:   cfg.Version,
		Logger:    logger,
		Recorder:  recorder,
	}

	mux := http.NewServeMux()
	s := &Server{
		engine:   eng,
		handlers: handlers,
		recorder: recorder,
		logger:   logger,
		config:   cfg,
	}
	if cfg.TenancyStore != nil {
		s.tenancyH = &TenancyHandlers{Store: cfg.TenancyStore, Recorder: recorder}
	}
	if cfg.AuditStore != nil {
		s.auditH = &AuditHandlers{Store: cfg.AuditStore}
	}
	s.registerRoutes(mux)

	// Construct the async log worker once so the middleware and
	// Shutdown share the same instance. A nil log store leaves the
	// worker as nil and InteractionCapture short-circuits.
	//
	// The log broadcaster fans every successfully-written row out to
	// SSE subscribers (GET /api/v1/logs/stream). It is always
	// constructed when logging is enabled — slow subscribers drop
	// events rather than block the writer, so the overhead on the
	// hot path is a single mutex-held map iteration.
	if cfg.LogStore != nil {
		s.logBroadcaster = &LogBroadcaster{}
		s.logWorker = NewLogWorker(cfg.LogStore, logger, LogWorkerConfig{
			Broadcaster: s.logBroadcaster,
		})
	}

	// Build middleware chain: outermost first.
	var handler http.Handler = mux
	handler = ExtractAPIKey(handler)
	if s.logWorker != nil {
		handler = InteractionCapture(s.logWorker)(handler)
	}
	handler = WithPrincipalTenantScope(handler)
	// Tenancy auth gates every /api/v1/* route when multi-tenant mode
	// is enabled. Health, the OpenAI/Anthropic LLM endpoints, and
	// /v1/models are left open so load balancers and existing SDKs
	// keep working without credentials; when those open routes carry
	// a valid API key, the middleware attaches the principal so model
	// listing and LLM resolution can be scoped to that tenant.
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
		Addr:         net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
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
	// ReloadAgent re-reads an agent's YAML from disk and replaces it in the
	// registry — a write. Gate it like one: Editor role in multi-tenant
	// mode (F-HD-001). Single-tenant mode runs without auth (local dev
	// tool), so it stays open there, consistent with the audit route.
	if s.tenancyH != nil {
		mux.Handle("POST /api/v1/agents/{name}/reload",
			tenancy.RequireRole(tenancy.RoleEditor, http.HandlerFunc(s.handlers.ReloadAgent)))
	} else {
		mux.HandleFunc("POST /api/v1/agents/{name}/reload", s.handlers.ReloadAgent)
	}

	// Tenancy CRUD — only mounted when multi-tenant mode is enabled.
	// All routes below require the admin role at the middleware level.
	if s.tenancyH != nil {
		mux.Handle("GET /api/v1/tenants", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.ListTenants)))
		mux.Handle("POST /api/v1/tenants", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.CreateTenant)))
		mux.Handle("DELETE /api/v1/tenants/{id}", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.DeleteTenant)))
		mux.Handle("GET /api/v1/tenants/{id}/keys", tenancy.RequireRole(tenancy.RoleEditor, http.HandlerFunc(s.tenancyH.ListAPIKeys)))
		mux.Handle("POST /api/v1/tenants/{id}/keys", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.CreateAPIKey)))
		// Bulk rotation: emergency response to a tenant-wide
		// suspected compromise. Rotates every key in the tenant
		// inside one transaction so operators never end up with a
		// mix of rotated + unrotated credentials. Admin only.
		mux.Handle("POST /api/v1/tenants/{id}/keys/rotate", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.BulkRotateTenantKeys)))
		mux.Handle("PATCH /api/v1/keys/{id}", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.UpdateAPIKeyRole)))
		mux.Handle("POST /api/v1/keys/{id}/rotate", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.RotateAPIKey)))
		// Self-rotation: any authenticated principal can rotate
		// its own secret. Viewer role is sufficient because the
		// handler reads the caller's key id from the request
		// context — there's no path parameter to abuse.
		mux.Handle("POST /api/v1/keys/me/rotate", tenancy.RequireRole(tenancy.RoleViewer, http.HandlerFunc(s.tenancyH.RotateMyAPIKey)))
		// Rotate-then-burn: rotates the caller's key without
		// returning the new plaintext. Operators who suspect a
		// compromised browser session use this to kill their
		// own credential without the fresh secret ever touching
		// the compromised machine. Viewer role is fine — same
		// argument as /me/rotate, the handler only ever touches
		// its own key.
		mux.Handle("POST /api/v1/keys/me/burn", tenancy.RequireRole(tenancy.RoleViewer, http.HandlerFunc(s.tenancyH.BurnMyAPIKey)))
		mux.Handle("DELETE /api/v1/keys/{id}", tenancy.RequireRole(tenancy.RoleAdmin, http.HandlerFunc(s.tenancyH.DeleteAPIKey)))
	}

	// Audit log read API. Open in single-tenant mode (it's a local
	// dev tool); admin-only when multi-tenant mode is on so the
	// who-did-what surface stays private to operators.
	if s.auditH != nil {
		listFn := http.HandlerFunc(s.auditH.ListEvents)
		if s.tenancyH != nil {
			mux.Handle("GET /api/v1/audit", tenancy.RequireRole(tenancy.RoleAdmin, listFn))
		} else {
			mux.Handle("GET /api/v1/audit", listFn)
		}
	}

	// OpenAI-compatible endpoints.
	openai := &adapter.OpenAIHandler{Engine: s.engine}
	mux.HandleFunc("POST /v1/chat/completions", openai.HandleChatCompletions)
	mux.HandleFunc("GET /v1/models", openai.HandleModels)

	// Anthropic-compatible endpoint.
	anthropic := &adapter.AnthropicHandler{Engine: s.engine}
	mux.HandleFunc("POST /v1/messages", anthropic.HandleMessages)

	// Log query API. Prices is threaded in so rows returned by
	// ListLogs carry a computed cost_usd field when a pricing table
	// is configured.
	logHandlers := &LogHandlers{
		Store:       s.config.LogStore,
		Prices:      s.config.Prices,
		Broadcaster: s.logBroadcaster,
	}
	mux.HandleFunc("GET /api/v1/logs", logHandlers.ListLogs)
	mux.HandleFunc("GET /api/v1/logs/{id}", logHandlers.GetLog)
	mux.HandleFunc("DELETE /api/v1/logs", logHandlers.DeleteLogs)
	// Live feed via SSE. Only mounted when the broadcaster was
	// constructed (i.e. when a log store is configured). Nothing
	// subscribes to the /api/v1/logs/stream endpoint in single-
	// tenant single-process mode until the GUI's live toggle is on.
	//
	// The /metrics sibling endpoint exposes an aggregate snapshot
	// of every currently-connected subscriber's drop count +
	// buffer utilization. Admin-gated in multi-tenant so viewers
	// can't fingerprint the operator's browser tabs.
	if s.logBroadcaster != nil {
		mux.HandleFunc("GET /api/v1/logs/stream", logHandlers.StreamLogs)
		streamMetrics := http.HandlerFunc(logHandlers.StreamMetrics)
		if s.tenancyH != nil {
			mux.Handle("GET /api/v1/logs/stream/metrics",
				tenancy.RequireRole(tenancy.RoleAdmin, streamMetrics))
		} else {
			mux.Handle("GET /api/v1/logs/stream/metrics", streamMetrics)
		}
	}

	// Cost aggregate endpoint. Silent no-op when the log store is
	// absent — handler returns 503 in that case, matching the
	// existing /api/v1/logs behavior.
	if s.config.LogStore != nil {
		costsH := &CostsHandlers{Store: s.config.LogStore, Prices: s.config.Prices}
		mux.HandleFunc("GET /api/v1/costs", costsH.ListCosts)
	}

	// Pipeline management API. Read-only list + detail used by the
	// GUI's /pipelines DAG viewer. The handler returns an empty
	// list when no registry is wired up, so single-tenant
	// deployments that never loaded a Pipeline YAML still get a
	// well-formed response.
	if s.config.Pipelines != nil {
		pipelineH := &PipelineHandlers{Registry: s.config.Pipelines}
		mux.HandleFunc("GET /api/v1/pipelines", pipelineH.ListPipelines)
		mux.HandleFunc("GET /api/v1/pipelines/{name}", pipelineH.GetPipeline)
	}

	// Agent config validation endpoint. Open in single-tenant mode
	// (matches /api/v1/logs); gated behind the editor role in
	// multi-tenant mode so viewers don't get a free surface for
	// spraying YAML at the parser.
	validateH := NewValidateHandler()
	if s.tenancyH != nil {
		mux.Handle("POST /api/v1/config/validate", tenancy.RequireRole(tenancy.RoleEditor, validateH))
	} else {
		mux.Handle("POST /api/v1/config/validate", validateH)
	}

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

	resp, err := s.engine.ProcessRequestContext(r.Context(), &req)
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
	tenantID := engine.TenantIDFromContext(r.Context())
	agent := s.engine.Registry.GetForTenant(resp.AgentName, tenantID)
	if agent == nil {
		agent = s.engine.Registry.GetByModelForTenant(resp.Model, tenantID)
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
		"addr", fmt.Sprintf("http://%s", s.listener.Addr().String()),
		"host", s.config.Host,
		"port", addr.Port,
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
// for in-flight requests to complete, then drains any pending
// interaction-log writes so operators do not lose the last seconds of
// traffic on a clean exit.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	s.logger.Info("shutting down server", "timeout", ShutdownTimeout)
	err := s.httpServer.Shutdown(ctx)
	// Drain the async log worker after the HTTP server has stopped
	// accepting new requests. Order matters: Submit paths must be
	// closed first so we know the queue only contains already-enqueued
	// entries.
	if s.logWorker != nil {
		s.logWorker.Shutdown(DefaultLogDrainTimeout)
		m := s.logWorker.Metrics()
		s.logger.Info("log worker drained",
			"submitted", m.Submitted,
			"written", m.Written,
			"dropped", m.Dropped,
			"failed", m.Failed,
		)
	}
	// Close the SSE broadcaster after the worker has stopped (no more
	// Publish can arrive) so any remaining /logs/stream subscribers' handler
	// goroutines exit instead of leaking past shutdown (F-SV-001).
	if s.logBroadcaster != nil {
		s.logBroadcaster.Close()
	}
	return err
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

// principalToActor extracts an audit.Actor from the authenticated
// principal on the request context. Returns an anonymous actor when
// the request is unauthenticated (single-tenant mode).
func principalToActor(r *http.Request) audit.Actor {
	p := tenancy.PrincipalFrom(r.Context())
	if p == nil {
		return audit.Actor{Name: "anonymous"}
	}
	return audit.Actor{
		Name:     p.KeyID, // identified by key id; plaintext never logged
		TenantID: p.TenantID,
		KeyID:    p.KeyID,
		Role:     string(p.Role),
	}
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
