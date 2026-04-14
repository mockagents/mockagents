package engine

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

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
	buckets map[string]*rateBucket
}

// NewChaosInjector returns an injector that uses the real wall clock and a
// time-seeded math/rand source.
func NewChaosInjector() *ChaosInjector {
	return &ChaosInjector{
		Now:     time.Now,
		Sleep:   time.Sleep,
		RandSrc: rand.New(rand.NewSource(time.Now().UnixNano())),
		buckets: make(map[string]*rateBucket),
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
func (c *ChaosInjector) Before(agent *types.AgentDefinition) error {
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
		if err := c.maybeInjectError(cfg.Errors); err != nil {
			return err
		}
	}
	return nil
}

// After sleeps for the configured latency distribution. Called once response
// generation has completed successfully, so latency is visible to clients
// without blocking error injection above.
func (c *ChaosInjector) After(agent *types.AgentDefinition) {
	cfg := chaosFor(agent)
	if cfg == nil || cfg.Latency == nil {
		return
	}
	d := c.sampleLatency(cfg.Latency)
	if d > 0 {
		c.Sleep(d)
	}
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

	c.mu.Lock()
	defer c.mu.Unlock()
	switch dist {
	case "fixed":
		return time.Duration(l.MinMs) * time.Millisecond
	case "uniform":
		if l.MaxMs <= l.MinMs {
			return time.Duration(l.MinMs) * time.Millisecond
		}
		span := l.MaxMs - l.MinMs
		return time.Duration(l.MinMs+c.RandSrc.Intn(span+1)) * time.Millisecond
	case "normal":
		ms := c.RandSrc.NormFloat64()*float64(l.StddevMs) + float64(l.MeanMs)
		if ms < 0 {
			ms = 0
		}
		return time.Duration(ms) * time.Millisecond
	default:
		return time.Duration(l.MinMs) * time.Millisecond
	}
}

// maybeInjectError returns a *ChaosError with probability Rate.
func (c *ChaosInjector) maybeInjectError(e *types.ChaosErrorConfig) error {
	c.mu.Lock()
	draw := c.RandSrc.Float64()
	c.mu.Unlock()
	if draw >= e.Rate {
		return nil
	}

	if e.Timeout {
		timeout := time.Duration(e.TimeoutMs) * time.Millisecond
		if timeout > 0 {
			c.Sleep(timeout)
		}
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
