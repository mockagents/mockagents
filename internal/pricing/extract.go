package pricing

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Usage holds the token counts extracted from a stored interaction
// response body. OpenAI, Anthropic, Gemini, Ollama, and Bedrock are supported.
type Usage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Model            string `json:"model,omitempty"`
}

// Total returns the sum of prompt + completion.
func (u Usage) Total() int { return u.PromptTokens + u.CompletionTokens }

// ExtractUsageForPath resolves Bedrock's model from its URL: Converse responses
// carry usage but no model field. It never changes the provider's wire body.
func ExtractUsageForPath(body []byte, path string) Usage {
	u := ExtractUsage(body)
	if u.Model == "" && len(body) > 0 {
		if rest, ok := strings.CutPrefix(path, "/model/"); ok {
			if id, ok := strings.CutSuffix(rest, "/converse"); ok && id != "" {
				u.Model = id
			}
		}
	}
	return u
}

// ExtractUsage parses a stored response body and returns the token
// counts plus the reported model name. Supported shapes include:
//
//	OpenAI: {"model": "...", "usage": {"prompt_tokens": N, "completion_tokens": N}}
//	Anthropic: {"model": "...", "usage": {"input_tokens": N, "output_tokens": N}}
//	Gemini: {"modelVersion": "...", "usageMetadata": {"promptTokenCount": N, "candidatesTokenCount": N}}
//
// Returns a zero Usage when body is empty, malformed, or lacks a
// usage block — callers treat this as "no cost" rather than an
// error because the absence of usage on some (streaming, error,
// tool-only) responses is expected.
func ExtractUsage(body []byte) Usage {
	if len(body) == 0 {
		return Usage{}
	}
	// A non-SSE Gemini streamGenerateContent response is a JSON *array* of
	// GenerateContentResponse objects; the usage + model live on (the last)
	// element. Unwrap and probe the last element that carries usage so the
	// streamed form is priced too, not just the single-object form.
	if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return Usage{}
		}
		for i := len(arr) - 1; i >= 0; i-- {
			if u := ExtractUsage(arr[i]); u.Total() > 0 || u.Model != "" {
				return u
			}
		}
		return Usage{}
	}
	var probe struct {
		Model           string `json:"model"`
		PromptEvalCount int    `json:"prompt_eval_count"`
		EvalCount       int    `json:"eval_count"`
		Usage           struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			BedrockInputTokens  int `json:"inputTokens"`
			BedrockOutputTokens int `json:"outputTokens"`
		} `json:"usage"`
		// Gemini shape: model in `modelVersion`, counts in `usageMetadata`.
		ModelVersion  string `json:"modelVersion"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return Usage{}
	}
	u := Usage{Model: probe.Model}
	if u.Model == "" {
		u.Model = probe.ModelVersion // Gemini reports the model here.
	}
	// Prefer OpenAI-shaped fields when present; otherwise fall back to
	// Anthropic-shaped, then Gemini-shaped. Never sum across shapes — a single
	// response only ever carries one.
	if probe.Usage.PromptTokens > 0 || probe.Usage.CompletionTokens > 0 {
		u.PromptTokens = probe.Usage.PromptTokens
		u.CompletionTokens = probe.Usage.CompletionTokens
		return u
	}
	if probe.Usage.InputTokens > 0 || probe.Usage.OutputTokens > 0 {
		u.PromptTokens = probe.Usage.InputTokens
		u.CompletionTokens = probe.Usage.OutputTokens
		return u
	}
	if probe.UsageMetadata.PromptTokenCount > 0 || probe.UsageMetadata.CandidatesTokenCount > 0 {
		u.PromptTokens = probe.UsageMetadata.PromptTokenCount
		u.CompletionTokens = probe.UsageMetadata.CandidatesTokenCount
	} else if probe.Usage.BedrockInputTokens > 0 || probe.Usage.BedrockOutputTokens > 0 {
		u.PromptTokens = probe.Usage.BedrockInputTokens
		u.CompletionTokens = probe.Usage.BedrockOutputTokens
	} else {
		u.PromptTokens = probe.PromptEvalCount
		u.CompletionTokens = probe.EvalCount
	}
	return u
}
