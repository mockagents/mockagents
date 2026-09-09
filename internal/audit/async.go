package audit

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultAsyncQueueSize bounds the number of events waiting to be written by
// an AsyncWriter. Auth denials arrive in bursts (credential stuffing, a
// misconfigured SDK in a loop); the queue absorbs a burst and the writer
// drains it at SQLite's pace instead of every request goroutine fsync-ing.
const DefaultAsyncQueueSize = 4096

// AsyncWriter appends events to a Store from a single background goroutine.
// Submit never blocks: when the queue is full the event is dropped and
// counted, because the alternative — the previous behaviour — was one
// synchronous SQLite INSERT on the request goroutine for every 401/403,
// which let an unauthenticated client drive a durable write per request
// (readiness audit M-08) even though the denial-hook contract says hooks
// must not block.
type AsyncWriter struct {
	store    Store
	queue    chan *Event
	logger   *slog.Logger
	dropped  atomic.Int64
	written  atomic.Int64
	failed   atomic.Int64
	lastWarn atomic.Int64 // unix seconds of the last drop/failure warning
	closed   atomic.Bool
	done     chan struct{}
	stopOnce sync.Once
}

// NewAsyncWriter starts the writer goroutine. A nil store yields a writer
// whose Submit is a no-op, so callers need no nil checks.
func NewAsyncWriter(store Store, queueSize int, logger *slog.Logger) *AsyncWriter {
	if store == nil {
		return nil
	}
	if queueSize <= 0 {
		queueSize = DefaultAsyncQueueSize
	}
	if logger == nil {
		logger = slog.Default()
	}
	w := &AsyncWriter{
		store:  store,
		queue:  make(chan *Event, queueSize),
		logger: logger,
		done:   make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *AsyncWriter) run() {
	defer close(w.done)
	for e := range w.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := w.store.Append(ctx, e)
		cancel()
		if err != nil {
			w.failed.Add(1)
			w.warn("audit append failed", "error", err, "kind", string(e.Kind))
			continue
		}
		w.written.Add(1)
	}
}

// warn logs at most once per second so a sustained burst does not turn the
// server log into the attack surface.
func (w *AsyncWriter) warn(msg string, args ...any) {
	now := time.Now().Unix()
	last := w.lastWarn.Load()
	if now == last || !w.lastWarn.CompareAndSwap(last, now) {
		return
	}
	w.logger.Warn(msg, append(args, "dropped_total", w.dropped.Load(), "failed_total", w.failed.Load())...)
}

// Submit enqueues e without blocking. It reports false when the event was
// dropped (queue full or writer stopped) so callers can count it too.
func (w *AsyncWriter) Submit(e *Event) (ok bool) {
	if w == nil || e == nil {
		return false
	}
	if w.closed.Load() {
		w.dropped.Add(1)
		return false
	}
	// A Submit racing Stop's close of the queue would panic on send; treat
	// it as the drop it is rather than crash the request goroutine.
	defer func() {
		if r := recover(); r != nil {
			w.dropped.Add(1)
			ok = false
		}
	}()
	select {
	case w.queue <- e:
		return true
	default:
		w.dropped.Add(1)
		w.warn("audit queue full; event dropped", "kind", string(e.Kind))
		return false
	}
}

// Dropped returns how many events were discarded because the queue was full.
func (w *AsyncWriter) Dropped() int64 {
	if w == nil {
		return 0
	}
	return w.dropped.Load()
}

// Written returns how many events reached the store.
func (w *AsyncWriter) Written() int64 {
	if w == nil {
		return 0
	}
	return w.written.Load()
}

// Stop closes the queue and waits up to timeout for the backlog to drain.
// Safe to call more than once; Submit after Stop drops.
func (w *AsyncWriter) Stop(timeout time.Duration) {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		w.closed.Store(true)
		close(w.queue)
	})
	select {
	case <-w.done:
	case <-time.After(timeout):
		w.logger.Warn("audit writer did not drain before shutdown deadline", "pending", len(w.queue))
	}
}
