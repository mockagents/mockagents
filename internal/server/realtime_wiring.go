// Cross-cutting server concerns for the Realtime WebSocket surface (round-7
// R7-20/R7-21). Realtime responses are generated in-process on an established
// socket, so the HTTP middleware that meters the request/response protocols
// (QuotaEnforce, InteractionCapture) never sees them; the adapter exposes
// per-response hooks the server wires here. Tenant scoping for browser-style
// credentials is handled by lifting the WebSocket subprotocol token into the
// Authorization header before the tenancy middleware runs.
package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/storage"
)

// wireRealtime attaches quota enforcement and interaction logging to the
// Realtime adapter's per-response seam.
func (s *Server) wireRealtime(rt *adapter.RealtimeHandler) {
	if enf := s.config.QuotaEnforcer; enf != nil {
		rt.CheckQuota = func(tenantID string) error {
			if tenantID == "" {
				return nil // single-tenant / anonymous traffic is never limited
			}
			if ok, _ := enf.AllowRequest(tenantID); !ok {
				return errors.New("tenant request-rate quota exceeded")
			}
			if !enf.CheckSpend(tenantID) {
				return errors.New("tenant monthly spend quota exceeded")
			}
			return nil
		}
	}

	worker, prices, enf := s.logWorker, s.config.Prices, s.config.QuotaEnforcer
	rt.OnResponse = func(tenantID, model string, inputTokens, outputTokens int, resp *engine.Response) {
		if enf != nil && prices != nil && tenantID != "" {
			if cost := prices.Estimate(model, inputTokens, outputTokens); cost > 0 {
				enf.AddSpend(tenantID, cost)
			}
		}
		if worker == nil {
			return
		}
		// The stored body is a synthetic usage record (no conversation
		// content — safe under every LOG_BODIES mode) shaped so
		// pricing.ExtractUsage and the costs dashboard read it like any other
		// provider row.
		body := fmt.Sprintf(`{"model":%q,"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`,
			model, inputTokens, outputTokens)
		worker.Submit(&storage.InteractionLog{
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
			TenantID:       tenantID,
			AgentName:      resp.AgentName,
			Protocol:       "openai-realtime",
			RequestMethod:  "WS",
			RequestPath:    "/v1/realtime",
			ResponseStatus: http.StatusOK,
			ResponseBody:   body,
			ScenarioName:   resp.ScenarioName,
			ToolCallsCount: len(resp.ToolCalls),
			Streaming:      true,
		})
	}
}

// RealtimeBrowserAuth lifts a browser WebSocket credential — the
// "openai-insecure-api-key.<key>" Sec-WebSocket-Protocol offer — into the
// Authorization header on /v1/realtime when none is present, so the tenancy
// middleware's best-effort principal resolution can scope the socket to a
// tenant (round-7 R7-20). Ephemeral ek_ tokens are NOT tenant credentials
// (the adapter resolves them against the mint store) and are left alone.
func RealtimeBrowserAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/realtime" && r.Header.Get("Authorization") == "" {
		lift:
			for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
				for _, p := range strings.Split(header, ",") {
					token, ok := strings.CutPrefix(strings.TrimSpace(p), "openai-insecure-api-key.")
					if ok && token != "" && !strings.HasPrefix(token, "ek_") {
						r.Header.Set("Authorization", "Bearer "+token)
						break lift
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
