package server

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
)

// Default sizing for the interaction-log worker pool. These are
// deliberately modest: the target deployment is a single-process mock
// server, not a fan-out ingest pipeline. Four workers saturate SQLite
// writes without oversubscribing the CPU, and a 1024-entry buffer is
// large enough to absorb a multi-thousand-request burst without
// dropping while small enough that its worst-case resident size is
// bounded (1024 * ~1 KiB per entry ≈ 1 MiB).
const (
	DefaultLogWorkerCount  = 4
	DefaultLogQueueSize    = 1024
	DefaultLogDrainTimeout = 2 * time.Second
)

// LogWorker owns a bounded queue and a fixed pool of goroutines that
// persist InteractionLog entries asynchronously. It replaces the old
// "spawn one goroutine per request" pattern, which was measured as
// 54 % cumulative GC time under load (see docs/benchmarks/README.md).
//
// Submit is non-blocking: when the queue is full the entry is dropped
// and the Dropped counter is incremented. The design intentionally
// favors request latency over observability completeness — a loaded
// mock server should never stall user requests waiting for a log
// write, and operators can watch Metrics().Dropped to know when the
// queue needs resizing.
//
// The zero value is not usable; callers must go through NewLogWorker.
type LogWorker struct {
	store       *storage.SQLiteStore
	broadcaster *LogBroadcaster
	queue       chan *storage.InteractionLog
	logger      *slog.Logger
	workers     int

	// Counters. Accessed via atomics so Metrics() is safe to call
	// from any goroutine without holding a lock.
	submitted atomic.Uint64
	written   atomic.Uint64
	dropped   atomic.Uint64
	failed    atomic.Uint64

	wg       sync.WaitGroup
	stopOnce sync.Once
	// mu serializes the stopped-check + queue-send in Submit against the
	// close(queue) in Shutdown (F-LW-001). Without it, a Submit that passes
	// the stopped check before Shutdown closes the queue could send on a
	// closed channel and panic. Submit takes RLock (the send is
	// non-blocking, so it's held only briefly); Shutdown takes the write
	// Lock before closing, so no in-flight send can overlap the close.
	mu      sync.RWMutex
	stopped atomic.Bool
}

// LogWorkerConfig tunes a LogWorker. Zero values fall back to the
// Default* constants above so the common case is `NewLogWorker(store,
// logger, LogWorkerConfig{})`.
type LogWorkerConfig struct {
	Workers   int
	QueueSize int
	// Broadcaster, when non-nil, receives every successfully-persisted
	// entry via Publish. The server wires this up for the /api/v1/logs/stream
	// SSE endpoint; tests and offline callers can leave it nil.
	Broadcaster *LogBroadcaster
}

// NewLogWorker constructs and starts a LogWorker. The returned worker
// is immediately ready to Submit entries. Callers must call Shutdown
// during server teardown to drain pending writes.
func NewLogWorker(store *storage.SQLiteStore, logger *slog.Logger, cfg LogWorkerConfig) *LogWorker {
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultLogWorkerCount
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = DefaultLogQueueSize
	}
	if logger == nil {
		logger = slog.Default()
	}
	w := &LogWorker{
		store:       store,
		broadcaster: cfg.Broadcaster,
		queue:       make(chan *storage.InteractionLog, cfg.QueueSize),
		logger:      logger,
		workers:     cfg.Workers,
	}
	w.start()
	return w
}

// Submit enqueues an entry for asynchronous write. Returns true when
// the entry was accepted into the queue and false when the queue was
// full or the worker is already shut down. A full queue increments
// the Dropped metric but never blocks the caller.
//
// Counter invariants:
//
//	attempts = Submitted + Dropped     // every call counts exactly once
//	Written + Failed <= Submitted      // persistence may still fail
//
// A nil worker is a no-op that returns true — this keeps middleware
// code simple when logging is disabled.
func (w *LogWorker) Submit(entry *storage.InteractionLog) bool {
	if w == nil || w.store == nil {
		return true
	}
	// Hold RLock so the stopped-check and the send below cannot straddle
	// Shutdown's close(queue) (F-LW-001).
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.stopped.Load() {
		w.dropped.Add(1)
		return false
	}
	// Increment Submitted before the channel send so a worker that
	// reads the entry immediately and bumps Written cannot briefly
	// violate the Written <= Submitted invariant. On a full queue we
	// roll the increment back via atomic subtract.
	w.submitted.Add(1)
	select {
	case w.queue <- entry:
		return true
	default:
		w.submitted.Add(^uint64(0)) // atomic decrement
		w.dropped.Add(1)
		return false
	}
}

// Shutdown stops accepting new entries and waits up to timeout for
// the workers to drain any pending queue. Entries still in the queue
// when timeout elapses are NOT persisted — callers that need strict
// durability should keep the timeout generous or flush via Metrics
// first.
//
// Safe to call multiple times; subsequent calls are no-ops.
func (w *LogWorker) Shutdown(timeout time.Duration) {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		// Take the write lock so the close cannot race an in-flight
		// Submit's send (F-LW-001).
		w.mu.Lock()
		w.stopped.Store(true)
		close(w.queue)
		w.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Clean drain.
	case <-time.After(timeout):
		w.logger.Warn("log worker drain timed out",
			"timeout", timeout,
			"queue_len", len(w.queue),
			"dropped", w.dropped.Load())
	}
}

// Metrics is a point-in-time snapshot of the worker's counters.
// Useful for expvar/Prometheus wiring and for tests.
type Metrics struct {
	Submitted uint64
	Written   uint64
	Dropped   uint64
	Failed    uint64
	QueueLen  int
	QueueCap  int
}

// Metrics returns a snapshot of the worker's counters.
func (w *LogWorker) Metrics() Metrics {
	if w == nil {
		return Metrics{}
	}
	return Metrics{
		Submitted: w.submitted.Load(),
		Written:   w.written.Load(),
		Dropped:   w.dropped.Load(),
		Failed:    w.failed.Load(),
		QueueLen:  len(w.queue),
		QueueCap:  cap(w.queue),
	}
}

// start spins up the worker goroutines. Each worker ranges over the
// queue until it is closed, calling Log on every entry. A per-call
// short context bounds any slow SQLite write.
func (w *LogWorker) start() {
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for entry := range w.queue {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := w.store.Log(ctx, entry); err != nil {
					w.failed.Add(1)
					w.logger.Warn("interaction log write failed", "error", err)
				} else {
					w.written.Add(1)
					// Fan out to live subscribers only after the
					// row is durable. Nil receiver is safe; the
					// Publish method short-circuits when the
					// broadcaster was not wired up.
					w.broadcaster.Publish(entry)
				}
				cancel()
			}
		}()
	}
}
