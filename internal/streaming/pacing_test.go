package streaming

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

func runOpenAIStream(t *testing.T, cfg *types.StreamingConfig, content string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	resp := &engine.Response{Content: content, Model: "gpt-4o"}
	if err := StreamOpenAI(context.Background(), rec, resp, cfg); err != nil {
		t.Fatalf("StreamOpenAI: %v", err)
	}
	return rec.Body.String()
}

// Regression: with no faults configured, a stream still terminates normally.
func TestStream_Default_EmitsDone(t *testing.T) {
	out := runOpenAIStream(t, &types.StreamingConfig{ChunkSize: 2}, "hello world")
	if !strings.Contains(out, "[DONE]") {
		t.Error("default stream should end with [DONE]")
	}
}

func TestStream_TruncateAfterChunks(t *testing.T) {
	out := runOpenAIStream(t, &types.StreamingConfig{ChunkSize: 1, TruncateAfterChunks: 2}, "one two three four five six")
	if n := strings.Count(out, `"content":`); n != 2 {
		t.Errorf("truncated stream emitted %d content chunks, want 2\n%s", n, out)
	}
	if strings.Contains(out, "[DONE]") {
		t.Error("truncated stream must NOT emit [DONE]")
	}
	if strings.Contains(out, `"finish_reason":"stop"`) {
		t.Error("truncated stream must NOT emit the finish frame")
	}
}

func TestStream_Malformed(t *testing.T) {
	out := runOpenAIStream(t, &types.StreamingConfig{ChunkSize: 4, Malformed: true}, "hi")
	if !strings.Contains(out, `{"mockagents_fault":"malformed",`) {
		t.Errorf("malformed stream should contain the invalid frame, got:\n%s", out)
	}
	if strings.Contains(out, "[DONE]") {
		t.Error("malformed stream must NOT emit [DONE]")
	}
}

// TTFT delays the first content chunk and respects context cancellation.
func TestStream_TTFT_Delays(t *testing.T) {
	start := time.Now()
	runOpenAIStream(t, &types.StreamingConfig{ChunkSize: 4, TTFTMs: 60}, "hello")
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond {
		t.Errorf("ttft_ms=60 produced only %v of delay", elapsed)
	}
}

func TestStreamPacer_FirstByteRespectsContext(t *testing.T) {
	p := newPacer(&types.StreamingConfig{TTFTMs: 10_000})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.firstByte(ctx); err == nil {
		t.Error("firstByte should return the context error when cancelled, not sleep 10s")
	}
}

func TestStreamPacer_BeforeChunkTruncation(t *testing.T) {
	p := newPacer(&types.StreamingConfig{TruncateAfterChunks: 3})
	for i := 0; i < 3; i++ {
		if trunc, _ := p.beforeChunk(context.Background(), i, 1); trunc {
			t.Fatalf("chunk %d should not truncate (limit 3)", i)
		}
	}
	if trunc, _ := p.beforeChunk(context.Background(), 3, 1); !trunc {
		t.Error("chunk index 3 should truncate (limit 3)")
	}
}

// Faults apply across protocols: a truncated Gemini stream emits no final
// finishReason event.
func TestStream_Gemini_Truncate(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Content: "one two three four five six", Model: "gemini-1.5-pro"}
	cfg := &types.StreamingConfig{ChunkSize: 1, TruncateAfterChunks: 2}
	if err := StreamGemini(context.Background(), rec, resp, cfg, 0, 0); err != nil {
		t.Fatalf("StreamGemini: %v", err)
	}
	out := rec.Body.String()
	if strings.Contains(out, "finishReason") {
		t.Errorf("truncated Gemini stream must not emit finishReason:\n%s", out)
	}
}

// The malformed fault is provider-agnostic and ends the stream without the
// normal terminating frames — verified across Anthropic and Gemini too.
func TestStream_Anthropic_Malformed(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Content: "hi there", Model: "claude-3"}
	cfg := &types.StreamingConfig{ChunkSize: 4, Malformed: true}
	if err := StreamAnthropic(context.Background(), rec, resp, cfg); err != nil {
		t.Fatalf("StreamAnthropic: %v", err)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `{"mockagents_fault":"malformed",`) {
		t.Errorf("anthropic malformed frame missing:\n%s", out)
	}
	if strings.Contains(out, "message_stop") {
		t.Error("malformed Anthropic stream must not emit message_stop")
	}
}

func TestStream_Gemini_Malformed(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &engine.Response{Content: "hi there", Model: "gemini-1.5-pro"}
	cfg := &types.StreamingConfig{ChunkSize: 4, Malformed: true}
	if err := StreamGemini(context.Background(), rec, resp, cfg, 3, 2); err != nil {
		t.Fatalf("StreamGemini: %v", err)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `{"mockagents_fault":"malformed",`) {
		t.Errorf("gemini malformed frame missing:\n%s", out)
	}
	if strings.Contains(out, "finishReason") {
		t.Error("malformed Gemini stream must not emit the finishReason terminator")
	}
}

// tokens_per_sec paces the stream (measurably slower than instant).
func TestStream_TokensPerSecPaces(t *testing.T) {
	start := time.Now()
	// 4 words at chunk_size 1 -> 4 chunks; 50 tok/s -> 20ms/chunk -> ~80ms.
	runOpenAIStream(t, &types.StreamingConfig{ChunkSize: 1, TokensPerSec: 50}, "one two three four")
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("tokens_per_sec=50 over 4 chunks should take >= ~50ms, took %v", elapsed)
	}
}

// Jitter is reproducible across pacers (fixed-seed PRNG) — the stated design goal.
func TestStreamPacer_JitterDeterministic(t *testing.T) {
	cfg := &types.StreamingConfig{TokensPerSec: 100, JitterMs: 20}
	p1, p2 := newPacer(cfg), newPacer(cfg)
	for i := 0; i < 12; i++ {
		if d1, d2 := p1.delayFor(1), p2.delayFor(1); d1 != d2 {
			t.Fatalf("jitter not deterministic at step %d: %v != %v", i, d1, d2)
		}
	}
}

// Boundary: a truncate value larger than the chunk count leaves the stream
// intact (terminator present) — guards against "any nonzero value truncates".
func TestStream_TruncateBeyondChunkCount_NotTruncated(t *testing.T) {
	out := runOpenAIStream(t, &types.StreamingConfig{ChunkSize: 4, TruncateAfterChunks: 99}, "short text")
	if !strings.Contains(out, "[DONE]") {
		t.Error("over-large truncate value should leave the stream complete (with [DONE])")
	}
}
