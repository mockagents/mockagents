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

func parseSSEDataLines(body string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
	return lines
}

func TestStreamOpenAI_BasicContentStream(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{
		AgentName: "test-agent",
		Model:     "gpt-4o",
		Content:   "Hello world",
	}
	cfg := &types.StreamingConfig{ChunkSize: 1, ChunkDelayMs: 0}

	err := StreamOpenAI(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))

	lines := parseSSEDataLines(rec.Body.String())
	require.GreaterOrEqual(t, len(lines), 4) // role + content chunks + finish + [DONE]

	// First chunk should have role.
	var firstChunk ChatCompletionChunk
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &firstChunk))
	assert.Equal(t, "assistant", firstChunk.Choices[0].Delta.Role)
	assert.Equal(t, "chat.completion.chunk", firstChunk.Object)
	assert.Equal(t, "gpt-4o", firstChunk.Model)

	// Last line should be [DONE].
	assert.Equal(t, "[DONE]", lines[len(lines)-1])

	// Second-to-last should have finish_reason.
	var finishChunk ChatCompletionChunk
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-2]), &finishChunk))
	require.NotNil(t, finishChunk.Choices[0].FinishReason)
	assert.Equal(t, "stop", *finishChunk.Choices[0].FinishReason)
}

func TestStreamOpenAI_ContentReassembly(t *testing.T) {
	rec := httptest.NewRecorder()
	content := "The quick brown fox jumps over the lazy dog"
	resp := &engine.Response{Model: "gpt-4o", Content: content}
	cfg := &types.StreamingConfig{ChunkSize: 2, ChunkDelayMs: 0}

	err := StreamOpenAI(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	lines := parseSSEDataLines(rec.Body.String())

	// Reassemble content from delta chunks.
	var reassembled strings.Builder
	for _, line := range lines {
		if line == "[DONE]" {
			continue
		}
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 {
			reassembled.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	assert.Equal(t, content, reassembled.String())
}

func TestStreamOpenAI_ToolCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{
		Model:   "gpt-4o",
		Content: "Checking weather.",
		ToolCalls: []types.ToolCallSpec{
			{Name: "get_weather", Arguments: map[string]any{"location": "NYC"}},
		},
		ToolResults: []engine.ToolCallResult{
			{ID: "call_abc123", ToolName: "get_weather"},
		},
	}
	cfg := &types.StreamingConfig{ChunkSize: 4, ChunkDelayMs: 0}

	err := StreamOpenAI(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	lines := parseSSEDataLines(rec.Body.String())

	// Find tool call chunks.
	var hasToolCallName bool
	var hasToolCallArgs bool
	for _, line := range lines {
		if line == "[DONE]" {
			continue
		}
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && len(chunk.Choices[0].Delta.ToolCalls) > 0 {
			tc := chunk.Choices[0].Delta.ToolCalls[0]
			if tc.Function.Name != "" {
				hasToolCallName = true
				assert.Equal(t, "get_weather", tc.Function.Name)
				assert.Equal(t, "call_abc123", tc.ID)
			}
			if tc.Function.Arguments != nil && *tc.Function.Arguments != "" {
				hasToolCallArgs = true
			}
			// R9-11: the name frame carries an explicit empty arguments string.
			if tc.Function.Name != "" {
				require.NotNil(t, tc.Function.Arguments, "name frame must carry arguments:\"\"")
			}
		}
	}
	assert.True(t, hasToolCallName, "should have tool call name chunk")
	assert.True(t, hasToolCallArgs, "should have tool call arguments chunk")

	// Finish reason should be "tool_calls".
	var finishChunk ChatCompletionChunk
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-2]), &finishChunk))
	require.NotNil(t, finishChunk.Choices[0].FinishReason)
	assert.Equal(t, "tool_calls", *finishChunk.Choices[0].FinishReason)
}

func TestStreamOpenAI_ContextCancellation(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{
		Model:   "gpt-4o",
		Content: "A very long response that should be interrupted before completion with many words here",
	}
	cfg := &types.StreamingConfig{ChunkSize: 1, ChunkDelayMs: 10}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := StreamOpenAI(ctx, rec, resp, cfg)
	assert.Error(t, err)
}

func TestStreamOpenAI_DefaultConfig(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Model: "gpt-4o", Content: "Hello"}

	err := StreamOpenAI(context.Background(), rec, resp, nil)
	require.NoError(t, err)

	lines := parseSSEDataLines(rec.Body.String())
	assert.Equal(t, "[DONE]", lines[len(lines)-1])
}

func TestStreamOpenAI_EmptyContent(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Model: "gpt-4o", Content: ""}

	err := StreamOpenAI(context.Background(), rec, resp, &types.StreamingConfig{ChunkDelayMs: 0})
	require.NoError(t, err)

	lines := parseSSEDataLines(rec.Body.String())
	// Should still have role chunk, finish chunk, and [DONE].
	assert.GreaterOrEqual(t, len(lines), 3)
	assert.Equal(t, "[DONE]", lines[len(lines)-1])
}

func TestStreamOpenAI_ConsistentID(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Model: "gpt-4o", Content: "word1 word2 word3 word4"}
	cfg := &types.StreamingConfig{ChunkSize: 1, ChunkDelayMs: 0}

	err := StreamOpenAI(context.Background(), rec, resp, cfg)
	require.NoError(t, err)

	lines := parseSSEDataLines(rec.Body.String())
	var ids []string
	for _, line := range lines {
		if line == "[DONE]" {
			continue
		}
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		ids = append(ids, chunk.ID)
	}

	// All chunks should have the same ID.
	for _, id := range ids {
		assert.Equal(t, ids[0], id, "all chunks should share the same stream ID")
	}
}
