package server

import (
	"fmt"
	"github.com/mockagents/mockagents/internal/engine/state"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/metrics"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/quota"
	"github.com/mockagents/mockagents/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestProviderAccounting_OllamaBedrock(t *testing.T) {
	for _, family := range []struct{ path, protocol, body, streamBody string }{
		{"/api/chat", adapter.ProtocolOllamaChat, `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"hello"}]}`, `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`},
		{"/model/gpt-4o/converse", adapter.ProtocolBedrockConverse, `{"messages":[{"role":"user","content":[{"text":"hello"}]}]}`, `{"messages":[{"role":"user","content":[{"text":"hello"}]}]}`},
	} {
		for _, tenant := range []string{"tenant-a", ""} {
			for _, mode := range []string{"success", "error", "stream"} {
				t.Run(family.protocol+"/"+tenant+"/"+mode, func(t *testing.T) {
					worker, store := newTestWorker(t)
					reg := metrics.New("test")
					registry := engine.NewAgentRegistry()
					registry.Register(testFullAgent("accounting", "gpt-4o"))
					eng := engine.NewEngine(registry, state.NewMemoryStore(time.Minute), slog.Default())
					mux := http.NewServeMux()
					for _, a := range adapter.DefaultRegistry(eng).Adapters() {
						for _, route := range a.Routes() {
							mux.HandleFunc(route.Pattern, route.Handler)
						}
					}
					enf := quota.NewEnforcer(quota.Config{RatePerSec: 0.0001, RateBurst: 1})
					var usage pricing.Usage
					handler := InteractionCapture(worker, LogBodyFull, func(_ string, path, body string) { usage = pricing.ExtractUsageForPath([]byte(body), path) })(MetricsCapture(reg)(QuotaEnforce(enf)(mux)))
					path, body := family.path, family.body
					if mode == "stream" {
						body = family.streamBody
						if strings.HasPrefix(path, "/model/") {
							path += "-stream"
						}
					}
					if mode == "error" {
						body = "{"
					}
					call := func() *httptest.ResponseRecorder {
						req := httptest.NewRequest("POST", path, strings.NewReader(body))
						req = req.WithContext(engine.WithTenantID(req.Context(), tenant))
						w := httptest.NewRecorder()
						handler.ServeHTTP(w, req)
						return w
					}
					first := call()
					want := 200
					if mode == "error" {
						want = 400
					}
					require.Equal(t, want, first.Code, first.Body.String())
					if mode == "success" {
						require.Positive(t, usage.Total())
						require.Equal(t, "gpt-4o", usage.Model)
					}
					if mode == "stream" {
						require.Zero(t, usage.Total())
					}
					second := call()
					if tenant != "" {
						require.Equal(t, 429, second.Code)
					} else {
						require.Equal(t, want, second.Code)
					}
					require.True(t, waitForLog(t, store, 2, 2*time.Second))
					rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
					require.NoError(t, err)
					require.Len(t, rows, 2)
					for _, row := range rows {
						require.Equal(t, tenant, row.TenantID)
						if row.ResponseStatus == 200 && mode == "stream" {
							require.True(t, row.Streaming)
							require.Empty(t, row.ResponseBody)
						}
					}
					var scrape strings.Builder
					_, err = reg.WriteTo(&scrape)
					require.NoError(t, err)
					require.Contains(t, scrape.String(), fmt.Sprintf("status=\"%d\"", want))
					if tenant != "" {
						require.Contains(t, scrape.String(), "status=\"429\"")
					}
					// Monthly caps apply to all three endpoints; anonymous requests bypass.
					enf = quota.NewEnforcer(quota.Config{MonthlySpendUSD: 1})
					enf.AddSpend(tenant, 2)
					handler = QuotaEnforce(enf)(mux)
					third := call()
					if tenant != "" {
						require.Equal(t, 402, third.Code)
					} else {
						require.Equal(t, want, third.Code)
					}
				})
			}
		}
	}
	for _, path := range []string{"/api/tags", "/api/chat/extra", "/model//converse", "/model/a/invoke"} {
		require.False(t, isLLMProviderPath(path), path)
	}
}

func TestProviderAccounting_BedrockARN(t *testing.T) {
	path := "/model/arn:aws:bedrock:us-east-1:123:inference-profile%2Fmodel/converse"
	enf := quota.NewEnforcer(quota.Config{RatePerSec: 0.0001, RateBurst: 1})
	h := QuotaEnforce(enf)(okHandler())
	for _, want := range []int{200, 429} {
		r := httptest.NewRequest("POST", path, nil)
		r = r.WithContext(engine.WithTenantID(r.Context(), "tenant"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, want, w.Code)
	}
	r := httptest.NewRequest("POST", path, nil)
	u := pricing.ExtractUsageForPath([]byte(`{"usage":{"inputTokens":10,"outputTokens":5}}`), r.URL.Path)
	require.Equal(t, "arn:aws:bedrock:us-east-1:123:inference-profile/model", u.Model)
	require.Equal(t, 15, u.Total())
}
