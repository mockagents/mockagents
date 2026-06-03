package tenancy

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestShouldBumpLastUsed covers the PERF-03 coarsening decision in isolation.
func TestShouldBumpLastUsed(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) sql.NullString {
		return sql.NullString{String: now.Add(d).Format(time.RFC3339), Valid: true}
	}
	cases := []struct {
		name     string
		lastUsed sql.NullString
		want     bool
	}{
		{"never used (null)", sql.NullString{}, true},
		{"empty string", sql.NullString{String: "", Valid: true}, true},
		{"within window", at(-30 * time.Second), false},
		{"at the window boundary", at(-lastUsedResolution), true},
		{"stale", at(-2 * time.Minute), true},
		{"corrupt value", sql.NullString{String: "not-a-time", Valid: true}, true},
	}
	for _, c := range cases {
		if got := shouldBumpLastUsed(c.lastUsed, now); got != c.want {
			t.Errorf("%s: shouldBumpLastUsed = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestResolve_CoarsensLastUsedWrite is the PERF-03 integration guard: with the
// auth cache disabled (so every Resolve takes the DB/bcrypt miss path), a key
// whose last_used is fresh is NOT rewritten, while a stale one is refreshed.
func TestResolve_CoarsensLastUsedWrite(t *testing.T) {
	s := newTestStore(t) // no EnableAuthCache → every Resolve is a miss
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAPIKey(ctx, tenant.ID, "k", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	id, plaintext := key.Key.ID, key.Plaintext

	readLastUsed := func() string {
		var lu sql.NullString
		if err := s.db.QueryRowContext(ctx, `SELECT last_used FROM api_keys WHERE id = ?`, id).Scan(&lu); err != nil {
			t.Fatal(err)
		}
		return lu.String
	}
	setLastUsed := func(ts string) {
		if _, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used = ? WHERE id = ?`, ts, id); err != nil {
			t.Fatal(err)
		}
	}

	// Fresh (within the window): Resolve must NOT rewrite last_used.
	recent := time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339)
	setLastUsed(recent)
	if _, err := s.Resolve(ctx, plaintext); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := readLastUsed(); got != recent {
		t.Errorf("last_used rewritten within the window: got %q, want unchanged %q (PERF-03)", got, recent)
	}

	// Stale: Resolve must refresh it.
	stale := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	setLastUsed(stale)
	if _, err := s.Resolve(ctx, plaintext); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := readLastUsed(); got == stale {
		t.Errorf("last_used not refreshed when stale: still %q (PERF-03)", got)
	}
}
