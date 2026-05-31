package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

func TestProcessRequestSameSessionConcurrentTurns(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Register(&types.AgentDefinition{
		APIVersion: "mockagents/v1",
		Kind:       "Agent",
		Metadata:   types.Metadata{Name: "turn-agent"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Model:    "turn-model",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name: "default",
						Response: types.ScenarioResponse{
							Content: "ok",
						},
					},
				},
			},
		},
	})
	store := state.NewMemoryStore(time.Minute)
	eng := NewEngine(registry, store, slog.New(slog.NewTextHandler(io.Discard, nil)))

	const requests = 50
	var wg sync.WaitGroup
	errCh := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := eng.ProcessRequestContext(context.Background(), &InboundRequest{
				Model:     "turn-model",
				SessionID: "shared-session",
				Messages:  []RequestMessage{{Role: "user", Content: fmt.Sprintf("hello %d", i)}},
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("ProcessRequestContext: %v", err)
	}

	// Anonymous requests store under the tenant-namespaced key.
	session := store.Get(scopedSessionKey("", "shared-session"))
	if session == nil {
		t.Fatal("session not found")
	}
	var turnCount, messageCount int
	session.WithLocked(func() {
		turnCount = session.TurnCount
		messageCount = len(session.Messages)
	})
	if turnCount != requests {
		t.Fatalf("turn count = %d, want %d", turnCount, requests)
	}
	if got, want := messageCount, requests*2; got != want {
		t.Fatalf("messages = %d, want %d", got, want)
	}
}
