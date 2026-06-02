package server

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
)

func newWorkerStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "worker_test.db")
	s, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func makeEntry(i int) *storage.InteractionLog {
	return &storage.InteractionLog{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		ResponseStatus: 200,
		AgentName:      "bench",
	}
}

// TestLogWorker_SubmitAndDrain covers the happy path: N entries in,
// Shutdown, N rows persisted, metrics reflect the flow.
func TestLogWorker_SubmitAndDrain(t *testing.T) {
	store := newWorkerStore(t)
	w := NewLogWorker(store, nil, LogWorkerConfig{Workers: 2, QueueSize: 32})

	const total = 10
	for i := 0; i < total; i++ {
		if !w.Submit(makeEntry(i)) {
			t.Fatalf("submit %d unexpectedly returned false", i)
		}
	}
	w.Shutdown(2 * time.Second)

	m := w.Metrics()
	if m.Submitted != total {
		t.Errorf("submitted = %d, want %d", m.Submitted, total)
	}
	if m.Written != total {
		t.Errorf("written = %d, want %d", m.Written, total)
	}
	if m.Dropped != 0 {
		t.Errorf("dropped = %d, want 0", m.Dropped)
	}
	if m.Failed != 0 {
		t.Errorf("failed = %d, want 0", m.Failed)
	}

	n, err := store.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != int64(total) {
		t.Errorf("store count = %d, want %d", n, total)
	}
}

// TestLogWorker_DropsOnOverflow pushes entries faster than the workers
// can drain and verifies the overflow policy: Submit returns false and
// the Dropped counter advances, but in-flight rows still land.
func TestLogWorker_DropsOnOverflow(t *testing.T) {
	store := newWorkerStore(t)
	// Single worker + tiny queue makes overflow deterministic. We
	// do NOT rely on the submit rate vs worker drain rate, which is
	// machine-sensitive; instead we fire entries faster than the
	// single worker can drain a 2-slot queue and assert at least one
	// drop.
	w := NewLogWorker(store, nil, LogWorkerConfig{Workers: 1, QueueSize: 2})

	const burst = 200
	for i := 0; i < burst; i++ {
		w.Submit(makeEntry(i))
	}
	w.Shutdown(2 * time.Second)

	m := w.Metrics()
	if m.Dropped == 0 {
		t.Errorf("expected at least one drop on a burst of %d into q=2, got 0; submitted=%d written=%d",
			burst, m.Submitted, m.Written)
	}
	if m.Submitted+m.Dropped > uint64(burst) {
		// Submitted is incremented before the select; Dropped fires
		// on the default branch. They partition the burst.
		t.Errorf("submitted + dropped = %d, want <= %d", m.Submitted+m.Dropped, burst)
	}
	if m.Written > m.Submitted {
		t.Errorf("written (%d) exceeds submitted (%d)", m.Written, m.Submitted)
	}
}

// TestLogWorker_SubmitAfterShutdownDrops proves Submit is safe to call
// after Shutdown and returns false rather than panicking on a closed
// channel.
func TestLogWorker_SubmitAfterShutdownDrops(t *testing.T) {
	store := newWorkerStore(t)
	w := NewLogWorker(store, nil, LogWorkerConfig{})
	w.Shutdown(time.Second)

	if w.Submit(makeEntry(0)) {
		t.Error("Submit after Shutdown should return false")
	}
	if w.Metrics().Dropped == 0 {
		t.Error("Submit after Shutdown should increment Dropped")
	}
}

// TestLogWorker_ShutdownIdempotent calls Shutdown twice; the second
// call must be a no-op.
func TestLogWorker_ShutdownIdempotent(t *testing.T) {
	store := newWorkerStore(t)
	w := NewLogWorker(store, nil, LogWorkerConfig{})
	w.Shutdown(time.Second)
	w.Shutdown(time.Second) // must not panic (double close)
}

// TestLogWorker_NilSubmit verifies the nil-worker no-op path that
// keeps middleware wiring simple when logging is disabled.
func TestLogWorker_NilSubmit(t *testing.T) {
	var w *LogWorker
	if !w.Submit(&storage.InteractionLog{}) {
		t.Error("nil worker Submit should return true")
	}
	w.Shutdown(time.Second) // no panic
	if got := w.Metrics(); got.Submitted != 0 {
		t.Error("nil worker Metrics should be zero")
	}
}

// TestLogWorker_ConcurrentSubmit stress-tests Submit from multiple
// goroutines to shake out any race in the atomic counters or channel
// semantics. Run with -race.
func TestLogWorker_ConcurrentSubmit(t *testing.T) {
	store := newWorkerStore(t)
	w := NewLogWorker(store, nil, LogWorkerConfig{Workers: 4, QueueSize: 256})

	var wg sync.WaitGroup
	const goroutines = 16
	const perGoroutine = 50
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				w.Submit(makeEntry(i))
			}
		}()
	}
	wg.Wait()
	w.Shutdown(5 * time.Second)

	m := w.Metrics()
	// Submitted + dropped must account for every attempted call.
	// (We allow drops because a large burst may briefly overflow.)
	total := uint64(goroutines * perGoroutine)
	if m.Submitted+m.Dropped < total {
		t.Errorf("submitted (%d) + dropped (%d) < total (%d)",
			m.Submitted, m.Dropped, total)
	}
	if m.Written > m.Submitted {
		t.Errorf("written (%d) exceeds submitted (%d)", m.Written, m.Submitted)
	}
}

// TestLogWorker_SubmitDuringShutdown stresses the F-LW-001 race: many
// goroutines hammer Submit while another calls Shutdown. Before the fix,
// a Submit could send on the just-closed queue and panic. With the
// RWMutex serializing the send against the close, this must never panic
// regardless of interleaving. (-race is unavailable here, so we rely on
// volume + repetition.)
func TestLogWorker_SubmitDuringShutdown(t *testing.T) {
	for iter := 0; iter < 50; iter++ {
		store := newWorkerStore(t)
		w := NewLogWorker(store, nil, LogWorkerConfig{Workers: 2, QueueSize: 8})

		var wg sync.WaitGroup
		for g := 0; g < 16; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 200; i++ {
					w.Submit(makeEntry(i)) // must never panic
				}
			}()
		}
		// Shut down concurrently with the submitters.
		w.Shutdown(time.Second)
		wg.Wait()
	}
}
