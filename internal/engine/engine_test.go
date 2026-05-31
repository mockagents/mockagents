package engine

import (
	"log/slog"
	"os"
	"testing"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEngine(agents ...*types.AgentDefinition) *Engine {
	registry := NewAgentRegistry()
	for _, a := range agents {
		registry.Register(a)
	}
	store := state.NewMemoryStore(0)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewEngine(registry, store, logger)
}

func fullAgent(name, model string) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: name},
		Spec: types.AgentSpec{
			Model: model,
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "greeting",
						Match: &types.MatchRule{ContentContains: "hello"},
						Response: types.ScenarioResponse{
							Content: "Hi there!",
						},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "How can I help?"},
					},
				},
			},
		},
	}
}

func TestEngine_ProcessRequest_BasicFlow(t *testing.T) {
	eng := newTestEngine(fullAgent("test-agent", "gpt-4o"))

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "hello world"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hi there!", resp.Content)
	assert.Equal(t, "greeting", resp.ScenarioName)
	assert.Equal(t, "test-agent", resp.AgentName)
	assert.Equal(t, "gpt-4o", resp.Model)
}

func TestEngine_ProcessRequest_DefaultScenario(t *testing.T) {
	eng := newTestEngine(fullAgent("test-agent", "gpt-4o"))

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "what's the weather?"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "How can I help?", resp.Content)
	assert.Equal(t, "default", resp.ScenarioName)
}

func TestEngine_ProcessRequest_ResolveByModel(t *testing.T) {
	eng := newTestEngine(fullAgent("test-agent", "gpt-4o"))

	resp, err := eng.ProcessRequest(&InboundRequest{
		Model:     "gpt-4o",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "test-agent", resp.AgentName)
}

func TestEngine_ProcessRequest_SingleAgentDefault(t *testing.T) {
	eng := newTestEngine(fullAgent("only-agent", "some-model"))

	resp, err := eng.ProcessRequest(&InboundRequest{
		Model:     "nonexistent-model",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "only-agent", resp.AgentName)
}

func TestEngine_ProcessRequest_AgentNotFound(t *testing.T) {
	eng := newTestEngine(
		fullAgent("agent-a", "model-a"),
		fullAgent("agent-b", "model-b"),
	)

	_, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "nonexistent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
	})
	assert.ErrorIs(t, err, ErrAgentNotFound)
}

func TestEngine_ProcessRequest_EmptyMessage(t *testing.T) {
	eng := newTestEngine(fullAgent("test-agent", "gpt-4o"))

	_, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-1",
		Messages:  []RequestMessage{{Role: "user", Content: ""}},
	})
	assert.ErrorIs(t, err, ErrEmptyMessage)
}

func TestEngine_ProcessRequest_NoMessages(t *testing.T) {
	eng := newTestEngine(fullAgent("test-agent", "gpt-4o"))

	_, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-1",
		Messages:  nil,
	})
	assert.ErrorIs(t, err, ErrEmptyMessage)
}

func TestEngine_ProcessRequest_SessionPersistence(t *testing.T) {
	eng := newTestEngine(fullAgent("test-agent", "gpt-4o"))

	// First request.
	_, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-persist",
		Messages:  []RequestMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)

	// Second request on same session.
	_, err = eng.ProcessRequest(&InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-persist",
		Messages:  []RequestMessage{{Role: "user", Content: "what?"}},
	})
	require.NoError(t, err)

	// Verify session state has accumulated. Sessions are stored under the
	// tenant+agent-namespaced key, so look up with the same scoping.
	session := eng.States.Get(scopedSessionKey("", "test-agent", "sess-persist"))
	require.NotNil(t, session)
	assert.Equal(t, 2, session.TurnCount)
	assert.Len(t, session.Messages, 4) // 2 user + 2 assistant
}

func TestEngine_ProcessRequest_LatestUserMessage(t *testing.T) {
	eng := newTestEngine(fullAgent("test-agent", "gpt-4o"))

	resp, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "test-agent",
		SessionID: "sess-1",
		Messages: []RequestMessage{
			{Role: "user", Content: "first message"},
			{Role: "assistant", Content: "response"},
			{Role: "user", Content: "hello"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "greeting", resp.ScenarioName)
}
