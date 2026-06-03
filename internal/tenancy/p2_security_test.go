package tenancy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestAuthCache_HashKeyFullDigest covers F-AC-001/002: the cache key is the
// full 256-bit SHA-256 digest (64 hex chars), and distinct plaintexts map to
// distinct keys.
func TestAuthCache_HashKeyFullDigest(t *testing.T) {
	c := newAuthCache(time.Hour, 8)
	if got := len(c.hashKey("x")); got != 64 {
		t.Errorf("hashKey length = %d, want 64 (256-bit digest)", got)
	}
	if c.hashKey("a") == c.hashKey("b") {
		t.Error("distinct plaintexts produced the same cache key")
	}
}

// TestAuthCache_EvictsExpiredFirst covers F-AC-003: capacity eviction prefers
// an expired entry over a live one.
func TestAuthCache_EvictsExpiredFirst(t *testing.T) {
	// One expired entry among many live ones: a correct (expired-first)
	// eviction always drops the dead one; a random eviction would keep it
	// ~90% of the time, so this reliably catches a regression to random.
	c := newAuthCache(time.Hour, 10)
	c.entries["dead"] = authCacheEntry{principal: Principal{KeyID: "dead"}, expiry: time.Now().Add(-time.Hour)}
	for i := range 9 {
		c.entries[fmt.Sprintf("hot%d", i)] = authCacheEntry{principal: Principal{KeyID: "hot"}, expiry: time.Now().Add(time.Hour)}
	}

	c.evictOneLocked()

	if _, ok := c.entries["dead"]; ok {
		t.Error("expired entry should have been evicted first")
	}
	if len(c.entries) != 9 {
		t.Errorf("len = %d, want 9 (only the expired entry removed)", len(c.entries))
	}
}

// TestAuthCache_OverwriteDoesNotEvict covers F-AC-004: re-Setting an existing
// key at capacity overwrites in place without evicting another entry.
func TestAuthCache_OverwriteDoesNotEvict(t *testing.T) {
	c := newAuthCache(time.Hour, 1)
	c.Set("keyA", &Principal{KeyID: "a"})
	c.Set("keyA", &Principal{KeyID: "a2"}) // same key — must not evict + must update
	if c.Len() != 1 {
		t.Errorf("len = %d, want 1", c.Len())
	}
	if got := c.Get("keyA"); got == nil || got.KeyID != "a2" {
		t.Errorf("overwrite lost the update: %+v", got)
	}
}

// TestNewAPIKeyResult_RedactsPlaintext covers F-TY-003: fmt and slog redact the
// secret, but JSON marshaling (the one-time response) keeps it.
func TestNewAPIKeyResult_RedactsPlaintext(t *testing.T) {
	const secret = "mak_abcd1234_SUPERSECRETVALUE"
	r := NewAPIKeyResult{Key: APIKey{ID: "key_1", Prefix: "mak_abcd1234"}, Plaintext: secret}

	if s := fmt.Sprintf("%v", r); strings.Contains(s, "SUPERSECRET") {
		t.Errorf("%%v leaked the plaintext: %s", s)
	}
	if strings.Contains(r.String(), "SUPERSECRET") {
		t.Error("String() leaked the plaintext")
	}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("created", "result", r)
	if strings.Contains(buf.String(), "SUPERSECRET") {
		t.Errorf("slog leaked the plaintext: %s", buf.String())
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), secret) {
		t.Error("JSON must still carry the plaintext for the one-time response")
	}
}

// TestAPIKey_JSONHasNoSecret covers F-TY-002: APIKey is metadata only — its
// JSON must never carry a hash/secret/plaintext field.
func TestAPIKey_JSONHasNoSecret(t *testing.T) {
	b, err := json.Marshal(APIKey{ID: "key_1", TenantID: "t", Name: "n", Prefix: "mak_abcd1234", Role: RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(b))
	for _, forbidden := range []string{"hash", "secret", "plaintext"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("APIKey JSON contains a forbidden field %q: %s", forbidden, b)
		}
	}
	// F-TY-001: a never-used key (nil LastUsed) omits last_used entirely
	// rather than emitting a zero timestamp.
	if strings.Contains(lower, "last_used") {
		t.Errorf("never-used APIKey JSON should omit last_used: %s", b)
	}
	used := time.Now().UTC()
	b2, _ := json.Marshal(APIKey{ID: "key_1", LastUsed: &used})
	if !strings.Contains(string(b2), "last_used") {
		t.Errorf("used APIKey JSON should include last_used: %s", b2)
	}
}

// TestResolve_RejectsMalformedShape covers F-ST-003: a key that can't be a
// well-formed mak_<8hex>_<secret> is rejected before any DB work, and a
// valid-shaped but non-existent key still returns ErrInvalidKey (exercising
// the X-TN-002 timing-equalizing path).
func TestResolve_RejectsMalformedShape(t *testing.T) {
	s := newTestStore(t)
	bad := []string{
		"",
		"short",
		"notmak_aaaaaaaa_secret",   // wrong scheme prefix
		"mak_aaaaaaaaXsecretvalue", // no '_' separator at the prefix boundary
		"mak_aaaaaaaa_secretvalue", // well-formed shape but no such key
	}
	for _, k := range bad {
		if _, err := s.Resolve(context.Background(), k); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Resolve(%q) err = %v, want ErrInvalidKey", k, err)
		}
	}
}
