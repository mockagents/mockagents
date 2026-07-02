package streaming

import (
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

// FB-03: the semantic error modes (finish_reason / refusal / raw_arguments)
// must behave identically on the streaming path as non-streaming.

var noDelayCfg = &types.StreamingConfig{ChunkSize: 4, ChunkDelayMs: 0}

func TestStreamOpenAI_SemanticModes(t *testing.T) {
	// finish_reason override on the finish chunk.
	rec := httptest.NewRecorder()
	require.NoError(t, StreamOpenAI(context.Background(), rec,
		&engine.Response{Model: "m", Content: "partial", FinishReason: "length"}, noDelayCfg))
	lines := parseSSEDataLines(rec.Body.String())
	var finish ChatCompletionChunk
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-2]), &finish))
	require.NotNil(t, finish.Choices[0].FinishReason)
	assert.Equal(t, "length", *finish.Choices[0].FinishReason)

	// refusal-only: a delta carries the refusal field; stream is non-empty.
	rec = httptest.NewRecorder()
	require.NoError(t, StreamOpenAI(context.Background(), rec,
		&engine.Response{Model: "m", Refusal: "I can't help"}, noDelayCfg))
	foundRefusal := false
	for _, l := range parseSSEDataLines(rec.Body.String()) {
		if l == "[DONE]" {
			continue
		}
		var c ChatCompletionChunk
		if json.Unmarshal([]byte(l), &c) == nil && c.Choices[0].Delta.Refusal != nil {
			assert.Equal(t, "I can't help", *c.Choices[0].Delta.Refusal)
			foundRefusal = true
		}
	}
	assert.True(t, foundRefusal, "streaming refusal was dropped")

	// raw_arguments emitted verbatim (malformed JSON survives).
	rec = httptest.NewRecorder()
	require.NoError(t, StreamOpenAI(context.Background(), rec,
		&engine.Response{Model: "m", Content: "x", ToolCalls: []types.ToolCallSpec{
			{Name: "get_weather", RawArguments: `{"city":`}}}, noDelayCfg))
	gotArgs := ""
	for _, l := range parseSSEDataLines(rec.Body.String()) {
		if l == "[DONE]" {
			continue
		}
		var c ChatCompletionChunk
		if json.Unmarshal([]byte(l), &c) == nil && len(c.Choices[0].Delta.ToolCalls) > 0 {
			if args := c.Choices[0].Delta.ToolCalls[0].Function.Arguments; args != nil {
				gotArgs += *args
			}
		}
	}
	assert.Equal(t, `{"city":`, gotArgs)
	assert.False(t, json.Valid([]byte(gotArgs)), "raw args should be invalid JSON")
}

func TestStreamAnthropic_SemanticModes(t *testing.T) {
	// finish_reason length -> stop_reason max_tokens on the message_delta.
	rec := httptest.NewRecorder()
	require.NoError(t, StreamAnthropic(context.Background(), rec,
		&engine.Response{Model: "m", Content: "partial", FinishReason: "length"}, noDelayCfg))
	assert.Contains(t, rec.Body.String(), `"stop_reason":"max_tokens"`)

	// refusal-only: refusal text is streamed + refusal stop reason.
	rec = httptest.NewRecorder()
	require.NoError(t, StreamAnthropic(context.Background(), rec,
		&engine.Response{Model: "m", Refusal: "I can't help"}, noDelayCfg))
	body := rec.Body.String()
	assert.Contains(t, body, "I can't help", "refusal text dropped from stream")
	assert.Contains(t, body, `"stop_reason":"refusal"`)
}

func TestStreamGemini_SemanticModes(t *testing.T) {
	// finish_reason length -> MAX_TOKENS on the terminal event.
	rec := httptest.NewRecorder()
	require.NoError(t, StreamGemini(context.Background(), rec,
		&engine.Response{Model: "m", Content: "partial", FinishReason: "length"}, noDelayCfg, 1, 1))
	assert.Contains(t, rec.Body.String(), `"finishReason":"MAX_TOKENS"`)

	// refusal-only: refusal text streamed.
	rec = httptest.NewRecorder()
	require.NoError(t, StreamGemini(context.Background(), rec,
		&engine.Response{Model: "m", Refusal: "I can't help"}, noDelayCfg, 1, 1))
	assert.Contains(t, rec.Body.String(), "I can't help")
}

func TestFinishReasonMappers(t *testing.T) {
	assert.Equal(t, "max_tokens", AnthropicStopReason("length"))
	assert.Equal(t, "tool_use", AnthropicStopReason("tool_calls"))
	assert.Equal(t, "end_turn", AnthropicStopReason(""))
	assert.Equal(t, "MAX_TOKENS", GeminiFinishReason("length"))
	assert.Equal(t, "SAFETY", GeminiFinishReason("content_filter"))
	assert.Equal(t, "STOP", GeminiFinishReason(""))
}

// Guard against accidental whitespace-only refusal slipping through as content.
func TestStreamOpenAI_RefusalKeepsStopFinish(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, StreamOpenAI(context.Background(), rec,
		&engine.Response{Model: "m", Refusal: "no"}, noDelayCfg))
	lines := parseSSEDataLines(rec.Body.String())
	var finish ChatCompletionChunk
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-2]), &finish))
	require.NotNil(t, finish.Choices[0].FinishReason)
	assert.Equal(t, "stop", *finish.Choices[0].FinishReason)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(rec.Body.String()), "[DONE]"))
}
