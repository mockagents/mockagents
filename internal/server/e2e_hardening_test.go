package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type quotaE2EEnv struct {
	base     string
	apiKey   string
	shutdown func()
}

func newQuotaE2EServer(t *testing.T, q quota.Config, prices *pricing.Table) quotaE2EEnv {
	t.Helper()

	tenancyStore, err := tenancy.NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
	require.NoError(t, err)
	tenant, err := tenancyStore.CreateTenant(t.Context(), "acme")
	require.NoError(t, err)
	key, err := tenancyStore.CreateAPIKey(t.Context(), tenant.ID, "admin", tenancy.RoleAdmin)
	require.NoError(t, err)

	agent := testFullAgent("tenant-agent", "gpt-4o")
	agent.Metadata.TenantID = tenant.ID
	registry := engine.NewAgentRegistry()
	registry.Register(agent)

	logStore, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "logs.db"))
	require.NoError(t, err)
	enforcer := quota.NewEnforcer(q)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(registry, state.NewMemoryStore(5*time.Minute), logger)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()
	cfg.TenancyStore = tenancyStore
	cfg.LogStore = logStore
	cfg.Prices = prices
	cfg.QuotaEnforcer = enforcer

	srv := New(eng, cfg, logger)
	require.NoError(t, srv.Listen())
	go func() { _ = srv.Serve() }()

	return quotaE2EEnv{
		base:   "http://" + srv.ListenAddr(),
		apiKey: key.Plaintext,
		shutdown: func() {
			_ = srv.Shutdown()
			_ = logStore.Close()
			_ = tenancyStore.Close()
		},
	}
}

func postChat(t *testing.T, base, apiKey string) *http.Response {
	t.Helper()
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	req, err := http.NewRequest(http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestE2E_QuotaRateLimitUsesAuthenticatedTenant(t *testing.T) {
	env := newQuotaE2EServer(t, quota.Config{RatePerSec: 1, RateBurst: 1}, pricing.NewDefaultTable())
	defer env.shutdown()

	first := postChat(t, env.base, env.apiKey)
	defer first.Body.Close()
	require.Equal(t, http.StatusOK, first.StatusCode)

	second := postChat(t, env.base, env.apiKey)
	defer second.Body.Close()
	require.Equal(t, http.StatusTooManyRequests, second.StatusCode)
	assert.NotEmpty(t, second.Header.Get("Retry-After"))
}

func TestE2E_QuotaSpendAccruesFromCapturedResponse(t *testing.T) {
	prices := pricing.NewDefaultTable()
	prices.Set(pricing.Price{Model: "gpt-4o", PromptPer1KUSD: 1000, CompletionPer1KUSD: 1000})
	env := newQuotaE2EServer(t, quota.Config{MonthlySpendUSD: 0.001}, prices)
	defer env.shutdown()

	first := postChat(t, env.base, env.apiKey)
	defer first.Body.Close()
	require.Equal(t, http.StatusOK, first.StatusCode)

	quotaReq, err := http.NewRequest(http.MethodGet, env.base+"/api/v1/quota", nil)
	require.NoError(t, err)
	quotaReq.Header.Set("Authorization", "Bearer "+env.apiKey)
	quotaResp, err := http.DefaultClient.Do(quotaReq)
	require.NoError(t, err)
	defer quotaResp.Body.Close()
	require.Equal(t, http.StatusOK, quotaResp.StatusCode)
	var usage struct {
		Usage quota.Usage `json:"usage"`
	}
	require.NoError(t, json.NewDecoder(quotaResp.Body).Decode(&usage))
	assert.Greater(t, usage.Usage.SpendUSD, 0.001)

	second := postChat(t, env.base, env.apiKey)
	defer second.Body.Close()
	require.Equal(t, http.StatusPaymentRequired, second.StatusCode)
}

func TestE2E_ConfiguredCORSAllowlistAndAPIKeyHeader(t *testing.T) {
	_, base := newServerWithCORS(t, []string{"https://app.example.com"})

	allowed, err := corsPreflight(base, "https://app.example.com")
	require.NoError(t, err)
	defer allowed.Body.Close()
	require.Equal(t, http.StatusNoContent, allowed.StatusCode)
	assert.Equal(t, "https://app.example.com", allowed.Header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, allowed.Header.Get("Access-Control-Allow-Headers"), "X-Api-Key")
	assert.Contains(t, allowed.Header.Get("Vary"), "Origin")

	denied, err := corsPreflight(base, "https://evil.example.com")
	require.NoError(t, err)
	defer denied.Body.Close()
	require.Equal(t, http.StatusNoContent, denied.StatusCode)
	assert.Empty(t, denied.Header.Get("Access-Control-Allow-Origin"))
}

func newServerWithCORS(t *testing.T, origins []string) (*Server, string) {
	t.Helper()
	registry := engine.NewAgentRegistry()
	registry.Register(testFullAgent("agent", "gpt-4o"))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(registry, state.NewMemoryStore(5*time.Minute), logger)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()
	cfg.CORSAllowedOrigins = origins

	srv := New(eng, cfg, logger)
	require.NoError(t, srv.Listen())
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return srv, fmt.Sprintf("http://%s", srv.ListenAddr())
}

func corsPreflight(base, origin string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodOptions, base+"/api/v1/health", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "X-Api-Key")
	return http.DefaultClient.Do(req)
}
