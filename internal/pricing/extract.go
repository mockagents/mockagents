package pricing

import (
	"encoding/json"
)

// Usage holds the token counts extracted from a stored interaction
// response body. Both OpenAI and Anthropic shapes are supported.
type Usage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Model            string `json:"model,omitempty"`
}

// Total returns the sum of prompt + completion.
func (u Usage) Total() int { return u.PromptTokens + u.CompletionTokens }

// ExtractUsage parses a stored response body and returns the token
// counts plus the reported model name. Both providers are handled:
//
//   OpenAI: {"model": "...", "usage": {"prompt_tokens": N, "completion_tokens": N}}
//   Anthropic: {"model": "...", "usage": {"input_tokens": N, "output_tokens": N}}
//
// Returns a zero Usage when body is empty, malformed, or lacks a
// usage block — callers treat this as "no cost" rather than an
// error because the absence of usage on some (streaming, error,
// tool-only) responses is expected.
func ExtractUsage(body []byte) Usage {
	if len(body) == 0 {
		return Usage{}
	}
	var probe struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return Usage{}
	}
	u := Usage{Model: probe.Model}
	// Prefer OpenAI-shaped fields when present; otherwise fall back
	// to Anthropic-shaped. Never sum the two — a single response
	// won't have both.
	if probe.Usage.PromptTokens > 0 || probe.Usage.CompletionTokens > 0 {
		u.PromptTokens = probe.Usage.PromptTokens
		u.CompletionTokens = probe.Usage.CompletionTokens
		return u
	}
	if probe.Usage.InputTokens > 0 || probe.Usage.OutputTokens > 0 {
		u.PromptTokens = probe.Usage.InputTokens
		u.CompletionTokens = probe.Usage.OutputTokens
	}
	return u
}
