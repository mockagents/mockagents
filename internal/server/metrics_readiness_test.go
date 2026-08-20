package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/metrics"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// obsTestServer builds a server wired to an ISOLATED metrics registry, so
// HTTP-level assertions are exact regardless of what else ran in this package.
// Engine-level families (scenario matches, chaos) always land on
// metrics.Default(); tests that need those use a unique agent name instead of
// isolation.
func obsTestServer(t *testing.T, mutate func(*Config), agents ...*types.AgentDefinition) (*httptest.Server, *metrics.Registry) {
	t.Helper()
	registry := engine.NewAgentRegistry()
	for _, a := range agents {
		registry.Register(a)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	eng := engine.NewEngine(registry, state.NewMemoryStore(5*time.Minute), logger)

	cfg := DefaultConfig()
	cfg.Port = 0
	cfg.AgentsDir = t.TempDir()
	reg := metrics.New("test-version")
	cfg.Metrics = reg
	if mutate != nil {
		mutate(&cfg)
	}

	srv := New(eng, cfg, logger)
	ts := httptest.NewServer(srv.httpServer.Handler)
	t.Cleanup(ts.Close)
	return ts, reg
}

func scrape(t *testing.T, ts *httptest.Server, opts ...func(*http.Request)) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	require.NoError(t, err)
	for _, o := range opts {
		o(req)
	}
	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body), resp.Header
}

