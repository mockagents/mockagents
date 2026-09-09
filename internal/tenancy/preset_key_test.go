package tenancy

import (
	"context"
	"path/filepath"
	"testing"
)

const presetKey = "mak_0123abcd_" + "aB3dE6gH9jK2mN5pQ8sT1vW4xZ7yC0eF"

func TestValidatePresetAPIKey(t *testing.T) {
	if p, err := ValidatePresetAPIKey(presetKey); err != nil || p != "mak_0123abcd" {
		t.Fatalf("valid key: prefix=%q err=%v", p, err)
	}
	bad := []string{
		"",
		"sk-not-a-mockagents-key",
		"mak_0123abcd",       // no secret
		"mak_0123abcd_short", // secret too short
		"mak_0123abcz_aB3dE6gH9jK2mN5pQ8sT1vW4xZ7yC0eF", // non-hex prefix
		"mak_0123abcd_aB3dE6gH9jK2mN5pQ8sT1vW4xZ7yC0e!", // bad secret char
	}
	for _, k := range bad {
		if _, err := ValidatePresetAPIKey(k); err == nil {
			t.Errorf("%q accepted, want error", k)
		}
	}
}

// TestCreateAPIKeyWithPlaintext_ResolvesLikeAGeneratedKey: a preset key must
// behave exactly like one the store generated — Resolve maps it to the right
// principal — so a bootstrap key supplied from a secret is a first-class
// credential.
func TestCreateAPIKeyWithPlaintext_ResolvesLikeAGeneratedKey(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	var pc PresetKeyCreator = store // compile-time: SQLiteStore implements it
	key, err := pc.CreateAPIKeyWithPlaintext(ctx, tenant.ID, "bootstrap-admin", RolePlatform, presetKey)
	if err != nil {
		t.Fatal(err)
	}
	if key.Prefix != "mak_0123abcd" || key.Role != RolePlatform || key.TenantID != tenant.ID {
		t.Fatalf("key = %+v", key)
	}
	p, err := store.Resolve(ctx, presetKey)
	if err != nil {
		t.Fatalf("Resolve(preset): %v", err)
	}
	if p.KeyID != key.ID || p.Role != RolePlatform || p.TenantID != tenant.ID {
		t.Fatalf("principal = %+v", p)
	}
	if _, err := store.Resolve(ctx, "mak_0123abcd_wrongwrongwrongwrongwrongwrong"); err == nil {
		t.Fatal("a different secret with the same prefix resolved")
	}
	if _, err := pc.CreateAPIKeyWithPlaintext(ctx, tenant.ID, "x", RoleViewer, "garbage"); err == nil {
		t.Fatal("invalid preset accepted")
	}
	// PostgresStore must implement the same optional interface.
	var _ PresetKeyCreator = (*PostgresStore)(nil)
}
