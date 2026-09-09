package streaming

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// TestStreamAnthropic_UsageFromAdapterCounts is the audit M-18 guard: when
// the adapter supplies its usage counts, message_start and message_delta
// carry exactly those, so a scenario is billed identically with and without
// stream:true.
func TestStreamAnthropic_UsageFromAdapterCounts(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Model: "claude-3", Content: "one two three four five six seven"}
	cfg := &types.StreamingConfig{ChunkSize: 4, ChunkDelayMs: types.Ptr(0)}
	require.NoError(t, StreamAnthropic(context.Background(), rec, resp, cfg, 17, 42))

	var sawStart, sawDelta bool
	for _, ev := range parseSSEEvents(rec.Body.String()) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(ev.Data), &m))
		switch ev.EventType {
		case "message_start":
			usage := m["message"].(map[string]any)["usage"].(map[string]any)
			assert.EqualValues(t, 17, usage["input_tokens"])
			sawStart = true
		case "message_delta":
			usage := m["usage"].(map[string]any)
			assert.EqualValues(t, 42, usage["output_tokens"])
			sawDelta = true
		}
	}
	assert.True(t, sawStart && sawDelta, "expected message_start and message_delta")
}

// TestStreamOpenAI_ToolArgumentsHonourCancellation is the audit M-17 guard:
// the per-chunk delay while streaming tool-call arguments used to be a bare
// time.Sleep, so a disconnected client kept the goroutine alive for
// chunks × delay (here 100 chunks × 200ms = 20s). It must return promptly
// with the context error.
func TestStreamOpenAI_ToolArgumentsHonourCancellation(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{
		Model: "gpt-4o",
		ToolCalls: []types.ToolCallSpec{
			{Name: "big", Arguments: map[string]any{"payload": strings.Repeat("x", 2000)}},
		},
		ToolResults: []engine.ToolCallResult{{ID: "call_1", ToolName: "big"}},
	}
	cfg := &types.StreamingConfig{ChunkSize: 4, ChunkDelayMs: types.Ptr(200)}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Past the name frame's delay and the first argument chunk's delay,
		// so the cancellation lands inside the argument loop.
		time.Sleep(450 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := StreamOpenAI(ctx, rec, resp, cfg)
	elapsed := time.Since(start)
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 5*time.Second, "stream did not stop on cancellation (took %v)", elapsed)
	assert.Contains(t, rec.Body.String(), `"arguments"`, "at least one argument frame should have been written before cancellation")
}
