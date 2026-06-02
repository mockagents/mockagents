package tenancy

import (
	"context"
	"testing"
)

// TestRotateAPIKey covers the happy path: rotation swaps the secret
// in place, the old plaintext stops resolving, and the new plaintext
// resolves to the same tenant + role + key id.
func TestRotateAPIKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")
	created, err := store.CreateAPIKey(ctx, tenant.ID, "ci-bot", RoleEditor)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	oldPlaintext := created.Plaintext
	oldPrefix := created.Key.Prefix

	// The old key must resolve BEFORE rotation.
	if _, err := store.Resolve(ctx, oldPlaintext); err != nil {
		t.Fatalf("pre-rotation Resolve: %v", err)
	}

	rotated, reportedOldPrefix, err := store.RotateAPIKey(ctx, created.Key.TenantID, created.Key.ID)
	if err != nil {
		t.Fatalf("RotateAPIKey: %v", err)
	}
	if reportedOldPrefix != oldPrefix {
		t.Errorf("reported old prefix = %q, want %q", reportedOldPrefix, oldPrefix)
	}
	if rotated.Key.ID != created.Key.ID {
		t.Errorf("id changed: %q -> %q", created.Key.ID, rotated.Key.ID)
	}
	if rotated.Key.TenantID != tenant.ID {
		t.Errorf("tenant_id changed: %q", rotated.Key.TenantID)
	}
	if rotated.Key.Role != RoleEditor {
		t.Errorf("role changed: %q", rotated.Key.Role)
	}
	if rotated.Key.Name != "ci-bot" {
		t.Errorf("name changed: %q", rotated.Key.Name)
	}
	if rotated.Plaintext == oldPlaintext {
		t.Error("plaintext unchanged after rotation")
	}
	if rotated.Key.Prefix == oldPrefix {
		t.Error("prefix unchanged after rotation")
	}

	// The old plaintext must no longer resolve.
	if _, err := store.Resolve(ctx, oldPlaintext); err != ErrInvalidKey {
		t.Errorf("old plaintext still resolves: %v", err)
	}
	// The new plaintext must resolve to the same principal shape.
	p, err := store.Resolve(ctx, rotated.Plaintext)
	if err != nil {
		t.Fatalf("new plaintext does not resolve: %v", err)
	}
	if p.KeyID != created.Key.ID || p.TenantID != tenant.ID || p.Role != RoleEditor {
		t.Errorf("principal = %+v", p)
	}
}

func TestRotateAPIKey_UnknownID(t *testing.T) {
	store := newTestStore(t)
	_, _, err := store.RotateAPIKey(context.Background(), "ten_any", "key_nope")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestRotateAPIKey_FlushesAuthCache verifies a cached Principal from
// the old plaintext cannot linger after rotation. We hit Resolve
// twice (once to populate the cache, once after rotation) and assert
// the post-rotation call returns ErrInvalidKey.
func TestRotateAPIKey_FlushesAuthCache(t *testing.T) {
	store := newTestStore(t)
	store.EnableAuthCache(0, 16)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "acme")
	created, _ := store.CreateAPIKey(ctx, tenant.ID, "ci", RoleAdmin)

	// Warm the cache.
	if _, err := store.Resolve(ctx, created.Plaintext); err != nil {
		t.Fatalf("warm Resolve: %v", err)
	}
	if _, _, err := store.RotateAPIKey(ctx, created.Key.TenantID, created.Key.ID); err != nil {
		t.Fatalf("RotateAPIKey: %v", err)
	}
	if _, err := store.Resolve(ctx, created.Plaintext); err != ErrInvalidKey {
		t.Errorf("old plaintext still resolves via cache: %v", err)
	}
}
