package server

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/tenancy"
)

type contextKey string

// reqScopeKey is the single context entry under which RequestContext stores the
// per-request id and (optional) bearer API key. Bundling both behind one
// *requestScope means one context node + one shallow Request copy per request
// instead of one of each per value (PERF-06).
const reqScopeKey contextKey = "req_scope"

// requestScope carries the per-request values the middleware chain derives once
// at the top of the chain. A pointer is stored in context so downstream reads
// never re-copy it, and so the struct boxes into the interface without the
// extra allocation a string value would incur.
type requestScope struct {
	requestID string
	apiKey    string
}

func requestScopeFrom(ctx context.Context) *requestScope {
	sc, _ := ctx.Value(reqScopeKey).(*requestScope)
	return sc
}

// requestIDFromContext returns the id stamped by RequestContext, or "" when no
// RequestContext middleware ran (e.g. a bare handler under test).
func requestIDFromContext(ctx context.Context) string {
	if sc := requestScopeFrom(ctx); sc != nil {
		return sc.requestID
	}
	return ""
}

// apiKeyFromContext returns the bearer token RequestContext parsed from the
// Authorization header, or "" when absent.
func apiKeyFromContext(ctx context.Context) string {
	if sc := requestScopeFrom(ctx); sc != nil {
		return sc.apiKey
	}
	return ""
}

// RequestContext derives the per-request id and the optional bearer API key in
// a single pass and stores both under one context entry (reqScopeKey). It
// replaces the former separate RequestID + ExtractAPIKey middlewares: the
// request is shallow-copied once instead of twice and a single context node is
// allocated regardless of whether an Authorization header is present (PERF-06).
//
// It runs at the top of the chain so the generated id lands on the response
// (X-Request-Id) and is visible to StructuredLogger/Recovery, which log it.
func RequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = generateRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		sc := &requestScope{requestID: id}
		// Reuse the tenancy parser so the two bearer-extraction paths can't drift
		// in their scheme handling (F-MW-002).
		if token, ok := tenancy.ParseBearerToken(r.Header.Get("Authorization")); ok {
			sc.apiKey = token
		}
		ctx := context.WithValue(r.Context(), reqScopeKey, sc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// StructuredLogger logs request method, path, status, duration, and request ID.
func StructuredLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := acquireStatusWriter(w)
			defer releaseStatusWriter(sw)

			next.ServeHTTP(sw, r)

			reqID := requestIDFromContext(r.Context())
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", reqID,
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// CORS adds CORS headers, with the allowed origins configurable (F-MW-001).
//
// An empty list — or one containing "*" — keeps the permissive
// `Access-Control-Allow-Origin: *` wildcard that suits local development and
// the existing default. With an explicit allowlist, only a request whose
// Origin is listed gets an Allow-Origin echo (plus `Vary: Origin` so caches
// don't cross-pollinate), letting a control-plane deployment lock the surface
// down without code changes. Auth is Bearer-token, not cookie, so no
// Access-Control-Allow-Credentials is emitted and origin-reflection is safe.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	wildcard := len(allowedOrigins) == 0
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			wildcard = true
		}
		allowed[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if wildcard {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin := r.Header.Get("Origin"); origin != "" {
				if _, ok := allowed[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recovery catches panics and returns a 500 error instead of crashing.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					reqID := requestIDFromContext(r.Context())
					logger.Error("panic recovered",
						"error", fmt.Sprintf("%v", rec),
						"request_id", reqID,
						"stack", string(debug.Stack()),
					)
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBodySize limits the request body to the specified number of bytes.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithPrincipalTenantScope copies the authenticated principal's
// tenant id onto the engine context. This lets protocol adapters stay
// tenancy-free while still resolving tenant-owned agents and models
// from the caller's actual API key rather than from a spoofable header.
func WithPrincipalTenantScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := tenancy.PrincipalFrom(r.Context()); p != nil && p.TenantID != "" {
			r = r.WithContext(engine.WithTenantID(r.Context(), p.TenantID))
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code.
// It implements http.Flusher (SSE) and Unwrap (so http.ResponseController
// can reach the net.Conn for the other optional interfaces — Hijacker,
// ReaderFrom, Pusher — and for SetWriteDeadline), F-MW-004.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// statusWriterPool recycles statusWriter instances across requests.
// StructuredLogger wraps every request, so the old `&statusWriter{...}`
// allocated one wrapper struct on the hot path per request (PERF-05).
// Unlike captureWriter, statusWriter carries no buffer, so pooling it is
// a pure struct-reuse win with no slice-retention concerns. acquire/release
// mirror the captureWriter pool: release nils the embedded ResponseWriter
// so a pooled entry can never pin a finished request's connection state.
var statusWriterPool = sync.Pool{
	New: func() any { return &statusWriter{} },
}

func acquireStatusWriter(w http.ResponseWriter) *statusWriter {
	sw := statusWriterPool.Get().(*statusWriter)
	sw.ResponseWriter = w
	sw.status = http.StatusOK
	sw.wroteHeader = false
	return sw
}

func releaseStatusWriter(sw *statusWriter) {
	sw.ResponseWriter = nil
	statusWriterPool.Put(sw)
}

func (w *statusWriter) WriteHeader(code int) {
	// Guard against a double WriteHeader: the first call wins and records the
	// status; a second would otherwise trigger the stdlib "superfluous
	// WriteHeader call" warning and clobber the captured status (F-MW-005).
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController descend to the underlying writer so
// SetWriteDeadline (used by the SSE handlers to defeat the global
// WriteTimeout, F-SV-004) can reach the net.Conn.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// generateRequestID returns a unique, non-cryptographic correlation id. It uses
// math/rand/v2 (auto-seeded, lock-free, goroutine-safe) rather than crypto/rand:
// a request id needs uniqueness, not unpredictability (same rationale as the
// engine's fallbackToolCallID), so the old crypto/rand syscall + fmt.Sprintf
// were pure hot-path overhead (PERF-07).
func generateRequestID() string {
	return "req-" + strconv.FormatUint(rand.Uint64(), 16)
}
