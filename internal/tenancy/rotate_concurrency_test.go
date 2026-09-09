package tenancy

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestRotateAPIKey_ConcurrentRotationsNeverClobber is the audit M-11 guard.
// The secret is now hashed outside the transaction and swapped by a single
// UPDATE guarded on the prefix that was read, so two rotations racing on one
// key cannot both "succeed" while only one plaintext works: every caller
// either wins (its plaintext resolves) or gets ErrConflict.
func TestRotateAPIKey_ConcurrentRotationsNeverClobber(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateAPIKey(ctx, tenant.ID, "svc", RoleEditor)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		winners   []string
		conflicts int
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _, err := s.RotateAPIKey(ctx, tenant.ID, created.Key.ID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, res.Plaintext)
			case errors.Is(err, ErrConflict):
				conflicts++
			default:
				t.Errorf("unexpected rotate error: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(winners) == 0 {
		t.Fatalf("no rotation succeeded (conflicts=%d)", conflicts)
	}
	if len(winners)+conflicts != workers {
		t.Fatalf("winners=%d conflicts=%d, want %d total", len(winners), conflicts, workers)
	}
	// Exactly one plaintext — the last committed rotation's — resolves; the
	// original never does again.
	resolving := 0
	for _, p := range winners {
		if _, err := s.Resolve(ctx, p); err == nil {
			resolving++
		}
	}
	if resolving != 1 {
		t.Fatalf("%d of %d winning plaintexts resolve, want exactly 1", resolving, len(winners))
	}
	if _, err := s.Resolve(ctx, created.Plaintext); err == nil {
		t.Fatal("the pre-rotation plaintext still resolves")
	}
}

// TestUpdateAPIKeyRole_DeletedKeyIsNotFound: the role change runs in one
// transaction and checks RowsAffected, so a key that vanished is reported
// rather than silently "updated" (audit M-10).
func TestUpdateAPIKeyRole_DeletedKeyIsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.CreateAPIKey(ctx, tenant.ID, "svc", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAPIKey(ctx, tenant.ID, k.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpdateAPIKeyRole(ctx, tenant.ID, k.Key.ID, RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("role change on a deleted key = %v, want ErrNotFound", err)
	}
	// And a live key still changes role with the previous role reported.
	k2, err := s.CreateAPIKey(ctx, tenant.ID, "svc2", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	prev, next, err := s.UpdateAPIKeyRole(ctx, tenant.ID, k2.Key.ID, RoleEditor)
	if err != nil || prev != RoleViewer || next != RoleEditor {
		t.Fatalf("role change = (%v, %v, %v)", prev, next, err)
	}
}
