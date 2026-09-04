package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/tenancy"
)

// UX-01 end-to-end: GET /api/v1/identity through the real mux and the real
// AuthMiddleware. The unit tests call the handler directly; these go through
// the chain that actually decides 401 vs 403, which is the behaviour the story
// is about.

type identityEnv struct {
	base     string
	keys     map[tenancy.Role]string
	shutdown func()
}

func newIdentityEnv(t *testing.T, multiTenant bool) identityEnv {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	registry := engine.NewAgentRegistry()
	registry.Register(testFullAgent("identity-agent", "gpt-4o"))
	eng := engine.NewEngine(registry, state.NewMemoryStore(5*time.Minute), logger)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()
	cfg.Version = "test-version"

	env := identityEnv{keys: map[tenancy.Role]string{}}
	var closeStore func()

	if multiTenant {
		store, err := tenancy.NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
		require.NoError(t, err)
		closeStore = func() { _ = store.Close() }

		tenant, err := store.CreateTenant(t.Context(), "acme")
		require.NoError(t, err)
		// Mint one key per role directly through the store. Platform is not
		// assignable through the management API by design (X-TN-001), so the
		// store is the only way to model a platform operator here — the same
		// path the CLI bootstrap uses.
		for _, role := range tenancy.AllRoles() {
			key, err := store.CreateAPIKey(t.Context(), tenant.ID, string(role), role)
			require.NoError(t, err)
			env.keys[role] = key.Plaintext
		}
		cfg.TenancyStore = store
	}

	srv := New(eng, cfg, logger)
	require.NoError(t, srv.Listen())
	go func() { _ = srv.Serve() }()

	env.base = "http://" + srv.ListenAddr()
	env.shutdown = func() {
		_ = srv.Shutdown()
		if closeStore != nil {
			closeStore()
		}
	}
	return env
}

func (e identityEnv) get(t *testing.T, path, key string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.base+path, nil)
	require.NoError(t, err)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := res.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return res, buf
}

func (e identityEnv) identity(t *testing.T, key string) IdentityResponse {
	t.Helper()
	res, body := e.get(t, "/api/v1/identity", key)
	require.Equal(t, http.StatusOK, res.StatusCode, "body: %s", body)
	var got IdentityResponse
	require.NoError(t, json.Unmarshal(body, &got))
	return got
}

// The headline UX-01 criterion: a viewer can sign in. The GUI previously
// validated keys against GET /api/v1/tenants, which is platform-gated — so
// viewer, editor and admin keys were all rejected at the door. Identity is
// reachable by every authenticated role; tenants still is not.
func TestIdentity_ViewerSucceedsWithoutTenantListPermission(t *testing.T) {
	env := newIdentityEnv(t, true)
	defer env.shutdown()

	got := env.identity(t, env.keys[tenancy.RoleViewer])
	require.True(t, got.Authenticated)
	require.NotNil(t, got.Role)
	require.Equal(t, "viewer", *got.Role)

	// The very endpoint the old login probe used must still be forbidden.
	res, _ := env.get(t, "/api/v1/tenants", env.keys[tenancy.RoleViewer])
	require.Equal(t, http.StatusForbidden, res.StatusCode,
		"viewer must remain forbidden from the tenant collection")
}

// 401 (no/!bad credential) must stay distinguishable from 403 (valid
// credential, insufficient role) — the UI renders them very differently.
func TestIdentity_401VersusForbidden(t *testing.T) {
	env := newIdentityEnv(t, true)
	defer env.shutdown()

	res, _ := env.get(t, "/api/v1/identity", "")
	require.Equal(t, http.StatusUnauthorized, res.StatusCode, "anonymous must be 401")

	res, _ = env.get(t, "/api/v1/identity", "mak_not-a-real-key")
	require.Equal(t, http.StatusUnauthorized, res.StatusCode, "invalid key must be 401")

	// Same credential, different route: valid but under-privileged is 403.
	// (/api/v1/tenants, not /api/v1/audit — audit is only mounted when an
	// audit store is configured, and an unmounted route 404s before authz.)
	res, _ = env.get(t, "/api/v1/tenants", env.keys[tenancy.RoleViewer])
	require.Equal(t, http.StatusForbidden, res.StatusCode,
		"viewer on a platform route must be 403, not 401 or 404")
}

