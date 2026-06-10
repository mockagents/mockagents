package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/quota"
)

// isLLMProviderPath reports whether a request path is a model-provider LLM
// endpoint: OpenAI (/v1/chat/completions), Anthropic (/v1/messages), or Gemini
// (/v1beta/models/{model}:generateContent and :streamGenerateContent). It is the
// single source of truth for "this is quota-able/billable LLM traffic", shared by
// the quota and interaction-logging middleware so the two can never disagree
// about which routes count. The Gemini match is the two generate methods only —
// not any /v1beta/models/... method (e.g. a future :countTokens is excluded).
func isLLMProviderPath(path string) bool {
	switch path {
	case "/v1/chat/completions", "/v1/messages":
		return true
	}
	if strings.HasPrefix(path, "/v1beta/models/") {
		return strings.HasSuffix(path, ":generateContent") ||
			strings.HasSuffix(path, ":streamGenerateContent")
	}
	return false
}

// isQuotaPath reports whether a request path is an LLM endpoint subject to
// per-tenant quotas. Management/health routes are never quota-limited.
func isQuotaPath(path string) bool { return isLLMProviderPath(path) }

// QuotaEnforce rejects requests that exceed a tenant's request-rate (429) or
// monthly-spend (402) cap (REF-08 slice C). It must run after the principal's
// tenant is on the context (inner of WithPrincipalTenantScope) and only acts on
// the LLM endpoints. A nil enforcer, a non-LLM path, or an empty tenant
// (single-tenant / anonymous traffic) passes through untouched.
func QuotaEnforce(enf *quota.Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if enf == nil || !isQuotaPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			tenantID := engine.TenantIDFromContext(r.Context())
			if tenantID == "" {
				next.ServeHTTP(w, r)
				return
			}
			if ok, retry := enf.AllowRequest(tenantID); !ok {
				secs := int(retry.Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				writeJSON(w, http.StatusTooManyRequests, map[string]any{
					"error": map[string]string{
						"type":    "rate_limit_exceeded",
						"message": "tenant request-rate quota exceeded",
					},
				})
				return
			}
			if !enf.CheckSpend(tenantID) {
				writeJSON(w, http.StatusPaymentRequired, map[string]any{
					"error": map[string]string{
						"type":    "spend_quota_exceeded",
						"message": "tenant monthly spend quota exceeded",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
