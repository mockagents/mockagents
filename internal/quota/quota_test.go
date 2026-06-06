package quota

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// errBackend always fails, to exercise the local-fallback path.
type errBackend struct{}

func (errBackend) AddSpend(context.Context, string, string, float64) (float64, error) {
	return 0, errors.New("backend down")
}
func (errBackend) GetSpend(context.Context, string, string) (float64, error) {
	return 0, errors.New("backend down")
}

// fakeSpendBackend is an in-test shared ledger standing in for the tenancy
// store, letting one Enforcer observe "another replica's" increments.
type fakeSpendBackend struct {
	mu     sync.Mutex
	totals map[string]float64
}

func newFakeBackend() *fakeSpendBackend { return &fakeSpendBackend{totals: map[string]float64{}} }

func (f *fakeSpendBackend) AddSpend(_ context.Context, tenantID, month string, usd float64) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := tenantID + "|" + month
	f.totals[k] += usd
	return f.totals[k], nil
}

func (f *fakeSpendBackend) GetSpend(_ context.Context, tenantID, month string) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.totals[tenantID+"|"+month], nil
}

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

func TestEnforcer_SpendBackend_CrossReplica(t *testing.T) {
	e, clock := newClocked(t, Config{MonthlySpendUSD: 10})
	be := newFakeBackend()
	e.SetSpendBackend(be)

	// This replica accrues 4 via write-through; its cache reflects the total.
	e.AddSpend("ten_a", 4)
	if !e.CheckSpend("ten_a") {
		t.Fatal("4 < 10 should pass")
	}
	if u := e.Usage("ten_a"); u.SpendUSD != 4 {
		t.Errorf("usage = %+v, want 4", u)
	}

	// Another replica accrues 7 straight to the shared ledger (total now 11).
	if _, err := be.AddSpend(context.Background(), "ten_a", "2026-06", 7); err != nil {
		t.Fatal(err)
	}
	// Within the cache TTL this replica still serves its stale local view (4).
	if !e.CheckSpend("ten_a") {
		t.Error("within TTL the cached under-cap view should still pass")
	}
	// After the TTL it refreshes from the shared ledger and sees the real 11.
	*clock = clock.Add(spendCacheTTL + time.Second)
	if e.CheckSpend("ten_a") {
		t.Error("after refresh, 11 >= 10 must fail — cross-replica spend counted")
	}
	if u := e.Usage("ten_a"); u.SpendUSD != 11 {
		t.Errorf("usage after refresh = %+v, want 11", u)
	}
}

func TestEnforcer_SpendBackend_ErrorFallsBackLocal(t *testing.T) {
	e, _ := newClocked(t, Config{MonthlySpendUSD: 5})
	e.SetSpendBackend(errBackend{})

	// A backend error must not lose the charge: it bumps the local counter.
	e.AddSpend("ten_a", 3)
	e.AddSpend("ten_a", 3) // local total 6 > cap 5
	if e.CheckSpend("ten_a") {
		t.Error("local fallback should still enforce the cap when the backend errors")
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
