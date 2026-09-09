package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mockagents/mockagents/internal/tenancy"
)

// TestEnvParsers_FailClosed is the audit M-35 guard: a MOCKAGENTS_* value that
// is set but unparsable is an error, never a silent default.
func TestEnvParsers_FailClosed(t *testing.T) {
	t.Run("bool", func(t *testing.T) {
		for _, v := range []string{"1", "true", "TRUE", "yes", "On"} {
			t.Setenv("X_B", v)
			if b, err := envBool("X_B"); err != nil || !b {
				t.Errorf("%q -> (%v, %v), want true", v, b, err)
			}
		}
		for _, v := range []string{"0", "false", "no", "OFF"} {
			t.Setenv("X_B", v)
			if b, err := envBool("X_B"); err != nil || b {
				t.Errorf("%q -> (%v, %v), want false", v, b, err)
			}
		}
		for _, v := range []string{"maybe", "2", "tru"} {
			t.Setenv("X_B", v)
			if _, err := envBool("X_B"); err == nil {
				t.Errorf("%q accepted", v)
			}
		}
		t.Setenv("X_B", "")
		if b, err := envBool("X_B"); err != nil || b {
			t.Errorf("unset -> (%v, %v), want (false, nil)", b, err)
		}
	})
	t.Run("int", func(t *testing.T) {
		t.Setenv("X_I", "808O")
		if _, _, err := envInt("X_I", 1, 65535); err == nil {
			t.Error("808O accepted as a port")
		}
		t.Setenv("X_I", "70000")
		if _, _, err := envInt("X_I", 1, 65535); err == nil {
			t.Error("70000 accepted as a port")
		}
		t.Setenv("X_I", "-1")
		if _, _, err := envInt("X_I", 0, 0); err == nil {
			t.Error("-1 accepted with min 0")
		}
		t.Setenv("X_I", "8081")
		if n, ok, err := envInt("X_I", 1, 65535); err != nil || !ok || n != 8081 {
			t.Errorf("8081 -> (%d, %v, %v)", n, ok, err)
		}
	})
	t.Run("float", func(t *testing.T) {
		t.Setenv("X_F", "10rps")
		if _, _, err := envFloat("X_F", 0); err == nil {
			t.Error("10rps accepted")
		}
		t.Setenv("X_F", "2.5")
		if f, ok, err := envFloat("X_F", 0); err != nil || !ok || f != 2.5 {
			t.Errorf("2.5 -> (%v, %v, %v)", f, ok, err)
		}
	})
	t.Run("duration", func(t *testing.T) {
		t.Setenv("X_D", "24")
		if _, _, err := envDuration("X_D"); err == nil {
			t.Error("bare number accepted as a duration")
		}
		t.Setenv("X_D", "-5m")
		if _, _, err := envDuration("X_D"); err == nil {
			t.Error("negative duration accepted")
		}
		t.Setenv("X_D", "36h")
		if d, ok, err := envDuration("X_D"); err != nil || !ok || d != 36*time.Hour {
			t.Errorf("36h -> (%v, %v, %v)", d, ok, err)
		}
	})
}

func TestQuotaDefaultsFromEnv_RejectsGarbage(t *testing.T) {
	t.Setenv("MOCKAGENTS_DEFAULT_RATE_PER_SEC", "10rps")
	if _, err := quotaDefaultsFromEnv(); err == nil {
		t.Fatal("a typo used to mean unlimited; it must be an error")
	}
	t.Setenv("MOCKAGENTS_DEFAULT_RATE_PER_SEC", "10")
	t.Setenv("MOCKAGENTS_DEFAULT_RATE_BURST", "20")
	t.Setenv("MOCKAGENTS_DEFAULT_MONTHLY_SPEND_USD", "99.5")
	cfg, err := quotaDefaultsFromEnv()
	if err != nil || cfg.RatePerSec != 10 || cfg.RateBurst != 20 || cfg.MonthlySpendUSD != 99.5 {
		t.Fatalf("cfg = %+v err = %v", cfg, err)
	}
}

func TestParseLogLevel_RejectsUnknown(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("log-level", "", "")
	for lvl, want := range map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "WARN": slog.LevelWarn, "error": slog.LevelError, "": slog.LevelInfo} {
		_ = cmd.Flags().Set("log-level", lvl)
		if got, err := parseLogLevel(cmd); err != nil || got != want {
			t.Errorf("%q -> (%v, %v), want %v", lvl, got, err, want)
		}
	}
	_ = cmd.Flags().Set("log-level", "verbose")
	if _, err := parseLogLevel(cmd); err == nil {
		t.Fatal("unknown level silently became info")
	}
}

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

const testPresetKey = "mak_0123abcd_aB3dE6gH9jK2mN5pQ8sT1vW4xZ7yC0eF"

