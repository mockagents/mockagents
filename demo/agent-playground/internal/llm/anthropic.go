package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Anthropic speaks the Messages wire format (POST /v1/messages).
type Anthropic struct{ opts Options }

// NewAnthropic returns an Anthropic-compatible client.
func NewAnthropic(opts Options) *Anthropic { return &Anthropic{opts: opts} }

// Name implements Provider.
func (p *Anthropic) Name() string { return "anthropic" }

// anthropicVersion is the API version header the Messages API requires.
const anthropicVersion = "2023-06-01"

type anBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

type anTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anRequest struct {
	Model       string      `json:"model"`
	System      string      `json:"system,omitempty"`
	Messages    []anMessage `json:"messages"`
	Tools       []anTool    `json:"tools,omitempty"`
	MaxTokens   int         `json:"max_tokens"`
	Temperature *float64    `json:"temperature,omitempty"`
	Stream      bool        `json:"stream,omitempty"`
}

type anResponse struct {
	Model      string    `json:"model"`
	Content    []anBlock `json:"content"`
	StopReason string    `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// buildRequest converts neutral messages into Anthropic's alternating
// user/assistant shape: tool results become tool_result blocks inside a
// user turn, and consecutive same-role turns are merged.
func (p *Anthropic) buildRequest(req *Request) anRequest {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	out := anRequest{Model: req.Model, System: req.System, MaxTokens: maxTokens, Temperature: req.Temperature, Stream: req.Stream}
	appendBlocks := func(role string, blocks ...anBlock) {
		if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == role {
			out.Messages[n-1].Content = append(out.Messages[n-1].Content, blocks...)
			return
		}
		out.Messages = append(out.Messages, anMessage{Role: role, Content: blocks})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			var blocks []anBlock
			if m.Content != "" {
				blocks = append(blocks, anBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Arguments)
				if !json.Valid(input) || !strings.HasPrefix(strings.TrimSpace(tc.Arguments), "{") {
					input = json.RawMessage(`{}`)
				}
				blocks = append(blocks, anBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
			}
			if len(blocks) == 0 {
				blocks = []anBlock{{Type: "text", Text: ""}}
			}
			appendBlocks("assistant", blocks...)
		case "tool":
			appendBlocks("user", anBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content, IsError: m.IsError})
		default:
			appendBlocks("user", anBlock{Type: "text", Text: m.Content})
		}
	}
	for _, t := range req.Tools {
		schema := t.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		out.Tools = append(out.Tools, anTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out
}

func decodeAnthropicError(_ int, _ http.Header, body []byte) *APIError {
	var env struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	return &APIError{Type: env.Error.Type, Message: env.Error.Message}
}

// Complete implements Provider.
func (p *Anthropic) Complete(ctx context.Context, req *Request) (*Response, error) {
	headers := map[string]string{
		"anthropic-version": anthropicVersion,
		"X-Session-Id":      req.SessionID,
		"x-api-key":         p.opts.APIKey,
	}
	resp, err := postJSON(ctx, p.opts.client(), p.Name(), p.opts.url("/v1/messages"), headers, p.buildRequest(req), decodeAnthropicError)
	if err != nil {
		return nil, err
	}
	if req.Stream {
		return p.readStream(ctx, resp, req.OnDelta)
	}
	var body anResponse
	if err := readJSON(ctx, p.Name(), resp, &body); err != nil {
		return nil, err
	}
	out := &Response{
		Model:        body.Model,
		FinishReason: normalizeFinish(body.StopReason),
		Usage:        Usage{InputTokens: body.Usage.InputTokens, OutputTokens: body.Usage.OutputTokens},
		Headers:      interestingHeaders(resp.Header),
	}
	var text strings.Builder
	for _, b := range body.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Arguments: args})
		}
	}
	out.Content = text.String()
	return out, nil
}

func (p *Anthropic) readStream(ctx context.Context, resp *http.Response, onDelta func(string)) (*Response, error) {
	defer resp.Body.Close()
	out := &Response{Streamed: true, Headers: interestingHeaders(resp.Header)}
	type block struct {
		typ, id, name string
		args          strings.Builder
	}
	blocks := map[int]*block{}
	var order []int
	var text strings.Builder
	sawStop := false
	err := readSSE(resp.Body, func(ev sseEvent) (bool, error) {
		var frame struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message *struct {
				Model string `json:"model"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			ContentBlock *anBlock `json:"content_block"`
			Delta        *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &frame); err != nil {
			return true, &StreamError{Provider: p.Name(), Reason: "malformed SSE frame", Partial: text.String()}
		}
		switch frame.Type {
		case "message_start":
			if frame.Message != nil {
				out.Model = frame.Message.Model
				out.Usage.InputTokens = frame.Message.Usage.InputTokens
			}
		case "content_block_start":
			b := &block{}
			if frame.ContentBlock != nil {
				b.typ, b.id, b.name = frame.ContentBlock.Type, frame.ContentBlock.ID, frame.ContentBlock.Name
			}
			blocks[frame.Index] = b
			order = append(order, frame.Index)
		case "content_block_delta":
			if frame.Delta == nil {
				break
			}
			switch frame.Delta.Type {
			case "text_delta":
				text.WriteString(frame.Delta.Text)
				if onDelta != nil && frame.Delta.Text != "" {
					onDelta(frame.Delta.Text)
				}
			case "input_json_delta":
				if b := blocks[frame.Index]; b != nil {
					b.args.WriteString(frame.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if frame.Delta != nil && frame.Delta.StopReason != "" {
				out.FinishReason = normalizeFinish(frame.Delta.StopReason)
			}
			if frame.Usage != nil {
				out.Usage.OutputTokens = frame.Usage.OutputTokens
			}
		case "message_stop":
			sawStop = true
			return true, nil
		case "error":
			msg := "stream error"
			if frame.Error != nil {
				msg = frame.Error.Type + ": " + frame.Error.Message
			}
			return true, &StreamError{Provider: p.Name(), Reason: msg, Partial: text.String()}
		}
		return false, nil
	})
	if err != nil {
		if se, ok := err.(*StreamError); ok {
			return nil, se
		}
		return nil, wrapReadErr(ctx, p.Name(), err)
	}
	if !sawStop {
		return nil, &StreamError{Provider: p.Name(), Reason: "stream ended without message_stop", Partial: text.String()}
	}
	out.Content = text.String()
	for _, i := range order {
		b := blocks[i]
		if b.typ != "tool_use" {
			continue
		}
		args := b.args.String()
		if args == "" {
			args = "{}"
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.id, Name: b.name, Arguments: args})
	}
	return out, nil
}

// ensure interface conformance at compile time.
var (
	_ Provider = (*OpenAI)(nil)
	_ Provider = (*Anthropic)(nil)
	_ Provider = (*Gemini)(nil)
)

// errNoTools is returned by providers that this demo implements text-only.
var errNoTools = fmt.Errorf("tool calling is not implemented for this provider in the playground")
