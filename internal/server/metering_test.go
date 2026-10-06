package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/tenancy"
	"github.com/mockagents/mockagents/internal/types"
)

// Work that reaches the engine in process — batched sub-requests and pipeline
// nodes — is metered like the direct calls it stands for (2026-10-06 review
// S-02, S-03, S-04, E-06). Before, a tenant at its rate limit could still run
// a whole batch or pipeline, and none of it accrued spend.

type meteredEnv struct {
	addr string
	key  string
	enf  *quota.Enforcer
	tid  string
}

func newMeteredServer(t *testing.T, enf *quota.Enforcer) *meteredEnv {
	t.Helper()
	tenancyStore, err := tenancy.NewSQLiteStore(filepath.Join(t.TempDir(), "tenancy.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tenancyStore.Close() })
	tenant, err := tenancyStore.CreateTenant(t.Context(), "acme")
	require.NoError(t, err)
	key, err := tenancyStore.CreateAPIKey(t.Context(), tenant.ID, "admin", tenancy.RoleAdmin)
	require.NoError(t, err)

	registry := engine.NewAgentRegistry()
	registry.Register(testFullAgent("metered", "gpt-4o"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.NewEngine(registry, state.NewMemoryStore(time.Minute), logger)

	pipelines := engine.NewPipelineRegistry()
	pipelines.Register(&types.PipelineDefinition{
		APIVersion: "mockagents/v1", Kind: types.PipelineKind,
		Metadata: types.Metadata{Name: "two-step"},
		Spec: types.PipelineSpec{Agents: []types.PipelineAgent{
			{ID: "first", Ref: "metered"}, {ID: "second", Ref: "metered"},
		}},
	})

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.TenancyStore = tenancyStore
	cfg.QuotaEnforcer = enf
	cfg.Prices = pricing.NewDefaultTable()
	cfg.Pipelines = pipelines
	srv := New(eng, cfg, logger)
	require.NoError(t, srv.Listen())
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return &meteredEnv{addr: fmt.Sprintf("http://%s", srv.ListenAddr()), key: key.Plaintext, enf: enf, tid: tenant.ID}
}

func (e *meteredEnv) post(t *testing.T, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.addr+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+e.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (e *meteredEnv) get(t *testing.T, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.addr+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+e.key)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

const anthropicBatchBody = `{"requests":[
 {"custom_id":"a","params":{"model":"gpt-4o","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}},
 {"custom_id":"b","params":{"model":"gpt-4o","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}}]}`

func TestMetering_RateLimitedTenantCannotBatchOrRunPipelines(t *testing.T) {
	// One request per (effectively) forever: the first direct call spends it.
	e := newMeteredServer(t, quota.NewEnforcer(quota.Config{RatePerSec: 0.0001, RateBurst: 1}))

	code, body := e.post(t, "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, http.StatusOK, code, body)
	code, _ = e.post(t, "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, http.StatusTooManyRequests, code, "precondition: the tenant is rate-limited")

	// Batch: every sub-request is refused by the same limit.
	code, body = e.post(t, "/v1/messages/batches", anthropicBatchBody)
	require.Equal(t, http.StatusOK, code, body)
	var batch struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &batch))
	results := e.get(t, "/v1/messages/batches/"+batch.ID+"/results")
	require.NotContains(t, results, `"succeeded"`, "a rate-limited tenant's batch must not succeed: %s", results)
	require.Contains(t, results, "rate_limit_exceeded")

	// Pipeline: the first node is refused, reported as a 429.
	code, body = e.post(t, "/api/v1/pipelines/two-step/run", `{"input":"hello"}`)
	require.Equal(t, http.StatusTooManyRequests, code, body)
	require.Contains(t, body, "rate_limit_exceeded")
}

func TestMetering_BatchAndPipelineAccrueSpend(t *testing.T) {
	e := newMeteredServer(t, quota.NewEnforcer(quota.Config{MonthlySpendUSD: 1000}))

	code, body := e.post(t, "/v1/messages/batches", anthropicBatchBody)
	require.Equal(t, http.StatusOK, code, body)
	afterBatch := e.enf.Usage(e.tid).SpendUSD
	require.Positive(t, afterBatch, "batched sub-requests must accrue spend")

	code, body = e.post(t, "/api/v1/pipelines/two-step/run", `{"input":"hello"}`)
	require.Equal(t, http.StatusOK, code, body)
	require.Greater(t, e.enf.Usage(e.tid).SpendUSD, afterBatch, "pipeline nodes must accrue spend")

	// Once over the cap, both paths are refused with 402.
	e.enf.AddSpend(e.tid, 10_000)
	code, _ = e.post(t, "/api/v1/pipelines/two-step/run", `{"input":"hello"}`)
	require.Equal(t, http.StatusPaymentRequired, code)
	code, body = e.post(t, "/v1/messages/batches", anthropicBatchBody)
	require.Equal(t, http.StatusOK, code, body)
	var batch struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &batch))
	require.Contains(t, e.get(t, "/v1/messages/batches/"+batch.ID+"/results"), "spend_quota_exceeded")
}

// A tenant admin's view of the live-feed metrics covers only its own
// subscriptions (review S-06).
func TestLogBroadcaster_SnapshotTenantScopes(t *testing.T) {
	b := &LogBroadcaster{}
	_, cancelA := b.SubscribeTenant(4, "ten_a")
	defer cancelA()
	_, cancelB := b.SubscribeTenant(4, "ten_b")
	defer cancelB()
	_, cancelAll := b.Subscribe(4)
	defer cancelAll()

	require.Equal(t, 1, b.SnapshotTenant("ten_a").SubscriberCount)
	require.Len(t, b.SnapshotTenant("ten_a").Subscribers, 1)
	require.Equal(t, 3, b.SnapshotTenant("").SubscriberCount)
	require.Equal(t, 3, b.Snapshot().SubscriberCount)
}
