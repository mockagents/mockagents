package server

import (
	"net/http"
	"strconv"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/quota"
)

// isQuotaPath reports whether a request path is an LLM endpoint subject to
// per-tenant quotas. Management/health routes are never quota-limited.
func isQuotaPath(path string) bool {
	return path == "/v1/chat/completions" || path == "/v1/messages"
}

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
