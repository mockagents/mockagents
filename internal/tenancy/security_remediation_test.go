package tenancy

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCannotMutatePlatformCredential(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tenant, err := s.CreateTenant(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	platformPlain := "mak_12345678_abcdefghijklmnopqrstuvwxyz012345"
	platform, err := s.CreateAPIKeyWithPlaintext(ctx, tenant.ID, "platform", RolePlatform, platformPlain)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.CreateAPIKey(ctx, tenant.ID, "admin", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	actorCtx := WithMutationActor(ctx, &Principal{TenantID: tenant.ID, KeyID: admin.Key.ID, Role: RoleAdmin})

	if _, _, err := s.RotateAPIKey(actorCtx, tenant.ID, platform.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("rotate error = %v", err)
	}
	if _, _, err := s.UpdateAPIKeyRole(actorCtx, tenant.ID, platform.ID, RoleViewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("role error = %v", err)
	}
	if err := s.DeleteAPIKey(actorCtx, tenant.ID, platform.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("delete error = %v", err)
	}
	if _, _, err := s.BulkRotateTenantKeys(actorCtx, tenant.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bulk error = %v", err)
	}
	if _, err := s.Resolve(ctx, platformPlain); err != nil {
		t.Fatalf("denied operations changed platform key: %v", err)
	}

	// Tenant admins retain management of ordinary keys in their own tenant.
	if _, _, err := s.RotateAPIKey(actorCtx, tenant.ID, admin.Key.ID); err != nil {
		t.Fatalf("ordinary rotate: %v", err)
	}
}

func TestCachedResolveChecksSharedDatabaseAuthority(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	a, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	a.EnableAuthCache(time.Hour, 16)
	b.EnableAuthCache(time.Hour, 16)
	tenant, _ := a.CreateTenant(ctx, "shared")
	key, _ := a.CreateAPIKey(ctx, tenant.ID, "key", RoleAdmin)
	if _, err := b.Resolve(ctx, key.Plaintext); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.UpdateAPIKeyRole(ctx, tenant.ID, key.Key.ID, RoleViewer); err != nil {
		t.Fatal(err)
	}
	p, err := b.Resolve(ctx, key.Plaintext)
	if err != nil || p.Role != RoleViewer {
		t.Fatalf("cached role = %#v, %v", p, err)
	}
	if _, _, err := a.RotateAPIKey(ctx, tenant.ID, key.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Resolve(ctx, key.Plaintext); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("rotated key accepted: %v", err)
	}

	key2, _ := a.CreateAPIKey(ctx, tenant.ID, "key2", RoleViewer)
	if _, err := b.Resolve(ctx, key2.Plaintext); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteAPIKey(ctx, tenant.ID, key2.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Resolve(ctx, key2.Plaintext); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("deleted key accepted: %v", err)
	}
}

func TestCachedResolveFailsClosedWhenAuthorityUnavailable(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.EnableAuthCache(time.Hour, 8)
	tenant, _ := s.CreateTenant(ctx, "closed")
	key, _ := s.CreateAPIKey(ctx, tenant.ID, "key", RoleViewer)
	if _, err := s.Resolve(ctx, key.Plaintext); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Resolve(ctx, key.Plaintext); err == nil || p != nil {
		t.Fatalf("authority outage failed open: p=%#v err=%v", p, err)
	}
}
