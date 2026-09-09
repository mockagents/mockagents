package server

import (
	"net/http"
	"strings"
)

// builtinOpenRoutes are the non-adapter routes that stay unauthenticated in
// multi-tenant mode. Health and readiness are probe targets: a load balancer
// or kubelet has no API key, and failing them closed would take the pod out
// of rotation for the wrong reason (they report only pass/fail plus a
// dependency name). The generic engine endpoint is an LLM surface like the
// adapters. The SSO endpoints start or clear a session, so they necessarily
// precede authentication and do their own checks (REF-08 slice D).
var builtinOpenRoutes = []string{
	"GET /api/v1/health",
	"GET /api/v1/ready",
	"GET /auth/login",
	"GET /auth/callback",
	"POST /auth/logout",
}

// openRoutes is the auth-exemption predicate behind Server.skipAuth. It is
// populated from the patterns registerRoutes actually mounts — every adapter
// route plus builtinOpenRoutes — instead of a parallel hand-written list.
// The former list omitted /v1/embeddings, /v1/responses, the Gemini,
// Bedrock, batch, file and conversation surfaces, so an OpenAI SDK sending
// its own `sk-…` key to those routes got a 401 in multi-tenant mode while
// /v1/chat/completions worked (audit H-03).
//
// Matching reuses http.ServeMux so wildcard segments (`{deployment}`,
// `{modelmethod}`) resolve exactly as the real router resolves them, and a
// prefix can never auto-exempt an unrelated path (SEC-03: /v1/models is
// open, /v1/models-internal is not).
type openRoutes struct {
	mux   *http.ServeMux
	paths map[string]struct{}
}

func newOpenRoutes() *openRoutes {
	return &openRoutes{mux: http.NewServeMux(), paths: make(map[string]struct{})}
}

// add registers a mux pattern ("METHOD /path" or "/path") as open. The
// method is dropped so a method mismatch on an open path still skips auth
// and reaches the real router's 405, rather than surfacing as a 401.
func (o *openRoutes) add(pattern string) {
	path := pattern
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		path = strings.TrimSpace(pattern[i+1:])
	}
	if _, dup := o.paths[path]; dup {
		return
	}
	o.paths[path] = struct{}{}
	o.mux.HandleFunc(path, func(http.ResponseWriter, *http.Request) {})
}

// skip reports whether r's path is one of the open routes. ServeMux.Handler
// returns an empty pattern for anything it would not route to a registered
// pattern (including its own redirect and not-found handlers), which is
// exactly the "not exempt" answer.
func (o *openRoutes) skip(r *http.Request) bool {
	if o == nil {
		return false
	}
	_, pattern := o.mux.Handler(r)
	return pattern != ""
}
