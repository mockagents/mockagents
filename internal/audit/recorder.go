package audit

import (
	"context"
	"net/http"

	"github.com/mockagents/mockagents/internal/clientip"
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

// Record appends an event synchronously. Errors are swallowed intentionally
// — audit must never block the critical path. Control-plane mutations
// (agent/key/tenant writes) use this path: they are rare and the caller
// expects the event to be queryable immediately. High-volume events that an
// unauthenticated client can trigger (auth denials) go through AsyncWriter
// instead — see EventFromHTTP.
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
// a nil fn) and stamps the client IP automatically.
func (r *Recorder) RecordHTTP(req *http.Request, kind EventKind, target, details string) {
	if r == nil || r.Store == nil {
		return
	}
	e := r.EventFromHTTP(req, kind, target, details)
	r.Record(req.Context(), kind, e.Actor, target, details)
}

// EventFromHTTP builds the event RecordHTTP would append, without appending
// it, so a caller can hand it to an AsyncWriter. Safe on a nil Recorder
// (the actor is then "anonymous" with the client IP).
func (r *Recorder) EventFromHTTP(req *http.Request, kind EventKind, target, details string) *Event {
	var actor Actor
	if r != nil && r.PrincipalFrom != nil {
		actor = r.PrincipalFrom(req)
	}
	if actor.Name == "" {
		actor.Name = "anonymous"
	}
	if actor.RemoteIP == "" {
		// Trusted-proxy aware (audit M-15): a client cannot choose its own
		// attribution by sending X-Forwarded-For.
		actor.RemoteIP = clientip.FromRequest(req)
	}
	return &Event{Kind: kind, Actor: actor, Target: target, Details: details}
}
