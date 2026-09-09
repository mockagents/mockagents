package engine

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// TestChaosStatusCodeOutOfRangeFallsBackTo500 is the engine-side half of the
// audit M-01 fix: a definition that reaches the engine without passing the
// validator (in-process registration, a hand-built AgentDefinition) must not
// hand net/http a code it panics on, nor a 2xx dressed up as a fault.
func TestChaosStatusCodeOutOfRangeFallsBackTo500(t *testing.T) {
	for _, code := range []int{42, 200, 399, 600, 1000} {
		inj, _, _ := newChaosInjectorForTest(1)
		agent := agentWithChaos(&types.ChaosConfig{
			Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCode: code},
		})
		ce := AsChaosError(inj.Before(context.Background(), agent))
		if ce == nil {
			t.Fatalf("status %d: expected a ChaosError", code)
		}
		if ce.StatusCode != http.StatusInternalServerError {
			t.Errorf("status %d: injected %d, want 500 fallback", code, ce.StatusCode)
		}
	}
	// And the list form: an out-of-range entry falls back, an in-range one is kept.
	inj, _, _ := newChaosInjectorForTest(1)
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCodes: []int{7}},
	})
	if ce := AsChaosError(inj.Before(context.Background(), agent)); ce == nil || ce.StatusCode != 500 {
		t.Errorf("status_codes [7]: got %v, want 500 fallback", ce)
	}
	inj, _, _ = newChaosInjectorForTest(1)
	agent = agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 1, StatusCodes: []int{503}},
	})
	if ce := AsChaosError(inj.Before(context.Background(), agent)); ce == nil || ce.StatusCode != 503 {
		t.Errorf("status_codes [503]: got %v, want 503", ce)
	}
}

// TestChaosTimeoutSleepIsCapped: the synthetic timeout really sleeps the
// request goroutine, so it shares the latency ceiling instead of honouring an
// arbitrary timeout_ms.
func TestChaosTimeoutSleepIsCapped(t *testing.T) {
	inj, _, _ := newChaosInjectorForTest(1)
	var slept time.Duration
	inj.Sleep = func(d time.Duration) { slept = d }
	agent := agentWithChaos(&types.ChaosConfig{
		Errors: &types.ChaosErrorConfig{FailFirst: 1, Timeout: true, TimeoutMs: 10 * maxChaosLatencyMs},
	})
	ce := AsChaosError(inj.Before(context.Background(), agent))
	if ce == nil || !ce.Timeout {
		t.Fatalf("expected a timeout ChaosError, got %v", ce)
	}
	if want := maxChaosLatencyMs * time.Millisecond; slept != want {
		t.Errorf("slept %v, want capped %v", slept, want)
	}
}
