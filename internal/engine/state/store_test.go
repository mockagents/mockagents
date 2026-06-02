package state

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_GetOrCreate(t *testing.T) {
	store := NewMemoryStore(5 * time.Minute)

	s := store.GetOrCreate("sess-1", "agent-a")
	require.NotNil(t, s)
	assert.Equal(t, "sess-1", s.ID)
	assert.Equal(t, "agent-a", s.AgentName)

	// Should return the same session.
	s2 := store.GetOrCreate("sess-1", "agent-a")
	assert.Equal(t, s, s2)
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	store := NewMemoryStore(5 * time.Minute)
	assert.Nil(t, store.Get("nonexistent"))
}

func TestMemoryStore_MutationsPersistWithoutSave(t *testing.T) {
	// F-ST-004: GetOrCreate returns the store's own *Session pointer, so
	// mutating it (as ApplyTurn does) is visible to the next lookup with no
	// explicit save step. The redundant Store.Save was removed.
	store := NewMemoryStore(time.Hour)
	s := store.GetOrCreate("sess", "agent")
	s.AppendUserMessage("hello")

	again := store.GetOrCreate("sess", "agent")
	assert.Same(t, s, again)
	assert.Equal(t, 1, again.TurnCount)
	assert.Len(t, again.Messages, 1)
}

func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore(5 * time.Minute)
	store.GetOrCreate("sess-1", "agent-a")

	store.Delete("sess-1")
	assert.Nil(t, store.Get("sess-1"))
	assert.Equal(t, 0, store.Count())
}

func TestMemoryStore_Count(t *testing.T) {
	store := NewMemoryStore(5 * time.Minute)
	assert.Equal(t, 0, store.Count())

	store.GetOrCreate("s1", "a")
	store.GetOrCreate("s2", "a")
	assert.Equal(t, 2, store.Count())
}

func TestMemoryStore_TTLExpiration(t *testing.T) {
	store := NewMemoryStore(1 * time.Millisecond)
	store.GetOrCreate("sess-1", "agent-a")

	// Wait for expiration.
	time.Sleep(5 * time.Millisecond)
	assert.Nil(t, store.Get("sess-1"))
}

func TestMemoryStore_Cleanup(t *testing.T) {
	store := NewMemoryStore(1 * time.Millisecond)
	store.GetOrCreate("s1", "a")
	store.GetOrCreate("s2", "a")

	time.Sleep(5 * time.Millisecond)
	store.Cleanup()
	assert.Equal(t, 0, store.Count())
}

func TestMemoryStore_CleanupTickerStops(t *testing.T) {
	store := NewMemoryStore(1 * time.Minute)
	stop := store.StartCleanupTicker(50 * time.Millisecond)
	// Just verify it doesn't panic.
	time.Sleep(100 * time.Millisecond)
	stop()
}

func TestMemoryStore_GetExpired_DoesNotClobberFreshSession(t *testing.T) {
	// F-ST-003: when Get evicts an expired session it must delete only that
	// pointer, not a fresh session another caller stored under the same id
	// in the window between Get's RUnlock and the delete.
	store := NewMemoryStore(time.Hour)
	stale := NewSession("sess", "agent", time.Nanosecond)
	time.Sleep(time.Millisecond) // stale is now expired

	fresh := store.GetOrCreate("sess", "agent") // live, ttl 1h, same id
	// deleteIfSame is exactly what Get calls after reading a now-stale
	// pointer; the map holds `fresh`, so evicting `stale` must be a no-op.
	store.deleteIfSame("sess", stale)

	assert.Same(t, fresh, store.Get("sess"))
	assert.Equal(t, 1, store.Count())
}

func TestMemoryStore_Cleanup_RemovesExpiredKeepsActive(t *testing.T) {
	// F-ST-005: the snapshot-based Cleanup must still drop expired sessions
	// while leaving active ones untouched.
	store := NewMemoryStore(time.Hour)
	store.GetOrCreate("active", "a") // fresh, ttl 1h
	store.sessions["stale"] = NewSession("stale", "a", time.Nanosecond)
	time.Sleep(time.Millisecond)

	store.Cleanup()

	assert.NotNil(t, store.Get("active"))
	assert.Nil(t, store.Get("stale"))
	assert.Equal(t, 1, store.Count())
}

func TestMemoryStore_ConcurrentGetCreateCleanup(t *testing.T) {
	// Smoke test for deadlocks/panics on the hardened store paths. `-race`
	// is unavailable on this codebase (no cgo), so this stresses the lock
	// ordering (store -> session) by inspection-backed load instead.
	store := NewMemoryStore(2 * time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "sess-" + string(rune('a'+i%8))
			for j := 0; j < 100; j++ {
				store.GetOrCreate(id, "agent")
				store.Get(id)
				if j%10 == 0 {
					store.Cleanup()
				}
			}
		}(i)
	}
	wg.Wait()
	// No assertion on Count (sessions expire mid-run); reaching here without
	// a deadlock or panic is the signal.
	assert.GreaterOrEqual(t, store.Count(), 0)
}

func TestSession_AppendMessages(t *testing.T) {
	s := NewSession("test", "agent", 5*time.Minute)

	s.AppendUserMessage("hello")
	assert.Equal(t, 1, s.TurnCount)
	assert.Len(t, s.Messages, 1)
	assert.Equal(t, "user", s.Messages[0].Role)

	s.AppendAssistantMessage("hi", nil)
	assert.Equal(t, 1, s.TurnCount) // Only user messages increment turns.
	assert.Len(t, s.Messages, 2)
}

func TestSession_LatestUserMessage(t *testing.T) {
	s := NewSession("test", "agent", 5*time.Minute)
	assert.Equal(t, "", s.LatestUserMessage())

	s.AppendUserMessage("first")
	s.AppendAssistantMessage("response", nil)
	s.AppendUserMessage("second")

	assert.Equal(t, "second", s.LatestUserMessage())
}

func TestSession_IsExpired(t *testing.T) {
	s := NewSession("test", "agent", 1*time.Millisecond)
	assert.False(t, s.IsExpired())

	time.Sleep(5 * time.Millisecond)
	assert.True(t, s.IsExpired())
}

func TestSession_NoTTL(t *testing.T) {
	s := NewSession("test", "agent", 0)
	assert.False(t, s.IsExpired())
}

func TestSession_ToolCallMessages(t *testing.T) {
	s := NewSession("test", "agent", 5*time.Minute)
	s.AppendAssistantMessage("I'll search for that.", []ToolCallMsg{
		{Name: "search", Arguments: map[string]any{"q": "test"}},
	})
	require.Len(t, s.Messages, 1)
	require.Len(t, s.Messages[0].ToolCalls, 1)
	assert.Equal(t, "search", s.Messages[0].ToolCalls[0].Name)
}
