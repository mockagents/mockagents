package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// blockingStore lets a test hold the writer goroutine so the queue fills.
type blockingStore struct {
	Store
	mu      sync.Mutex
	release chan struct{}
	seen    []*Event
	fail    bool
}

func (b *blockingStore) Append(_ context.Context, e *Event) error {
	<-b.release
	if b.fail {
		return errors.New("disk full")
	}
	b.mu.Lock()
	b.seen = append(b.seen, e)
	b.mu.Unlock()
	return nil
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestAsyncWriter_NeverBlocksAndCountsDrops is the audit M-08 guard: a full
// queue drops rather than blocking the request goroutine, drops are
// counted, and everything that fitted is eventually written.
func TestAsyncWriter_NeverBlocksAndCountsDrops(t *testing.T) {
	bs := &blockingStore{release: make(chan struct{})}
	w := NewAsyncWriter(bs, 4, discard())

	// The goroutine takes one event and blocks on Append; 4 more fill the
	// queue; the rest must be dropped immediately.
	accepted := 0
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < 20; i++ {
		if w.Submit(&Event{Kind: EventAuthDenied, Actor: Actor{Name: "anonymous"}}) {
			accepted++
		}
		if time.Now().After(deadline) {
			t.Fatal("Submit blocked")
		}
	}
	if accepted < 4 || accepted > 5 {
		t.Fatalf("accepted %d, want 4-5 (queue 4 + the one in flight)", accepted)
	}
	if got := int(w.Dropped()); got != 20-accepted {
		t.Fatalf("Dropped = %d, want %d", got, 20-accepted)
	}

	close(bs.release)
	w.Stop(2 * time.Second)
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if len(bs.seen) != accepted || int(w.Written()) != accepted {
		t.Fatalf("written %d / seen %d, want %d", w.Written(), len(bs.seen), accepted)
	}
	// After Stop a Submit is a counted drop, not a panic.
	if w.Submit(&Event{Kind: EventAuthDenied}) {
		t.Fatal("Submit after Stop accepted")
	}
}

func TestAsyncWriter_NilSafeAndStoreFailure(t *testing.T) {
	var nilW *AsyncWriter
	if nilW.Submit(&Event{}) || nilW.Dropped() != 0 {
		t.Fatal("nil writer must be a no-op")
	}
	nilW.Stop(time.Millisecond)

	bs := &blockingStore{release: make(chan struct{}), fail: true}
	close(bs.release)
	w := NewAsyncWriter(bs, 8, discard())
	for i := 0; i < 3; i++ {
		w.Submit(&Event{Kind: EventAuthDenied})
	}
	w.Stop(2 * time.Second)
	if w.Written() != 0 || w.failed.Load() != 3 {
		t.Fatalf("written=%d failed=%d, want 0/3", w.Written(), w.failed.Load())
	}
}

// TestPruneToMaxRows keeps exactly the newest N rows.
func TestPruneToMaxRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := s.Append(ctx, &Event{Kind: EventAuthDenied, Actor: Actor{Name: "anonymous"}, Target: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.PruneToMaxRows(ctx, 3)
	if err != nil || n != 7 {
		t.Fatalf("pruned %d err=%v, want 7", n, err)
	}
	rows, err := s.List(ctx, Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Target != "j" || rows[2].Target != "h" {
		t.Fatalf("remaining = %v", rows)
	}
	if n, err := s.PruneToMaxRows(ctx, 3); err != nil || n != 0 {
		t.Fatalf("second prune = %d err=%v, want 0", n, err)
	}
	if n, err := s.PruneToMaxRows(ctx, 0); err != nil || n != 0 {
		t.Fatalf("maxRows 0 must be a no-op, got %d err=%v", n, err)
	}
}
