// Package api is the playground's HTTP surface: a JSON REST API (described by
// openapi.yaml, served at /openapi.yaml and /openapi.json), Server-Sent
// Events for live runs and streaming agent output, Prometheus metrics, and
// the embedded web UI.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	playground "github.com/mockagents/mockagents/demo/agent-playground"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/config"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/metrics"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/mockctl"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/tools"
	"github.com/mockagents/mockagents/demo/agent-playground/internal/workflow"
	"gopkg.in/yaml.v3"
)

// Version is the playground version reported by /api/info.
const Version = "1.0.0"

// maxBody bounds request bodies.
const maxBody = 1 << 20

// Server serves the playground API.
type Server struct {
	Engine  *workflow.Engine
	Config  *config.Store
	Tools   *tools.Registry
	Metrics *metrics.Registry
	Mock    *mockctl.Client
	// MockMode is "embedded" or "external" (shown in /api/info and the UI).
	MockMode string
	Logger   *slog.Logger
	// Token, when set, is required as "Authorization: Bearer <token>" on
	// every mutating /api request.
	Token string
	// MaxWait caps ?wait= on run creation and synchronous invokes.
	MaxWait time.Duration

	openapiJSON []byte
}

// Handler builds the HTTP handler.
func (s *Server) Handler() (http.Handler, error) {
	if s.MaxWait == 0 {
		s.MaxWait = 120 * time.Second
	}
	if err := s.loadOpenAPI(); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	for _, r := range s.routes() {
		mux.HandleFunc(r.pattern, r.handler)
	}
	web, err := fs.Sub(playground.FS, "web")
	if err != nil {
		return nil, err
	}
	static := http.FileServerFS(web)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web, "index.html")
	})
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", static))
	return s.middleware(mux), nil
}

type route struct {
	pattern string
	handler http.HandlerFunc
}

// routes is the single source of truth for the API surface. The OpenAPI
// contract test checks this table against openapi.yaml in both directions.
func (s *Server) routes() []route {
	return []route{
		{"GET /healthz", s.healthz},
		{"GET /readyz", s.readyz},
		{"GET /metrics", s.metrics},
		{"GET /openapi.yaml", s.openapiYAML},
		{"GET /openapi.json", s.openapiJSONHandler},
		{"GET /api/info", s.info},

		{"GET /api/workflows", s.listWorkflows},
		{"GET /api/workflows/{name}", s.getWorkflow},
		{"POST /api/workflows/{name}/runs", s.startRun},

		{"GET /api/runs", s.listRuns},
		{"GET /api/runs/{id}", s.getRun},
		{"GET /api/runs/{id}/trace", s.getTrace},
		{"GET /api/runs/{id}/events", s.runEvents},
		{"POST /api/runs/{id}/cancel", s.cancelRun},

		{"GET /api/reviews", s.listReviews},
		{"GET /api/reviews/{id}", s.getReview},
		{"POST /api/reviews/{id}/decision", s.decideReview},

		{"GET /api/agents", s.listAgents},
		{"GET /api/agents/{name}", s.getAgent},
		{"PATCH /api/agents/{name}", s.patchAgent},
		{"POST /api/agents/{name}/invoke", s.invokeAgent},

		{"GET /api/tools", s.listTools},
		{"POST /api/tools/{name}/execute", s.executeTool},

		{"GET /api/config", s.getConfig},
		{"PATCH /api/config", s.patchConfig},
		{"POST /api/config/reset", s.resetConfig},

		{"GET /api/mock/status", s.mockStatus},
		{"GET /api/mock/logs", s.mockLogs},
		{"GET /api/mock/costs", s.mockCosts},
		{"GET /api/mock/pipelines", s.mockPipelines},
		{"POST /api/mock/pipelines/{name}/run", s.mockRunPipeline},
		{"GET /api/mock/agents/{name}", s.mockGetAgent},
		{"PUT /api/mock/agents/{name}", s.mockPutAgent},
		{"POST /api/mock/agents/{name}/reload", s.mockReloadAgent},
		{"POST /api/mock/validate", s.mockValidate},
	}
}

// RoutePatterns lists every API route pattern (tests).
func (s *Server) RoutePatterns() []string {
	var out []string
	for _, r := range s.routes() {
		out = append(out, r.pattern)
	}
	return out
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush keeps SSE working through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				s.Logger.Error("handler panic", "path", r.URL.Path, "panic", rec)
				writeError(sw, http.StatusInternalServerError, "internal", fmt.Sprint(rec), nil)
			}
			if r.URL.Path != "/healthz" && r.URL.Path != "/metrics" {
				s.Logger.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
			}
		}()
		sw.Header().Set("X-Content-Type-Options", "nosniff")
		if s.Token != "" && strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
				writeError(sw, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required for mutating requests", nil)
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(sw, r.Body, maxBody)
		}
		next.ServeHTTP(sw, r)
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ErrorBody is the uniform error envelope.
type ErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details any    `json:"details,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, details any) {
	var b ErrorBody
	b.Error.Code, b.Error.Message, b.Error.Details = code, msg, details
	writeJSON(w, status, b)
}

// decode reads a JSON body strictly (unknown fields are an error). An empty
// body is allowed when allowEmpty is true.
func decode(r *http.Request, v any, allowEmpty bool) error {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fmt.Errorf("request body exceeds %d bytes", maxBody)
		}
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		if allowEmpty {
			return nil
		}
		return errors.New("request body is required")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if dec.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func (s *Server) loadOpenAPI() error {
	raw, err := playground.FS.ReadFile("openapi.yaml")
	if err != nil {
		return err
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse openapi.yaml: %w", err)
	}
	s.openapiJSON, err = json.MarshalIndent(doc, "", "  ")
	return err
}

func (s *Server) openapiYAML(w http.ResponseWriter, _ *http.Request) {
	raw, _ := playground.FS.ReadFile("openapi.yaml")
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(raw)
}

func (s *Server) openapiJSONHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(s.openapiJSON)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	h, err := s.Mock.Health(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "mock": map[string]any{"url": s.Mock.BaseURL(), "error": err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "mock": map[string]any{"url": s.Mock.BaseURL(), "health": h}})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.Metrics.Write(w)
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	cfg, version := s.Config.Get()
	counts := s.Engine.Store.Counts()
	pending := len(s.Engine.Reviews.List(workflow.Filter{Status: workflow.ReviewPending, Blocking: ptr(true)}))
	writeJSON(w, http.StatusOK, map[string]any{
		"name":            "mockagents Agent Playground",
		"version":         Version,
		"mock":            map[string]any{"url": s.Mock.BaseURL(), "mode": s.MockMode},
		"config_version":  version,
		"review_mode":     cfg.Defaults.Review.Mode,
		"agents":          len(cfg.Agents),
		"workflows":       len(s.Engine.Definitions(false)),
		"runs":            counts,
		"pending_reviews": pending,
		"auth_required":   s.Token != "",
		"links": map[string]string{
			"ui": "/", "openapi": "/openapi.yaml", "metrics": "/metrics", "mock_gui_hint": "run `make gui-dev` in the repo root for the mockagents console",
		},
	})
}

func ptr[T any](v T) *T { return &v }

// agentView is the API representation of an agent, with its effective
// routing and retry policy resolved.
type agentView struct {
	config.Agent
	EffectiveRoute any `json:"effective_route"`
	EffectiveRetry any `json:"effective_retry"`
	ToolDetails    any `json:"tool_details,omitempty"`
}
