package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	commonchaos "github.com/mockagents/mockagents/internal/chaos"
	"github.com/mockagents/mockagents/internal/metrics"
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
	// Connection, when non-empty, marks a connection-LAYER fault (FB-03 slice 5)
	// rather than an HTTP-status fault: the adapter hijacks the TCP connection and
	// performs the named mode ("reset"|"empty"|"random", plus aliases) instead of
	// writing an HTTP response.
	Connection string
}

// Error satisfies the error interface.
func (e *ChaosError) Error() string {
	if e.Connection != "" {
		return fmt.Sprintf("chaos: connection fault %q", e.Connection)
	}
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
	// buckets holds one rolling-window counter per agent, keyed by
	// chaosKey (tenant + name — audit M-02). Keying on the bare name let two
	// tenants owning same-named agents share one rate-limit budget, so one
	// tenant's traffic produced 429s for another's. It grows by at most one
	// entry per rate-limited agent and is never pruned (F-CH-006); that is
	// bounded by the number of configured agents (a fixed, small set), not by
	// request volume, so unbounded growth is not a concern.
	buckets map[string]*rateBucket
	// errorCounts holds the cumulative request count per agent for the
	// FailFirst stateful trigger, keyed the same way — otherwise tenant A's
	// requests exhausted tenant B's "fail the first N" allowance. Bounded by
	// the agent set, same as buckets.
	errorCounts map[string]int
	// connCounts is the FailFirst counter for connection-layer faults, kept
	// SEPARATE from errorCounts so an agent configuring both errors.fail_first
	// and connection.fail_first gets independent per-fault counts.
	connCounts   map[string]int
	globalSeed   int64
	globalRate   *float64
	globalCounts map[string]uint64
}

// NewChaosInjector returns an injector that uses the real wall clock and a
// time-seeded math/rand source. Sleep is left nil so the real, ctx-aware
// sleep path is used; tests may set Sleep to a deterministic recorder.
func NewChaosInjector() *ChaosInjector {
	return &ChaosInjector{
		Now:          time.Now,
		RandSrc:      rand.New(rand.NewSource(time.Now().UnixNano())),
		buckets:      make(map[string]*rateBucket),
		errorCounts:  make(map[string]int),
		connCounts:   make(map[string]int),
		globalCounts: make(map[string]uint64),
	}
}

// SetGlobalPolicy configures the lowest-precedence deterministic rate for
// configured agent actions that do not have their own trigger.
func (c *ChaosInjector) SetGlobalPolicy(seed int64, rate *float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.globalSeed = seed
	if rate == nil {
		c.globalRate = nil
	} else {
		value := *rate
		c.globalRate = &value
	}
	c.globalCounts = make(map[string]uint64)
}

// ensureMaps lazily initializes the per-agent counter maps so a
// directly-constructed ChaosInjector{...} (tests) is usable without setting
// every map. Cheap and idempotent.
func (c *ChaosInjector) ensureMaps() {
	c.mu.Lock()
	if c.buckets == nil {
		c.buckets = make(map[string]*rateBucket)
	}
	if c.errorCounts == nil {
		c.errorCounts = make(map[string]int)
	}
	if c.connCounts == nil {
		c.connCounts = make(map[string]int)
	}
	if c.globalCounts == nil {
		c.globalCounts = make(map[string]uint64)
	}
	c.mu.Unlock()
}

// globalAllows draws the lowest-precedence global-rate decision for one
// action. scope is the tenant-qualified agent key (M-02) and counts the
// sequence; agentName is what feeds the deterministic decision, unchanged, so
// a fixed --chaos-seed reproduces exactly the same faults it always did.
// Two tenants owning same-named agents now advance independent sequences and
// each sees that same reproducible pattern, instead of interleaving into one.
func (c *ChaosInjector) globalAllows(scope, agentName, action string) bool {
	c.mu.Lock()
	if c.globalRate == nil {
		c.mu.Unlock()
		return true
	}
	rate := *c.globalRate
	seed := c.globalSeed
	counterKey := scope + "\x00" + action
	c.globalCounts[counterKey]++
	sequence := c.globalCounts[counterKey]
	c.mu.Unlock()
	decisionKey := agentName + "\x00" + action
	decision := commonchaos.Decide(commonchaos.Policy{Seed: seed, Rate: &rate, Source: "global-rate"}, fmt.Sprintf("%s\x00%d", decisionKey, sequence), action, "")
	return decision.Apply
}

