package engine

import (
	"sync"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeAgent(name, model string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: name},
		Spec:     types.AgentSpec{Model: model},
	}
}

func TestAgentRegistry_RegisterAndGet(t *testing.T) {
	r := NewAgentRegistry()
	agent := makeAgent("test-agent", "gpt-4o")

	r.Register(agent)
	got := r.Get("test-agent")
	require.NotNil(t, got)
	assert.Equal(t, "test-agent", got.Metadata.Name)
}

func TestAgentRegistry_GetNotFound(t *testing.T) {
	r := NewAgentRegistry()
	assert.Nil(t, r.Get("nonexistent"))
}

func TestAgentRegistry_GetByModel(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeAgent("agent-a", "gpt-4o"))
	r.Register(makeAgent("agent-b", "claude-3-opus"))

	got := r.GetByModel("claude-3-opus")
	require.NotNil(t, got)
	assert.Equal(t, "agent-b", got.Metadata.Name)
}

func TestAgentRegistry_GetByModelNotFound(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeAgent("agent-a", "gpt-4o"))
	assert.Nil(t, r.GetByModel("nonexistent"))
}

func TestAgentRegistry_List(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeAgent("bravo", "m1"))
	r.Register(makeAgent("alpha", "m2"))
	r.Register(makeAgent("charlie", "m3"))

	list := r.List()
	require.Len(t, list, 3)
	assert.Equal(t, "alpha", list[0].Metadata.Name)
	assert.Equal(t, "bravo", list[1].Metadata.Name)
	assert.Equal(t, "charlie", list[2].Metadata.Name)
}

func TestAgentRegistry_ListNames(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeAgent("bravo", "m1"))
	r.Register(makeAgent("alpha", "m2"))

	names := r.ListNames()
	assert.Equal(t, []string{"alpha", "bravo"}, names)
}

func TestAgentRegistry_Remove(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeAgent("test", "m"))

	err := r.Remove("test")
	assert.NoError(t, err)
	assert.Nil(t, r.Get("test"))
	assert.Equal(t, 0, r.Count())
}

func TestAgentRegistry_RemoveNotFound(t *testing.T) {
	r := NewAgentRegistry()
	err := r.Remove("nonexistent")
	assert.Error(t, err)
}

func TestAgentRegistry_Replace(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(makeAgent("test", "old-model"))
	r.Register(makeAgent("test", "new-model"))

	got := r.Get("test")
	assert.Equal(t, "new-model", got.Spec.Model)
	assert.Equal(t, 1, r.Count())
}

func TestAgentRegistry_RegisterNilIgnored(t *testing.T) {
	// F-AR-004: a nil def must not panic on the Name deref under the lock.
	r := NewAgentRegistry()
	assert.NotPanics(t, func() { r.Register(nil) })
	assert.Equal(t, 0, r.Count())
	// A real agent still registers normally afterwards.
	r.Register(makeAgent("ok", "m"))
	assert.Equal(t, 1, r.Count())
}

func TestAgentRegistry_ConcurrentAccess(t *testing.T) {
	r := NewAgentRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "agent-" + string(rune('a'+i%26))
			r.Register(makeAgent(name, "model"))
			r.Get(name)
			r.List()
			r.Count()
		}(i)
	}
	wg.Wait()
	assert.Greater(t, r.Count(), 0)
}
