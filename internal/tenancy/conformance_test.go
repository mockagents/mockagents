package tenancy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreConformance exercises the Store contract against every available
// backend, so a Postgres impl can't silently diverge from SQLite. SQLite always
// runs (temp file per subtest); Postgres runs only when MOCKAGENTS_TEST_PG_DSN
// is set (e.g. a CI service container), and is skipped otherwise.
//
// Each backend factory hands back a clean store: SQLite gets a fresh temp DB,
// Postgres truncates the shared tables, so subtests are isolated.
func TestStoreConformance(t *testing.T) {
	type factory struct {
		name string
		make func(t *testing.T) Store
	}
	factories := []factory{{
		name: "sqlite",
		make: func(t *testing.T) Store {
			s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
			if err != nil {
				t.Fatalf("NewSQLiteStore: %v", err)
			}
			s.EnableAuthCache(time.Minute, 128)
			t.Cleanup(func() { s.Close() })
			return s
		},
	}}
	if dsn := os.Getenv("MOCKAGENTS_TEST_PG_DSN"); dsn != "" {
		factories = append(factories, factory{
			name: "postgres",
			make: func(t *testing.T) Store {
				s, err := NewPostgresStore(dsn)
				if err != nil {
					t.Fatalf("NewPostgresStore: %v", err)
				}
				if _, err := s.db.ExecContext(context.Background(),
					`DELETE FROM api_keys; DELETE FROM tenants;`); err != nil {
					t.Fatalf("reset postgres: %v", err)
				}
				s.EnableAuthCache(time.Minute, 128)
				t.Cleanup(func() { s.Close() })
				return s
			},
		})
	}

	cases := []struct {
		name string
		run  func(t *testing.T, s Store)
	}{
		{"TenantCRUD", conformTenantCRUD},
		{"DuplicateTenantConflict", conformDuplicateTenant},
		{"APIKeyLifecycleAndResolve", conformKeyLifecycle},
		{"ResolveCacheInvalidatedOnRoleChange", conformCacheInvalidation},
		{"CrossTenantScoping", conformCrossTenantScoping},
		{"UpdateRole", conformUpdateRole},
		{"Rotate", conformRotate},
		{"BulkRotateWithExclude", conformBulkRotate},
		{"DeleteTenantCascade", conformDeleteCascade},
		{"InvalidKeyRejected", conformInvalidKey},
		{"UsersAndSessions", conformUsersAndSessions},
	}
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) { c.run(t, f.make(t)) })
			}
		})
	}
}

func mustTenant(t *testing.T, s Store, name string) *Tenant {
	t.Helper()
	ten, err := s.CreateTenant(context.Background(), name)
	if err != nil {
		t.Fatalf("CreateTenant(%q): %v", name, err)
	}
	return ten
}

func mustKey(t *testing.T, s Store, tenantID, name string, role Role) *NewAPIKeyResult {
	t.Helper()
	res, err := s.CreateAPIKey(context.Background(), tenantID, name, role)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return res
}

func conformTenantCRUD(t *testing.T, s Store) {
	ctx := context.Background()
	a := mustTenant(t, s, "acme")
	if a.ID == "" || a.Name != "acme" {
		t.Fatalf("bad tenant: %+v", a)
	}
	got, err := s.GetTenant(ctx, a.ID)
	if err != nil || got.Name != "acme" {
		t.Fatalf("GetTenant: %+v err=%v", got, err)
	}
	mustTenant(t, s, "beta")
	list, err := s.ListTenants(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListTenants len=%d err=%v", len(list), err)
	}
	if _, err := s.GetTenant(ctx, "ten_missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTenant(missing) = %v, want ErrNotFound", err)
	}
	if err := s.DeleteTenant(ctx, a.ID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if err := s.DeleteTenant(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteTenant(again) = %v, want ErrNotFound", err)
	}
}

func conformDuplicateTenant(t *testing.T, s Store) {
	mustTenant(t, s, "dup")
	if _, err := s.CreateTenant(context.Background(), "dup"); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate tenant = %v, want ErrConflict", err)
	}
}

func conformKeyLifecycle(t *testing.T, s Store) {
	ctx := context.Background()
	ten := mustTenant(t, s, "acme")
	res := mustKey(t, s, ten.ID, "ci", RoleEditor)
	if res.Plaintext == "" || res.Key.Role != RoleEditor {
		t.Fatalf("bad key result: %+v", res)
	}
	keys, err := s.ListAPIKeys(ctx, ten.ID)
	if err != nil || len(keys) != 1 || keys[0].ID != res.Key.ID {
		t.Fatalf("ListAPIKeys = %+v err=%v", keys, err)
	}

	p, err := s.Resolve(ctx, res.Plaintext)
	if err != nil || p.TenantID != ten.ID || p.Role != RoleEditor || p.KeyID != res.Key.ID {
		t.Fatalf("Resolve = %+v err=%v", p, err)
	}
	if _, err := s.Resolve(ctx, res.Plaintext+"x"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Resolve(wrong) = %v, want ErrInvalidKey", err)
	}

	if err := s.DeleteAPIKey(ctx, ten.ID, res.Key.ID); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}
	if _, err := s.Resolve(ctx, res.Plaintext); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Resolve after delete = %v, want ErrInvalidKey (cache must flush)", err)
	}
}

