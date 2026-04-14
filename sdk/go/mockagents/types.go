package mockagents

import (
	"encoding/json"
	"fmt"
)

// TokenUsage is the prompt/completion/total token breakdown returned by
// the mock server. Values are best-effort heuristics unless the agent
// definition overrides them.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ToolCall is one tool invocation produced by the agent.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ChatMessage is a single conversational turn in a request payload.
type ChatMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// ChatResponse is the parsed response from either OpenAI Chat
// Completions or Anthropic Messages endpoints. The Raw field holds the
// untouched JSON payload for callers that need provider-specific fields.
type ChatResponse struct {
	Content      string     `json:"content"`
	Model        string     `json:"model"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Usage        TokenUsage `json:"usage"`
	Raw          any        `json:"raw,omitempty"`
	StatusCode   int        `json:"status_code"`
	LatencyMs    float64    `json:"latency_ms"`
}

// AgentSummary is a row returned by Client.ListAgents.
type AgentSummary struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Model         string   `json:"model"`
	Protocol      string   `json:"protocol"`
	ScenarioCount int      `json:"scenario_count"`
	ToolCount     int      `json:"tool_count"`
	Tags          []string `json:"tags,omitempty"`
}

// HTTPError is returned by Client methods when the server replies with
// a non-2xx status code.
type HTTPError struct {
	Status int
	Body   string
}

// Error satisfies the error interface.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("mockagents: HTTP %d: %s", e.Status, e.Body)
}

// parseOpenAIResponse converts the raw JSON payload returned by the
// OpenAI Chat Completions endpoint into a ChatResponse. Exported so
// tests can exercise the parser without HTTP machinery.
func parseOpenAIResponse(raw json.RawMessage, status int, latencyMs float64) (*ChatResponse, error) {
	var body struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}
	resp := &ChatResponse{
		Model:      body.Model,
		StatusCode: status,
		LatencyMs:  latencyMs,
		Usage: TokenUsage{
			PromptTokens:     body.Usage.PromptTokens,
			CompletionTokens: body.Usage.CompletionTokens,
			TotalTokens:      body.Usage.TotalTokens,
		},
	}
	if len(body.Choices) > 0 {
		resp.Content = body.Choices[0].Message.Content
		resp.FinishReason = body.Choices[0].FinishReason
		for _, tc := range body.Choices[0].Message.ToolCalls {
			args := decodeArgs(tc.Function.Arguments)
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
	}
	// Preserve the raw payload so callers can dip into provider-specific
	// fields without re-parsing.
	var rawAny any
	_ = json.Unmarshal(raw, &rawAny)
	resp.Raw = rawAny
	return resp, nil
}

// parseAnthropicResponse converts an Anthropic Messages payload into a
// ChatResponse. Content blocks of type "text" are joined with a single
// space; blocks of type "tool_use" become ToolCalls.
func parseAnthropicResponse(raw json.RawMessage, status int, latencyMs float64) (*ChatResponse, error) {
	var body struct {
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	resp := &ChatResponse{
		Model:        body.Model,
		FinishReason: body.StopReason,
		StatusCode:   status,
		LatencyMs:    latencyMs,
		Usage: TokenUsage{
			PromptTokens:     body.Usage.InputTokens,
			CompletionTokens: body.Usage.OutputTokens,
			TotalTokens:      body.Usage.InputTokens + body.Usage.OutputTokens,
		},
	}
	var textParts []string
	for _, block := range body.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: decodeArgs(block.Input),
			})
		}
	}
	resp.Content = joinNonEmpty(textParts, " ")
	var rawAny any
	_ = json.Unmarshal(raw, &rawAny)
	resp.Raw = rawAny
	return resp, nil
}

// decodeArgs unmarshals a tool-argument blob into a map, tolerating both
// object-shaped arguments and JSON-encoded strings (OpenAI's "function"
// schema uses the latter).
func decodeArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	// OpenAI's function.arguments can be either a JSON object or a JSON
	// string containing an object. Try the string form first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		var obj map[string]any
		if jerr := json.Unmarshal([]byte(s), &obj); jerr == nil {
			return obj
		}
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	return nil
}

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && out != "" {
			out += sep
		}
		out += p
	}
	return out
}
