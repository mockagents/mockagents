package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTransient = errors.New("transient")
var errPermanent = errors.New("permanent")

func classify(err error) (bool, string, time.Duration) {
	switch {
	case errors.Is(err, errTransient):
		return true, "transient", 0
	default:
		return false, "permanent", 0
	}
}

// noSleep records requested delays instead of sleeping.
func noSleep(rec *[]time.Duration) func(context.Context, time.Duration) error {
	return func(ctx context.Context, d time.Duration) error {
		*rec = append(*rec, d)
		return ctx.Err()
	}
}

func TestValidate(t *testing.T) {
	ok := []Policy{Defaults(), {MaxRetries: 0}, {MaxRetries: 5, InitialBackoffMS: 100, MaxBackoffMS: 100, Multiplier: 1, Jitter: 1}}
	for _, p := range ok {
		if err := p.Validate(); err != nil {
			t.Errorf("%+v: unexpected %v", p, err)
		}
	}
	bad := []Policy{{MaxRetries: 6}, {MaxRetries: -1}, {Jitter: 1.5}, {Multiplier: 0.5}, {InitialBackoffMS: 500, MaxBackoffMS: 100}}
	for _, p := range bad {
		if err := p.Validate(); err == nil {
			t.Errorf("%+v: expected an error", p)
		}
	}
}

func TestDelayIsExponentialAndCapped(t *testing.T) {
	p := Policy{MaxRetries: 5, InitialBackoffMS: 200, MaxBackoffMS: 1000, Multiplier: 2}
	want := []time.Duration{200, 400, 800, 1000, 1000}
	for i, w := range want {
		if got := p.Delay(i+1, 0, nil); got != w*time.Millisecond {
			t.Errorf("retry %d: delay %v, want %v", i+1, got, w*time.Millisecond)
		}
	}
}

func TestDelayJitterStaysInBand(t *testing.T) {
	p := Policy{InitialBackoffMS: 1000, MaxBackoffMS: 1000, Multiplier: 2, Jitter: 0.2}
	if got := p.Delay(1, 0, func() float64 { return 0 }); got != 800*time.Millisecond {
		t.Errorf("min jitter: %v", got)
	}
	if got := p.Delay(1, 0, func() float64 { return 0.999999 }); got < 1199*time.Millisecond || got > 1200*time.Millisecond {
		t.Errorf("max jitter: %v", got)
	}
}

func TestRetryAfterIsHonouredAndCapped(t *testing.T) {
	p := Policy{InitialBackoffMS: 100, MaxBackoffMS: 500, Multiplier: 2}
	if got := p.Delay(1, 2*time.Second, nil); got != 2*time.Second {
		t.Errorf("Retry-After above max_backoff must still be honoured: %v", got)
	}
	if got := p.Delay(1, time.Hour, nil); got != MaxRetryAfter {
		t.Errorf("Retry-After must be capped at %v: %v", MaxRetryAfter, got)
	}
	if got := p.Delay(3, 10*time.Millisecond, nil); got != 400*time.Millisecond {
		t.Errorf("a small Retry-After must not shorten backoff: %v", got)
	}
}

func TestDoRetriesThenSucceeds(t *testing.T) {
	var delays []time.Duration
	var observed []Attempt
	calls := 0
	n, err := Do(context.Background(), Policy{MaxRetries: 3, InitialBackoffMS: 10, MaxBackoffMS: 100, Multiplier: 2},
		Options{Classify: classify, Sleep: noSleep(&delays), Observe: func(a Attempt) { observed = append(observed, a) }},
		func(context.Context, int) error {
			calls++
			if calls < 3 {
				return errTransient
			}
			return nil
		})
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(delays) != 2 || delays[0] != 10*time.Millisecond || delays[1] != 20*time.Millisecond {
		t.Errorf("delays %v", delays)
	}
	if len(observed) != 3 || observed[2].Err != nil || observed[0].NextDelay == 0 {
		t.Errorf("observed %+v", observed)
	}
}

func TestDoStopsOnPermanentError(t *testing.T) {
	calls := 0
	var delays []time.Duration
	n, err := Do(context.Background(), Defaults(), Options{Classify: classify, Sleep: noSleep(&delays)},
		func(context.Context, int) error { calls++; return errPermanent })
	if !errors.Is(err, errPermanent) || n != 1 || calls != 1 || len(delays) != 0 {
		t.Fatalf("n=%d calls=%d err=%v", n, calls, err)
	}
}

func TestDoExhaustsAtMaxRetries(t *testing.T) {
	for retries := 0; retries <= MaxAllowedRetries; retries++ {
		calls := 0
		var delays []time.Duration
		n, err := Do(context.Background(), Policy{MaxRetries: retries}, Options{Classify: classify, Sleep: noSleep(&delays)},
			func(context.Context, int) error { calls++; return errTransient })
		var ex *ExhaustedError
		if !errors.As(err, &ex) || ex.Attempts != retries+1 || calls != retries+1 || n != retries+1 {
			t.Fatalf("retries=%d: calls=%d err=%v", retries, calls, err)
		}
		if !errors.Is(err, errTransient) {
			t.Errorf("ExhaustedError must unwrap to the last error")
		}
	}
}

func TestDoRejectsInvalidPolicy(t *testing.T) {
	if _, err := Do(context.Background(), Policy{MaxRetries: 9}, Options{}, func(context.Context, int) error { return nil }); err == nil {
		t.Fatal("expected an invalid-policy error")
	}
}

func TestDoGivesUpBeforeSleepingPastTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Do(ctx, Policy{MaxRetries: 5, InitialBackoffMS: 10_000, MaxBackoffMS: 10_000}, Options{Classify: classify},
		func(context.Context, int) error { return errTransient })
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Do slept instead of giving up")
	}
}

func TestDoHonoursCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := Do(ctx, Policy{MaxRetries: 5, InitialBackoffMS: 5_000, MaxBackoffMS: 5_000}, Options{Classify: classify},
		func(context.Context, int) error { return errTransient })
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}