func conformCacheInvalidation(t *testing.T, s Store) {
	ctx := context.Background()
	ten := mustTenant(t, s, "acme")
	res := mustKey(t, s, ten.ID, "k", RoleViewer)

	// Prime the cache.
	if p, _ := s.Resolve(ctx, res.Plaintext); p == nil || p.Role != RoleViewer {
		t.Fatalf("initial resolve role = %v, want viewer", p)
	}
	// A role change must flush the cache so the next Resolve reflects it.
	if _, _, err := s.UpdateAPIKeyRole(ctx, ten.ID, res.Key.ID, RoleAdmin); err != nil {
		t.Fatalf("UpdateAPIKeyRole: %v", err)
	}
	p, err := s.Resolve(ctx, res.Plaintext)
	if err != nil || p.Role != RoleAdmin {
		t.Errorf("post-update resolve role = %v err=%v, want admin (cache stale!)", p, err)
	}
}

func conformCrossTenantScoping(t *testing.T, s Store) {
	ctx := context.Background()
	a := mustTenant(t, s, "acme")
	b := mustTenant(t, s, "beta")
	ak := mustKey(t, s, a.ID, "a-key", RoleEditor)

	// Tenant b must not be able to act on tenant a's key.
	if err := s.DeleteAPIKey(ctx, b.ID, ak.Key.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant delete = %v, want ErrNotFound", err)
	}
	if _, _, err := s.UpdateAPIKeyRole(ctx, b.ID, ak.Key.ID, RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant role change = %v, want ErrNotFound", err)
	}
	if _, _, err := s.RotateAPIKey(ctx, b.ID, ak.Key.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-tenant rotate = %v, want ErrNotFound", err)
	}
	// a's key still works.
	if _, err := s.Resolve(ctx, ak.Plaintext); err != nil {
		t.Errorf("a-key should still resolve: %v", err)
	}
}

func conformUpdateRole(t *testing.T, s Store) {
	ctx := context.Background()
	ten := mustTenant(t, s, "acme")
	res := mustKey(t, s, ten.ID, "k", RoleViewer)

	prev, next, err := s.UpdateAPIKeyRole(ctx, ten.ID, res.Key.ID, RoleAdmin)
	if err != nil || prev != RoleViewer || next != RoleAdmin {
		t.Fatalf("UpdateAPIKeyRole = (%v,%v,%v), want (viewer,admin,nil)", prev, next, err)
	}
	if _, _, err := s.UpdateAPIKeyRole(ctx, ten.ID, res.Key.ID, Role("bogus")); err == nil {
		t.Error("invalid role should error")
	}
	if _, _, err := s.UpdateAPIKeyRole(ctx, ten.ID, "key_missing", RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing key = %v, want ErrNotFound", err)
	}
}

func conformRotate(t *testing.T, s Store) {
	ctx := context.Background()
	ten := mustTenant(t, s, "acme")
	res := mustKey(t, s, ten.ID, "k", RoleEditor)
	oldPlain := res.Plaintext

	rotated, oldPrefix, err := s.RotateAPIKey(ctx, ten.ID, res.Key.ID)
	if err != nil || rotated.Plaintext == oldPlain || oldPrefix != res.Key.Prefix {
		t.Fatalf("Rotate = %+v oldPrefix=%q err=%v", rotated, oldPrefix, err)
	}
	if _, err := s.Resolve(ctx, oldPlain); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("old plaintext after rotate = %v, want ErrInvalidKey", err)
	}
	p, err := s.Resolve(ctx, rotated.Plaintext)
	if err != nil || p.KeyID != res.Key.ID || p.Role != RoleEditor {
		t.Errorf("new plaintext resolve = %+v err=%v, want same id+role", p, err)
	}
}

