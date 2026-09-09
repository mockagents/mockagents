package tenancy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestResolve_NegativeCacheSkipsBcrypt is the audit M-09 guard: a repeated
// wrong key costs one bcrypt, not one per attempt, and a key mutation
// clears the memory so a freshly valid key is never blocked.
func TestResolve_NegativeCacheSkipsBcrypt(t *testing.T) {
	s := newTestStore(t)
	s.EnableAuthCache(time.Minute, 64)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	real, err := s.CreateAPIKey(ctx, tenant.ID, "svc", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	wrong := real.Plaintext[:len(real.Plaintext)-4] + "XXXX"        // same prefix, wrong secret
	unknown := "mak_00000000_" + "aB3dE6gH9jK2mN5pQ8sT1vW4xZ7yC0eF" // unknown prefix

	before := bcryptCompares.Load()
	for i := 0; i < 5; i++ {
		if _, err := s.Resolve(ctx, wrong); err != ErrInvalidKey {
			t.Fatalf("wrong key attempt %d: %v", i, err)
		}
		if _, err := s.Resolve(ctx, unknown); err != ErrInvalidKey {
			t.Fatalf("unknown key attempt %d: %v", i, err)
		}
	}
	if n := bcryptCompares.Load() - before; n != 2 {
		t.Fatalf("bcrypt compares for 5+5 repeated bad keys = %d, want 2 (one per distinct key)", n)
	}
	if s.cache.NegativeLen() != 2 {
		t.Fatalf("negative entries = %d, want 2", s.cache.NegativeLen())
	}
	// The real key still resolves (a negative for a different plaintext must
	// not bleed over).
	if p, err := s.Resolve(ctx, real.Plaintext); err != nil || p.KeyID != real.Key.ID {
		t.Fatalf("real key: %+v %v", p, err)
	}
	// A key mutation flushes negatives with the rest of the cache.
	if _, err := s.CreateAPIKey(ctx, tenant.ID, "other", RoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpdateAPIKeyRole(ctx, tenant.ID, real.Key.ID, RoleEditor); err != nil {
		t.Fatal(err)
	}
	if s.cache.NegativeLen() != 0 {
		t.Fatalf("negatives survived Invalidate: %d", s.cache.NegativeLen())
	}
}

// countingStore rejects every credential and counts how often it was asked.
type countingStore struct {
	Store
	calls atomic.Int64
}

func (c *countingStore) Resolve(context.Context, string) (*Principal, error) {
	c.calls.Add(1)
	return nil, ErrInvalidKey
}

func (c *countingStore) ResolveSession(context.Context, string) (*Principal, error) {
	c.calls.Add(1)
	return nil, ErrInvalidSession
}

// TestAuthMiddleware_PerIPFailureLimit: after the per-minute failure budget
// is spent, further attempts from that address get 429 before the store is
// consulted; another address is unaffected; and the budget refills.
func TestAuthMiddleware_PerIPFailureLimit(t *testing.T) {
	SetAuthFailureLimit(3)
	t.Cleanup(func() { SetAuthFailureLimit(DefaultAuthFailuresPerMinute) })
	// Deterministic clock for the refill check.
	l := authLimiter.Load()
	now := time.Unix(1_700_000_000, 0)
	l.now = func() time.Time { return now }

	store := &countingStore{}
	h := AuthMiddleware(store, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	attempt := func(addr, key string) int {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		r.RemoteAddr = addr
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}

	for i := 0; i < 3; i++ {
		if code := attempt("203.0.113.5:1000", "mak_bad"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d, want 401", i, code)
		}
	}
	if store.calls.Load() != 3 {
		t.Fatalf("store consulted %d times, want 3", store.calls.Load())
	}
	if code := attempt("203.0.113.5:1001", "mak_bad"); code != http.StatusTooManyRequests {
		t.Fatalf("4th attempt: %d, want 429", code)
	}
	if code := attempt("203.0.113.5:1002", ""); code != http.StatusTooManyRequests {
		t.Fatalf("credential-less attempt over budget: %d, want 429", code)
	}
	if store.calls.Load() != 3 {
		t.Fatalf("store consulted after the budget was spent (%d calls)", store.calls.Load())
	}
	// A different source is independent.
	if code := attempt("198.51.100.9:1000", "mak_bad"); code != http.StatusUnauthorized {
		t.Fatalf("other address: %d, want 401", code)
	}
	// The budget refills: 20s later at 3/min one token is back.
	now = now.Add(21 * time.Second)
	if code := attempt("203.0.113.5:1003", "mak_bad"); code != http.StatusUnauthorized {
		t.Fatalf("after refill: %d, want 401", code)
	}
	if code := attempt("203.0.113.5:1004", "mak_bad"); code != http.StatusTooManyRequests {
		t.Fatalf("budget spent again: %d, want 429", code)
	}
	// Disabled limiter never 429s.
	SetAuthFailureLimit(0)
	for i := 0; i < 10; i++ {
		if code := attempt("203.0.113.5:2000", "mak_bad"); code != http.StatusUnauthorized {
			t.Fatalf("limiter disabled: %d, want 401", code)
		}
	}
}