func TestIdentity_CapabilitiesMatchServerEnforcement(t *testing.T) {
	env := newIdentityEnv(t, true)
	defer env.shutdown()

	// For each role, a capability the identity response grants (or withholds)
	// must agree with what the server actually does on the matching route.
	cases := []struct {
		role       tenancy.Role
		capability string
		path       string
		wantStatus int
	}{
		{tenancy.RoleViewer, "agents.read", "/api/v1/agents", http.StatusOK},
		{tenancy.RoleViewer, "identity.read", "/api/v1/identity", http.StatusOK},
		{tenancy.RoleViewer, "health.read", "/api/v1/health", http.StatusOK},
		{tenancy.RoleViewer, "tenants.read", "/api/v1/tenants", http.StatusForbidden},
		{tenancy.RoleEditor, "tenants.read", "/api/v1/tenants", http.StatusForbidden},
		{tenancy.RoleAdmin, "tenants.read", "/api/v1/tenants", http.StatusForbidden},
		{tenancy.RolePlatform, "tenants.read", "/api/v1/tenants", http.StatusOK},
		{tenancy.RoleViewer, "tenants.keys.read", "/api/v1/tenants/x/keys", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(string(c.role)+" "+c.capability, func(t *testing.T) {
			ident := env.identity(t, env.keys[c.role])
			hasCap := slices.Contains(ident.Capabilities, c.capability)

			res, body := env.get(t, c.path, env.keys[c.role])
			require.Equal(t, c.wantStatus, res.StatusCode, "body: %s", body)

			// The claim and the gate must agree in both directions.
			allowed := res.StatusCode != http.StatusForbidden
			require.Equal(t, allowed, hasCap,
				"capability %q = %v but %s returned %d", c.capability, hasCap, c.path, res.StatusCode)
		})
	}
}

// A capability must never name a route this process does not serve. /audit and
// /costs are mounted only when their stores are configured; this env has
// neither, so no role may advertise them — otherwise the console would render
// an audit tab that 404s, which is exactly the dead-end action the epic's
// acceptance gate forbids.
func TestIdentity_DoesNotAdvertiseUnmountedRoutes(t *testing.T) {
	env := newIdentityEnv(t, true)
	defer env.shutdown()

	for _, probe := range []struct{ capability, path string }{
		{"audit.read", "/api/v1/audit"},
		{"costs.read", "/api/v1/costs"},
	} {
		// Confirm the premise: the route really is absent from this server.
		res, _ := env.get(t, probe.path, env.keys[tenancy.RolePlatform])
		require.Equal(t, http.StatusNotFound, res.StatusCode,
			"%s should be unmounted in this env", probe.path)

		for _, role := range tenancy.AllRoles() {
			ident := env.identity(t, env.keys[role])
			require.NotContains(t, ident.Capabilities, probe.capability,
				"%s advertises %q but %s is not mounted", role, probe.capability, probe.path)
		}
	}

	// Sanity: a mounted route IS still advertised, so the test above is not
	// passing merely because capabilities came back empty.
	ident := env.identity(t, env.keys[tenancy.RoleViewer])
	require.Contains(t, ident.Capabilities, "agents.read")
}

func TestIdentity_ReportsServerVersionAndTenant(t *testing.T) {
	env := newIdentityEnv(t, true)
	defer env.shutdown()

	got := env.identity(t, env.keys[tenancy.RoleEditor])
	require.Equal(t, "test-version", got.Server.Version)
	require.NotEmpty(t, got.TenantID, "an authenticated principal must report its tenant")
	require.NotEmpty(t, got.KeyID)
	require.Equal(t, identityModeMultiTenant, got.Mode)
}

// Local mode: no credential required, and reported as local rather than as a
// signed-in viewer.
func TestIdentity_LocalModeOverHTTP(t *testing.T) {
	env := newIdentityEnv(t, false)
	defer env.shutdown()

	got := env.identity(t, "")
	require.Equal(t, identityModeLocal, got.Mode)
	require.False(t, got.Authenticated)
	require.Nil(t, got.Role, "local mode must not synthesize a role")
	require.NotEmpty(t, got.Capabilities)
}
