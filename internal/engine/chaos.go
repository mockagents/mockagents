package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// maxChaosLatencyMs caps a single injected latency draw so a misconfigured
// or long-tailed (normal-distribution) value cannot block a request
// goroutine for minutes. Combined with ctx-aware sleeping, injected
// latency is always bounded and cancellable.
const maxChaosLatencyMs = 60_000

// ChaosError is returned by ChaosInjector when an agent is configured to
// inject a fault before or after response generation. HTTP handlers unwrap
// it to translate the embedded status code into a wire-level error.
type ChaosError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
	Timeout    bool
}

// Error satisfies the error interface.
func (e *ChaosError) Error() string {
	if e.Timeout {
		return fmt.Sprintf("chaos: timeout after %s", e.RetryAfter)
	}
	return fmt.Sprintf("chaos: injected %d: %s", e.StatusCode, e.Message)
}

// AsChaosError returns the wrapped *ChaosError, or nil if err is not one.
func AsChaosError(err error) *ChaosError {
	if err == nil {
		return nil
	}
	var ce *ChaosError
	if errors.As(err, &ce) {
		return ce
	}
	return nil
}

// ChaosInjector is request-pipeline middleware that evaluates a per-agent
// ChaosConfig and decides whether to sleep, error, or rate-limit a call.
// It is safe for concurrent use.
type ChaosInjector struct {
	Now     func() time.Time
	Sleep   func(time.Duration)
	RandSrc *rand.Rand
	mu      sync.Mutex
	// buckets holds one rolling-window counter per agent name. It grows by
	// at most one entry per rate-limited agent and is never pruned (F-CH-006);
	// that is bounded by the number of configured agents (a fixed, small
	// set), not by request volume, so unbounded growth is not a concern.
	buckets map[string]*rateBucket
}

// NewChaosInjector returns an injector that uses the real wall clock and a
// time-seeded math/rand source. Sleep is left nil so the real, ctx-aware
// sleep path is used; tests may set Sleep to a deterministic recorder.
func NewChaosInjector() *ChaosInjector {
	return &ChaosInjector{
		Now:     time.Now,
		RandSrc: rand.New(rand.NewSource(time.Now().UnixNano())),
		buckets: make(map[string]*rateBucket),
	}
}

