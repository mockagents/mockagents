package adapter

import (
	"net/http"

	"github.com/mockagents/mockagents/internal/engine"
)

// chaosErrorType maps a ChaosError to the error-type label each provider
// surfaces in its JSON error envelope. OpenAI and Anthropic share the same
// set of category strings for these HTTP codes.
func chaosErrorType(ce *engine.ChaosError) string {
	switch ce.StatusCode {
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return "timeout_error"
	case http.StatusServiceUnavailable:
		return "overloaded_error"
	default:
		return "api_error"
	}
}
