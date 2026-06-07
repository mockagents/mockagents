package streaming

import (
	"context"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// tokenLen approximates a chunk's token count as its whitespace-word count, so
// tokens_per_sec paces by words (the chunker's unit) rather than characters.
func tokenLen(chunk string) int {
	if n := len(strings.Fields(chunk)); n > 0 {
		return n
	}
	return 1
}

// streamPacer applies stream-timing physics (TTFT, tokens-per-sec, jitter) and
// mid-stream fault injection (truncation, malformed frame) from the agent's
// StreamingConfig (RR-07). It is shared by every protocol's Stream* function so
// the fault model is identical across OpenAI, Anthropic, and Gemini.
//
// The zero/nil config paces with the package defaults and injects no faults, so
// existing streams are unchanged.
type streamPacer struct {
	ttft          time.Duration
	chunkDelay    time.Duration
	perToken      time.Duration // derived from tokens_per_sec; 0 = use chunkDelay
	jitter        time.Duration
	truncateAfter int
	malformed     bool
	rng           *rand.Rand
}

func newPacer(cfg *types.StreamingConfig) *streamPacer {
	delayMs := DefaultChunkDelayMs
	p := &streamPacer{
		// Fixed seed: jitter is reproducible across runs (deterministic).
		rng: rand.New(rand.NewPCG(0x6d6f636b61, 0x67656e7473)),
	}
	if cfg != nil {
		if cfg.ChunkDelayMs >= 0 {
			delayMs = cfg.ChunkDelayMs
		}
		p.ttft = time.Duration(cfg.TTFTMs) * time.Millisecond
		if cfg.TokensPerSec > 0 {
			p.perToken = time.Duration(float64(time.Second) / cfg.TokensPerSec)
		}
		p.jitter = time.Duration(cfg.JitterMs) * time.Millisecond
		p.truncateAfter = cfg.TruncateAfterChunks
		p.malformed = cfg.Malformed
	}
	p.chunkDelay = time.Duration(delayMs) * time.Millisecond
	return p
}

// firstByte sleeps the time-to-first-token before the first content chunk.
func (p *streamPacer) firstByte(ctx context.Context) error {
	return sleepCtx(ctx, p.ttft)
}

// beforeChunk is called before emitting content chunk i (0-based). It reports
// whether the stream should be truncated here, and otherwise sleeps the
// inter-chunk delay (tokens-per-sec or chunk delay, plus jitter).
func (p *streamPacer) beforeChunk(ctx context.Context, i, chunkLen int) (truncate bool, err error) {
	if p.truncateAfter > 0 && i >= p.truncateAfter {
		return true, nil
	}
	return false, sleepCtx(ctx, p.delayFor(chunkLen))
}

// delayFor computes the inter-chunk delay (tokens-per-sec or chunk delay, plus
// deterministic jitter) without sleeping — separated out for testability.
func (p *streamPacer) delayFor(chunkLen int) time.Duration {
	d := p.chunkDelay
	if p.perToken > 0 {
		d = time.Duration(chunkLen) * p.perToken
	}
	if p.jitter > 0 {
		d += time.Duration(p.rng.Int64N(int64(2*p.jitter+1))) - p.jitter
		if d < 0 {
			d = 0
		}
	}
	return d
}

// writeStop is called at the truncation/stop point. It emits one malformed SSE
// frame when configured, then ends the stream (the caller must NOT write the
// normal finish frame / [DONE] afterward). Returns nil so a faulted stream is
// "successful" from the writer's perspective.
func (p *streamPacer) writeStop(sse *SSEWriter) error {
	if p.malformed {
		// Deliberately invalid JSON (unterminated object) — provider-agnostic,
		// so the same fault is emitted across OpenAI/Anthropic/Gemini. Clients
		// that json.Unmarshal each data frame must handle the parse error.
		_ = sse.WriteRaw(`{"mockagents_fault":"malformed",`)
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