// chaosKey namespaces per-agent chaos state by owning tenant, following the
// scopedSessionKey convention (NUL separator; tenant ids are server-generated
// and never contain NUL). An empty tenant — single-tenant mode — keeps every
// agent in one namespace exactly as before.
func chaosKey(agent *types.AgentDefinition) string {
	return agent.Metadata.TenantID + "\x00" + agent.Metadata.Name
}

// ResetAgent drops the stateful chaos counters for one agent, so a
// re-registered or deleted-then-recreated definition starts from a clean
// FailFirst count and rate-limit window instead of inheriting the previous
// definition's progress (audit M-02).
func (c *ChaosInjector) ResetAgent(tenantID, name string) {
	scope := tenantID + "\x00" + name
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.buckets, scope)
	delete(c.errorCounts, scope)
	delete(c.connCounts, scope)
	// globalCounts is keyed by scope + action, so clear every action for it.
	for key := range c.globalCounts {
		if strings.HasPrefix(key, scope+"\x00") {
			delete(c.globalCounts, key)
		}
	}
}

func (c *ChaosInjector) hasGlobalPolicy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.globalRate != nil
}

// shouldTrigger decides whether a fault fires this request, applying the shared
// FailFirst-then-recover / Rate-probability semantics. FailFirst takes
// precedence: the first N requests fault deterministically (bumping counter),
// then it recovers. Otherwise it draws against the clamped rate. Caller must
// have run ensureMaps so counter is non-nil.
func (c *ChaosInjector) shouldTrigger(scope string, rate float64, failFirst int, counter map[string]int) bool {
	if failFirst > 0 {
		c.mu.Lock()
		counter[scope]++
		n := counter[scope]
		c.mu.Unlock()
		return n <= failFirst
	}
	r := clampUnitInterval(rate)
	c.mu.Lock()
	draw := c.RandSrc.Float64()
	c.mu.Unlock()
	return draw < r
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
	c.ensureMaps()
	// All stateful chaos counters are per (owning tenant, agent name).
	scope := chaosKey(agent)
	if cfg.RateLimit != nil && cfg.RateLimit.Requests > 0 {
		if err := c.checkRateLimit(scope, cfg.RateLimit); err != nil {
			// Metered here rather than inside each maybeInject* helper so the
			// counter can never drift from what was actually returned to the
			// caller: exactly one increment per fault that reached the client.
			metrics.RecordChaos(agent.Metadata.Name, metrics.ChaosRateLimit)
			return err
		}
	}
	if cfg.Errors != nil && (cfg.Errors.Rate > 0 || cfg.Errors.FailFirst > 0 || (c.hasGlobalPolicy() && hasConfiguredError(cfg.Errors))) {
		errorCfg := cfg.Errors
		if cfg.Errors.Rate == 0 && cfg.Errors.FailFirst == 0 {
			if !c.globalAllows(scope, agent.Metadata.Name, "error") {
				errorCfg = nil
			} else {
				copy := *cfg.Errors
				copy.Rate = 1
				errorCfg = &copy
			}
		}
		if errorCfg != nil {
			if err := c.maybeInjectError(ctx, scope, errorCfg); err != nil {
				metrics.RecordChaos(agent.Metadata.Name, metrics.ChaosError)
				return err
			}
		}
	}
	// Connection-layer fault (FB-03 slice 5): evaluated last — a low-level test
	// fixture below the HTTP-status faults above.
	if cfg.Connection != nil && (cfg.Connection.Rate > 0 || cfg.Connection.FailFirst > 0 || (c.hasGlobalPolicy() && cfg.Connection.Mode != "")) {
		connectionCfg := cfg.Connection
		if cfg.Connection.Rate == 0 && cfg.Connection.FailFirst == 0 {
			if !c.globalAllows(scope, agent.Metadata.Name, "connection") {
				connectionCfg = nil
			} else {
				copy := *cfg.Connection
				copy.Rate = 1
				connectionCfg = &copy
			}
		}
		if connectionCfg != nil {
			if err := c.maybeInjectConnectionFault(scope, connectionCfg); err != nil {
				metrics.RecordChaos(agent.Metadata.Name, metrics.ChaosConnection)
				return err
			}
		}
	}
	return nil
}

