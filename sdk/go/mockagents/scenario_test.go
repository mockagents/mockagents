package mockagents

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// scenarioFakeServer returns a fake MockAgents that numbers replies so
// scenario tests can verify each turn independently.
func scenarioFakeServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var turn int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n := atomic.AddInt32(&turn, 1)
		toolBlock := ""
		if n == 1 {
			toolBlock = `, "tool_calls": [{"id":"call_1","function":{"name":"lookup_order","arguments":"{\"id\":\"ORD-1\"}"}}]`
		}
		body := fmt.Sprintf(`{
			"model":"gpt-4o",
			"choices":[{"message":{"content":"reply %d"%s},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}
		}`, n, toolBlock)
		_, _ = w.Write([]byte(body))
	}))
	return ts, &turn
}

func TestRunScenarioWalksSteps(t *testing.T) {
	srv, _ := scenarioFakeServer(t)
	defer srv.Close()

	client := NewClient(ClientOptions{BaseURL: srv.URL})
	scenario := NewScenario("two-turn", []ScenarioStep{
		{Role: "user", Content: "first"},
		{Role: "user", Content: "second"},
	})
	result, err := RunScenario(context.Background(), client, scenario)
	if err != nil {
		t.Fatalf("RunScenario: %v", err)
	}
	if len(result.Responses) != 2 {
		t.Fatalf("responses = %d, want 2", len(result.Responses))
	}
	if result.Responses[0].Content != "reply 1" || result.Responses[1].Content != "reply 2" {
		t.Errorf("unexpected contents: %+v", result.Responses)
	}
	if result.LastContent() != "reply 2" {
		t.Errorf("LastContent = %q", result.LastContent())
	}
}

func TestRunScenarioNilClient(t *testing.T) {
	_, err := RunScenario(context.Background(), nil, NewScenario("x", []ScenarioStep{{Role: "user", Content: "hi"}}))
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestNewScenarioPanicsOnEmptySteps(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewScenario("x", nil)
}
