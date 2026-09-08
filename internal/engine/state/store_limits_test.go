package state

import (
	"fmt"
	"testing"
	"time"
)

// TestMemoryStore_MaxSessionsEvictsLRU is the audit H-06 guard for the
// session count: past the cap the least-recently-used sessions go, and the
// store never exceeds it.
func TestMemoryStore_MaxSessionsEvictsLRU(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	s.SetLimits(32, 0)
	for i := 0; i < 32; i++ {
		s.GetOrCreate(fmt.Sprintf("s%d", i), "a")
		time.Sleep(time.Millisecond) // distinct LastAccess ordering
	}
	// Touch s0 so it is the most recently used despite being oldest.
	s.GetOrCreate("s0", "a").AppendUserMessage("hi")

	s.GetOrCreate("s32", "a")
	if n := s.Count(); n > 32 {
		t.Fatalf("Count = %d, want <= 32 after eviction", n)
	}
	if s.Get("s0") == nil {
		t.Fatal("recently used s0 was evicted")
	}
	if s.Get("s32") == nil {
		t.Fatal("newest session missing")
	}
	// The oldest untouched session (s1) is the eviction candidate.
	if s.Get("s1") != nil {
		t.Fatal("least-recently-used s1 survived eviction")
	}

	// Unlimited: no eviction at all.
	u := NewMemoryStore(time.Hour)
	u.SetLimits(0, 0)
	for i := 0; i < 200; i++ {
		u.GetOrCreate(fmt.Sprintf("u%d", i), "a")
	}
	if u.Count() != 200 {
		t.Fatalf("unlimited store Count = %d, want 200", u.Count())
	}
}

// TestMemoryStore_ExpiredEvictedBeforeLive: at the cap, expired sessions are
// reclaimed first so a live one is never sacrificed for a dead one.
func TestMemoryStore_ExpiredEvictedBeforeLive(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	s.SetLimits(2, 0)
	dead := s.GetOrCreate("dead", "a")
	dead.WithLocked(func() { dead.TTL = time.Nanosecond; dead.LastAccess = time.Now().Add(-time.Hour) })
	live := s.GetOrCreate("live", "a")
	s.GetOrCreate("new", "a")
	if s.Get("live") != live {
		t.Fatal("live session evicted while an expired one existed")
	}
	if s.Get("dead") != nil {
		t.Fatal("expired session survived")
	}
}

// TestSession_MaxHistoryTrimsOldest: a pinned session keeps only the newest
// MaxHistory messages while TurnCount keeps counting.
func TestSession_MaxHistoryTrimsOldest(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	s.SetLimits(0, 4)
	sess := s.GetOrCreate("pinned", "a")
	for i := 0; i < 5; i++ {
		err := sess.ApplyTurn(fmt.Sprintf("u%d", i), func(turn int, _ map[string]any) (string, []ToolCallMsg, error) {
			return fmt.Sprintf("a%d", i), nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if sess.TurnCount != 5 {
		t.Fatalf("TurnCount = %d, want 5", sess.TurnCount)
	}
	if len(sess.Messages) != 4 {
		t.Fatalf("len(Messages) = %d, want 4", len(sess.Messages))
	}
	if sess.Messages[0].Content != "u3" || sess.Messages[3].Content != "a4" {
		t.Fatalf("retained window = %q..%q, want u3..a4", sess.Messages[0].Content, sess.Messages[3].Content)
	}
	if got := sess.LatestUserMessage(); got != "u4" {
		t.Fatalf("LatestUserMessage = %q, want u4", got)
	}
}
