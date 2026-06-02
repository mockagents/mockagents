package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/tenancy"
)

type contextKey string

const (
	// RequestIDKey is the context key for the request ID.
	RequestIDKey contextKey = "request_id"
	// APIKeyKey is the context key for the extracted API key.
	APIKeyKey contextKey = "api_key"
)

// RequestID injects a unique X-Request-Id header into every request/response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = generateRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), RequestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// StructuredLogger logs request method, path, status, duration, and request ID.
func StructuredLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			reqID, _ := r.Context().Value(RequestIDKey).(string)
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
					reqID, _ := r.Context().Value(RequestIDKey).(string)
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

// ExtractAPIKey extracts the Bearer token from the Authorization header
// and stores it in context.
func ExtractAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reuse the tenancy parser so the two extraction paths can't drift in
		// their scheme handling (F-MW-002).
		if token, ok := tenancy.ParseBearerToken(r.Header.Get("Authorization")); ok {
			r = r.WithContext(context.WithValue(r.Context(), APIKeyKey, token))
		}
		next.ServeHTTP(w, r)
	})
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

func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("req-%x", b)
}
