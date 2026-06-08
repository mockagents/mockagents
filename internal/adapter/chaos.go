package adapter

import (
	"math"
	"net/http"
	"strconv"

	"github.com/mockagents/mockagents/internal/engine"
)

// statusCodeOverloaded is Anthropic's non-standard "overloaded" HTTP status.
const statusCodeOverloaded = 529

// anthropicChaosErrorType maps an injected chaos status to Anthropic's
// documented error `type` (the values the real API returns), so an injected
// 401/403/429 yields a wire-accurate envelope a client SDK recognizes.
func anthropicChaosErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable, statusCodeOverloaded:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

// openAIChaosError maps an injected chaos status to OpenAI's error `type` and
// `code`. OpenAI categorizes 4xx as invalid_request_error and 5xx as
// server_error, attaching a stable `code` for auth and rate-limit failures that
// SDKs key on (invalid_api_key, rate_limit_exceeded).
func openAIChaosError(status int) (errType, code string) {
	switch status {
	case http.StatusUnauthorized:
		return "invalid_request_error", "invalid_api_key"
	case http.StatusTooManyRequests:
		// OpenAI labels rate-limit errors by the limited dimension ("requests")
		// with a stable code; "rate_limit_error" is an Anthropic value, not OpenAI's.
		return "requests", "rate_limit_exceeded"
	}
	if status >= 500 {
		return "server_error", ""
	}
	// 400/403/404 and other 4xx.
	return "invalid_request_error", ""
}

// chaosRetryAfter returns the Retry-After header value (whole seconds) a real
// provider attaches to this error, and whether to set it. A configured retry
// hint wins; otherwise ANY 429 gets a default of 1s, because real rate-limit
// responses always carry Retry-After and clients/SDKs read it to back off.
func chaosRetryAfter(ce *engine.ChaosError) (string, bool) {
	// A timeout (504) is not a rate-limit — real providers don't send Retry-After
	// on one, even though the ChaosError carries the timeout duration there.
	if ce.Timeout {
		return "", false
	}
	if ce.RetryAfter > 0 {
		secs := int(math.Ceil(ce.RetryAfter.Seconds()))
		if secs < 1 {
			secs = 1
		}
		return strconv.Itoa(secs), true
	}
	if ce.StatusCode == http.StatusTooManyRequests {
		return "1", true
	}
	return "", false
}
