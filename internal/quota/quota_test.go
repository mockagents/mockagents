package quota

import (
	"testing"
	"time"
)

// newClocked returns an Enforcer whose clock is driven by the returned pointer,
// so tests advance time deterministically.
func newClocked(t *testing.T, defaults Config) (*Enforcer, *time.Time) {
	t.Helper()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	e := NewEnforcer(defaults)
	clock := &now
	e.now = func() time.Time { return *clock }
	return e, clock
}

func TestEnforcer_RateLimit(t *testing.T) {
	e, clock := newClocked(t, Config{RatePerSec: 2, RateBurst: 2})

	// A fresh tenant starts with a full bucket of 2.
	for i := 0; i < 2; i++ {
		if ok, _ := e.AllowRequest("ten_a"); !ok {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	// Third is denied with a retry hint.
	ok, retry := e.AllowRequest("ten_a")
	if ok {
		t.Fatal("third request should be rate-limited")
	}
	if retry <= 0 || retry > time.Second {
		t.Errorf("retryAfter = %v, want (0, 1s] at 2/sec", retry)
	}

	// After 0.5s one token refills (rate 2/sec).
	*clock = clock.Add(500 * time.Millisecond)
	if ok, _ := e.AllowRequest("ten_a"); !ok {
		t.Error("a token should have refilled after 0.5s")
	}
	// Bucket empty again.
	if ok, _ := e.AllowRequest("ten_a"); ok {
		t.Error("bucket should be empty immediately after the refilled token is spent")
	}
}

func TestEnforcer_RateUnlimitedAndAnonymous(t *testing.T) {
	e, _ := newClocked(t, Config{RatePerSec: 0}) // unlimited
	for i := 0; i < 100; i++ {
		if ok, _ := e.AllowRequest("ten_a"); !ok {
			t.Fatalf("unlimited rate should always allow (i=%d)", i)
		}
	}
	// Anonymous (empty tenant) is never limited, even with a strict default.
	strict, _ := newClocked(t, Config{RatePerSec: 1, RateBurst: 1})
	for i := 0; i < 10; i++ {
		if ok, _ := strict.AllowRequest(""); !ok {
			t.Fatal("empty tenant must never be rate-limited")
		}
	}
}

func TestEnforcer_SpendCap(t *testing.T) {
	e, clock := newClocked(t, Config{MonthlySpendUSD: 1.0})

	if !e.CheckSpend("ten_a") {
		t.Fatal("new tenant should be under cap")
	}
	e.AddSpend("ten_a", 0.6)
	if !e.CheckSpend("ten_a") {
		t.Fatal("0.6 < 1.0 should still pass")
	}
	e.AddSpend("ten_a", 0.5) // total 1.1
	if e.CheckSpend("ten_a") {
		t.Fatal("1.1 >= 1.0 should fail")
	}
	if u := e.Usage("ten_a"); u.SpendUSD != 1.1 || u.Month != "2026-06" {
		t.Errorf("usage = %+v, want spend 1.1 month 2026-06", u)
	}

	// Crossing into July resets the counter.
	*clock = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if !e.CheckSpend("ten_a") {
		t.Error("spend should reset at the month boundary")
	}
	if u := e.Usage("ten_a"); u.SpendUSD != 0 || u.Month != "2026-07" {
		t.Errorf("july usage = %+v, want 0 / 2026-07", u)
	}
}

func TestEnforcer_SpendUnlimitedAndAnonymous(t *testing.T) {
	e, _ := newClocked(t, Config{MonthlySpendUSD: 0}) // unlimited
	e.AddSpend("ten_a", 1000)
	if !e.CheckSpend("ten_a") {
		t.Error("unlimited spend cap should always pass")
	}
	// Anonymous traffic is never spend-limited and AddSpend is a no-op.
	strict, _ := newClocked(t, Config{MonthlySpendUSD: 0.01})
	strict.AddSpend("", 5)
	if !strict.CheckSpend("") {
		t.Error("empty tenant must never be spend-limited")
	}
}

func TestEnforcer_Override(t *testing.T) {
	e, _ := newClocked(t, Config{RatePerSec: 1, RateBurst: 1, MonthlySpendUSD: 1})

	// Default gives ten_a a burst of 1.
	if ok, _ := e.AllowRequest("ten_a"); !ok {
		t.Fatal("first default request allowed")
	}
	if ok, _ := e.AllowRequest("ten_a"); ok {
		t.Fatal("second default request denied (burst 1)")
	}

	// Override ten_b with a higher cap; it should reflect immediately.
	e.SetOverride("ten_b", Config{RatePerSec: 100, RateBurst: 50, MonthlySpendUSD: 999})
	if got := e.Effective("ten_b"); got.RateBurst != 50 || got.MonthlySpendUSD != 999 {
		t.Errorf("effective override = %+v", got)
	}
	for i := 0; i < 50; i++ {
		if ok, _ := e.AllowRequest("ten_b"); !ok {
			t.Fatalf("override burst should allow request %d", i)
		}
	}
	// ten_a still on the strict default.
	if got := e.Effective("ten_a"); got.RateBurst != 1 {
		t.Errorf("ten_a should keep defaults, got %+v", got)
	}
}
