// Package trace records a span tree per run: workflow, steps, agent calls,
// individual attempts, tool executions, router decisions, guards and review
// gates. The UI renders it as a waterfall, and the CLI as an indented tree.
//
// It is deliberately tiny and in-process. A production system would export
// the same structure to OpenTelemetry; the span kinds and attributes here map
// one-to-one onto OTel spans.
package trace

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

// Span kinds.
const (
	KindWorkflow = "workflow"
	KindStep     = "step"
	KindAgent    = "agent"
	KindRouter   = "router"
	KindLLM      = "llm"
	KindAttempt  = "attempt"
	KindTool     = "tool"
	KindGuard    = "guard"
	KindReview   = "review"
)

// Span statuses.
const (
	StatusRunning = "running"
	StatusOK      = "ok"
	StatusError   = "error"
)

// Event is a timestamped annotation inside a span.
type Event struct {
	Time  time.Time      `json:"time"`
	Name  string         `json:"name"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Span is one timed unit of work.
type Span struct {
	ID       string         `json:"id"`
	ParentID string         `json:"parent_id,omitempty"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Start    time.Time      `json:"start"`
	End      *time.Time     `json:"end,omitempty"`
	Status   string         `json:"status"`
	Error    string         `json:"error,omitempty"`
	Attrs    map[string]any `json:"attrs,omitempty"`
	Events   []Event        `json:"events,omitempty"`
}

// DurationMS returns the span duration so far, in milliseconds.
func (s Span) DurationMS() int64 {
	end := time.Now()
	if s.End != nil {
		end = *s.End
	}
	return end.Sub(s.Start).Milliseconds()
}

// Trace collects the spans of one run.
type Trace struct {
	mu       sync.Mutex
	spans    []*Span
	next     atomic.Int64
	onChange func()
}

// New returns an empty trace. onChange, if set, is called after every
// mutation. The workflow engine uses it as a heartbeat and to push live
// updates.
func New(onChange func()) *Trace { return &Trace{onChange: onChange} }

type ctxKey struct{}

type ctxVal struct {
	t      *Trace
	spanID string
}

// WithTrace attaches t to ctx as the root.
func WithTrace(ctx context.Context, t *Trace) context.Context {
	return context.WithValue(ctx, ctxKey{}, ctxVal{t: t})
}

// FromContext returns the trace in ctx, or nil.
func FromContext(ctx context.Context) *Trace {
	if v, ok := ctx.Value(ctxKey{}).(ctxVal); ok {
		return v.t
	}
	return nil
}

// Handle mutates one span. A nil *Handle (no trace in context) is a valid
// no-op, so instrumented code never needs to check.
type Handle struct {
	t    *Trace
	span *Span
}

// Start opens a child span of the span in ctx and returns a derived context.
// When ctx carries no trace it returns ctx unchanged and a nil handle.
func Start(ctx context.Context, name, kind string, attrs map[string]any) (context.Context, *Handle) {
	v, ok := ctx.Value(ctxKey{}).(ctxVal)
	if !ok || v.t == nil {
		return ctx, nil
	}
	t := v.t
	s := &Span{
		ID:       fmt.Sprintf("s%03d", t.next.Add(1)),
		ParentID: v.spanID,
		Name:     name,
		Kind:     kind,
		Start:    time.Now(),
		Status:   StatusRunning,
		Attrs:    maps.Clone(attrs),
	}
	t.mu.Lock()
	t.spans = append(t.spans, s)
	t.mu.Unlock()
	t.changed()
	return context.WithValue(ctx, ctxKey{}, ctxVal{t: t, spanID: s.ID}), &Handle{t: t, span: s}
}

// Set records an attribute.
func (h *Handle) Set(key string, value any) {
	if h == nil {
		return
	}
	h.t.mu.Lock()
	if h.span.Attrs == nil {
		h.span.Attrs = map[string]any{}
	}
	h.span.Attrs[key] = value
	h.t.mu.Unlock()
	h.t.changed()
}

// Event appends a timestamped event.
func (h *Handle) Event(name string, attrs map[string]any) {
	if h == nil {
		return
	}
	h.t.mu.Lock()
	h.span.Events = append(h.span.Events, Event{Time: time.Now(), Name: name, Attrs: maps.Clone(attrs)})
	h.t.mu.Unlock()
	h.t.changed()
}

// End closes the span; err != nil marks it failed. Ending twice is a no-op.
func (h *Handle) End(err error) {
	if h == nil {
		return
	}
	h.t.mu.Lock()
	if h.span.End != nil {
		h.t.mu.Unlock()
		return
	}
	now := time.Now()
	h.span.End = &now
	if err != nil {
		h.span.Status = StatusError
		h.span.Error = err.Error()
	} else {
		h.span.Status = StatusOK
	}
	h.t.mu.Unlock()
	h.t.changed()
}

// ID returns the span id ("" for a nil handle).
func (h *Handle) ID() string {
	if h == nil {
		return ""
	}
	return h.span.ID
}

// Snapshot returns a deep copy of every span, in start order.
func (t *Trace) Snapshot() []Span {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Span, len(t.spans))
	for i, s := range t.spans {
		c := *s
		c.Attrs = maps.Clone(s.Attrs)
		c.Events = append([]Event(nil), s.Events...)
		if s.End != nil {
			e := *s.End
			c.End = &e
		}
		out[i] = c
	}
	return out
}

// CloseOpen ends every still-running span with err. The engine calls it when
// a run terminates, so a trace never shows a span "running" forever.
func (t *Trace) CloseOpen(err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	now := time.Now()
	for _, s := range t.spans {
		if s.End == nil {
			s.End = &now
			s.Status = StatusError
			if err != nil {
				s.Error = err.Error()
			} else {
				s.Error = "closed when the run ended"
			}
		}
	}
	t.mu.Unlock()
	t.changed()
}

// Restore rebuilds a trace from a snapshot (state-file recovery).
func Restore(spans []Span) *Trace {
	t := &Trace{}
	for i := range spans {
		s := spans[i]
		t.spans = append(t.spans, &s)
	}
	t.next.Store(int64(len(spans)))
	return t
}

func (t *Trace) changed() {
	if t.onChange != nil {
		t.onChange()
	}
}

// SetOnChange replaces the change callback (used after Restore).
func (t *Trace) SetOnChange(fn func()) { t.onChange = fn }
