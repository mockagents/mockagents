package streaming

import (
	"context"
	"math"
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
	// rng is fixed-seeded so the deterministic-jitter physics (RR-07) reproduce
	// across runs/tests.
	rng *rand.Rand
	// distRng is seeded with per-stream entropy so the load-test distribution
	// (FB-05) draws an INDEPENDENT TTFT/ITL sample for every request — a fixed
	// seed would make every concurrent stream emit the identical latency
	// sequence, collapsing the very distribution the feature exists to provide.
	distRng *rand.Rand

	// Distribution-based timing (FB-05): when set, TTFT / inter-token latency
	// are sampled per-stream / per-chunk from a lognormal fit to (p50, p95),
	// overriding the fixed ttft / perToken.
	ttftP50, ttftP95 time.Duration
	itlP50, itlP95   time.Duration
}

// maxStreamSampleMs caps a single sampled delay so a pathological lognormal
// tail can't block a stream for minutes (mirrors the chaos latency cap).
const maxStreamSampleMs = 60_000

// z95 is the standard-normal 95th-percentile quantile, used to fit a lognormal
// from its p50 (median) and p95.
const z95 = 1.6448536269514722

func newPacer(cfg *types.StreamingConfig) *streamPacer {
	// Per-stream entropy for the distribution path so concurrent streams draw
	// independent TTFT/ITL samples (FB-05). rand/v2's top-level Uint64 is
	// auto-seeded and goroutine-safe.
	return newPacerSeeded(cfg, rand.Uint64())
}

// newPacerSeeded is newPacer with an explicit distribution seed so tests can
// pin the distribution draws deterministically.
func newPacerSeeded(cfg *types.StreamingConfig, distSeed uint64) *streamPacer {
	delayMs := DefaultChunkDelayMs
	p := &streamPacer{
		// Fixed seed: deterministic jitter (RR-07).
		rng: rand.New(rand.NewPCG(0x6d6f636b61, 0x67656e7473)),
		// Per-stream seed: independent distribution draws (FB-05).
		distRng: rand.New(rand.NewPCG(distSeed, distSeed^0x9e3779b97f4a7c15)),
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
		p.ttftP50 = time.Duration(cfg.TTFTP50Ms) * time.Millisecond
		p.ttftP95 = time.Duration(cfg.TTFTP95Ms) * time.Millisecond
		p.itlP50 = time.Duration(cfg.ITLP50Ms) * time.Millisecond
		p.itlP95 = time.Duration(cfg.ITLP95Ms) * time.Millisecond
	}
	p.chunkDelay = time.Duration(delayMs) * time.Millisecond
	return p
}

// sampleLognormal draws a delay from a lognormal distribution fit to the given
// median (p50) and 95th percentile (p95): mu = ln(p50), sigma = (ln(p95)-mu)/z95.
// A degenerate p95 <= p50 collapses to the fixed p50. The draw is clamped to
// [0, maxStreamSampleMs].
func (p *streamPacer) sampleLognormal(p50, p95 time.Duration) time.Duration {
	if p50 <= 0 {
		return 0
	}
	if p95 <= p50 {
		return p50
	}
	mu := math.Log(float64(p50))
	sigma := (math.Log(float64(p95)) - mu) / z95
	// Clamp on the float64 BEFORE the int64 cast: a pathological tail can
	// overflow int64 (huge Exp), and float64->int64 overflow yields a large
	// NEGATIVE value that would otherwise slip past a post-cast clamp and read
	// as 0ns. NaN guards against degenerate inputs.
	v := math.Exp(mu + sigma*p.distRng.NormFloat64())
	maxV := float64(maxStreamSampleMs) * float64(time.Millisecond)
	switch {
	case math.IsNaN(v) || v < 0:
		v = 0
	case v > maxV:
		v = maxV
	}
	return time.Duration(v)
}

// firstByte sleeps the time-to-first-token before the first content chunk —
// sampled from the TTFT distribution when configured, else the fixed value.
func (p *streamPacer) firstByte(ctx context.Context) error {
	if p.ttftP50 > 0 {
		return sleepCtx(ctx, p.sampleLognormal(p.ttftP50, p.ttftP95))
	}
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
	// Inter-token latency distribution (FB-05) takes precedence: sample a
	// per-token delay and scale by the chunk's token count. The distribution
	// already carries the variance, so fixed jitter is not added on top.
	if p.itlP50 > 0 {
		return time.Duration(chunkLen) * p.sampleLognormal(p.itlP50, p.itlP95)
	}
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
