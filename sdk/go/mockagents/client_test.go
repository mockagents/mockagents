package mockagents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newFakeServer returns a minimal MockAgents look-alike: one
// httptest.Server handling /v1/chat/completions, /v1/messages, and the
// management API. Tests point a Client at its URL and assert parsing.
func newFakeServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-1",
			"model": "gpt-4o",
			"choices": [
				{
					"message": {
						"content": "pong",
						"tool_calls": [
							{"id": "call_1", "function": {"name": "lookup_order", "arguments": "{\"id\":\"ORD-1\"}"}}
						]
					},
					"finish_reason": "stop"
				}
			],
			"usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7}
		}`))
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1",
			"model": "claude-3-5-sonnet-latest",
			"content": [
				{"type": "text", "text": "hello"},
				{"type": "tool_use", "id": "tu_1", "name": "search", "input": {"q": "cats"}}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 4}
		}`))
	})
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","version":"test"}`))
	})
	mux.HandleFunc("/api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"a1","model":"gpt-4o","protocol":"openai-chat-completions","scenario_count":1,"tool_count":0}]`))
	})
	mux.HandleFunc("/api/v1/agents/boom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})
	return httptest.NewServer(mux)
}

func TestClientChatParsesResponse(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	client := NewClient(ClientOptions{BaseURL: srv.URL})
	resp, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "ping"}}, ChatOptions{SessionID: "sess-42"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "pong" {
		t.Errorf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup_order" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Arguments["id"] != "ORD-1" {
		t.Errorf("arg id = %v", resp.ToolCalls[0].Arguments["id"])
	}
	if resp.Usage.TotalTokens != 7 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if resp.LatencyMs <= 0 {
		t.Errorf("latency should be > 0, got %v", resp.LatencyMs)
	}
}

func TestClientChatSendsSessionHeader(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Session-Id")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""},"finish_reason":""}]}`))
	}))
	defer srv.Close()

	client := NewClient(ClientOptions{BaseURL: srv.URL})
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, ChatOptions{SessionID: "abc"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if seen != "abc" {
		t.Errorf("X-Session-Id = %q, want abc", seen)
	}
}

func TestClientMessageJoinsTextBlocks(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	client := NewClient(ClientOptions{BaseURL: srv.URL})
	resp, err := client.Message(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, MessageOptions{})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Arguments["q"] != "cats" {
		t.Errorf("tool calls = %+v", resp.ToolCalls)
	}
	if resp.Usage.TotalTokens != 14 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestClientManagementEndpoints(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	client := NewClient(ClientOptions{BaseURL: srv.URL})
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if health["status"] != "ok" {
		t.Errorf("health = %+v", health)
	}

	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 || agents[0].Name != "a1" {
		t.Errorf("agents = %+v", agents)
	}
}

func TestClient404ReturnsHTTPError(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	client := NewClient(ClientOptions{BaseURL: srv.URL})
	_, err := client.GetAgent(context.Background(), "boom")
	if err == nil {
		t.Fatal("expected error")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if herr.Status != 404 {
		t.Errorf("status = %d", herr.Status)
	}
}

// --- parser round-trip ---

func TestParseOpenAIResponseHandlesEmptyChoices(t *testing.T) {
	raw := json.RawMessage(`{}`)
	resp, err := parseOpenAIResponse(raw, 200, 5.0)
	if err != nil {
		t.Fatalf("parseOpenAIResponse: %v", err)
	}
	if resp.Content != "" || len(resp.ToolCalls) != 0 {
		t.Errorf("unexpected content/tool-calls: %+v", resp)
	}
}

func TestParseAnthropicResponseHandlesEmptyContent(t *testing.T) {
	raw := json.RawMessage(`{}`)
	resp, err := parseAnthropicResponse(raw, 200, 5.0)
	if err != nil {
		t.Fatalf("parseAnthropicResponse: %v", err)
	}
	if resp.Content != "" || len(resp.ToolCalls) != 0 {
		t.Errorf("unexpected content/tool-calls: %+v", resp)
	}
}
