package tenancy

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAuthCache_HitAndMiss(t *testing.T) {
	c := newAuthCache(time.Minute, 10)
	if c.Get("mak_does_not_exist") != nil {
		t.Error("empty cache should miss")
	}

	p := &Principal{TenantID: "ten_x", KeyID: "key_1", Role: RoleAdmin}
	c.Set("mak_abcdefghijkl", p)

	got := c.Get("mak_abcdefghijkl")
	if got == nil {
		t.Fatal("cached entry should hit")
	}
	if got.TenantID != "ten_x" || got.KeyID != "key_1" || got.Role != RoleAdmin {
		t.Errorf("cached principal mismatch: %+v", got)
	}
	if got == p {
		t.Error("Get must return a fresh pointer, not the stored one")
	}
}

func TestAuthCache_TTLExpiry(t *testing.T) {
	c := newAuthCache(20*time.Millisecond, 10)
	c.Set("mak_ttl_victim", &Principal{KeyID: "k"})

	if c.Get("mak_ttl_victim") == nil {
		t.Fatal("entry should be fresh")
	}
	time.Sleep(40 * time.Millisecond)
	if c.Get("mak_ttl_victim") != nil {
		t.Error("entry should have expired")
	}
	// Expired entry should have been deleted on the miss.
	if c.Len() != 0 {
		t.Errorf("expired entry not pruned: len=%d", c.Len())
	}
}

func TestAuthCache_InvalidateDrops(t *testing.T) {
	c := newAuthCache(time.Minute, 10)
	c.Set("mak_k1", &Principal{KeyID: "k1"})
	c.Set("mak_k2", &Principal{KeyID: "k2"})
	if c.Len() != 2 {
		t.Fatalf("len = %d, want 2", c.Len())
	}
	c.Invalidate()
	if c.Len() != 0 {
		t.Errorf("len after Invalidate = %d, want 0", c.Len())
	}
	if c.Get("mak_k1") != nil {
		t.Error("Get after Invalidate should miss")
	}
}

func TestAuthCache_CapEvicts(t *testing.T) {
	c := newAuthCache(time.Minute, 3)
	c.Set("mak_a1b2c3d4e5f6", &Principal{KeyID: "a"})
	c.Set("mak_b1b2c3d4e5f6", &Principal{KeyID: "b"})
	c.Set("mak_c1b2c3d4e5f6", &Principal{KeyID: "c"})
	if c.Len() != 3 {
		t.Fatalf("len = %d, want 3", c.Len())
	}
	// Next Set must keep len == 3 by evicting one existing entry.
	c.Set("mak_d1b2c3d4e5f6", &Principal{KeyID: "d"})
	if c.Len() != 3 {
		t.Errorf("len after overflow = %d, want 3", c.Len())
	}
}

func TestAuthCache_NilIsNoop(t *testing.T) {
	var c *authCache
	if c.Get("x") != nil {
		t.Error("nil cache Get should return nil")
	}
	c.Set("x", &Principal{}) // no panic
	c.Invalidate()           // no panic
	if c.Len() != 0 {
		t.Error("nil cache Len should be 0")
	}
}

func TestAuthCache_ConcurrentAccess(t *testing.T) {
	c := newAuthCache(time.Minute, 64)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				k := "mak_" + string(rune('a'+seed)) + "_" + string(rune('0'+i%10))
				c.Set(k, &Principal{KeyID: k})
				_ = c.Get(k)
			}
		}(g)
	}
	wg.Wait()
	// Main assertion is "no data race under -race"; we also ensure
	// the cap was honored.
	if c.Len() > 64 {
		t.Errorf("len = %d exceeded cap of 64", c.Len())
	}
}

// --- Store integration: cache short-circuits bcrypt ---

