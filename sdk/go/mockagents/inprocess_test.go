package mockagents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAgents writes name -> YAML documents into a temp dir.
func writeAgents(t *testing.T, docs map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, doc := range docs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestInProcessClientExpandsChaosPresets: a chaos preset must fault exactly as
// it does under `mockagents start`. Before defaults were applied, the preset
// name was never expanded and the agent answered 200.
func TestInProcessClientExpandsChaosPresets(t *testing.T) {
	dir := writeAgents(t, map[string]string{"limited.yaml": `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: limited
spec:
  protocol: openai-chat-completions
  model: limited-model
  behavior:
    chaos:
      preset: rate-limited
    scenarios:
      - name: default
        response:
          content: "unreachable"
`})
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer client.Close()

	_, err = client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, ChatOptions{Model: "limited-model"})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 429 {
		t.Fatalf("expected an HTTP 429 from the rate-limited preset, got %v", err)
	}
}

// TestInProcessClientAppliesStreamingDefaults: a streaming block that omits
// chunk_size streams in default-sized (4-word) chunks, as the server does.
// (The streaming layer also falls back to that size on its own; this pins
// the parity either way.)
func TestInProcessClientAppliesStreamingDefaults(t *testing.T) {
	const content = "one two three four five six seven eight nine"
	dir := writeAgents(t, map[string]string{"streamer.yaml": `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: streamer
spec:
  protocol: openai-chat-completions
  model: stream-model
  behavior:
    streaming:
      enabled: true
      chunk_delay_ms: 0
    scenarios:
      - name: default
        response:
          content: "` + content + `"
`})
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer client.Close()

	stream, err := client.IterStream(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, IterStreamOptions{Model: "stream-model"})
	if err != nil {
		t.Fatalf("IterStream: %v", err)
	}
	defer stream.Close()
	var text strings.Builder
	chunks := 0
	for stream.Next() {
		if v := stream.Value(); v.Text != "" {
			text.WriteString(v.Text)
			chunks++
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if strings.TrimSpace(text.String()) != content {
		t.Errorf("streamed text = %q, want %q", text.String(), content)
	}
	if chunks != 3 {
		t.Errorf("chunks = %d, want 3 (9 words at the default chunk_size of 4)", chunks)
	}
}

// TestInProcessClientSkipsInvalidAgents: invalid definitions are skipped (as
// `mockagents start` does) while valid ones still load.
func TestInProcessClientSkipsInvalidAgents(t *testing.T) {
	invalid := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: broken
spec:
  protocol: openai-chat-completions
  model: broken-model
  behavior:
    scenarios: []
`
	dir := writeAgents(t, map[string]string{"broken.yaml": invalid})
	if _, err := NewInProcessClient(InProcessOptions{AgentsDir: dir}); err == nil {
		t.Fatal("a directory with only invalid agents must fail")
	}

	dir = writeTempAgent(t)
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(invalid), 0o644); err != nil {
		t.Fatal(err)
	}
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err != nil {
		t.Fatalf("one invalid agent must not block the valid one: %v", err)
	}
	defer client.Close()
	resp, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "ping"}}, ChatOptions{Model: "gpt-4o"})
	if err != nil || resp.Content != "pong" {
		t.Fatalf("valid agent: %v %+v", err, resp)
	}
	// With the invalid agent skipped, ping-agent is the only one registered,
	// so even a request for "broken-model" is served by it.
	resp, err = client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, ChatOptions{Model: "broken-model"})
	if err != nil || resp.Content != "pong" {
		t.Errorf("the invalid agent must not be registered: %v %+v", err, resp)
	}
}

// writeTempAgent drops a minimal agent YAML into a temp dir and returns
// the directory path. The agent replies with "pong" on any message.
func writeTempAgent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	yaml := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: ping-agent
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        match:
          default: true
        response:
          content: "pong"
`
	if err := os.WriteFile(filepath.Join(dir, "ping.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInProcessClientChat(t *testing.T) {
	dir := writeTempAgent(t)
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer client.Close()

	if client.BaseURL() == "" {
		t.Error("expected non-empty BaseURL")
	}

	resp, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: "user", Content: "ping"}},
		ChatOptions{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "pong" {
		t.Errorf("content = %q, want pong", resp.Content)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestInProcessClientHealth(t *testing.T) {
	dir := writeTempAgent(t)
	client, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer client.Close()

	h, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h["mode"] != "in-process" {
		t.Errorf("health = %+v", h)
	}
}

func TestInProcessClientMissingDir(t *testing.T) {
	_, err := NewInProcessClient(InProcessOptions{AgentsDir: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestInProcessClientRequiresDir(t *testing.T) {
	_, err := NewInProcessClient(InProcessOptions{})
	if err == nil {
		t.Fatal("expected error for empty AgentsDir")
	}
}

func TestInProcessClientEmptyDirFails(t *testing.T) {
	dir := t.TempDir()
	_, err := NewInProcessClient(InProcessOptions{AgentsDir: dir})
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}
