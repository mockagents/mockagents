package streaming

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sseEvent struct {
	EventType string
	Data      string
}

func parseSSEEvents(body string) []sseEvent {
	var events []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	var currentEvent sseEvent

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent.EventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentEvent.Data = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent.Data != "" {
			events = append(events, currentEvent)
			currentEvent = sseEvent{}
		}
	}
	return events
}

func TestStreamAnthropic_BasicContentStream(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{
		AgentName: "test-agent",
		Model:     "claude-3-opus",
		Content:   "Hello world",
	}
	cfg := &types.StreamingConfig{ChunkSize: 1, ChunkDelayMs: 0}

	err := StreamAnthropic(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))

	events := parseSSEEvents(rec.Body.String())
	require.GreaterOrEqual(t, len(events), 6)

	// Verify event sequence.
	assert.Equal(t, "message_start", events[0].EventType)
	assert.Equal(t, "content_block_start", events[1].EventType)

	// Check message_start content.
	var msgStart anthropicMessageStart
	require.NoError(t, json.Unmarshal([]byte(events[0].Data), &msgStart))
	assert.Equal(t, "message_start", msgStart.Type)
	assert.Equal(t, "assistant", msgStart.Message.Role)
	assert.Equal(t, "claude-3-opus", msgStart.Message.Model)

	// Find text deltas.
	var textContent strings.Builder
	for _, ev := range events {
		if ev.EventType == "content_block_delta" {
			var delta anthropicContentBlockDelta
			require.NoError(t, json.Unmarshal([]byte(ev.Data), &delta))
			if deltaMap, ok := delta.Delta.(map[string]any); ok {
				if deltaMap["type"] == "text_delta" {
					textContent.WriteString(deltaMap["text"].(string))
				}
			}
		}
	}
	assert.Equal(t, "Hello world", textContent.String())

	// Last events should be message_delta and message_stop.
	assert.Equal(t, "message_delta", events[len(events)-2].EventType)
	assert.Equal(t, "message_stop", events[len(events)-1].EventType)
}

func TestStreamAnthropic_EventSequence(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Model: "claude-3", Content: "Hi there"}
	cfg := &types.StreamingConfig{ChunkSize: 4, ChunkDelayMs: 0}

	err := StreamAnthropic(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	events := parseSSEEvents(rec.Body.String())
	eventTypes := make([]string, len(events))
	for i, ev := range events {
		eventTypes[i] = ev.EventType
	}

	// Expected sequence: message_start, content_block_start, content_block_delta(s),
	// content_block_stop, message_delta, message_stop
	assert.Equal(t, "message_start", eventTypes[0])
	assert.Equal(t, "content_block_start", eventTypes[1])
	assert.Equal(t, "content_block_stop", eventTypes[len(eventTypes)-3])
	assert.Equal(t, "message_delta", eventTypes[len(eventTypes)-2])
	assert.Equal(t, "message_stop", eventTypes[len(eventTypes)-1])
}

func TestStreamAnthropic_ToolUseBlocks(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{
		Model:   "claude-3",
		Content: "Checking weather.",
		ToolCalls: []types.ToolCallSpec{
			{Name: "get_weather", Arguments: map[string]any{"location": "NYC"}},
		},
		ToolResults: []engine.ToolCallResult{
			{ID: "toolu_abc123", ToolName: "get_weather"},
		},
	}
	cfg := &types.StreamingConfig{ChunkSize: 4, ChunkDelayMs: 0}

	err := StreamAnthropic(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	events := parseSSEEvents(rec.Body.String())

	// Find tool_use content_block_start.
	var hasToolBlockStart bool
	var hasInputDelta bool
	for _, ev := range events {
		if ev.EventType == "content_block_start" {
			var cbs anthropicContentBlockStart
			if err := json.Unmarshal([]byte(ev.Data), &cbs); err != nil {
				continue
			}
			if blockMap, ok := cbs.ContentBlock.(map[string]any); ok {
				if blockMap["type"] == "tool_use" {
					hasToolBlockStart = true
					assert.Equal(t, "get_weather", blockMap["name"])
				}
			}
		}
		if ev.EventType == "content_block_delta" {
			var cbd anthropicContentBlockDelta
			if err := json.Unmarshal([]byte(ev.Data), &cbd); err != nil {
				continue
			}
			if deltaMap, ok := cbd.Delta.(map[string]any); ok {
				if deltaMap["type"] == "input_json_delta" {
					hasInputDelta = true
				}
			}
		}
	}
	assert.True(t, hasToolBlockStart, "should have tool_use content_block_start")
	assert.True(t, hasInputDelta, "should have input_json_delta")

	// Stop reason should be "tool_use".
	var msgDelta anthropicMessageDelta
	for _, ev := range events {
		if ev.EventType == "message_delta" {
			require.NoError(t, json.Unmarshal([]byte(ev.Data), &msgDelta))
		}
	}
	assert.Equal(t, "tool_use", msgDelta.Delta.StopReason)
}

func TestStreamAnthropic_StopReasonEndTurn(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Model: "claude-3", Content: "Just text, no tools."}
	cfg := &types.StreamingConfig{ChunkSize: 4, ChunkDelayMs: 0}

	err := StreamAnthropic(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	events := parseSSEEvents(rec.Body.String())
	for _, ev := range events {
		if ev.EventType == "message_delta" {
			var msgDelta anthropicMessageDelta
			require.NoError(t, json.Unmarshal([]byte(ev.Data), &msgDelta))
			assert.Equal(t, "end_turn", msgDelta.Delta.StopReason)
		}
	}
}

func TestStreamAnthropic_ContextCancellation(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{
		Model:   "claude-3",
		Content: "A long response that should be cancelled before completion with many words",
	}
	cfg := &types.StreamingConfig{ChunkSize: 1, ChunkDelayMs: 10}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := StreamAnthropic(ctx, rec, resp, cfg)
	assert.Error(t, err)
}

func TestStreamAnthropic_DefaultConfig(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Model: "claude-3", Content: "Hello"}

	err := StreamAnthropic(context.Background(), rec, resp, nil)
	require.NoError(t, err)

	events := parseSSEEvents(rec.Body.String())
	assert.Equal(t, "message_stop", events[len(events)-1].EventType)
}
