package server

import (
	"sync"
	"sync/atomic"

	"github.com/mockagents/mockagents/internal/storage"
)

// LogBroadcaster fans out interaction-log events to any number of
// subscribed readers. It exists to power the GET /api/v1/logs/stream
// SSE endpoint that replaced the 3-second poll loop in the GUI's live
// feed panel.
//
// Design: per-subscriber buffered channel. A slow subscriber drops
// events rather than blocking the publisher, so the hot path
// (LogWorker write loop) is never stalled waiting on a laggy browser
// tab. The drop count is tracked per subscriber so the SSE handler
// can surface a dedicated `event: dropped` frame when a client falls
// behind — see LogSubscription.Dropped and StreamLogs.
//
// The zero value is usable; callers can embed or var-declare a
// LogBroadcaster and call Subscribe/Publish without any init.
type LogBroadcaster struct {
	mu          sync.Mutex
	subscribers map[*LogSubscription]struct{}
	closed      bool
}

// LogSubscription is one live subscriber. The caller reads events
// via C(), checks how many have been dropped due to backpressure via
// Dropped(), and releases the subscription via Cancel (or the
// returned closure from Subscribe).
//
// The drop counter is an atomic so Dropped() reads never contend
// with the publisher side's writes on a hot publish path.
type LogSubscription struct {
	ch      chan *storage.InteractionLog
	dropped atomic.Uint64
}

// C returns the receive-only channel the caller should range over.
// Closed by Cancel.
func (s *LogSubscription) C() <-chan *storage.InteractionLog {
	return s.ch
}

// Dropped returns the cumulative number of events the publisher
// tried to deliver to this subscription but had to skip because the
// subscriber's buffer was full. Monotonically non-decreasing.
func (s *LogSubscription) Dropped() uint64 {
	return s.dropped.Load()
}

// DefaultLogStreamBuffer is the per-subscriber channel capacity.
// Small enough that a slow client cannot inflate server memory;
// large enough to absorb a short burst during a page navigation.
const DefaultLogStreamBuffer = 64

// Subscribe registers a new listener and returns the subscription
// handle plus a cancel function. The returned cancel closes the
// subscription's channel; callers must still drain it to exit
// cleanly.
//
// Passing buffer <= 0 uses DefaultLogStreamBuffer.
func (b *LogBroadcaster) Subscribe(buffer int) (*LogSubscription, func()) {
	if buffer <= 0 {
		buffer = DefaultLogStreamBuffer
	}
	sub := &LogSubscription{
		ch: make(chan *storage.InteractionLog, buffer),
	}
	b.mu.Lock()
	if b.closed {
		// Broadcaster is shutting down: hand back an already-closed
		// subscription so the caller's read loop exits immediately.
		b.mu.Unlock()
		close(sub.ch)
		return sub, func() {}
	}
	if b.subscribers == nil {
		b.subscribers = make(map[*LogSubscription]struct{})
	}
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subscribers[sub]; ok {
			delete(b.subscribers, sub)
			close(sub.ch)
		}
		b.mu.Unlock()
	}
	return sub, cancel
}

// Publish delivers an entry to every current subscriber. Slow
// subscribers drop the entry and increment their drop counter
// instead of blocking the publisher. Safe to call with a nil
// receiver (no-op) so middleware does not need to branch on "is
// broadcasting enabled".
func (b *LogBroadcaster) Publish(entry *storage.InteractionLog) {
	if b == nil || entry == nil {
		return
	}
	b.mu.Lock()
	for sub := range b.subscribers {
		select {
		case sub.ch <- entry:
		default:
			sub.dropped.Add(1)
		}
	}
	b.mu.Unlock()
}

// Close closes every active subscriber's channel and marks the broadcaster
// shut down, so the SSE handler goroutines blocked on C() exit on server
// teardown (F-SV-001). After Close, Subscribe hands back an already-closed
// subscription and Publish is a no-op. A subscriber's own cancel after
// Close is a no-op (the sub is already gone from the map). Idempotent.
func (b *LogBroadcaster) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for sub := range b.subscribers {
		close(sub.ch)
	}
	b.subscribers = nil
}

// SubscriberCount returns the number of active subscribers. Used by
// tests and the /metrics-style surfaces; not exposed to clients.
func (b *LogBroadcaster) SubscriberCount() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}

// BroadcasterSnapshot is a point-in-time view of the broadcaster's
// state, suitable for JSON encoding on a metrics endpoint. Exposes
// enough detail for operators to answer "is anyone currently
// subscribed?" and "is anyone currently falling behind?".
type BroadcasterSnapshot struct {
	SubscriberCount int                  `json:"subscriber_count"`
	TotalDropped    uint64               `json:"total_dropped"`
	MaxDropped      uint64               `json:"max_dropped"`
	Subscribers     []SubscriberSnapshot `json:"subscribers"`
}

// SubscriberSnapshot carries the per-subscription metrics returned by
// BroadcasterSnapshot. Subscribers are anonymous — the snapshot does
// not include any identifying info because the broadcaster itself
// has none to give. Operators who want to correlate a specific
// browser tab with a specific subscription can match by
// (buffer_len, dropped) pair during the observation window.
type SubscriberSnapshot struct {
	Dropped   uint64 `json:"dropped"`
	BufferCap int    `json:"buffer_cap"`
	BufferLen int    `json:"buffer_len"`
}

// Snapshot returns an ordered point-in-time view of every active
// subscription. Subscribers are ordered by drop count descending
// so the worst offenders are at the top of the list — easier to
// eyeball than an insertion-order dump.
//
// Safe to call with a nil receiver (returns a zero-value snapshot)
// so handlers don't have to branch on "is broadcasting enabled".
func (b *LogBroadcaster) Snapshot() BroadcasterSnapshot {
	if b == nil {
		return BroadcasterSnapshot{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	snap := BroadcasterSnapshot{
		SubscriberCount: len(b.subscribers),
		Subscribers:     make([]SubscriberSnapshot, 0, len(b.subscribers)),
	}
	for sub := range b.subscribers {
		dropped := sub.dropped.Load()
		snap.TotalDropped += dropped
		if dropped > snap.MaxDropped {
			snap.MaxDropped = dropped
		}
		snap.Subscribers = append(snap.Subscribers, SubscriberSnapshot{
			Dropped:   dropped,
			BufferCap: cap(sub.ch),
			BufferLen: len(sub.ch),
		})
	}
	// Sort descending by dropped so operators see worst-offender-
	// first. Ties fall back to buffer_len descending (most-full
	// channel next).
	sortSubscriberSnapshots(snap.Subscribers)
	return snap
}

// sortSubscriberSnapshots sorts in place by Dropped descending, then
// by BufferLen descending. Split out so the Snapshot method reads
// cleanly without an inline closure.
func sortSubscriberSnapshots(s []SubscriberSnapshot) {
	// Tiny N (tens at most) — insertion sort is fine and avoids
	// the sort package dependency inside the broadcaster for a
	// purely internal helper. Idiomatic sort.Slice would also
	// work; insertion sort keeps the call site obvious.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0; j-- {
			a, b := s[j-1], s[j]
			less := a.Dropped < b.Dropped ||
				(a.Dropped == b.Dropped && a.BufferLen < b.BufferLen)
			if !less {
				break
			}
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
