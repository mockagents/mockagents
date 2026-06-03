package tenancy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
)

// contextKey is unexported so other packages can't accidentally collide
// on context keys. They go through ContextPrincipal() instead.
type contextKey string

const principalContextKey contextKey = "mockagents.tenancy.principal"

// WithPrincipal returns a new context that carries the given Principal.
// Exposed so tests and alternative transports (like the gRPC layer that
// may land in a future slice) can plug the principal in directly.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, p)
}

// PrincipalFrom retrieves the authenticated caller from the request
// context, or returns nil if the request is unauthenticated (which
// only happens when multi-tenant mode is disabled).
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalContextKey).(*Principal)
	return p
}

// denialFn is the denial-callback signature. The hook is invoked
// synchronously by AuthMiddleware and RequireRole whenever a request is
// rejected with 401 or 403; the server wiring installs one that forwards the
// event into the audit log. Hooks must not block — they run on the hot
// request path before the 401/403 response is written (the reference
// implementation records asynchronously via audit.Recorder).
type denialFn = func(r *http.Request, status int, reason string)

// denialHook holds the package-wide denial callback, accessed atomically so
// SetDenialHook (called at server construction) cannot data-race the reads on
// the hot request path in fireDenial (F-SV-007). This matters because the
// codebase can't run `go test -race` (no cgo), so a plain-variable race here
// would go undetected. A nil pointer is a cheap no-op.
//
// Semantics are last-writer-wins: there is a single process-wide hook.
// Constructing two Servers in one process is race-free, but the second New()
// replaces the first's hook, so denials from both route to the most recently
// installed audit recorder. Single-server-per-process is the normal
// deployment, and the hook only forwards 401/403 events to an audit log, so
// the shared-global trade-off is intentional.
var denialHook atomic.Pointer[denialFn]

// SetDenialHook installs h as the package-wide denial callback. Pass nil to
// disable. The store is atomic, so this is safe to call concurrently with
// request handling; in practice it is called once at server construction.
func SetDenialHook(h denialFn) {
	if h == nil {
		denialHook.Store(nil)
		return
	}
	denialHook.Store(&h)
}

func fireDenial(r *http.Request, status int, reason string) {
	if fn := denialHook.Load(); fn != nil {
		(*fn)(r, status, reason)
	}
}

// ParseBearerToken extracts the credential from an Authorization header
// value. It matches the "Bearer" scheme case-insensitively and tolerates
// surrounding whitespace (F-MW-002), so `bearer x`, `  Bearer  x  `, etc. are
// all accepted, while a header that is not a well-formed bearer credential
// returns ok=false — a deliberate reject rather than a silent fall-through to
// anonymous on a near-miss like `Bearertoken`.
func ParseBearerToken(authHeader string) (string, bool) {
	authHeader = strings.TrimSpace(authHeader)
	const scheme = "bearer"
	if len(authHeader) <= len(scheme) || !strings.EqualFold(authHeader[:len(scheme)], scheme) {
		return "", false
	}
	// The scheme must be delimited by whitespace so "Bearertoken" is not a
	// match.
	rest := authHeader[len(scheme):]
	if rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	token := strings.TrimSpace(rest)
	// A real credential has no internal whitespace; reject `Bearer ab cd`
	// rather than forwarding a multi-token string to the store (F-MW-001).
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

// ExtractAPIKey pulls a bearer token from the Authorization header or
// X-Api-Key header, in that order. Empty on miss.
func ExtractAPIKey(r *http.Request) string {
	if token, ok := ParseBearerToken(r.Header.Get("Authorization")); ok {
		return token
	}
	return strings.TrimSpace(r.Header.Get("X-Api-Key"))
}

// AuthMiddleware builds an HTTP middleware that requires a valid API
// key for every request. The skip predicate lets callers exempt routes
// that must remain unauthenticated (e.g. /api/v1/health probes used by
// load balancers).
func AuthMiddleware(store Store, skip func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip(r) {
				// Best-effort: a valid key still attaches the principal so a
				// skip route can self-check it, but any Resolve error (invalid
				// key OR a store outage) is swallowed and the request proceeds
				// anonymously — skip routes (health probes, the LLM endpoints)
				// must never fail closed (F-MW-003).
				if key := ExtractAPIKey(r); key != "" {
					if principal, err := store.Resolve(r.Context(), key); err == nil {
						r = r.WithContext(WithPrincipal(r.Context(), principal))
					}
				}
				next.ServeHTTP(w, r)
				return
			}
			key := ExtractAPIKey(r)
			if key == "" {
				fireDenial(r, http.StatusUnauthorized, "missing credentials")
				writeAuthError(w, http.StatusUnauthorized, "missing Authorization bearer token or X-Api-Key header")
				return
			}
			principal, err := store.Resolve(r.Context(), key)
			if err != nil {
				// A missing/wrong key is a 401; anything else (a store outage)
				// is a 500 — fail closed. ErrNotFound is matched too for label
				// accuracy even though Resolve currently only returns
				// ErrInvalidKey (F-MW-002).
				if errors.Is(err, ErrInvalidKey) || errors.Is(err, ErrNotFound) {
					fireDenial(r, http.StatusUnauthorized, "invalid api key")
					writeAuthError(w, http.StatusUnauthorized, "invalid api key")
					return
				}
				fireDenial(r, http.StatusInternalServerError, "auth store error")
				writeAuthError(w, http.StatusInternalServerError, "auth store error")
				return
			}
			ctx := WithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole wraps a handler so that only principals at or above the
// given role can execute it. 401 if the request is unauthenticated,
// 403 if the role is insufficient.
//
// It checks privilege LEVEL only — it does NOT verify that the caller owns the
// resource the request targets. Tenant-ownership scoping (e.g. that a path's
// {id} belongs to the caller's tenant) is the handler's responsibility
// (F-MW-007).
func RequireRole(required Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p == nil {
			fireDenial(r, http.StatusUnauthorized, "authentication required")
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !p.Role.AtLeast(required) {
			fireDenial(r, http.StatusForbidden,
				"role "+string(p.Role)+" insufficient; "+string(required)+" required")
			writeAuthError(w, http.StatusForbidden,
				"role "+string(p.Role)+" is insufficient; "+string(required)+" required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="mockagents"`)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    "authentication_error",
			"message": message,
		},
	})
}
