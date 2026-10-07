// Package retry implements bounded retries with exponential backoff and
// jitter, Retry-After support and deadline awareness.
//
// Three properties keep a retrying caller from ever hanging:
//
//  1. Retries are capped (MaxAllowedRetries = 5, enforced by Validate).
//  2. Every wait is cancellable through the context.
//  3. A retry whose backoff would end after the context's deadline is not
//     attempted. Do gives up early with ErrBudgetExceeded instead of
//     sleeping into a guaranteed failure.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// MaxAllowedRetries is the hard ceiling on retries per call.
const MaxAllowedRetries = 5

// MaxRetryAfter caps how long a server-provided Retry-After can make us wait.
const MaxRetryAfter = 30 * time.Second

// Policy configures retries. Zero values are replaced by Defaults().
type Policy struct {
	// MaxRetries is the number of retries after the first attempt (0..5).
	MaxRetries int `json:"max_retries"`
	// InitialBackoffMS is the delay before the first retry.
	InitialBackoffMS int `json:"initial_backoff_ms"`
	// MaxBackoffMS caps the exponential delay.
	MaxBackoffMS int `json:"max_backoff_ms"`
	// Multiplier grows the delay each retry (2.0 doubles it).
	Multiplier float64 `json:"multiplier"`
	// Jitter randomizes each delay by up to ±Jitter (0..1) of itself.
	Jitter float64 `json:"jitter"`
}

// Defaults is the policy used when none is configured.
func Defaults() Policy {
	return Policy{MaxRetries: 3, InitialBackoffMS: 200, MaxBackoffMS: 4000, Multiplier: 2.0, Jitter: 0.2}
}

// Validate rejects out-of-range policies. MaxRetries above 5 is an error,
// not silently clamped: the cap is a documented contract.
func (p Policy) Validate() error {
	var errs []error
	if p.MaxRetries < 0 || p.MaxRetries > MaxAllowedRetries {
		errs = append(errs, fmt.Errorf("max_retries must be between 0 and %d (got %d)", MaxAllowedRetries, p.MaxRetries))
	}
	if p.InitialBackoffMS < 0 || p.InitialBackoffMS > 60_000 {
		errs = append(errs, fmt.Errorf("initial_backoff_ms must be between 0 and 60000 (got %d)", p.InitialBackoffMS))
	}
	if p.MaxBackoffMS < 0 || p.MaxBackoffMS > 120_000 {
		errs = append(errs, fmt.Errorf("max_backoff_ms must be between 0 and 120000 (got %d)", p.MaxBackoffMS))
	}
	if p.MaxBackoffMS > 0 && p.InitialBackoffMS > p.MaxBackoffMS {
		errs = append(errs, fmt.Errorf("initial_backoff_ms (%d) must not exceed max_backoff_ms (%d)", p.InitialBackoffMS, p.MaxBackoffMS))
	}
	if p.Multiplier != 0 && (p.Multiplier < 1 || p.Multiplier > 10) {
		errs = append(errs, fmt.Errorf("multiplier must be between 1 and 10 (got %g)", p.Multiplier))
	}
	if p.Jitter < 0 || p.Jitter > 1 {
		errs = append(errs, fmt.Errorf("jitter must be between 0 and 1 (got %g)", p.Jitter))
	}
	return errors.Join(errs...)
}

// Normalize fills unset delay fields from Defaults. MaxRetries is kept
// as-is because 0 is a meaningful choice ("never retry").
func (p Policy) Normalize() Policy {
	d := Defaults()
	if p.InitialBackoffMS == 0 {
		p.InitialBackoffMS = d.InitialBackoffMS
	}
	if p.MaxBackoffMS == 0 {
		p.MaxBackoffMS = d.MaxBackoffMS
	}
	if p.Multiplier == 0 {
		p.Multiplier = d.Multiplier
	}
	return p
}

