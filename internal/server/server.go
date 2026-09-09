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
	"sync/atomic"
	"time"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/audit"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/metrics"
	"github.com/mockagents/mockagents/internal/observability"
	pricingpkg "github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/streaming"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/mockagents/mockagents/internal/vector"
)

const (
	DefaultHost         = "127.0.0.1"
	DefaultPort         = 8080
	DefaultReadTimeout  = 30 * time.Second
	DefaultWriteTimeout = 60 * time.Second
	DefaultIdleTimeout  = 120 * time.Second
	// DefaultReadHeaderTimeout bounds the request-header read on its own, so a
	// slow-loris client dribbling headers can't tie up a connection for the full
	// ReadTimeout window (PERF-21, slow-loris hardening).
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultMaxBodyBytes      = 10 * 1024 * 1024 // 10 MB
	// DefaultShutdownTimeout is how long Shutdown waits for in-flight requests
	// (paced streams, pipeline runs) before cancelling them. Kubernetes' default
	// termination grace is 30s and the chart's preStop sleep takes 5s of it, so
	// 20s fits inside; the former 5s cut long streams on every rollout.
	DefaultShutdownTimeout = 20 * time.Second
	// ShutdownTimeout is the pre-Config name for the default; Config.ShutdownTimeout
	// overrides it per server.
	ShutdownTimeout = DefaultShutdownTimeout
)

// ErrShutdownDeadline reports that Shutdown had to cancel in-flight requests
// because they did not finish within the timeout. It is a warning, not a
// failure: the listeners closed, the streams were cancelled, and the stores
// were drained — callers should log it, not exit non-zero (audit M-34).
var ErrShutdownDeadline = errors.New("server: shutdown deadline reached; in-flight requests were cancelled")

// Config holds HTTP server configuration.
type Config struct {
	Host              string
	Port              int
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	MaxBodyBytes      int64
	// CORSAllowedOrigins restricts Access-Control-Allow-Origin. Empty (the
	// default) or a list containing "*" keeps the permissive wildcard; an
	// explicit list locks CORS down to those origins (F-MW-001).
	CORSAllowedOrigins []string
	AgentsDir          string
	Version            string
	LogStore           *storage.SQLiteStore // Optional interaction log store.
	// LogBodyMode controls how much of a captured response body is persisted
	// (full | sanitized | none) for privacy-sensitive deployments (SEC-05).
	// Empty/unknown normalizes to "full" (the historical behavior).
	LogBodyMode LogBodyMode
	// LogMaxRows bounds the interaction-log table: a background pruner keeps
	// only the newest LogMaxRows rows. 0 (default) means unlimited / no pruning.
	LogMaxRows int
	// TenancyStore enables multi-tenant mode when non-nil. Every
	// /api/v1/* request then requires a valid API key and the routes
	// /api/v1/tenants and /api/v1/keys are mounted for admin CRUD.
	TenancyStore tenancy.Store
	// AuditStore enables the audit log. When non-nil every
	// control-plane write produces an audit event and the
	// /api/v1/audit read endpoint is mounted.
	AuditStore audit.Store
	// AuditMaxRows bounds the audit table: a background pruner keeps only the
	// newest AuditMaxRows rows. 0 (default) means unlimited (audit M-08).
	AuditMaxRows int
	// ShutdownTimeout bounds how long Shutdown waits for in-flight requests
	// before cancelling them. 0 means DefaultShutdownTimeout.
	ShutdownTimeout time.Duration
	// ShutdownDrainDelay is how long Shutdown keeps serving — with readiness
	// reporting "draining" — before closing the listeners, so a load balancer
	// that polls readiness can stop routing new connections first. 0 (default)
	// closes immediately; the Helm chart provides this window with a preStop
	// sleep instead.
	ShutdownDrainDelay time.Duration
	// EnableEngineEndpoint mounts POST /v1/engines/process, the generic
	// engine endpoint used by conformance tests and the SDK harness. It lets
	// the caller name any global agent directly, is exempt from auth like the
	// provider surfaces, and is not quota-metered — so it stays off unless a
	// deployment opts in (audit M-07).
	EnableEngineEndpoint bool
	// Prices is the per-model cost table used by /api/v1/logs and
	// /api/v1/costs. Nil disables cost annotation (fields are zero).
	Prices *pricingpkg.Table
	// Pipelines is the pipeline definition registry. Non-nil enables
	// the /api/v1/pipelines management endpoints so the GUI can
	// render a DAG viewer. Nil leaves the routes unmounted.
	Pipelines *engine.PipelineRegistry
	// QuotaEnforcer enforces per-tenant rate + monthly-spend caps on the LLM
	// endpoints and powers the /api/v1/quota routes (REF-08 slice C). Nil
	// disables quota enforcement (the single-tenant default).
	QuotaEnforcer *quota.Enforcer
	// SSO, when non-nil, mounts the OIDC relying-party endpoints
	// (/auth/login, /auth/callback, /auth/logout) for SSO login (REF-08
	// slice D). Configured only when the OIDC env vars are set.
	SSO *SSOHandlers
	// Metrics is the Prometheus registry that GET /metrics renders and that
	// MetricsCapture records into. Nil uses metrics.Default(), which is also
	// what the engine records scenario matches and chaos injections into —
	// so overriding this only isolates the HTTP-level families, and is
	// intended for tests (FR-J02).
	Metrics *metrics.Registry
	// VectorStore holds VectorMock collections. Nil creates an empty store.
	VectorStore *vector.Store
	// SearchService supplies declarative Tavily fixtures and service faults.
	SearchService *types.SearchServiceDefinition
	// ServiceFaults applies common deterministic controls by adapter protocol.
	ServiceFaults map[string]types.SearchFaults
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Host:              DefaultHost,
		Port:              DefaultPort,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		MaxBodyBytes:      DefaultMaxBodyBytes,
		Version:           "dev",
	}
}