func newBootstrapStore(t *testing.T) *tenancy.SQLiteStore {
	t.Helper()
	store, err := tenancy.NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func platformKeys(t *testing.T, store tenancy.Store) []*tenancy.APIKey {
	t.Helper()
	tenants, err := store.ListTenants(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []*tenancy.APIKey
	for _, tn := range tenants {
		keys, err := store.ListAPIKeys(context.Background(), tn.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range keys {
			if k.Role == tenancy.RolePlatform {
				out = append(out, k)
			}
		}
	}
	return out
}

// TestBootstrapTenancy_PresetKey: the platform key comes from
// MOCKAGENTS_BOOTSTRAP_KEY, resolves, and no secret reaches stderr (audit H-09).
func TestBootstrapTenancy_PresetKey(t *testing.T) {
	store := newBootstrapStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	out := captureStderr(t, func() {
		if err := bootstrapTenancy(context.Background(), store, logger, testPresetKey, filepath.Join(t.TempDir(), "unused.key")); err != nil {
			t.Errorf("bootstrap: %v", err)
		}
	})
	if strings.Contains(out, testPresetKey[13:]) {
		t.Fatal("secret part of the preset key was printed to stderr")
	}
	p, err := store.Resolve(context.Background(), testPresetKey)
	if err != nil || p.Role != tenancy.RolePlatform {
		t.Fatalf("preset key does not resolve to the platform principal: %+v %v", p, err)
	}
	// Idempotent: a second boot with the same preset changes nothing.
	if err := bootstrapTenancy(context.Background(), store, logger, testPresetKey, ""); err != nil {
		t.Fatal(err)
	}
	if n := len(platformKeys(t, store)); n != 1 {
		t.Fatalf("platform keys after re-bootstrap = %d, want 1", n)
	}
}

func TestBootstrapTenancy_InvalidPresetFails(t *testing.T) {
	store := newBootstrapStore(t)
	err := bootstrapTenancy(context.Background(), store, slog.New(slog.NewTextHandler(io.Discard, nil)), "sk-not-a-mockagents-key", "")
	if err == nil || !strings.Contains(err.Error(), "MOCKAGENTS_BOOTSTRAP_KEY") {
		t.Fatalf("err = %v, want a MOCKAGENTS_BOOTSTRAP_KEY validation error", err)
	}
	if n := len(platformKeys(t, store)); n != 0 {
		t.Fatalf("platform keys = %d, want 0", n)
	}
}

// TestBootstrapTenancy_GeneratedKeyGoesToFile: without a preset the key is
// written 0600 to the key file; stderr carries only the path and prefix.
func TestBootstrapTenancy_GeneratedKeyGoesToFile(t *testing.T) {
	store := newBootstrapStore(t)
	keyFile := filepath.Join(t.TempDir(), "sub", "bootstrap-admin.key")
	out := captureStderr(t, func() {
		if err := bootstrapTenancy(context.Background(), store, slog.New(slog.NewTextHandler(io.Discard, nil)), "", keyFile); err != nil {
			t.Errorf("bootstrap: %v", err)
		}
	})
	data, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("key file: %v", err)
	}
	plaintext := strings.TrimSpace(string(data))
	if !strings.HasPrefix(plaintext, "mak_") {
		t.Fatalf("key file content %q", plaintext)
	}
	if strings.Contains(out, plaintext) {
		t.Fatal("generated plaintext was printed to stderr")
	}
	if !strings.Contains(out, keyFile) || !strings.Contains(out, plaintext[:12]) {
		t.Fatalf("stderr should name the file and the prefix: %q", out)
	}
	if p, err := store.Resolve(context.Background(), plaintext); err != nil || p.Role != tenancy.RolePlatform {
		t.Fatalf("key from file does not resolve: %+v %v", p, err)
	}
}

// TestBootstrapTenancy_UnwritableFileFailsClosed: if the key cannot be handed
// over it is discarded again and startup fails, rather than printing it.
func TestBootstrapTenancy_UnwritableFileFailsClosed(t *testing.T) {
	store := newBootstrapStore(t)
	// A directory where the file should be makes OpenFile fail on every OS.
	keyFile := t.TempDir()
	var err error
	out := captureStderr(t, func() {
		err = bootstrapTenancy(context.Background(), store, slog.New(slog.NewTextHandler(io.Discard, nil)), "", keyFile)
	})
	if err == nil || !strings.Contains(err.Error(), "MOCKAGENTS_BOOTSTRAP_KEY") {
		t.Fatalf("err = %v, want a failure naming MOCKAGENTS_BOOTSTRAP_KEY", err)
	}
	if strings.Contains(out, "mak_") {
		t.Fatalf("a key reached stderr on the failure path: %q", out)
	}
	if n := len(platformKeys(t, store)); n != 0 {
		t.Fatalf("platform keys left behind = %d, want 0 (would block every future bootstrap)", n)
	}
}
