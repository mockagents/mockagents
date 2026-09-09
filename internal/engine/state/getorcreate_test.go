package state

import (
	"sync"
	"testing"
	"time"
)

// TestGetOrCreateReturnsOneSessionUnderConcurrency is the correctness guard for
// the audit M-20 read-lock fast path: the hit case no longer takes the
// exclusive lock, so the create path must re-check under the write lock.
// Without that re-check, two goroutines racing on a fresh id would each build
// a session and one would silently overwrite the other, forking the
// conversation state.
func TestGetOrCreateReturnsOneSessionUnderConcurrency(t *testing.T) {
	store := NewMemoryStore(time.Minute)

	const goroutines = 64
	var wg sync.WaitGroup
	seen := make([]*Session, goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			seen[i] = store.GetOrCreate("shared-id", "agent")
		}(i)
	}
	close(start)
	wg.Wait()

	first := seen[0]
	if first == nil {
		t.Fatal("GetOrCreate returned nil")
	}
	for i, s := range seen {
		if s != first {
			t.Fatalf("goroutine %d got a different *Session for the same id; the store forked the conversation", i)
		}
	}
	if got := store.Count(); got != 1 {
		t.Errorf("store holds %d sessions for one id, want 1", got)
	}
}

// TestGetOrCreateHitReturnsTheStoredSession: the fast path must return the
// same pointer the store holds, since callers mutate it in place (the
// aliasing contract).
func TestGetOrCreateHitReturnsTheStoredSession(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	created := store.GetOrCreate("s1", "agent")
	again := store.GetOrCreate("s1", "agent")
	if created != again {
		t.Error("a second GetOrCreate returned a different pointer; in-place mutations would be lost")
	}
	if got := store.Get("s1"); got != created {
		t.Error("Get returned a different pointer than GetOrCreate")
	}
}

// TestGetOrCreateReplacesAnExpiredSession: an expired entry must not be
// returned by the fast path — it has to fall through and build a fresh one.
func TestGetOrCreateReplacesAnExpiredSession(t *testing.T) {
	store := NewMemoryStore(time.Millisecond)
	first := store.GetOrCreate("s1", "agent")
	time.Sleep(5 * time.Millisecond)

	second := store.GetOrCreate("s1", "agent")
	if second == first {
		t.Fatal("an expired session was returned by the fast path")
	}
	if second.IsExpired() {
		t.Error("the replacement session is already expired")
	}
	if got := store.Count(); got != 1 {
		t.Errorf("store holds %d sessions, want 1", got)
	}
}
