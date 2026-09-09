package mcp

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock for the session sweep.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestSessionManager(t *testing.T, max int, ttl time.Duration) (*sessionManager, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	m := newSessionManagerTTL(max, ttl)
	m.now = clk.now
	return m, clk
}

// TestSessionIdleExpiry is the audit M-30 guard: a client that never sends
// DELETE used to leave its session (and its 1024-event replay log) resident
// until 256 newer sessions pushed it out.
func TestSessionIdleExpiry(t *testing.T) {
	m, clk := newTestSessionManager(t, 16, time.Minute)
	s := m.create()

	clk.advance(90 * time.Second)
	if _, ok := m.get(s.id); ok {
		t.Fatalf("session idle past its TTL should be gone")
	}
	if !s.closed {
		t.Errorf("expired session should be closed")
	}
	m.mu.Lock()
	live := len(m.sessions)
	order := len(m.order)
	m.mu.Unlock()
	if live != 0 || order != 0 {
		t.Errorf("expired session left behind: sessions=%d order=%d", live, order)
	}
}

// TestSessionUseDefersExpiry: an active client keeps its session alive.
func TestSessionUseDefersExpiry(t *testing.T) {
	m, clk := newTestSessionManager(t, 16, time.Minute)
	s := m.create()

	// Use it every 40s across a span far longer than the 60s TTL.
	for i := 0; i < 5; i++ {
		clk.advance(40 * time.Second)
		if _, ok := m.get(s.id); !ok {
			t.Fatalf("session expired after %d touches despite continuous use", i)
		}
	}
	clk.advance(2 * time.Minute)
	if _, ok := m.get(s.id); ok {
		t.Errorf("session should expire once the client stops using it")
	}
}

// TestIdleSessionsEvictedBeforeLiveOnes: FIFO alone evicts the OLDEST session,
// which may be the one in active use. Idle sessions must be reclaimed first.
func TestIdleSessionsEvictedBeforeLiveOnes(t *testing.T) {
	m, clk := newTestSessionManager(t, 2, time.Minute)

	abandoned := m.create()
	clk.advance(30 * time.Second)
	active := m.create()

	// The abandoned session crosses the TTL; the active one keeps being used.
	clk.advance(45 * time.Second)
	if _, ok := m.get(active.id); !ok {
		t.Fatalf("active session should still be live")
	}

	fresh := m.create() // at cap: reclaim the abandoned one, not the active one
	if _, ok := m.get(abandoned.id); ok {
		t.Errorf("abandoned session should have been reclaimed")
	}
	if _, ok := m.get(active.id); !ok {
		t.Errorf("active session was evicted while an idle session held a slot")
	}
	if _, ok := m.get(fresh.id); !ok {
		t.Errorf("newest session should be live")
	}
}

// TestSessionFIFOStillAppliesWhenNothingIsIdle keeps the existing cap behavior
// honest: with every session in use, the oldest still goes.
func TestSessionFIFOStillAppliesWhenNothingIsIdle(t *testing.T) {
	m, _ := newTestSessionManager(t, 2, time.Hour)
	s1 := m.create()
	s2 := m.create()
	s3 := m.create()
	if _, ok := m.get(s1.id); ok {
		t.Errorf("s1 should have been evicted by the FIFO cap")
	}
	if _, ok := m.get(s2.id); !ok {
		t.Errorf("s2 should still be live")
	}
	if _, ok := m.get(s3.id); !ok {
		t.Errorf("s3 should be live")
	}
}

// TestStreamable_ExpiredSessionIs404 drives expiry through the HTTP surface: a
// stale Mcp-Session-Id must get the same 404 as an unknown one, so the client
// knows to reinitialize.
func TestStreamable_ExpiredSessionIs404(t *testing.T) {
	srv, h := newStreamableTestServer(t)
	h.SetSessionIdleTTL(time.Millisecond)

	sid := initSession(t, srv.URL+"/mcp")
	time.Sleep(10 * time.Millisecond)

	resp := postJSON(t, srv.URL+"/mcp", sid, "application/json", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for an expired session", resp.StatusCode)
	}
}
