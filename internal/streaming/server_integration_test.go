package streaming_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/server"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestServer(t *testing.T, agents ...*types.AgentDefinition) string {
	t.Helper()
	registry := engine.NewAgentRegistry()
	for _, a := range agents {
		registry.Register(a)
	}
	store := state.NewMemoryStore(5 * time.Minute)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(registry, store, logger)

	cfg := server.DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()

	srv := server.New(eng, cfg, logger)
	// Bind synchronously so ListenAddr is race-free when we read it
	// immediately after. See server.Listen / server.Serve for details.
	require.NoError(t, srv.Listen())
	addr := fmt.Sprintf("http://%s", srv.ListenAddr())
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { srv.Shutdown() })
	return addr
}

func streamingAgent(protocol string) *types.AgentDefinition {
	return &types.AgentDefinition{
		APIVersion: "mockagents/v1",
		Kind:       "Agent",
		Metadata:   types.Metadata{Name: "stream-agent"},
		Spec: types.AgentSpec{
			Protocol: protocol,
			Model:    "test-model",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{
					{
						Name:  "greeting",
						Match: &types.MatchRule{ContentContains: "hello"},
						Response: types.ScenarioResponse{
							Content: "Hello! How can I help you today?",
						},
					},
					{
						Name:  "tool-use",
						Match: &types.MatchRule{ContentContains: "weather"},
						Response: types.ScenarioResponse{
							Content: "Let me check.",
							ToolCalls: []types.ToolCallSpec{
								{Name: "get_weather", Arguments: map[string]any{"city": "NYC"}},
							},
						},
					},
					{
						Name:     "default",
						Response: types.ScenarioResponse{Content: "I'm here to help."},
					},
				},
				Streaming: &types.StreamingConfig{
					Enabled:      true,
					ChunkSize:    2,
					ChunkDelayMs: 0,
				},
			},
			Tools: []types.ToolDefinition{
				{
					Name: "get_weather",
					Responses: []types.ToolResponseRule{
						{IsDefault: true, Response: map[string]any{"temp": 72}},
					},
				},
			},
		},
	}
}

func TestServerIntegration_OpenAIStreaming(t *testing.T) {
	addr := startTestServer(t, streamingAgent("openai-chat-completions"))

	body, _ := json.Marshal(engine.InboundRequest{
		AgentName: "stream-agent",
		SessionID: "s1",
		Messages:  []engine.RequestMessage{{Role: "user", Content: "hello"}},
		Stream:    true,
	})

	resp, err := http.Post(addr+"/v1/engines/process", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Read all SSE data lines.
	var dataLines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}

	require.GreaterOrEqual(t, len(dataLines), 3)
	assert.Equal(t, "[DONE]", dataLines[len(dataLines)-1])

	// Reassemble content.
	var content strings.Builder
	for _, line := range dataLines {
		if line == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		choices, ok := chunk["choices"].([]any)
		if !ok || len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]any)
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			continue
		}
		if c, ok := delta["content"].(string); ok {
			content.WriteString(c)
		}
	}
	assert.Equal(t, "Hello! How can I help you today?", content.String())
}

func TestServerIntegration_AnthropicStreaming(t *testing.T) {
	addr := startTestServer(t, streamingAgent("anthropic-messages"))

	body, _ := json.Marshal(engine.InboundRequest{
		AgentName: "stream-agent",
		SessionID: "s1",
		Messages:  []engine.RequestMessage{{Role: "user", Content: "hello"}},
		Stream:    true,
	})

	resp, err := http.Post(addr+"/v1/engines/process", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Parse events.
	var eventTypes []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventTypes = append(eventTypes, strings.TrimPrefix(line, "event: "))
		}
	}

	require.GreaterOrEqual(t, len(eventTypes), 5)
	assert.Equal(t, "message_start", eventTypes[0])
	assert.Equal(t, "message_stop", eventTypes[len(eventTypes)-1])
}

func TestServerIntegration_NonStreamingStillWorks(t *testing.T) {
	addr := startTestServer(t, streamingAgent("openai-chat-completions"))

	body, _ := json.Marshal(engine.InboundRequest{
		AgentName: "stream-agent",
		SessionID: "s1",
		Messages:  []engine.RequestMessage{{Role: "user", Content: "hello"}},
		Stream:    false,
	})

	resp, err := http.Post(addr+"/v1/engines/process", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result engine.Response
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "Hello! How can I help you today?", result.Content)
}

func TestServerIntegration_StreamingWithToolCalls(t *testing.T) {
	addr := startTestServer(t, streamingAgent("openai-chat-completions"))

	body, _ := json.Marshal(engine.InboundRequest{
		AgentName: "stream-agent",
		SessionID: "s1",
		Messages:  []engine.RequestMessage{{Role: "user", Content: "check weather"}},
		Stream:    true,
	})

	resp, err := http.Post(addr+"/v1/engines/process", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	var dataLines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}

	assert.Equal(t, "[DONE]", dataLines[len(dataLines)-1])

	// Verify tool_calls finish reason.
	var lastJSON map[string]any
	for i := len(dataLines) - 1; i >= 0; i-- {
		if dataLines[i] == "[DONE]" {
			continue
		}
		if err := json.Unmarshal([]byte(dataLines[i]), &lastJSON); err == nil {
			break
		}
	}
	choices := lastJSON["choices"].([]any)
	choice := choices[0].(map[string]any)
	assert.Equal(t, "tool_calls", choice["finish_reason"])
}