// sleep blocks for d, but returns early if ctx is cancelled. When a Sleep
// hook is set (tests) it is used as-is for deterministic, non-blocking
// behavior; the hook does not observe ctx, which is fine because cancel
// tests use the real path.
func (c *ChaosInjector) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	if c.Sleep != nil {
		c.Sleep(d)
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// rateBucket tracks how many requests an agent has served inside the current
// rolling window.
type rateBucket struct {
	windowStart time.Time
	count       int
}

// Before evaluates rate-limit and error-injection rules that must run prior
// to response generation. When it returns a ChaosError the engine should
// abort the request with that error as-is.
func (c *ChaosInjector) Before(ctx context.Context, agent *types.AgentDefinition) error {
	cfg := chaosFor(agent)
	if cfg == nil {
		return nil
	}
	if cfg.RateLimit != nil && cfg.RateLimit.Requests > 0 {
		if err := c.checkRateLimit(agent.Metadata.Name, cfg.RateLimit); err != nil {
			return err
		}
	}
	if cfg.Errors != nil && cfg.Errors.Rate > 0 {
		if err := c.maybeInjectError(ctx, cfg.Errors); err != nil {
			return err
		}
	}
	return nil
}

// After sleeps for the configured latency distribution. Called once response
// generation has completed successfully, so latency is visible to clients
// without blocking error injection above.
func (c *ChaosInjector) After(ctx context.Context, agent *types.AgentDefinition) {
	cfg := chaosFor(agent)
	if cfg == nil || cfg.Latency == nil {
		return
	}
	c.sleep(ctx, c.sampleLatency(cfg.Latency))
}

// chaosFor returns the effective chaos config for an agent, or nil when
// the agent has no chaos block or it is disabled.
func chaosFor(agent *types.AgentDefinition) *types.ChaosConfig {
	if agent == nil {
		return nil
	}
	cfg := agent.Spec.Behavior.Chaos
	if cfg == nil {
		return nil
	}
	// Treat explicit `enabled: false` as off; leaving `enabled` unset is
	// on-by-presence so agents don't have to repeat the flag.
	if !cfg.Enabled && !hasAnyChaosSection(cfg) {
		return nil
	}
	return cfg
}

func hasAnyChaosSection(cfg *types.ChaosConfig) bool {
	return cfg.Latency != nil || cfg.Errors != nil || cfg.RateLimit != nil
}

// clampUnitInterval bounds a probability to [0,1] so an out-of-range
// config value can't silently flip a rate into always/never.
func clampUnitInterval(r float64) float64 {
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// sampleLatency draws a delay from the configured distribution.
func (c *ChaosInjector) sampleLatency(l *types.ChaosLatencyConfig) time.Duration {
	dist := l.Distribution
	if dist == "" {
		if l.MaxMs > l.MinMs {
			dist = "uniform"
		} else {
			dist = "fixed"
		}
	}

	// Lock only around the RandSrc draws (F-CH-003): the fixed/default
	// branches use no randomness, so serializing them on c.mu needlessly
	// blocked concurrent requests. math/rand's Rand is not safe for
	// concurrent use, so the uniform/normal draws still take the lock — but
	// for the minimum span: a single Intn/NormFloat64 call.
	switch dist {
	case "fixed":
		return time.Duration(l.MinMs) * time.Millisecond
	case "uniform":
		if l.MaxMs <= l.MinMs {
			return time.Duration(l.MinMs) * time.Millisecond
		}
		span := l.MaxMs - l.MinMs
		c.mu.Lock()
		n := c.RandSrc.Intn(span + 1)
		c.mu.Unlock()
		return time.Duration(l.MinMs+n) * time.Millisecond
	case "normal":
		c.mu.Lock()
		norm := c.RandSrc.NormFloat64()
		c.mu.Unlock()
		ms := norm*float64(l.StddevMs) + float64(l.MeanMs)
		if ms < 0 {
			ms = 0
		}
		if ms > maxChaosLatencyMs {
			ms = maxChaosLatencyMs
		}
		return time.Duration(ms) * time.Millisecond
	default:
		return time.Duration(l.MinMs) * time.Millisecond
	}
}

// maybeInjectError returns a *ChaosError with probability Rate. When the
// timeout fault is selected it sleeps for the configured duration, cut
// short if ctx is cancelled.
func (c *ChaosInjector) maybeInjectError(ctx context.Context, e *types.ChaosErrorConfig) error {
	// Clamp Rate to [0,1] (F-CH-004): an out-of-range rate otherwise
	// silently means "always" (>1) or "never" (<0). Since draw is in
	// [0,1), a clamped rate of 1 always injects and 0 never does.
	rate := clampUnitInterval(e.Rate)
	c.mu.Lock()
	draw := c.RandSrc.Float64()
	c.mu.Unlock()
	if draw >= rate {
		return nil
	}

	if e.Timeout {
		timeout := time.Duration(e.TimeoutMs) * time.Millisecond
		c.sleep(ctx, timeout)
		return &ChaosError{
			StatusCode: http.StatusGatewayTimeout,
			Message:    "request timed out",
			RetryAfter: timeout,
			Timeout:    true,
		}
	}

	status := c.pickStatusCode(e)
	if status == 0 {
		status = http.StatusInternalServerError
	}
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &ChaosError{StatusCode: status, Message: msg}
}

func (c *ChaosInjector) pickStatusCode(e *types.ChaosErrorConfig) int {
	if len(e.StatusCodes) > 0 {
		c.mu.Lock()
		defer c.mu.Unlock()
		return e.StatusCodes[c.RandSrc.Intn(len(e.StatusCodes))]
	}
	return e.StatusCode
}

// checkRateLimit maintains a per-agent rolling window and returns a 429
// ChaosError when the agent has exceeded its allotment.
func (c *ChaosInjector) checkRateLimit(agent string, rl *types.ChaosRateLimitConfig) error {
	if rl.WindowMs <= 0 {
		return nil
	}
	now := c.Now()
	window := time.Duration(rl.WindowMs) * time.Millisecond

	c.mu.Lock()
	defer c.mu.Unlock()

	bucket, ok := c.buckets[agent]
	if !ok || now.Sub(bucket.windowStart) >= window {
		c.buckets[agent] = &rateBucket{windowStart: now, count: 1}
		return nil
	}
	bucket.count++
	if bucket.count > rl.Requests {
		remaining := window - now.Sub(bucket.windowStart)
		return &ChaosError{
			StatusCode: http.StatusTooManyRequests,
			Message:    "rate limit exceeded",
			RetryAfter: remaining,
		}
	}
	return nil
}
