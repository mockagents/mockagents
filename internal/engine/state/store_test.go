package state

import (
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
