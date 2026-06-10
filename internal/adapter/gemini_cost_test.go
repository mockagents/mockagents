package adapter

import (
	"encoding/json"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeminiResponse_CostExtractionSeam pins the contract between the Gemini
// adapter's wire output and the cost extractor. ExtractUsage reads the Gemini
// response's `modelVersion` + `usageMetadata.{promptTokenCount,candidatesTokenCount}`;
// the adapter must keep emitting exactly those JSON fields, or per-tenant cost
// silently drops to $0 and the monthly spend cap (402) stops working — with the
// per-package unit tests still green. This test marshals the REAL
// formatGeminiResponse output (not a hand-written body) and runs it through the
// extractor end to end.
func TestGeminiResponse_CostExtractionSeam(t *testing.T) {
	resp := &engine.Response{Content: "hello from the mock", Model: "gemini-2.0-flash"}

	// The actual adapter output, as it would be serialized onto the wire and
	// later persisted in the interaction log.
	wire := formatGeminiResponse(resp, "gemini-2.0-flash", 11, 23)
	body, err := json.Marshal(wire)
	require.NoError(t, err)

	usage := pricing.ExtractUsage(body)
	assert.Equal(t, "gemini-2.0-flash", usage.Model, "model must survive the adapter->extractor seam (from modelVersion)")
	assert.Equal(t, 11, usage.PromptTokens, "promptTokenCount must map to PromptTokens")
	assert.Equal(t, 23, usage.CompletionTokens, "candidatesTokenCount must map to CompletionTokens")

	// And it must price to something non-zero, so the cost dashboard + 402 cap work.
	cost := pricing.NewDefaultTable().Estimate(usage.Model, usage.PromptTokens, usage.CompletionTokens)
	assert.Greater(t, cost, 0.0, "a priced Gemini model with usage must yield non-zero cost")
}
