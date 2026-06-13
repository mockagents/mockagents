package server

import "testing"

// TestIsLLMProviderPath_Azure pins the Azure surface into the billable/loggable
// path classifier so Azure traffic is logged + quota-counted like /v1/*.
func TestIsLLMProviderPath_Azure(t *testing.T) {
	billable := []string{
		"/openai/deployments/gpt-4o/chat/completions",
		"/openai/deployments/text-embedding-3-small/embeddings",
		"/openai/v1/chat/completions",
		"/openai/v1/embeddings",
	}
	for _, p := range billable {
		if !isLLMProviderPath(p) {
			t.Errorf("isLLMProviderPath(%q) = false, want true", p)
		}
		if !isLoggablePath(p) {
			t.Errorf("isLoggablePath(%q) = false, want true", p)
		}
	}

	notBillable := []string{
		"/openai/deployments/gpt-4o/models",
		"/openai/v1/models",
		"/openai/health",
		"/openai/deployments/",
	}
	for _, p := range notBillable {
		if isLLMProviderPath(p) {
			t.Errorf("isLLMProviderPath(%q) = true, want false", p)
		}
	}
}