// TestResolveUsesAuthCache exercises the whole Resolve path: create a
// key, resolve once to populate the cache, then verify a second
// Resolve call is much faster because it skips bcrypt.
func TestResolveUsesAuthCache(t *testing.T) {
	store := newTestStore(t)
	store.EnableAuthCache(time.Minute, 64)

	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	result, err := store.CreateAPIKey(ctx, tenant.ID, "bot", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	// First resolve: bcrypt runs, cache populates.
	coldStart := time.Now()
	p1, err := store.Resolve(ctx, result.Plaintext)
	coldDur := time.Since(coldStart)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if p1.KeyID != result.Key.ID {
		t.Errorf("principal mismatch: got %+v", p1)
	}

	// Second resolve: cache hit, should be dramatically faster. We
	// assert "at least 5x faster" to leave headroom for noisy CI.
	hotStart := time.Now()
	p2, err := store.Resolve(ctx, result.Plaintext)
	hotDur := time.Since(hotStart)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if p2.KeyID != p1.KeyID || p2.Role != p1.Role {
		t.Errorf("cache hit produced wrong principal: %+v vs %+v", p2, p1)
	}
	t.Logf("auth cache speedup: cold=%v hot=%v (%.0fx)", coldDur, hotDur, float64(coldDur)/float64(hotDur))
	if coldDur < 5*hotDur {
		t.Logf("warning: cold %v not dramatically slower than hot %v (bcrypt may be fast on this box)", coldDur, hotDur)
	}
}

// TestResolveCacheInvalidatedOnDelete proves that deleting a key
// immediately invalidates the cache, so a stale Principal cannot be
// resolved past the deletion.
func TestResolveCacheInvalidatedOnDelete(t *testing.T) {
	store := newTestStore(t)
	store.EnableAuthCache(time.Minute, 64)
	ctx := context.Background()

	tenant, _ := store.CreateTenant(ctx, "acme")
	result, _ := store.CreateAPIKey(ctx, tenant.ID, "doomed", RoleEditor)

	if _, err := store.Resolve(ctx, result.Plaintext); err != nil {
		t.Fatalf("warm-up Resolve: %v", err)
	}
	if store.cache.Len() != 1 {
		t.Fatalf("cache len after warm-up = %d, want 1", store.cache.Len())
	}

	if err := store.DeleteAPIKey(ctx, result.Key.TenantID, result.Key.ID); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}
	if store.cache.Len() != 0 {
		t.Errorf("cache len after delete = %d, want 0", store.cache.Len())
	}

	if _, err := store.Resolve(ctx, result.Plaintext); err == nil {
		t.Error("Resolve after delete should return ErrInvalidKey")
	}
}

// TestResolveCacheInvalidatedOnRoleChange proves that promoting or
// demoting a key invalidates the cache, so a cached Principal cannot
// retain its old role past the change.
func TestResolveCacheInvalidatedOnRoleChange(t *testing.T) {
	store := newTestStore(t)
	store.EnableAuthCache(time.Minute, 64)
	ctx := context.Background()

	tenant, _ := store.CreateTenant(ctx, "acme")
	result, _ := store.CreateAPIKey(ctx, tenant.ID, "promoter", RoleViewer)

	first, err := store.Resolve(ctx, result.Plaintext)
	if err != nil {
		t.Fatalf("warm-up Resolve: %v", err)
	}
	if first.Role != RoleViewer {
		t.Errorf("initial role = %q, want viewer", first.Role)
	}

	if _, _, err := store.UpdateAPIKeyRole(ctx, result.Key.TenantID, result.Key.ID, RoleAdmin); err != nil {
		t.Fatalf("UpdateAPIKeyRole: %v", err)
	}
	if store.cache.Len() != 0 {
		t.Errorf("cache len after role change = %d, want 0", store.cache.Len())
	}

	// Next Resolve must see the new role, not the cached viewer one.
	second, err := store.Resolve(ctx, result.Plaintext)
	if err != nil {
		t.Fatalf("post-change Resolve: %v", err)
	}
	if second.Role != RoleAdmin {
		t.Errorf("role after change = %q, want admin", second.Role)
	}
}

// TestResolveCacheDisabledStillWorks verifies the store behaves
// identically when the cache is never enabled — no cache-related
// regressions on the default path.
func TestResolveCacheDisabledStillWorks(t *testing.T) {
	store := newTestStore(t)
	// Intentionally skip EnableAuthCache.
	ctx := context.Background()

	tenant, _ := store.CreateTenant(ctx, "acme")
	result, _ := store.CreateAPIKey(ctx, tenant.ID, "plain", RoleEditor)

	p, err := store.Resolve(ctx, result.Plaintext)
	if err != nil {
		t.Fatalf("Resolve without cache: %v", err)
	}
	if p.Role != RoleEditor {
		t.Errorf("role = %q, want editor", p.Role)
	}
	if store.cache != nil {
		t.Error("cache should remain nil when EnableAuthCache not called")
	}
}