// Server wraps http.Server with the MockAgents router and lifecycle management.
type Server struct {
	httpServer *http.Server
	engine     *engine.Engine
	handlers   *Handlers
	tenancyH   *TenancyHandlers
	auditH     *AuditHandlers
	// mountedRoutes records every management route this process actually
	// serves, with its role floor, as mountManaged registers it. UX-01's
	// capability response is derived from this rather than from the static
	// policy table, because conditional routes (audit, costs) are absent when
	// their store is not configured. Populated at startup only.
	mountedRoutes map[string]tenancy.Role
	// open is the set of routes that stay unauthenticated in multi-tenant
	// mode. It is derived from what registerRoutes actually mounts (every
	// adapter route plus builtinOpenRoutes) rather than hand-listed, so a
	// provider surface can never be left out and fail closed (audit H-03).
	open           *openRoutes
	recorder       *audit.Recorder
	logWorker      *LogWorker
	logBroadcaster *LogBroadcaster
	logPruner      *logPruner
	auditWriter    *audit.AsyncWriter
	auditPruner    *logPruner
	logger         *slog.Logger
	config         Config
	listener       net.Listener
	// realtime is the WebSocket adapter, kept so Shutdown can close its
	// hijacked connections (http.Server.Shutdown does not know about them).
	realtime *adapter.RealtimeHandler
	// baseCtx is the parent of every request context; Shutdown cancels it
	// once the timeout passes so streaming handlers stop instead of pinning
	// the process (audit M-34).
	baseCtx    context.Context
	cancelBase context.CancelFunc
	// draining flips readiness to 503 for the whole shutdown sequence.
	draining atomic.Bool
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
	//
	// Denials go through a bounded async writer (audit M-08): they are the
	// one audit event an unauthenticated client can generate at will, and a
	// synchronous SQLite INSERT per 401 let a credential-stuffing burst
	// serialize on the store's write lock from the request goroutine. The
	// hook contract in tenancy says it must not block; now it doesn't.
	auditWriter := audit.NewAsyncWriter(cfg.AuditStore, audit.DefaultAsyncQueueSize, logger)
	tenancy.SetDenialHook(func(r *http.Request, status int, reason string) {
		if auditWriter == nil {
			return
		}
		auditWriter.Submit(recorder.EventFromHTTP(r, audit.EventAuthDenied,
			r.Method+" "+r.URL.Path,
			audit.MarshalDetails(map[string]any{
				"status_code": status,
				"reason":      reason,
			})))
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
		open:     newOpenRoutes(),
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

	// Construct the async log worker + broadcaster BEFORE registerRoutes so
	// the SSE feed wiring is in place when routes are mounted. registerRoutes
	// reads s.logBroadcaster to mount GET /api/v1/logs/stream[/metrics] and to
	// hand the broadcaster to LogHandlers; building these afterwards left both
	// nil, so the live-feed routes were never mounted (F-SRV-ORDER-001). A nil
	// log store leaves the worker nil and InteractionCapture short-circuits.
	//
	// The broadcaster fans every successfully-written row out to SSE
	// subscribers; slow subscribers drop events rather than block the writer,
	// so the hot-path overhead is a single mutex-held map iteration.
	if cfg.LogStore != nil {
		s.logBroadcaster = &LogBroadcaster{}
		s.logWorker = NewLogWorker(cfg.LogStore, logger, LogWorkerConfig{
			Broadcaster: s.logBroadcaster,
		})
		// Retention pruner (SEC-05): keep only the newest LogMaxRows rows. Only
		// started when a bound is configured; 0 means unlimited.
		if cfg.LogMaxRows > 0 {
			s.logPruner = newLogPruner("interaction-log", cfg.LogStore, cfg.LogMaxRows, DefaultLogPruneInterval, logger)
			s.logPruner.start()
		}
	}
	// Audit retention (audit M-08): every auth denial is a row, so without a
	// bound anonymous traffic could fill the disk. 0 keeps the historical
	// unbounded behaviour.
	s.auditWriter = auditWriter
	if cfg.AuditMaxRows > 0 {
		if ps, ok := cfg.AuditStore.(pruneStore); ok {
			s.auditPruner = newLogPruner("audit-log", ps, cfg.AuditMaxRows, DefaultLogPruneInterval, logger)
			s.auditPruner.start()
		}
	}

	s.registerRoutes(mux)

	// Build middleware chain: outermost first.
	var handler http.Handler = mux
	// QuotaEnforce is innermost (just around the mux) so the tenant is already
	// on the context (WithPrincipalTenantScope is outer) and a 429/402 is still
	// captured by InteractionCapture (also outer). REF-08 slice C.
	if cfg.QuotaEnforcer != nil {
		handler = QuotaEnforce(cfg.QuotaEnforcer)(handler)
	}
	// Metrics sit OUTSIDE QuotaEnforce (so a 429/402 is counted) and INSIDE
	// InteractionCapture (so the adapter-stamped protocol/agent are readable).
	// See MetricsCapture's doc comment.
	handler = MetricsCapture(cfg.Metrics)(handler)
	if s.logWorker != nil {
		// When quotas + pricing are configured, accrue each response's cost
		// against the tenant's monthly spend as it's captured.
		var spendHook func(tenantID, respBody string)
		if cfg.QuotaEnforcer != nil && cfg.Prices != nil {
			enf, prices := cfg.QuotaEnforcer, cfg.Prices
			spendHook = func(tenantID, respBody string) {
				if tenantID == "" || respBody == "" {
					return
				}
				usage := pricingpkg.ExtractUsage([]byte(respBody))
				if cost := prices.Estimate(usage.Model, usage.PromptTokens, usage.CompletionTokens); cost > 0 {
					enf.AddSpend(tenantID, cost)
				}
			}
		}
		handler = InteractionCapture(s.logWorker, NormalizeLogBodyMode(string(cfg.LogBodyMode)), spendHook)(handler)
	}
	handler = WithPrincipalTenantScope(handler)
	// Tenancy auth gates every /api/v1/* route when multi-tenant mode
	// is enabled. Health, the OpenAI/Anthropic LLM endpoints, and
	// /v1/models are left open so load balancers and existing SDKs
	// keep working without credentials; when those open routes carry
	// a valid API key, the middleware attaches the principal so model
	// listing and LLM resolution can be scoped to that tenant.
	if cfg.TenancyStore != nil {
		handler = tenancy.AuthMiddleware(cfg.TenancyStore, s.skipAuth)(handler)
		// Runs BEFORE the auth middleware (wrapped outside it): browser
		// WebSocket clients carry their key in a subprotocol offer, not a
		// header — lift it so best-effort principal resolution can scope the
		// realtime socket to a tenant (round-7 R7-20).
		handler = RealtimeBrowserAuth(handler)
	}
	handler = MaxBodySize(cfg.MaxBodyBytes)(handler)
	handler = CORS(cfg.CORSAllowedOrigins)(handler)
	handler = StructuredLogger(logger)(handler)
	handler = Recovery(logger)(handler)
	// RequestContext merges the former RequestID + ExtractAPIKey middlewares
	// (PERF-06): it stamps X-Request-Id and extracts the bearer API key in one
	// pass, so it must stay above StructuredLogger/Recovery (which log the id).
	handler = RequestContext(handler)
	handler = observability.HTTPMiddleware(handler)

	// Fall back to the default so a hand-built Config (one that skipped
	// DefaultConfig) still gets slow-loris protection rather than an unbounded
	// header read (PERF-21).
	readHeaderTimeout := cfg.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = DefaultReadHeaderTimeout
	}
	// Every request context descends from baseCtx so Shutdown can cancel
	// in-flight streams once the grace period is over (audit M-34).
	s.baseCtx, s.cancelBase = context.WithCancel(context.Background())
	s.httpServer = &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return s.baseCtx },
	}

	return s
}