func conformBulkRotate(t *testing.T, s Store) {
	ctx := context.Background()
	ten := mustTenant(t, s, "acme")
	k1 := mustKey(t, s, ten.ID, "k1", RoleViewer)
	k2 := mustKey(t, s, ten.ID, "k2", RoleEditor)
	keep := mustKey(t, s, ten.ID, "keep", RoleAdmin)

	// Rotate all except `keep`.
	results, oldPrefixes, err := s.BulkRotateTenantKeys(ctx, ten.ID, keep.Key.ID)
	if err != nil || len(results) != 2 || len(oldPrefixes) != 2 {
		t.Fatalf("BulkRotate = %d results, %d prefixes, err=%v; want 2,2", len(results), len(oldPrefixes), err)
	}
	// k1 and k2 old secrets are dead; keep still works.
	if _, err := s.Resolve(ctx, k1.Plaintext); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("k1 old plaintext should be dead: %v", err)
	}
	if _, err := s.Resolve(ctx, k2.Plaintext); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("k2 old plaintext should be dead: %v", err)
	}
	if _, err := s.Resolve(ctx, keep.Plaintext); err != nil {
		t.Errorf("excluded keep key should survive: %v", err)
	}
	// Each returned new plaintext resolves.
	for _, r := range results {
		if _, err := s.Resolve(ctx, r.Plaintext); err != nil {
			t.Errorf("rotated key %s new plaintext should resolve: %v", r.Key.ID, err)
		}
	}

	// An empty tenant is a no-op (no error).
	empty := mustTenant(t, s, "empty")
	res, pfx, err := s.BulkRotateTenantKeys(ctx, empty.ID)
	if err != nil || len(res) != 0 || len(pfx) != 0 {
		t.Errorf("empty BulkRotate = %d,%d,%v; want 0,0,nil", len(res), len(pfx), err)
	}
}

func conformDeleteCascade(t *testing.T, s Store) {
	ctx := context.Background()
	ten := mustTenant(t, s, "acme")
	res := mustKey(t, s, ten.ID, "k", RoleEditor)

	if err := s.DeleteTenant(ctx, ten.ID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	// The cascade removed the key row, so its plaintext no longer resolves.
	if _, err := s.Resolve(ctx, res.Plaintext); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("key after tenant delete = %v, want ErrInvalidKey (cascade)", err)
	}
}

func conformUsersAndSessions(t *testing.T, s Store) {
	ctx := context.Background()
	ten := mustTenant(t, s, "acme")

	if _, err := s.GetUserByEmail(ctx, "ghost@acme.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByEmail(missing) = %v, want ErrNotFound", err)
	}
	u, err := s.CreateUser(ctx, "alice@acme.com", ten.ID, RoleEditor)
	if err != nil || u.Email != "alice@acme.com" || u.Role != RoleEditor {
		t.Fatalf("CreateUser = %+v err=%v", u, err)
	}
	got, err := s.GetUserByEmail(ctx, "alice@acme.com")
	if err != nil || got.ID != u.ID || got.TenantID != ten.ID {
		t.Fatalf("GetUserByEmail = %+v err=%v", got, err)
	}
	if _, err := s.CreateUser(ctx, "alice@acme.com", ten.ID, RoleViewer); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate user = %v, want ErrConflict", err)
	}

	// Session lifecycle: create → resolve → reject bogus/tampered → logout.
	token, sess, err := s.CreateSession(ctx, u.ID, ten.ID, u.Role, time.Hour)
	if err != nil || token == "" || sess.UserID != u.ID {
		t.Fatalf("CreateSession = %q %+v err=%v", token, sess, err)
	}
	p, err := s.ResolveSession(ctx, token)
	if err != nil || p.TenantID != ten.ID || p.Role != RoleEditor || p.KeyID != u.ID {
		t.Fatalf("ResolveSession = %+v err=%v", p, err)
	}
	if _, err := s.ResolveSession(ctx, "notasession"); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("bad token = %v, want ErrInvalidSession", err)
	}
	if _, err := s.ResolveSession(ctx, token+"x"); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("tampered token = %v, want ErrInvalidSession", err)
	}
	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.ResolveSession(ctx, token); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("post-logout = %v, want ErrInvalidSession", err)
	}

	// An already-expired session is rejected.
	expToken, _, err := s.CreateSession(ctx, u.ID, ten.ID, u.Role, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveSession(ctx, expToken); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("expired session = %v, want ErrInvalidSession", err)
	}

	// Deleting the tenant cascades to its users and their sessions.
	live, _, err := s.CreateSession(ctx, u.ID, ten.ID, u.Role, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTenant(ctx, ten.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUserByEmail(ctx, "alice@acme.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("user after tenant delete = %v, want ErrNotFound (cascade)", err)
	}
	if _, err := s.ResolveSession(ctx, live); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("session after tenant delete = %v, want ErrInvalidSession (cascade)", err)
	}
}

func conformInvalidKey(t *testing.T, s Store) {
	ctx := context.Background()
	for _, bad := range []string{"", "notakey", "mak_short", "bearer xyz", "mak_0011223"} {
		if _, err := s.Resolve(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Resolve(%q) = %v, want ErrInvalidKey", bad, err)
		}
	}
}
