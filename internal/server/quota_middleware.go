package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/quota"
)

// isLLMProviderPath reports whether a request path is a model-provider LLM
// endpoint: OpenAI (/v1/chat/completions, /v1/responses, /v1/embeddings),
// Anthropic (/v1/messages), Gemini (/v1beta/models/{model}:generateContent and
// :streamGenerateContent), or the Azure OpenAI surface (the
// /openai/deployments/{id}/{chat/completions,embeddings} classic paths and the
// /openai/v1/... unified paths). It is the single source of truth for "this is
// quota-able/billable LLM traffic", shared by the quota and interaction-logging
// middleware so the two can never disagree about which routes count. The Gemini
// match is the two generate methods only — not any /v1beta/models/... method
// (e.g. a future :countTokens is excluded).
func isLLMProviderPath(path string) bool {
	switch path {
	case "/v1/chat/completions", "/v1/responses", "/v1/embeddings", "/v1/messages":
		return true
	case "/openai/v1/chat/completions", "/openai/v1/embeddings":
		return true
	}
	if strings.HasPrefix(path, "/v1beta/models/") {
		return strings.HasSuffix(path, ":generateContent") ||
			strings.HasSuffix(path, ":streamGenerateContent")
	}
	if strings.HasPrefix(path, "/openai/deployments/") {
		return strings.HasSuffix(path, "/chat/completions") ||
			strings.HasSuffix(path, "/embeddings")
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
				writeJSON(w, http.StatusTooManyRequests, ProviderQuotaError{
					Error: providerError{
						Type:    "rate_limit_exceeded",
						Message: "tenant request-rate quota exceeded",
					},
				})
				return
			}
			if !enf.CheckSpend(tenantID) {
				writeJSON(w, http.StatusPaymentRequired, ProviderQuotaError{
					Error: providerError{
						Type:    "spend_quota_exceeded",
						Message: "tenant monthly spend quota exceeded",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ProviderQuotaError is the error shape the PROVIDER endpoints return when a
// quota rejects a request. Exported so the OpenAPI schema of the same name can
// be checked against it.
//
// Deliberately not ErrorResponse. These come back on /v1/chat/completions and
// /v1/messages, where an SDK is parsing the response — so they mimic the
// upstream `{"error": {"type", "message"}}` object, which is what those SDKs
// know how to surface. Flattening them to match the management API's envelope
// would make the rejection unreadable to the very clients it is aimed at.
type ProviderQuotaError struct {
	Error providerError `json:"error"`
}

type providerError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
