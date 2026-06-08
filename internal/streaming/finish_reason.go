package streaming

import "strings"

// Finish/stop-reason mapping for the FB-03 semantic error modes. These live in
// the streaming package (the lowest-level wire renderer) so both the streaming
// and non-streaming (adapter) paths share one source of truth — adapter imports
// streaming, not the reverse.

// AnthropicStopReason maps an OpenAI-style finish_reason to Anthropic's
// stop_reason vocabulary (end_turn, max_tokens, tool_use, refusal, ...).
func AnthropicStopReason(finishReason string) string {
	switch finishReason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "refusal"
	case "stop", "":
		return "end_turn"
	default:
		return finishReason
	}
}

// GeminiFinishReason maps an OpenAI-style finish_reason to Gemini's enum
// (STOP, MAX_TOKENS, SAFETY, ...).
func GeminiFinishReason(finishReason string) string {
	switch finishReason {
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	case "stop", "tool_calls", "":
		return "STOP"
	default:
		return strings.ToUpper(finishReason)
	}
}