// Delay returns the wait before retry number n (1-based). retryAfter, when
// positive, is a server hint: the wait is at least that long (capped at
// MaxRetryAfter) even if it exceeds MaxBackoffMS, because ignoring a 429's
// Retry-After just earns another 429. rnd returns a value in [0,1); nil
// uses math/rand.
func (p Policy) Delay(n int, retryAfter time.Duration, rnd func() float64) time.Duration {
	p = p.Normalize()
	if n < 1 {
		n = 1
	}
	base := float64(p.InitialBackoffMS) * math.Pow(p.Multiplier, float64(n-1))
	if max := float64(p.MaxBackoffMS); base > max {
		base = max
	}
	if p.Jitter > 0 {
		if rnd == nil {
			rnd = rand.Float64
		}
		// Symmetric jitter: base * (1 ± jitter).
		base *= 1 + p.Jitter*(2*rnd()-1)
	}
	d := time.Duration(base) * time.Millisecond
	if retryAfter > 0 {
		if retryAfter > MaxRetryAfter {
			retryAfter = MaxRetryAfter
		}
		if retryAfter > d {
			d = retryAfter
		}
	}
	return d
}

// Classifier decides whether an error is retryable, gives a short reason,
// and returns any Retry-After hint.
type Classifier func(error) (retryable bool, reason string, retryAfter time.Duration)

// Attempt describes one finished attempt, reported to the observer.
type Attempt struct {
	// Number is 1 for the first try, 2 for the first retry, and so on.
	Number    int
	Err       error
	Retryable bool
	Reason    string
	// NextDelay is the wait before the next attempt. It is 0 when the attempt
	// succeeded or no retry follows.
	NextDelay time.Duration
	Duration  time.Duration
}

// ErrBudgetExceeded means the next backoff would outlive the context deadline.
var ErrBudgetExceeded = errors.New("retry budget exceeded: next backoff would pass the deadline")

// ExhaustedError reports a call that failed after its last allowed attempt.
type ExhaustedError struct {
	Attempts int
	Reason   string
	Last     error
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("gave up after %d attempt(s) (%s): %v", e.Attempts, e.Reason, e.Last)
}

func (e *ExhaustedError) Unwrap() error { return e.Last }

// Options tune Do.
type Options struct {
	Classify Classifier
	// Observe is called after every attempt (success or failure).
	Observe func(Attempt)
	// Rand, when set, makes jitter deterministic (tests).
	Rand func() float64
	// Sleep, when set, replaces the real timer (tests). It must honour ctx.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Do runs fn until it succeeds, fails permanently, or runs out of retries.
// fn receives the 1-based attempt number. Do returns the number of attempts
// made and the final error: nil, the permanent error, an *ExhaustedError, or
// the context error.
func Do(ctx context.Context, p Policy, opts Options, fn func(ctx context.Context, attempt int) error) (int, error) {
	if err := p.Validate(); err != nil {
		return 0, fmt.Errorf("invalid retry policy: %w", err)
	}
	p = p.Normalize()
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	maxAttempts := p.MaxRetries + 1
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return attempt - 1, err
		}
		start := time.Now()
		err := fn(ctx, attempt)
		a := Attempt{Number: attempt, Err: err, Duration: time.Since(start)}
		if err == nil {
			observe(opts.Observe, a)
			return attempt, nil
		}
		retryable, reason, retryAfter := false, "error", time.Duration(0)
		if opts.Classify != nil {
			retryable, reason, retryAfter = opts.Classify(err)
		}
		a.Retryable, a.Reason = retryable, reason
		if ctx.Err() != nil {
			observe(opts.Observe, a)
			return attempt, ctx.Err()
		}
		if !retryable {
			observe(opts.Observe, a)
			return attempt, err
		}
		if attempt >= maxAttempts {
			observe(opts.Observe, a)
			return attempt, &ExhaustedError{Attempts: attempt, Reason: reason, Last: err}
		}
		delay := p.Delay(attempt, retryAfter, opts.Rand)
		if dl, ok := ctx.Deadline(); ok && time.Now().Add(delay).After(dl) {
			observe(opts.Observe, a)
			return attempt, &ExhaustedError{Attempts: attempt, Reason: reason, Last: fmt.Errorf("%w (last error: %v)", ErrBudgetExceeded, err)}
		}
		a.NextDelay = delay
		observe(opts.Observe, a)
		if err := sleep(ctx, delay); err != nil {
			return attempt, err
		}
	}
}

func observe(fn func(Attempt), a Attempt) {
	if fn != nil {
		fn(a)
	}
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