func chat(t *testing.T, ts *httptest.Server, model, message string) int {
	t.Helper()
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"` + message + `"}]}`
	resp, err := ts.Client().Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestMetricsEndpointServesPrometheusText(t *testing.T) {
	ts, _ := obsTestServer(t, nil, testFullAgent("obs-agent", "gpt-obs"))

	status, body, hdr := scrape(t, ts)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, metrics.ContentType, hdr.Get("Content-Type"))
	assert.Equal(t, "no-store", hdr.Get("Cache-Control"))

	// Every family is declared even before any traffic, so an operator can
	// tell "zero chaos injections" from "this build has no chaos metrics".
	for _, family := range []string{
		"mockagents_requests_total",
		"mockagents_scenario_matches_total",
		"mockagents_chaos_injections_total",
		"mockagents_request_duration_seconds",
		"mockagents_agents_loaded",
		"mockagents_ready",
		"mockagents_build_info",
	} {
		assert.Contains(t, body, "# TYPE "+family+" ", "family %s not declared", family)
	}
	assert.Contains(t, body, `mockagents_build_info{version="test-version"`)
	assert.Contains(t, body, "mockagents_agents_loaded 1")
	assert.Contains(t, body, "mockagents_ready 1")
}

func TestMetricsCountsRequestsByProtocolAgentStatus(t *testing.T) {
	// Two agents on purpose: with exactly one loaded, the engine treats it as
	// the implicit default for ANY model, so the unknown-model case below
	// would resolve instead of 404ing.
	ts, _ := obsTestServer(t, nil,
		testFullAgent("obs-counted", "gpt-counted"),
		testFullAgent("obs-counted-2", "gpt-counted-2"))

	require.Equal(t, http.StatusOK, chat(t, ts, "gpt-counted", "hello"))
	require.Equal(t, http.StatusOK, chat(t, ts, "gpt-counted", "hello"))
	// Unknown model → the engine never resolves an agent, so the request is
	// still counted, under agent="unknown" with its real status.
	require.Equal(t, http.StatusNotFound, chat(t, ts, "no-such-model", "hello"))

	_, body, _ := scrape(t, ts)
	assert.Contains(t, body,
		`mockagents_requests_total{protocol="openai-chat-completions",agent="obs-counted",status="200"} 2`)
	assert.Contains(t, body,
		`mockagents_requests_total{protocol="openai-chat-completions",agent="unknown",status="404"} 1`)

	// The latency histogram counted the same three requests.
	assert.Contains(t, body,
		`mockagents_request_duration_seconds_count{protocol="openai-chat-completions"} 3`)
}

// TestMetricsIgnoresManagementTraffic: "requests by protocol/agent/status" is
// about the mocked LLM surfaces. Counting the scrape itself, or the GUI's
// polling of /api/v1/agents, would drown the signal.
func TestMetricsIgnoresManagementTraffic(t *testing.T) {
	ts, _ := obsTestServer(t, nil, testFullAgent("obs-mgmt", "gpt-mgmt"))

	for i := 0; i < 3; i++ {
		resp, err := ts.Client().Get(ts.URL + "/api/v1/agents")
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	_, body, _ := scrape(t, ts)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "mockagents_requests_total{") {
			t.Errorf("management traffic produced a request series: %s", line)
		}
	}
}

// TestMetricsCaptureWithoutLogStore pins the fallback in MetricsCapture: with
// no log store there is no InteractionCapture middleware, so nothing else
// attaches the RequestMeta the adapters stamp. Without the fallback every
// request would be reported as protocol="unknown".
func TestMetricsCaptureWithoutLogStore(t *testing.T) {
	ts, _ := obsTestServer(t, nil, testFullAgent("obs-nostore", "gpt-nostore"))
	require.Equal(t, http.StatusOK, chat(t, ts, "gpt-nostore", "hello"))

	_, body, _ := scrape(t, ts)
	assert.Contains(t, body,
		`mockagents_requests_total{protocol="openai-chat-completions",agent="obs-nostore",status="200"} 1`)
	assert.NotContains(t, body, `protocol="unknown"`)
}

// TestMetricsCaptureWithLogStore is the same assertion with the interaction
// log enabled, i.e. the default deployment. Both wirings must agree.
func TestMetricsCaptureWithLogStore(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "logs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ts, _ := obsTestServer(t, func(c *Config) { c.LogStore = store }, testFullAgent("obs-store", "gpt-store"))
	require.Equal(t, http.StatusOK, chat(t, ts, "gpt-store", "hello"))

	_, body, _ := scrape(t, ts)
	assert.Contains(t, body,
		`mockagents_requests_total{protocol="openai-chat-completions",agent="obs-store",status="200"} 1`)
}

// TestMetricsCaptureSeesQuotaRejections pins the OTHER half of the middleware
// ordering claim: metrics wrap QuotaEnforce, so a 429 is counted rather than
// silently dropped before the meter.
func TestMetricsCaptureSeesQuotaRejections(t *testing.T) {
	reg := metrics.New("test")
	enf := quota.NewEnforcer(quota.Config{RatePerSec: 1, RateBurst: 1})
	h := MetricsCapture(reg)(QuotaEnforce(enf)(okHandler()))

	h.ServeHTTP(httptest.NewRecorder(), llmReq("ten_a"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, llmReq("ten_a"))
	require.Equal(t, http.StatusTooManyRequests, rec.Code, "precondition: second call is rate-limited")

	body := reg.Render()
	assert.Contains(t, body, `status="429"`,
		"a quota rejection was not counted — MetricsCapture must wrap QuotaEnforce, not the reverse")
}

func TestScenarioMatchKindsAreRecorded(t *testing.T) {
	metrics.Default().Reset()
	ts, _ := obsTestServer(t, nil, testFullAgent("obs-scenarios", "gpt-scenarios"))

	require.Equal(t, http.StatusOK, chat(t, ts, "gpt-scenarios", "hello there")) // matches the greeting rule
	require.Equal(t, http.StatusOK, chat(t, ts, "gpt-scenarios", "anything"))    // falls to the default scenario

	body := metrics.Default().Render()
	assert.Contains(t, body,
		`mockagents_scenario_matches_total{agent="obs-scenarios",scenario="greeting",kind="rule"} 1`)
	assert.Contains(t, body,
		`mockagents_scenario_matches_total{agent="obs-scenarios",scenario="default",kind="default"} 1`)
}

func TestChaosInjectionsAreRecorded(t *testing.T) {
	metrics.Default().Reset()
	agent := testFullAgent("obs-chaos", "gpt-chaos")
	agent.Spec.Behavior.Chaos = &types.ChaosConfig{
		Enabled: true,
		Errors:  &types.ChaosErrorConfig{Rate: 1.0, StatusCodes: []int{503}},
	}
	ts, reg := obsTestServer(t, nil, agent)

	require.Equal(t, http.StatusServiceUnavailable, chat(t, ts, "gpt-chaos", "hello"))

	assert.Contains(t, metrics.Default().Render(),
		`mockagents_chaos_injections_total{agent="obs-chaos",kind="error"} 1`)

	// A chaos fault aborts BEFORE the adapter would stamp the agent from the
	// response, so the engine stamps it at resolution time instead. Without
	// that, every injected failure is filed under agent="unknown" — useless
	// for the one question an operator asks: which agent is failing?
	assert.Contains(t, reg.Render(),
		`mockagents_requests_total{protocol="openai-chat-completions",agent="obs-chaos",status="503"} 1`)
}

// ---- readiness -------------------------------------------------------------

func readiness(t *testing.T, ts *httptest.Server) (int, readinessResponse) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + "/api/v1/ready")
	require.NoError(t, err)
	defer resp.Body.Close()
	var body readinessResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return resp.StatusCode, body
}

func TestReadinessPassesWithFixturesLoaded(t *testing.T) {
	ts, _ := obsTestServer(t, nil, testFullAgent("obs-ready", "gpt-ready"))

	status, body := readiness(t, ts)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "ready", body.Status)
	require.Len(t, body.Checks, 1)
	assert.Equal(t, "fixtures", body.Checks[0].Name)
	assert.Equal(t, "ok", body.Checks[0].Status)
}

// TestReadinessFailsWithNoFixtures is the whole point of R9: a process with no
// agents loaded can only 404, so it must not be put into rotation. Before this
// slice both probes hit /api/v1/health and this pod was declared ready.
func TestReadinessFailsWithNoFixtures(t *testing.T) {
	ts, _ := obsTestServer(t, nil) // no agents

	status, body := readiness(t, ts)
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "not_ready", body.Status)
	require.Len(t, body.Checks, 1)
	assert.Equal(t, "fixtures", body.Checks[0].Name)
	assert.Equal(t, "failed", body.Checks[0].Status)
	assert.Contains(t, body.Checks[0].Error, "no agent fixtures loaded")

	// ...and liveness is deliberately unchanged: the process IS alive, so
	// restarting it would fix nothing.
	resp, err := ts.Client().Get(ts.URL + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"liveness must stay unconditional — a fixture-less pod needs re-configuring, not restarting")

	// The gauge agrees with the probe.
	_, metricsBody, _ := scrape(t, ts)
	assert.Contains(t, metricsBody, "mockagents_ready 0")
	assert.Contains(t, metricsBody, "mockagents_agents_loaded 0")
}

func TestReadinessFailsWhenLogStoreUnreachable(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "logs.db"))
	require.NoError(t, err)
	ts, _ := obsTestServer(t, func(c *Config) { c.LogStore = store }, testFullAgent("obs-store-down", "gpt-store-down"))

	status, body := readiness(t, ts)
	require.Equal(t, http.StatusOK, status, "precondition: ready while the store is open")
	require.Len(t, body.Checks, 2)

	require.NoError(t, store.Close())

	status, body = readiness(t, ts)
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "not_ready", body.Status)
	var storeCheck readinessCheckResult
	for _, c := range body.Checks {
		if c.Name == "log_store" {
			storeCheck = c
		}
	}
	assert.Equal(t, "failed", storeCheck.Status, "checks: %+v", body.Checks)
	assert.Contains(t, storeCheck.Error, "interaction log unreachable")
	// The fixture check still passes, so the body names WHICH dependency broke.
	assert.Equal(t, "ok", body.Checks[0].Status)
}

// ---- multi-tenant exposure -------------------------------------------------

func TestReadinessOpenInMultiTenantMode(t *testing.T) {
	_, addr, _ := setupTenantServer(t, testFullAgent("mt-ready", "gpt-mt-ready"))

	resp, err := http.Get(addr + "/api/v1/ready")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"a kubelet has no API key; gating readiness would take every pod out of rotation")
}

func TestMetricsRequiresCredentialsInMultiTenantMode(t *testing.T) {
	_, addr, key := setupTenantServer(t, testFullAgent("mt-metrics", "gpt-mt-metrics"))

	resp, err := http.Get(addr + "/metrics")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"agent and scenario names are metric labels — an unauthenticated scrape would leak them")

	req, err := http.NewRequest(http.MethodGet, addr+"/metrics", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "# TYPE mockagents_requests_total counter")
}
