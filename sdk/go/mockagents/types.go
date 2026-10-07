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
	ID   string `json:"id"`
	Name string `json:"name"`
	// Arguments holds the decoded arguments. It is nil when the wire
	// arguments were missing, malformed or not a JSON object; check
	// ArgumentsValid to tell that apart from a call with no arguments.
	Arguments map[string]any `json:"arguments,omitempty"`
	// RawArguments is the arguments exactly as they arrived on the wire
	// (OpenAI sends a JSON string; for Anthropic it is the raw `input`
	// object). Empty when the wire carried no arguments at all.
	RawArguments string `json:"raw_arguments,omitempty"`
	// ArgumentsValid reports whether RawArguments decoded to a JSON object.
	// False on a malformed-arguments fault fixture, a bare array or scalar,
	// or a missing field.
	ArgumentsValid bool `json:"arguments_valid"`
}

// MessageToolCall returns tc in the OpenAI wire shape used by an assistant
// ChatMessage, preferring the raw wire arguments so a round trip replays them
// byte for byte.
func (tc ToolCall) MessageToolCall() MessageToolCall {
	args := tc.RawArguments
	if args == "" {
		args = "{}"
		if tc.Arguments != nil {
			if buf, err := json.Marshal(tc.Arguments); err == nil {
				args = string(buf)
			}
		}
	}
	return MessageToolCall{
		ID:       tc.ID,
		Type:     "function",
		Function: MessageToolCallFunction{Name: tc.Name, Arguments: args},
	}
}

// ChatMessage is a single conversational turn in a request payload.
type ChatMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	// ToolCalls carries an assistant turn's tool calls (OpenAI wire shape) so
	// a tool round trip (assistant call, then a "tool" turn with ToolCallID)
	// can be replayed, as strict-tools id validation requires.
	ToolCalls []MessageToolCall `json:"tool_calls,omitempty"`
}

// MessageToolCall is one assistant tool call in OpenAI wire shape.
type MessageToolCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function MessageToolCallFunction `json:"function"`
}

// MessageToolCallFunction is the function part of a MessageToolCall.
// Arguments is the JSON-encoded argument object, as OpenAI sends it.
type MessageToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AssistantMessage builds the assistant history turn for resp, including its
// tool calls. Append it, then one Role "tool" message per call (ToolCallID
// set), to continue a tool round trip.
func AssistantMessage(resp *ChatResponse) ChatMessage {
	msg := ChatMessage{Role: "assistant"}
	if resp == nil {
		return msg
	}
	msg.Content = resp.Content
	for _, tc := range resp.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, tc.MessageToolCall())
	}
	return msg
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
			call := ToolCall{ID: tc.ID, Name: tc.Function.Name}
			call.Arguments, call.RawArguments, call.ArgumentsValid = decodeArgs(tc.Function.Arguments)
			resp.ToolCalls = append(resp.ToolCalls, call)
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
			call := ToolCall{ID: block.ID, Name: block.Name}
			call.Arguments, call.RawArguments, call.ArgumentsValid = decodeArgs(block.Input)
			resp.ToolCalls = append(resp.ToolCalls, call)
		}
	}
	resp.Content = joinNonEmpty(textParts, " ")
	var rawAny any
	_ = json.Unmarshal(raw, &rawAny)
	resp.Raw = rawAny
	return resp, nil
}

// decodeArgs decodes a tool-argument blob, tolerating both object-shaped
// arguments (Anthropic `input`) and JSON-encoded strings (OpenAI's
// function.arguments). It returns the decoded object, the raw wire text and
// whether that text was a JSON object. Malformed arguments are reported, not
// silently collapsed, so a raw_arguments fault fixture stays observable.
func decodeArgs(raw json.RawMessage) (map[string]any, string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, "", false
	}
	text := string(raw)
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		// OpenAI form: a JSON string whose content is the argument object.
		text = s
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil || obj == nil {
		return nil, text, false
	}
	return obj, text, true
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