// registerRoutes mounts the management API, protocol adapters, and engine endpoints.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	for _, p := range builtinOpenRoutes {
		s.open.add(p)
	}
	// Management API under /api/v1/. Every route below goes through
	// mountManaged, which applies the role floor declared in
	// managementRouteFloors (the single authorization source of truth) when
	// multi-tenant mode is on. ReloadAgent is a write (re-reads YAML and
	// replaces the registry entry) so its floor is Editor (F-HD-001).
	s.mountManaged(mux, "GET /api/v1/health", http.HandlerFunc(s.handlers.HealthCheck))
	// Readiness is a DIFFERENT question from liveness (PRD §11): health says
	// "the process is up", readiness says "this process can actually serve a
	// mock". Both Helm probes used to point at /api/v1/health, so the two were
	// the same check and a pod whose interaction log had died, or whose last
	// agent had been deleted through the write API, stayed in rotation.
	s.mountManaged(mux, "GET /api/v1/ready", http.HandlerFunc(s.readinessHandlers().Ready))
	// UX-01: who am I, and what may I do? Open to any authenticated role
	// (a viewer must be able to discover its own identity), but NOT in
	// skipAuth — an anonymous caller gets 401 from the middleware.
	s.mountManaged(mux, "GET /api/v1/identity", http.HandlerFunc(s.Identity))
	s.mountManaged(mux, "GET /api/v1/agents", http.HandlerFunc(s.handlers.ListAgents))
	s.mountManaged(mux, "GET /api/v1/agents/{name}", http.HandlerFunc(s.handlers.GetAgent))
	s.mountManaged(mux, "POST /api/v1/agents/{name}/reload", http.HandlerFunc(s.handlers.ReloadAgent))
	// Agent write API (FB-04): create / replace / delete at runtime.
	s.mountManaged(mux, "POST /api/v1/agents", http.HandlerFunc(s.handlers.CreateAgent))
	s.mountManaged(mux, "PUT /api/v1/agents/{name}", http.HandlerFunc(s.handlers.PutAgent))
	s.mountManaged(mux, "DELETE /api/v1/agents/{name}", http.HandlerFunc(s.handlers.DeleteAgent))

	// Tenancy CRUD — only mounted when multi-tenant mode is enabled.
	// Per-route floors (admin for tenant/key writes, editor for key list,
	// viewer for self-service rotate/burn) live in managementRouteFloors.
	if s.tenancyH != nil {
		s.mountManaged(mux, "GET /api/v1/tenants", http.HandlerFunc(s.tenancyH.ListTenants))
		s.mountManaged(mux, "POST /api/v1/tenants", http.HandlerFunc(s.tenancyH.CreateTenant))
		s.mountManaged(mux, "DELETE /api/v1/tenants/{id}", http.HandlerFunc(s.tenancyH.DeleteTenant))
		s.mountManaged(mux, "GET /api/v1/tenants/{id}/keys", http.HandlerFunc(s.tenancyH.ListAPIKeys))
		s.mountManaged(mux, "POST /api/v1/tenants/{id}/keys", http.HandlerFunc(s.tenancyH.CreateAPIKey))
		// Bulk rotation: emergency response to a tenant-wide suspected
		// compromise. Rotates every key in the tenant inside one
		// transaction so operators never end up with a mix of rotated +
		// unrotated credentials. Admin only.
		s.mountManaged(mux, "POST /api/v1/tenants/{id}/keys/rotate", http.HandlerFunc(s.tenancyH.BulkRotateTenantKeys))
		s.mountManaged(mux, "PATCH /api/v1/keys/{id}", http.HandlerFunc(s.tenancyH.UpdateAPIKeyRole))
		s.mountManaged(mux, "POST /api/v1/keys/{id}/rotate", http.HandlerFunc(s.tenancyH.RotateAPIKey))
		// Self-service rotate/burn: any authenticated principal acts on
		// its own key (the handler reads the caller's key id from context,
		// no path param to abuse), so viewer is sufficient.
		s.mountManaged(mux, "POST /api/v1/keys/me/rotate", http.HandlerFunc(s.tenancyH.RotateMyAPIKey))
		s.mountManaged(mux, "POST /api/v1/keys/me/burn", http.HandlerFunc(s.tenancyH.BurnMyAPIKey))
		s.mountManaged(mux, "DELETE /api/v1/keys/{id}", http.HandlerFunc(s.tenancyH.DeleteAPIKey))
	}

	// Audit log read API. Open in single-tenant mode (local dev tool);
	// admin-only when multi-tenant mode is on so the who-did-what surface
	// stays private to operators (floor in managementRouteFloors).
	if s.auditH != nil {
		s.mountManaged(mux, "GET /api/v1/audit", http.HandlerFunc(s.auditH.ListEvents))
	}

	// Protocol adapters (OpenAI, Anthropic, ...) mount through a common
	// registration boundary instead of hardwiring each provider's routes
	// here. Adding a provider is "implement adapter.Adapter + add it to
	// adapter.DefaultRegistry" — no edits to this route wiring (REF-05).
	// These stay open (no mountManaged): the outer middleware chain still
	// applies, and tenant scope / ProcessRequestContext plumbing lives in
	// the handlers, unchanged by the move.
	for _, a := range adapter.DefaultRegistryWithServiceFaults(s.engine, s.config.VectorStore, s.config.SearchService, s.config.ServiceFaults).Adapters() {
		// Realtime generates responses in-process over a WebSocket, so the
		// HTTP middleware that meters the request/response protocols never
		// sees them — the adapter exposes per-response hooks instead.
		if rt, ok := a.(*adapter.RealtimeHandler); ok {
			s.wireRealtime(rt)
		}
		for _, route := range a.Routes() {
			mux.HandleFunc(route.Pattern, route.Handler)
			// Every provider surface is open: clients send their own provider
			// key, which the mock ignores. Registering the pattern here is
			// what makes skipAuth true for it in multi-tenant mode.
			s.open.add(route.Pattern)
		}
	}

	// Log query API. Prices is threaded in so rows returned by
	// ListLogs carry a computed cost_usd field when a pricing table
	// is configured.
	logHandlers := &LogHandlers{
		Store:       s.config.LogStore,
		Prices:      s.config.Prices,
		Broadcaster: s.logBroadcaster,
	}
	s.mountManaged(mux, "GET /api/v1/logs", http.HandlerFunc(logHandlers.ListLogs))
	s.mountManaged(mux, "GET /api/v1/logs/{id}", http.HandlerFunc(logHandlers.GetLog))
	s.mountManaged(mux, "DELETE /api/v1/logs", http.HandlerFunc(logHandlers.DeleteLogs))
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
		s.mountManaged(mux, "GET /api/v1/logs/stream", http.HandlerFunc(logHandlers.StreamLogs))
		s.mountManaged(mux, "GET /api/v1/logs/stream/metrics", http.HandlerFunc(logHandlers.StreamMetrics))
	}

	// Cost aggregate endpoint. Silent no-op when the log store is
	// absent — handler returns 503 in that case, matching the
	// existing /api/v1/logs behavior.
	if s.config.LogStore != nil {
		costsH := &CostsHandlers{Store: s.config.LogStore, Prices: s.config.Prices}
		s.mountManaged(mux, "GET /api/v1/costs", http.HandlerFunc(costsH.ListCosts)) // F-CO-005
	}

	// Pipeline management API. Read-only list + detail used by the
	// GUI's /pipelines DAG viewer. The handler returns an empty
	// list when no registry is wired up, so single-tenant
	// deployments that never loaded a Pipeline YAML still get a
	// well-formed response.
	if s.config.Pipelines != nil {
		// A run drives the engine in process, so it bypasses InteractionCapture
		// entirely. Wiring the recorder here is what makes a run leave the same
		// trace as the traffic it simulates; without it, three agents could
		// execute and the log would show nothing.
		executor := engine.NewPipelineExecutor(s.engine)
		executor.Recorder = newPipelineRecorder(s.logWorker, NormalizeLogBodyMode(string(s.config.LogBodyMode)))
		pipelineH := &PipelineHandlers{
			Registry:      s.config.Pipelines,
			Executor:      executor,
			AgentRegistry: s.engine.Registry,
			AgentsDir:     s.config.AgentsDir,
			Recorder:      s.recorder,
			Logger:        s.logger,
		}
		s.mountManaged(mux, "GET /api/v1/pipelines", http.HandlerFunc(pipelineH.ListPipelines))           // F-PL-001
		s.mountManaged(mux, "GET /api/v1/pipelines/{name}", http.HandlerFunc(pipelineH.GetPipeline))      // F-PL-001
		s.mountManaged(mux, "POST /api/v1/pipelines/{name}/run", http.HandlerFunc(pipelineH.RunPipeline)) // R13 / #33
		s.mountManaged(mux, "PUT /api/v1/pipelines/{name}", http.HandlerFunc(pipelineH.UpdatePipeline))   // REF-07
	}

	// Agent config validation endpoint. Open in single-tenant mode
	// (matches /api/v1/logs); gated behind the editor role in
	// multi-tenant mode so viewers don't get a free surface for
	// spraying YAML at the parser.
	validateH := NewValidateHandler()
	s.mountManaged(mux, "POST /api/v1/config/validate", validateH)

	// Per-tenant quota read + management (REF-08 slice C). Only mounted when an
	// enforcer is configured (multi-tenant mode with quotas).
	if s.config.QuotaEnforcer != nil {
		quotaH := &QuotaHandlers{Enforcer: s.config.QuotaEnforcer, Store: s.config.TenancyStore}
		s.mountManaged(mux, "GET /api/v1/quota", http.HandlerFunc(quotaH.GetQuota))
		s.mountManaged(mux, "PUT /api/v1/tenants/{id}/quota", http.HandlerFunc(quotaH.SetTenantQuota))
	}

	// SSO / OIDC relying-party endpoints (REF-08 slice D). Open (in skipAuth),
	// since login/callback precede authentication and logout clears its own
	// session cookie. Mounted only when OIDC is configured.
	if s.config.SSO != nil {
		mux.HandleFunc("GET /auth/login", s.config.SSO.Login)
		mux.HandleFunc("GET /auth/callback", s.config.SSO.Callback)
		mux.HandleFunc("POST /auth/logout", s.config.SSO.Logout)
	}

	// Prometheus scrape target (FR-J02). Open in single-tenant mode like the
	// rest of the local-dev control plane; viewer-gated in multi-tenant mode,
	// where agent and scenario NAMES are labels and therefore not public. A
	// Prometheus scrape config for a multi-tenant deployment needs a viewer
	// API key in an Authorization header.
	s.mountManaged(mux, "GET /metrics", MetricsHandler(s.config.Metrics))

	// Generic engine endpoint (internal/testing): opt-in, see
	// Config.EnableEngineEndpoint (audit M-07). Only an enabled mount is
	// added to the auth-exempt set.
	if s.config.EnableEngineEndpoint {
		mux.HandleFunc("POST /v1/engines/process", s.handleProcessRequest)
		s.open.add("POST /v1/engines/process")
	}
}

