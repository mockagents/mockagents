package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/types"
)

// newChaosInjectorForTest builds a ChaosInjector with a deterministic rand
// source and a stub sleeper so tests never block on real time.
func newChaosInjectorForTest(seed int64) (*ChaosInjector, *[]time.Duration, *time.Time) {
	sleeps := &[]time.Duration{}
	now := time.Unix(1_700_000_000, 0)
	nowPtr := &now
	inj := &ChaosInjector{
		Now:     func() time.Time { return *nowPtr },
		Sleep:   func(d time.Duration) { *sleeps = append(*sleeps, d) },
		RandSrc: rand.New(rand.NewSource(seed)),
		buckets: make(map[string]*rateBucket),
	}
	return inj, sleeps, nowPtr
}

func agentWithChaos(cfg *types.ChaosConfig) *types.AgentDefinition {
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "chaotic"},
		Spec: types.AgentSpec{
			Behavior: types.BehaviorConfig{
				Chaos: cfg,
			},
		},
	}
}

func TestChaosLatencyFixed(t *testing.T) {
	inj, sleeps, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Latency: &types.ChaosLatencyConfig{Distribution: "fixed", MinMs: 250},
	})

	inj.After(context.Background(), agent)

	if len(*sleeps) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(*sleeps))
	}
	if (*sleeps)[0] != 250*time.Millisecond {
		t.Errorf("sleep = %s, want 250ms", (*sleeps)[0])
	}
}

func TestChaosLatencyUniformBounds(t *testing.T) {
	inj, sleeps, _ := newChaosInjectorForTest(42)
	agent := agentWithChaos(&types.ChaosConfig{
		Latency: &types.ChaosLatencyConfig{Distribution: "uniform", MinMs: 100, MaxMs: 200},
	})

	for i := 0; i < 50; i++ {
		inj.After(context.Background(), agent)
	}
	if len(*sleeps) != 50 {
		t.Fatalf("expected 50 sleeps, got %d", len(*sleeps))
	}
	for i, s := range *sleeps {
		if s < 100*time.Millisecond || s > 200*time.Millisecond {
			t.Errorf("sleep %d = %s out of [100,200]ms", i, s)
		}
	}
}

func TestChaosLatencyUniformDegradesToFixed(t *testing.T) {
	// F-CH-005: an explicit "uniform" with MaxMs <= MinMs returns MinMs
	// (silently behaves like "fixed") rather than erroring.
	for _, tc := range []struct{ min, max, want int }{
		{200, 100, 200}, // mis-ordered
		{150, 150, 150}, // single value
	} {
		inj, sleeps, _ := newChaosInjectorForTest(1)
		agent := agentWithChaos(&types.ChaosConfig{
			Latency: &types.ChaosLatencyConfig{Distribution: "uniform", MinMs: tc.min, MaxMs: tc.max},
		})
		inj.After(context.Background(), agent)
		if got := (*sleeps)[0]; got != time.Duration(tc.want)*time.Millisecond {
			t.Errorf("uniform(min=%d,max=%d) = %s, want %dms", tc.min, tc.max, got, tc.want)
		}
	}
}

func TestClampUnitInterval(t *testing.T) {
	// F-CH-004: probabilities must be bounded to [0,1].
	cases := []struct{ in, want float64 }{
		{-0.5, 0}, {0, 0}, {0.3, 0.3}, {1, 1}, {1.5, 1}, {1e9, 1},
	}
	for _, c := range cases {
		if got := clampUnitInterval(c.in); got != c.want {
			t.Errorf("clampUnitInterval(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestChaosLatencyNormalCappedAndNonNegative(t *testing.T) {
	// Exercises the restructured normal branch (F-CH-003 lock narrowing)
	// plus the F-CH-002 upper clamp. A mean far above the cap must produce
	// exactly maxChaosLatencyMs; a zero-mean distribution must never sleep
	// for a negative duration.
	injHigh, sleepsHigh, _ := newChaosInjectorForTest(7)
	high := agentWithChaos(&types.ChaosConfig{
		Latency: &types.ChaosLatencyConfig{Distribution: "normal", MeanMs: 10_000_000, StddevMs: 1},
	})
	injHigh.After(context.Background(), high)
	if got := (*sleepsHigh)[0]; got != maxChaosLatencyMs*time.Millisecond {
		t.Errorf("capped normal sleep = %s, want %dms", got, maxChaosLatencyMs)
	}

	injZero, sleepsZero, _ := newChaosInjectorForTest(7)
	zero := agentWithChaos(&types.ChaosConfig{
		Latency: &types.ChaosLatencyConfig{Distribution: "normal", MeanMs: 0, StddevMs: 1000},
	})
	for i := 0; i < 50; i++ {
		injZero.After(context.Background(), zero)
	}
	for i, s := range *sleepsZero {
		if s < 0 {
			t.Errorf("normal sleep %d = %s is negative", i, s)
		}
	}
}

func TestChaosErrorInjectionAlways(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{Rate: 1.0, StatusCode: http.StatusInternalServerError},
	})

	err := inj.Before(context.Background(), agent)
	ce := AsChaosError(err)
	if ce == nil {
		t.Fatalf("expected ChaosError, got %v", err)
	}
	if ce.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", ce.StatusCode)
	}
}

