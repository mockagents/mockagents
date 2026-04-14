package tenancy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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

// ExtractAPIKey pulls a bearer token from the Authorization header or
// X-Api-Key header, in that order. Empty on miss.
func ExtractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		const bearer = "Bearer "
		if strings.HasPrefix(auth, bearer) {
			return strings.TrimSpace(auth[len(bearer):])
		}
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
				next.ServeHTTP(w, r)
				return
			}
			key := ExtractAPIKey(r)
			if key == "" {
				writeAuthError(w, http.StatusUnauthorized, "missing Authorization bearer token or X-Api-Key header")
				return
			}
			principal, err := store.Resolve(r.Context(), key)
			if err != nil {
				if errors.Is(err, ErrInvalidKey) {
					writeAuthError(w, http.StatusUnauthorized, "invalid api key")
					return
				}
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
func RequireRole(required Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p == nil {
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !p.Role.AtLeast(required) {
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
