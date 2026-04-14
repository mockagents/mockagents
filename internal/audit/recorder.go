package audit

import (
	"context"
	"net/http"
)

// Recorder is a thin convenience layer over Store. It owns the
// principal-extraction policy so call sites never need to reach into
// the tenancy package themselves. The nil Recorder is a valid no-op
// — every method returns nil — so handlers can unconditionally call
// recorder.Record(...) whether or not audit is enabled.
type Recorder struct {
	Store Store
	// PrincipalFrom extracts an Actor from the request context. Set
	// by the server wiring so audit doesn't import internal/tenancy
	// (avoiding an import cycle).
	PrincipalFrom func(*http.Request) Actor
}

// NewRecorder constructs a Recorder. Pass nil for principalFn to get
// an always-"anonymous" actor — useful for single-tenant mode where
// no authenticated principal exists.
func NewRecorder(store Store, principalFn func(*http.Request) Actor) *Recorder {
	return &Recorder{Store: store, PrincipalFrom: principalFn}
}

// Record appends an event. Errors are swallowed intentionally —
// audit must never block the critical path. Operators see append
// failures via the server log when they occur.
func (r *Recorder) Record(ctx context.Context, kind EventKind, actor Actor, target, details string) {
	if r == nil || r.Store == nil {
		return
	}
	_ = r.Store.Append(ctx, &Event{
		Kind:    kind,
		Actor:   actor,
		Target:  target,
		Details: details,
	})
}

// RecordHTTP is the variant handlers call. It extracts the actor
// from the request via PrincipalFrom (falling back to "anonymous" on
// a nil fn) and stamps the remote IP automatically.
func (r *Recorder) RecordHTTP(req *http.Request, kind EventKind, target, details string) {
	if r == nil || r.Store == nil {
		return
	}
	var actor Actor
	if r.PrincipalFrom != nil {
		actor = r.PrincipalFrom(req)
	}
	if actor.Name == "" {
		actor.Name = "anonymous"
	}
	if actor.RemoteIP == "" {
		actor.RemoteIP = clientIP(req)
	}
	r.Record(req.Context(), kind, actor, target, details)
}

// clientIP extracts a best-effort remote IP. We prefer the Go stdlib
// RemoteAddr but fall back to X-Forwarded-For when present (common
// behind Kubernetes ingresses). Intentionally does not strip the port
// — the raw value is more informative in an audit context.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