func TestChaosErrorInjectionNever(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{Rate: 0.0, StatusCode: http.StatusInternalServerError},
	})
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestChaosErrorStatusCodesPicked(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(7)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{
			Rate:        1.0,
			StatusCodes: []int{500, 503, 429},
		},
	})
	seen := make(map[int]bool)
	for i := 0; i < 30; i++ {
		err := inj.Before(context.Background(), agent)
		ce := AsChaosError(err)
		if ce == nil {
			t.Fatalf("expected ChaosError at iter %d, got %v", i, err)
		}
		seen[ce.StatusCode] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected at least 2 distinct status codes, saw %v", seen)
	}
}

func TestChaosTimeoutSleepsAndErrors(t *testing.T) {
	inj, sleeps, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{Rate: 1.0, Timeout: true, TimeoutMs: 750},
	})

	err := inj.Before(context.Background(), agent)
	ce := AsChaosError(err)
	if ce == nil || !ce.Timeout {
		t.Fatalf("expected timeout ChaosError, got %v", err)
	}
	if len(*sleeps) != 1 || (*sleeps)[0] != 750*time.Millisecond {
		t.Errorf("expected one 750ms sleep, got %v", *sleeps)
	}
}

func TestChaosRateLimitBlocksAfterBudget(t *testing.T) {
	inj, _, nowPtr := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		RateLimit: &types.ChaosRateLimitConfig{Requests: 2, WindowMs: 1000},
	})

	if err := inj.Before(context.Background(), agent); err != nil {
		t.Fatalf("request 1: %v", err)
	}
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	err := inj.Before(context.Background(), agent)
	ce := AsChaosError(err)
	if ce == nil || ce.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 ChaosError, got %v", err)
	}

	// Advance past the window and the agent should be unblocked.
	*nowPtr = nowPtr.Add(2 * time.Second)
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("after window expired, got %v", err)
	}
}

func TestEngineProcessRequestPropagatesChaos(t *testing.T) {
	reg := NewAgentRegistry()
	reg.Register(&types.AgentDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.AgentKind,
		Metadata: types.Metadata{Name: "chaotic"},
		Spec: types.AgentSpec{
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Scenarios: []types.Scenario{{
					Name:     "default",
					Response: types.ScenarioResponse{Content: "ok"},
				}},
				Chaos: &types.ChaosConfig{
					Errors: &types.ChaosErrorConfig{Rate: 1.0, StatusCode: http.StatusServiceUnavailable},
				},
			},
		},
	})
	eng := NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Swap in a deterministic chaos injector.
	eng.Chaos, _, _ = newChaosInjectorForTest(1)

	_, err := eng.ProcessRequest(&InboundRequest{
		AgentName: "chaotic",
		SessionID: "s1",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected chaos error, got nil")
	}
	ce := AsChaosError(err)
	if ce == nil {
		t.Fatalf("expected ChaosError, got %v (type %T)", err, err)
	}
	if ce.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", ce.StatusCode)
	}
}

func TestAsChaosErrorReturnsNilForOther(t *testing.T) {
	if ce := AsChaosError(nil); ce != nil {
		t.Errorf("nil -> want nil, got %v", ce)
	}
	if ce := AsChaosError(errors.New("plain")); ce != nil {
		t.Errorf("plain -> want nil, got %v", ce)
	}
}

// --- Context cancellation (review finding X-01) ---

