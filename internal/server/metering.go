package server

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/mockagents/mockagents/internal/engine"
	pricingpkg "github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/quota"
)

// Metering for work that reaches the engine in process.
//
// Quota (request rate, monthly spend) is enforced by HTTP middleware, and spend
// is accrued as InteractionCapture sees each provider response. Batched
// sub-requests and pipeline nodes call the engine below that chain, so they
// used to run unmetered: one batch could replay 50,000 requests for a tenant
// that was already rate-limited, and none of it counted toward the spend cap
// (2026-10-06 review S-02, S-03, S-04, E-06). Both now pass through the same
// checks as a direct call.

// spendHook returns the function that accrues one provider response's cost
// against its tenant's monthly spend, or nil when quotas or pricing are off.
func (s *Server) spendHook() func(tenantID, path, respBody string) {
	enf, prices := s.config.QuotaEnforcer, s.config.Prices
	if enf == nil || prices == nil {
		return nil
	}
	return func(tenantID, path, respBody string) {
		if tenantID == "" || respBody == "" {
			return
		}
		usage := pricingpkg.ExtractUsageForPath([]byte(respBody), path)
		s.warnUnpricedModel(usage.Model)
		if cost := prices.Estimate(usage.Model, usage.PromptTokens, usage.CompletionTokens); cost > 0 {
			enf.AddSpend(tenantID, cost)
		}
	}
}

// warnUnpricedModel logs, once per model, that spend is accruing at the
// fallback price because the price table does not know the model. With the
// default $0 fallback a monthly spend cap can never trigger for such a model,
// which used to happen silently (audit L-31).
func (s *Server) warnUnpricedModel(model string) {
	if model == "" || s.config.Prices == nil {
		return
	}
	if _, known := s.config.Prices.Lookup(model); known {
		return
	}
	if _, seen := s.unpricedModels.LoadOrStore(model, struct{}{}); seen {
		return
	}
	fallback := s.config.Prices.Fallback
	s.logger.Warn("model is not in the price table; spend accrues at the fallback price",
		"model", model,
		"fallback_prompt_per_1k_usd", fallback.PromptPer1KUSD,
		"fallback_completion_per_1k_usd", fallback.CompletionPer1KUSD,
		"hint", "add the model, or a non-zero fallback, to the MOCKAGENTS_PRICING file so monthly spend caps can see it")
}

// subrequestMeter returns the middleware batched sub-requests are served
// through: the request-rate and spend checks QuotaEnforce applies to a direct
// call, then spend accrual from the response body. Nil when quotas are off.
func (s *Server) subrequestMeter() func(http.Handler) http.Handler {
	enf := s.config.QuotaEnforcer
	if enf == nil {
		return nil
	}
	charge := s.spendHook()
	quota := QuotaEnforce(enf)
	return func(next http.Handler) http.Handler {
		if charge != nil {
			next = chargeSpend(charge)(next)
		}
		return quota(next)
	}
}

// chargeSpend tees the response body and accrues its cost once the handler
// returns.
func chargeSpend(charge func(tenantID, path, respBody string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tee := &teeWriter{ResponseWriter: w}
			next.ServeHTTP(tee, r)
			charge(engine.TenantIDFromContext(r.Context()), r.URL.Path, tee.body.String())
		})
	}
}

type teeWriter struct {
	http.ResponseWriter
	body bytes.Buffer
}

func (t *teeWriter) Write(b []byte) (int, error) {
	t.body.Write(b)
	return t.ResponseWriter.Write(b)
}

// quotaDenial is a quota rejection raised outside QuotaEnforce (pipeline
// nodes), carrying what the HTTP layer needs to render it the same way.
type quotaDenial struct {
	status     int
	retryAfter time.Duration
	errType    string
	message    string
}

func (d *quotaDenial) Error() string { return d.message }

// pipelineMeter applies tenant quotas to pipeline nodes: each node counts as
// one request against the rate limit, must fit under the spend cap, and
// accrues the estimated cost of its response.
type pipelineMeter struct {
	enf    *quota.Enforcer
	prices *pricingpkg.Table
	warn   func(model string)
}

func (m *pipelineMeter) AdmitNode(ctx context.Context) error {
	tenantID := engine.TenantIDFromContext(ctx)
	if tenantID == "" {
		return nil
	}
	if ok, retry := m.enf.AllowRequest(tenantID); !ok {
		return &quotaDenial{status: http.StatusTooManyRequests, retryAfter: retry,
			errType: "rate_limit_exceeded", message: "tenant request-rate quota exceeded"}
	}
	if !m.enf.CheckSpend(tenantID) {
		return &quotaDenial{status: http.StatusPaymentRequired,
			errType: "spend_quota_exceeded", message: "tenant monthly spend quota exceeded"}
	}
	return nil
}

func (m *pipelineMeter) ChargeNode(ctx context.Context, n engine.NodeInteraction) {
	if m.prices == nil || n.TenantID == "" || n.Response == nil {
		return
	}
	if m.warn != nil {
		m.warn(n.Response.Model)
	}
	prompt := adapter.EstimateTokens(n.Input)
	completion := adapter.EstimateTokens(n.Response.Content)
	if cost := m.prices.Estimate(n.Response.Model, prompt, completion); cost > 0 {
		m.enf.AddSpend(n.TenantID, cost)
	}
}

// newPipelineMeter returns nil when quotas are off.
func (s *Server) newPipelineMeter() engine.NodeMeter {
	if s.config.QuotaEnforcer == nil {
		return nil
	}
	return &pipelineMeter{enf: s.config.QuotaEnforcer, prices: s.config.Prices, warn: s.warnUnpricedModel}
}

// writeQuotaDenial renders a quota rejection in the provider error shape
// QuotaEnforce uses, with Retry-After for a rate-limit denial.
func writeQuotaDenial(w http.ResponseWriter, d *quotaDenial) {
	if d.status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(d.retryAfter)))
	}
	writeJSON(w, d.status, ProviderQuotaError{Error: providerError{Type: d.errType, Message: d.message}})
}
