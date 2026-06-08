package streaming

import (
	"sort"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// TestPacer_LognormalFitsPercentiles checks that the sampled TTFT distribution
// actually matches its configured p50/p95 (FB-05): the median is near p50 and
// roughly 95% of draws fall below p95. The pacer rng is fixed-seeded, so this
// is deterministic.
func TestPacer_LognormalFitsPercentiles(t *testing.T) {
	p := newPacerSeeded(&types.StreamingConfig{TTFTP50Ms: 300, TTFTP95Ms: 1200}, 12345)
	const n = 4000
	samples := make([]time.Duration, n)
	below95 := 0
	for i := 0; i < n; i++ {
		s := p.sampleLognormal(p.ttftP50, p.ttftP95)
		samples[i] = s
		if s <= 1200*time.Millisecond {
			below95++
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	median := samples[n/2]
	if median < 255*time.Millisecond || median > 345*time.Millisecond {
		t.Errorf("median = %s, want ~300ms (within 15%%)", median)
	}
	frac := float64(below95) / n
	if frac < 0.92 || frac > 0.985 {
		t.Errorf("fraction below p95 = %.3f, want ~0.95", frac)
	}
}

// TestPacer_IndependentAcrossStreams is the regression guard for the fixed-seed
// defect: two separate streams (two newPacer calls) must draw DIFFERENT TTFT
// samples, otherwise every concurrent request under load would see the identical
// latency and the configured distribution would collapse to a constant.
func TestPacer_IndependentAcrossStreams(t *testing.T) {
	cfg := &types.StreamingConfig{TTFTP50Ms: 300, TTFTP95Ms: 1200}
	const streams = 50
	seen := map[time.Duration]int{}
	for i := 0; i < streams; i++ {
		p := newPacer(cfg) // entropy-seeded per stream
		seen[p.sampleLognormal(p.ttftP50, p.ttftP95)]++
	}
	// With independent per-stream entropy, ~all draws are distinct; a fixed seed
	// would collapse them to a single value.
	if len(seen) < streams-2 {
		t.Errorf("only %d distinct TTFT draws across %d streams — seed not independent", len(seen), streams)
	}
}

func TestPacer_LognormalDegenerate(t *testing.T) {
	p := newPacer(&types.StreamingConfig{})
	// p95 <= p50 collapses to the fixed p50.
	if d := p.sampleLognormal(200*time.Millisecond, 100*time.Millisecond); d != 200*time.Millisecond {
		t.Errorf("degenerate p95<=p50: got %s, want 200ms", d)
	}
	if d := p.sampleLognormal(0, 100*time.Millisecond); d != 0 {
		t.Errorf("p50==0: got %s, want 0", d)
	}
}

// TestPacer_DistributionOverridesFixed verifies the distribution fields take
// precedence over the fixed ttft_ms / tokens_per_sec.
func TestPacer_DistributionOverridesFixed(t *testing.T) {
	// ITL distribution overrides perToken/chunkDelay; delay scales with tokens.
	p := newPacer(&types.StreamingConfig{TokensPerSec: 1000, ITLP50Ms: 50, ITLP95Ms: 50})
	// p50==p95 -> deterministic 50ms per token; 3-token chunk -> 150ms.
	if d := p.delayFor(3); d != 150*time.Millisecond {
		t.Errorf("itl delay for 3 tokens = %s, want 150ms", d)
	}

	// No distribution -> fixed behavior preserved.
	p2 := newPacer(&types.StreamingConfig{ChunkDelayMs: 40})
	if d := p2.delayFor(2); d != 40*time.Millisecond {
		t.Errorf("fixed chunk delay = %s, want 40ms", d)
	}
}