// readinessHandlers builds the readiness check set from what this server was
// actually configured with, and registers the two gauges that mirror it so a
// /metrics scrape and a /api/v1/ready probe can never disagree.
//
// The checks answer the two questions that distinguish a serving mock from a
// merely-running process:
//
//   - fixtures: at least one agent is loaded. A registry with zero agents can
//     only return 404s, which is worse than being out of rotation.
//   - log_store: the SQLite interaction log is reachable. Only checked when a
//     store is configured; an in-memory deployment simply has no such check.
func (s *Server) readinessHandlers() *ReadinessHandlers {
	h := &ReadinessHandlers{
		Checks: []ReadinessCheck{{
			// First, so a draining pod is reported as such even when every
			// dependency is healthy: kube-proxy must stop sending new
			// connections before the listeners close (audit M-34).
			Name: "draining",
			Check: func(context.Context) error {
				if s.draining.Load() {
					return errors.New("server is shutting down")
				}
				return nil
			},
		}, {
			Name: "fixtures",
			Check: func(context.Context) error {
				if s.engine == nil || s.engine.Registry == nil || s.engine.Registry.Count() == 0 {
					return errors.New("no agent fixtures loaded")
				}
				return nil
			},
		}},
	}
	if store := s.config.LogStore; store != nil {
		h.Checks = append(h.Checks, ReadinessCheck{
			Name: "log_store",
			Check: func(ctx context.Context) error {
				if err := store.Ping(ctx); err != nil {
					return fmt.Errorf("interaction log unreachable: %w", err)
				}
				return nil
			},
		})
	}

	reg := s.config.Metrics
	if reg == nil {
		reg = metrics.Default()
	}
	reg.RegisterGauge(metrics.Namespace+"_agents_loaded",
		"Agent fixtures currently loaded in the registry.", nil, nil,
		func() float64 {
			if s.engine == nil || s.engine.Registry == nil {
				return 0
			}
			return float64(s.engine.Registry.Count())
		})
	reg.RegisterGauge(metrics.Namespace+"_ready",
		"1 when every readiness check passes, 0 otherwise — the same verdict GET /api/v1/ready returns.",
		nil, nil,
		func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), readinessTimeout)
			defer cancel()
			if h.IsReady(ctx) {
				return 1
			}
			return 0
		})
	return h
}