// maybeInjectConnectionFault returns a *ChaosError carrying the connection mode
// when this request should be faulted (by Rate or FailFirst), else nil.
func (c *ChaosInjector) maybeInjectConnectionFault(scope string, cc *types.ChaosConnectionConfig) error {
	if !c.shouldTrigger(scope, cc.Rate, cc.FailFirst, c.connCounts) {
		return nil
	}
	// Defense in depth: a blank Mode (e.g. from a caller that skipped the config
	// validator) defaults to "empty" so the ChaosError still marks a connection
	// fault — otherwise an empty Connection would fall through to the HTTP-status
	// path with StatusCode 0 (an invalid WriteHeader).
	mode := cc.Mode
	if mode == "" {
		mode = "empty"
	}
	return &ChaosError{Connection: mode}
}

// After sleeps for the configured latency distribution. Called once response
// generation has completed successfully, so latency is visible to clients
// without blocking error injection above.
func (c *ChaosInjector) After(ctx context.Context, agent *types.AgentDefinition) {
	cfg := chaosFor(agent)
	if cfg == nil || cfg.Latency == nil {
		return
	}
	if !c.globalAllows(chaosKey(agent), agent.Metadata.Name, "latency") {
		return
	}
	metrics.RecordChaos(agent.Metadata.Name, metrics.ChaosLatency)
	c.sleep(ctx, c.sampleLatency(cfg.Latency))
}

func hasConfiguredError(cfg *types.ChaosErrorConfig) bool {
	return cfg != nil && (cfg.StatusCode != 0 || len(cfg.StatusCodes) > 0 || cfg.Timeout || cfg.Message != "")
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
	return cfg.Latency != nil || cfg.Errors != nil || cfg.RateLimit != nil || cfg.Connection != nil
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
//
// Degenerate ranges fall back rather than erroring (F-CH-005): an explicit
// "uniform" with MaxMs <= MinMs returns MinMs — i.e. it silently acts like
// "fixed" — and "normal" with StddevMs == 0 collapses to MeanMs. So a
// mis-ordered or single-value range still produces a sensible fixed delay
// instead of a panic or validation failure.
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
// timeout fault is selected it BLOCKS the calling request goroutine for the
// full TimeoutMs before returning the 504 (F-CH-007) — this is a real sleep
// on the request's critical path, not a deadline annotation. It is cut short
// only if ctx is cancelled (e.g. the client disconnects).
func (c *ChaosInjector) maybeInjectError(ctx context.Context, agentName string, e *types.ChaosErrorConfig) error {
	// FailFirst-then-recover / Rate-probability trigger (shared with the
	// connection-fault path). Rate is clamped to [0,1] inside shouldTrigger.
	if !c.shouldTrigger(agentName, e.Rate, e.FailFirst, c.errorCounts) {
		return nil
	}

	if e.Timeout {
		timeout := time.Duration(e.TimeoutMs) * time.Millisecond
		// Same ceiling as a latency draw: the validator rejects larger values,
		// but a definition can reach the engine without passing through it
		// (in-process registration), and the sleep is on the request path.
		if timeout > maxChaosLatencyMs*time.Millisecond {
			timeout = maxChaosLatencyMs * time.Millisecond
		}
		c.sleep(ctx, timeout)
		return &ChaosError{
			StatusCode: http.StatusGatewayTimeout,
			Message:    "request timed out",
			RetryAfter: timeout,
			Timeout:    true,
		}
	}

	status := c.pickStatusCode(e)
	if status < 400 || status > 599 {
		// 0 means "unset"; anything else outside the error range would reach
		// ResponseWriter.WriteHeader, which panics on codes outside 100-999
		// and would mislabel a 2xx as a fault. The validator rejects these,
		// but the engine must not trust that every definition went through it.
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
func (c *ChaosInjector) checkRateLimit(scope string, rl *types.ChaosRateLimitConfig) error {
	if rl.WindowMs <= 0 {
		return nil
	}
	now := c.Now()
	window := time.Duration(rl.WindowMs) * time.Millisecond

	c.mu.Lock()
	defer c.mu.Unlock()

	bucket, ok := c.buckets[scope]
	if !ok || now.Sub(bucket.windowStart) >= window {
		c.buckets[scope] = &rateBucket{windowStart: now, count: 1}
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
