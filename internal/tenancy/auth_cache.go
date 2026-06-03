package tenancy

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// authCache is a bounded, TTL-gated cache of successful API-key
// resolutions. Its reason for existing is bcrypt: every Resolve()
// against an uncached key runs bcrypt.CompareHashAndPassword, which
// is intentionally slow (~5–30 ms depending on the stored cost) and
// runs under `MaxOpenConns=1` — so a sustained authenticated workload
// serializes every request behind a slow CPU job.
//
// Once a key has been verified once, subsequent requests with the
// same plaintext inside the TTL skip bcrypt entirely and return the
// previously-resolved Principal. The cache key is the full SHA-256
// digest of the plaintext, so the raw plaintext never sits in memory;
// this also makes the cache safe to log about without leaking
// credentials.
//
// Invalidation is coarse: any mutation (key delete, role change)
// calls Invalidate() to drop all entries. Mutations are rare compared
// to reads, so this is both correct and cheap. If cache size becomes
// a concern we can swap in a per-id reverse index later.
//
// The TTL is also the worst-case stale-auth bound: if the store ever failed
// to Invalidate() on a mutation, a rotated/revoked key could still resolve
// from cache until its entry expires. Every store mutator does flush, so this
// is a backstop, not a live window (F-AC-005).
//
// mu is an RWMutex so the hot path — a cache hit — reads under a shared lock;
// only inserts, eviction, expired-entry cleanup, and Invalidate take the
// exclusive lock (F-AC-006).
type authCache struct {
	mu      sync.RWMutex
	entries map[string]authCacheEntry
	ttl     time.Duration
	maxSize int
}

type authCacheEntry struct {
	// principal is stored by value (copied on Get) so mutating a
	// returned Principal cannot poison a subsequent cache hit.
	principal Principal
	expiry    time.Time
}

// newAuthCache builds an empty cache with the given TTL and cap.
func newAuthCache(ttl time.Duration, maxSize int) *authCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &authCache{
		entries: make(map[string]authCacheEntry, maxSize),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// hashKey derives the cache key from a plaintext API key. SHA-256 is
// cryptographically overkill for a cache lookup but avoids any
// possibility of plaintext appearing in a heap dump or a log line.
func (c *authCache) hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	// Use the full 256-bit digest (the cache is not memory-bound): collision
	// is a 2^128 birthday bound, so cross-key auth confusion is infeasible
	// (F-AC-001/002 — the previous 128-bit truncation didn't match this doc).
	return hex.EncodeToString(sum[:])
}

// Get returns the cached Principal for a plaintext key when there is
// a non-expired entry. The returned pointer is a fresh allocation so
// callers can mutate it without affecting the cache.
func (c *authCache) Get(plaintext string) *Principal {
	if c == nil || plaintext == "" {
		return nil
	}
	k := c.hashKey(plaintext)
	c.mu.RLock()
	e, ok := c.entries[k]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Now().After(e.expiry) {
		// Expired: drop it under the exclusive lock, re-checking so a
		// concurrent refresh isn't clobbered.
		c.mu.Lock()
		if cur, ok := c.entries[k]; ok && time.Now().After(cur.expiry) {
			delete(c.entries, k)
		}
		c.mu.Unlock()
		return nil
	}
	p := e.principal // local copy; safe after RUnlock
	return &p
}

// Set records a successful resolution so subsequent requests with the same
// plaintext skip bcrypt. Eviction (only when inserting a NEW key at capacity)
// prefers an already-expired entry over a random live one, so a hot key isn't
// evicted while dead entries linger (F-AC-003); overwriting an existing key
// never evicts (F-AC-004).
func (c *authCache) Set(plaintext string, principal *Principal) {
	if c == nil || principal == nil || plaintext == "" {
		return
	}
	k := c.hashKey(plaintext)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[k]; !exists && len(c.entries) >= c.maxSize {
		c.evictOneLocked()
	}
	c.entries[k] = authCacheEntry{
		principal: *principal,
		expiry:    time.Now().Add(c.ttl),
	}
}

// evictOneLocked removes a single entry, preferring an expired one. The caller
// must hold c.mu. Falls back to a (randomized) map-order entry when none are
// expired.
func (c *authCache) evictOneLocked() {
	now := time.Now()
	for key, e := range c.entries {
		if now.After(e.expiry) {
			delete(c.entries, key)
			return
		}
	}
	for key := range c.entries { // none expired — drop a random one
		delete(c.entries, key)
		return
	}
}

// Invalidate drops every cached entry. Called whenever the tenancy
// store mutates a key (delete, role change) so stale privileges can
// never outlive the change even by the TTL.
func (c *authCache) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Rebuild the map rather than iterating+deleting. Cheaper when
	// the cache is close to full and GC-friendly.
	c.entries = make(map[string]authCacheEntry, c.maxSize)
}

// Len returns the current number of entries. Used by tests and
// metrics wiring; not safe to use for capacity decisions because the
// result is stale the moment it returns.
func (c *authCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