// handleProcessRequest is a generic engine endpoint for testing.
func (s *Server) handleProcessRequest(w http.ResponseWriter, r *http.Request) {
	var req engine.InboundRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := s.engine.ProcessRequestContext(r.Context(), &req)
	if err != nil {
		status := http.StatusInternalServerError
		// Use errors.Is throughout: the engine wraps ErrAgentNotFound with
		// %w, so a `==` compare would miss it and fall through to 500 (F-SV-002).
		if isNotFound(err) {
			status = http.StatusNotFound
		} else if errors.Is(err, engine.ErrEmptyMessage) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
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
	if err := s.httpServer.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	timeout := s.config.ShutdownTimeout
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.logger.Info("shutting down server", "timeout", timeout)

	// Readiness goes to 503 before anything closes, and stays there. With a
	// drain delay the listeners keep accepting for that long so a balancer
	// polling /api/v1/ready can take the instance out of rotation first.
	s.draining.Store(true)
	if d := s.config.ShutdownDrainDelay; d > 0 {
		time.Sleep(d)
	}
	// Hijacked WebSocket connections are invisible to http.Server.Shutdown;
	// close them so their handlers return instead of pinning the process.
	if s.realtime != nil {
		if n := s.realtime.CloseAll(); n > 0 {
			s.logger.Info("closed realtime sessions", "count", n)
		}
	}

	// Close the SSE broadcaster FIRST so any in-flight /logs/stream handlers
	// unblock (their sub.C() closes) and return, instead of pinning
	// httpServer.Shutdown for the full ShutdownTimeout while it waits on a
	// still-streaming connection (F-SRV-SHUT-002). Closing here is safe: a
	// late Publish from the log-worker drain below is a no-op on a closed
	// broadcaster, and Subscribe-after-Close hands back an already-closed
	// subscription, so a client that connects during the shutdown window gets
	// an immediately-terminating stream rather than a hang (F-SV-001).
	if s.logBroadcaster != nil {
		s.logBroadcaster.Close()
	}
	// Stop the retention pruner before draining the worker; it's independent of
	// the write path and must not outlive the store (SEC-05).
	if s.logPruner != nil {
		s.logPruner.Stop()
	}
	if s.auditPruner != nil {
		s.auditPruner.Stop()
	}

	err := s.httpServer.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		// Requests still running after the grace period (paced streams,
		// pipeline runs) are cancelled through their context, then the
		// connections are closed. This is the normal end of a rollout with
		// a long stream open, so it is reported as a warning, not a failure.
		s.cancelBase()
		_ = s.httpServer.Close()
		err = ErrShutdownDeadline
	}
	s.cancelBase()
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
	// Drain the audit denial writer last, for the same reason: no request can
	// enqueue once the HTTP server has stopped.
	if s.auditWriter != nil {
		s.auditWriter.Stop(DefaultLogDrainTimeout)
		if d := s.auditWriter.Dropped(); d > 0 {
			s.logger.Warn("audit denial writer dropped events", "dropped", d)
		}
	}
	return err
}

// Addr returns the server's actual listen address. After Listen with port 0
// this is the OS-assigned address (host:port), which is what tests need;
// before Listen it falls back to the configured address. Delegates to
// ListenAddr so the two never disagree (F-SV-006).
func (s *Server) Addr() string {
	return s.ListenAddr()
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
	// The engine wraps ErrAgentNotFound with %w, so errors.Is is sufficient
	// and correct; the old strings.Contains fallback (F-SV-003) matched on
	// the message text and could misclassify unrelated errors as 404.
	return errors.Is(err, engine.ErrAgentNotFound)
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

// skipAuth reports whether a request path remains unauthenticated when
// multi-tenant mode is enabled. Health probes need to work without
// credentials so load balancers don't start failing closed; the LLM
// endpoints are open by design because clients send their own provider API
// keys that MockAgents deliberately ignores. The set is derived from the
// routes registerRoutes mounts (see openRoutes), so it cannot drift from the
// real surface the way the former hand-written list did.
func (s *Server) skipAuth(r *http.Request) bool {
	return s.open.skip(r)
}