func TestChaos_AfterHonorsContextCancellation(t *testing.T) {
	// Real injector (Sleep nil) so After uses the ctx-aware sleep path.
	inj := &ChaosInjector{
		RandSrc: rand.New(rand.NewSource(1)),
		buckets: make(map[string]*rateBucket),
	}
	agent := &types.AgentDefinition{
		Spec: types.AgentSpec{
			Behavior: types.BehaviorConfig{
				Chaos: &types.ChaosConfig{
					Latency: &types.ChaosLatencyConfig{Distribution: "fixed", MinMs: 10_000},
				},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the sleep starts

	start := time.Now()
	inj.After(ctx, agent)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("After ignored cancellation: blocked %s of a 10s latency", elapsed)
	}
}

func TestEngine_CancelledContextShortCircuits(t *testing.T) {
	e := newTurnEchoEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.ProcessRequestContext(ctx, &InboundRequest{
		AgentName: "echo",
		Messages:  []RequestMessage{{Role: "user", Content: "hi"}},
		SessionID: "s",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: got err = %v, want context.Canceled", err)
	}
}

// TestChaosFailFirstThenRecovers covers the Nth-call stateful trigger (FB-03):
// the first N requests fail deterministically, then every request recovers —
// the canonical retry/backoff fixture. Rate is 0, proving FailFirst drives
// injection on its own.
func TestChaosFailFirstThenRecovers(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 2, StatusCode: http.StatusServiceUnavailable},
	})

	for i := 1; i <= 2; i++ {
		ce := AsChaosError(inj.Before(context.Background(), agent))
		if ce == nil || ce.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("call %d: expected 503 ChaosError, got %v", i, ce)
		}
	}
	for i := 3; i <= 5; i++ {
		if err := inj.Before(context.Background(), agent); err != nil {
			t.Fatalf("call %d: expected recovery, got %v", i, err)
		}
	}
}

// TestChaosFailFirstIsPerAgent confirms each agent has its own counter, so one
// agent's first-N failures don't consume another's allotment.
func TestChaosFailFirstIsPerAgent(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	mk := func(name string) *types.AgentDefinition {
		return &types.AgentDefinition{
			Metadata: types.Metadata{Name: name},
			Spec: types.AgentSpec{Behavior: types.BehaviorConfig{
				Chaos: &types.ChaosConfig{Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCode: 500}},
			}},
		}
	}
	a, b := mk("agent-a"), mk("agent-b")
	if AsChaosError(inj.Before(context.Background(), a)) == nil {
		t.Fatal("agent-a call 1 should fail")
	}
	// agent-b is independent: its first call still fails.
	if AsChaosError(inj.Before(context.Background(), b)) == nil {
		t.Fatal("agent-b call 1 should fail (independent counter)")
	}
	// agent-a has recovered.
	if err := inj.Before(context.Background(), a); err != nil {
		t.Fatalf("agent-a call 2 should recover, got %v", err)
	}
}

// TestChaosFailFirstWithRateLimit pins the gate ordering (rate-limit BEFORE
// error injection) and the fail_first counter-consumption semantics: a request
// rejected by the rate gate does NOT consume the fail_first budget.
func TestChaosFailFirstWithRateLimit(t *testing.T) {
	inj, _, nowPtr := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors:    &types.ChaosErrorConfig{FailFirst: 2, StatusCode: http.StatusServiceUnavailable},
		RateLimit: &types.ChaosRateLimitConfig{Requests: 1, WindowMs: 1000},
	})
	ctx := context.Background()

	// req1: passes the rate gate (count 1), consumes 1 fail_first -> 503.
	if ce := AsChaosError(inj.Before(ctx, agent)); ce == nil || ce.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("req1: want 503, got %v", ce)
	}
	// req2 (same window): rate gate trips -> 429; fail_first NOT consumed.
	if ce := AsChaosError(inj.Before(ctx, agent)); ce == nil || ce.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("req2: want 429, got %v", ce)
	}
	// Advance past the window: req3 passes the rate gate, consumes the 2nd
	// fail_first -> 503 (proving req2 did not advance the counter).
	*nowPtr = nowPtr.Add(2 * time.Second)
	if ce := AsChaosError(inj.Before(ctx, agent)); ce == nil || ce.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("req3: want 503, got %v", ce)
	}
	// Advance again: the 2-failure budget is exhausted -> recovery.
	*nowPtr = nowPtr.Add(2 * time.Second)
	if err := inj.Before(ctx, agent); err != nil {
		t.Fatalf("req4: want recovery, got %v", err)
	}
}

// TestChaosFailFirstTimeout confirms the stateful trigger composes with the
// timeout fault: the first call blocks then 504s, the next recovers.
func TestChaosFailFirstTimeout(t *testing.T) {
	inj, sleeps, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 1, Timeout: true, TimeoutMs: 1000},
	})
	ce := AsChaosError(inj.Before(context.Background(), agent))
	if ce == nil || !ce.Timeout {
		t.Fatalf("expected timeout ChaosError, got %v", ce)
	}
	if len(*sleeps) != 1 || (*sleeps)[0] != time.Second {
		t.Errorf("expected one 1s sleep, got %v", *sleeps)
	}
	if err := inj.Before(context.Background(), agent); err != nil {
		t.Errorf("expected recovery after first failure, got %v", err)
	}
}
