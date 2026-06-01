package engine

import (
	"context"
	"net/http"
)

// RequestMeta carries per-request metadata that is produced deep in
// the handler chain (after ProcessRequest resolves an agent) but
// consumed by middleware outside the handler (InteractionCapture's
// async logger). Context values are immutable, so the middleware
// stores a pointer to this struct and the handler mutates it after
// the match is known.
//
// Zero value is safe: a nil *RequestMeta from RequestMetaFromContext
// means "no capture middleware installed", in which case callers
// simply skip the annotation.
type RequestMeta struct {
	AgentName string
	Model     string
}

// tenantKey is the unexported context key the engine uses to thread
// a tenant id from the HTTP layer down to AgentRegistry lookups
// without importing the tenancy package (which would create a
// cycle: tenancy → engine → tenancy). The HTTP layer calls
// WithTenantID and the engine calls TenantIDFromContext.
type tenantKey struct{}

// WithTenantID returns a new context that carries the given tenant
// id. Passing "" does not remove a previously stored id — context
// values are immutable, so it layers an empty value that shadows the
// old one; TenantIDFromContext then reads back "", which the registry
// treats as no tenant (global agents only). Used by adapter handlers
// and the management-API handlers when a request can be associated
// with a tenant (either via an authenticated principal or an opt-in
// `X-Mockagents-Tenant` header).
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

// TenantIDFromContext fetches the tenant id previously stored via
// WithTenantID. Returns the empty string when no tenant has been
// set, which the registry treats as "global agents only".
func TenantIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tenantKey{}).(string); ok {
		return v
	}
	return ""
}

type requestMetaKey struct{}

// WithRequestMeta returns a new request whose context carries a
// freshly allocated RequestMeta. The returned *RequestMeta is the
// same pointer embedded in the context, so callers can read back the
// fields that downstream handlers wrote.
func WithRequestMeta(r *http.Request) (*http.Request, *RequestMeta) {
	m := &RequestMeta{}
	ctx := context.WithValue(r.Context(), requestMetaKey{}, m)
	return r.WithContext(ctx), m
}

// RequestMetaFromContext fetches the RequestMeta pointer attached by
// WithRequestMeta. Returns nil when no capture middleware ran.
func RequestMetaFromContext(ctx context.Context) *RequestMeta {
	m, _ := ctx.Value(requestMetaKey{}).(*RequestMeta)
	return m
}
