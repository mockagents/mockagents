package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

// OpenAI speaks the Chat Completions wire format (POST /v1/chat/completions).
type OpenAI struct{ opts Options }

// NewOpenAI returns an OpenAI-compatible client.
func NewOpenAI(opts Options) *OpenAI { return &OpenAI{opts: opts} }

// Name implements Provider.
func (p *OpenAI) Name() string { return "openai" }

type oaMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Refusal    string       `json:"refusal,omitempty"`
}

type oaToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	} `json:"function"`
}

type oaRequest struct {
	Model         string      `json:"model"`
	Messages      []oaMessage `json:"messages"`
	Tools         []oaTool    `json:"tools,omitempty"`
	Temperature   *float64    `json:"temperature,omitempty"`
	MaxTokens     int         `json:"max_tokens,omitempty"`
	Stream        bool        `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

type oaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type oaResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      oaMessage `json:"message"`
		Delta        oaMessage `json:"delta"`
		FinishReason *string   `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaUsage `json:"usage"`
}

func strPtr(s string) *string { return &s }

func (p *OpenAI) buildRequest(req *Request) oaRequest {
	out := oaRequest{Model: req.Model, Temperature: req.Temperature, MaxTokens: req.MaxTokens, Stream: req.Stream}
	if req.Stream {
		out.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{IncludeUsage: true}
	}
	if req.System != "" {
		out.Messages = append(out.Messages, oaMessage{Role: "system", Content: strPtr(req.System)})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "tool":
			out.Messages = append(out.Messages, oaMessage{Role: "tool", Content: strPtr(m.Content), ToolCallID: m.ToolCallID})
		case "assistant":
			om := oaMessage{Role: "assistant", Content: strPtr(m.Content)}
			for _, tc := range m.ToolCalls {
				var otc oaToolCall
				otc.ID, otc.Type = tc.ID, "function"
				otc.Function.Name, otc.Function.Arguments = tc.Name, tc.Arguments
				om.ToolCalls = append(om.ToolCalls, otc)
			}
			out.Messages = append(out.Messages, om)
		default:
			out.Messages = append(out.Messages, oaMessage{Role: "user", Content: strPtr(m.Content)})
		}
	}
	for _, t := range req.Tools {
		var ot oaTool
		ot.Type = "function"
		ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Parameters
		out.Tools = append(out.Tools, ot)
	}
	return out
}

func decodeOpenAIError(_ int, _ http.Header, body []byte) *APIError {
	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	return &APIError{Type: env.Error.Type, Message: env.Error.Message}
}

// Complete implements Provider.
func (p *OpenAI) Complete(ctx context.Context, req *Request) (*Response, error) {
	headers := map[string]string{"X-Session-Id": req.SessionID}
	if p.opts.APIKey != "" {
		headers["Authorization"] = "Bearer " + p.opts.APIKey
	}
	resp, err := postJSON(ctx, p.opts.client(), p.Name(), p.opts.url("/v1/chat/completions"), headers, p.buildRequest(req), decodeOpenAIError)
	if err != nil {
		return nil, err
	}
	if req.Stream {
		return p.readStream(ctx, resp, req.OnDelta)
	}
	var body oaResponse
	if err := readJSON(ctx, p.Name(), resp, &body); err != nil {
		return nil, err
	}
	if len(body.Choices) == 0 {
		return nil, &TransportError{Provider: p.Name(), Err: fmt.Errorf("response has no choices")}
	}
	ch := body.Choices[0]
	out := &Response{Model: body.Model, Headers: interestingHeaders(resp.Header), Refusal: ch.Message.Refusal}
	if ch.Message.Content != nil {
		out.Content = *ch.Message.Content
	}
	if ch.FinishReason != nil {
		out.FinishReason = normalizeFinish(*ch.FinishReason)
	}
	for _, tc := range ch.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	if body.Usage != nil {
		out.Usage = Usage{InputTokens: body.Usage.PromptTokens, OutputTokens: body.Usage.CompletionTokens}
	}
	return out, nil
}

func (p *OpenAI) readStream(ctx context.Context, resp *http.Response, onDelta func(string)) (*Response, error) {
	defer resp.Body.Close()
	out := &Response{Streamed: true, Headers: interestingHeaders(resp.Header)}
	type partial struct {
		id, name string
		args     []byte
	}
	calls := map[int]*partial{}
	sawFinish, sawDone := false, false
	var text []byte
	err := readSSE(resp.Body, func(ev sseEvent) (bool, error) {
		if ev.Data == "[DONE]" {
			sawDone = true
			return true, nil
		}
		var chunk oaResponse
		if err := json.Unmarshal([]byte(ev.Data), &chunk); err != nil {
			return true, &StreamError{Provider: p.Name(), Reason: "malformed SSE frame", Partial: string(text)}
		}
		if chunk.Model != "" {
			out.Model = chunk.Model
		}
		if chunk.Usage != nil {
			out.Usage = Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != nil && *ch.Delta.Content != "" {
				text = append(text, *ch.Delta.Content...)
				if onDelta != nil {
					onDelta(*ch.Delta.Content)
				}
			}
			for i, tc := range ch.Delta.ToolCalls {
				idx := i
				if tc.Index != nil {
					idx = *tc.Index
				}
				pc := calls[idx]
				if pc == nil {
					pc = &partial{}
					calls[idx] = pc
				}
				if tc.ID != "" {
					pc.id = tc.ID
				}
				if tc.Function.Name != "" {
					pc.name = tc.Function.Name
				}
				pc.args = append(pc.args, tc.Function.Arguments...)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				sawFinish = true
				out.FinishReason = normalizeFinish(*ch.FinishReason)
			}
		}
		return false, nil
	})
	if err != nil {
		if se, ok := err.(*StreamError); ok {
			return nil, se
		}
		return nil, wrapReadErr(ctx, p.Name(), err)
	}
	if !sawFinish && !sawDone {
		return nil, &StreamError{Provider: p.Name(), Reason: "stream ended without a finish frame", Partial: string(text)}
	}
	out.Content = string(text)
	idxs := make([]int, 0, len(calls))
	for i := range calls {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		pc := calls[i]
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: pc.id, Name: pc.name, Arguments: string(pc.args)})
	}
	return out, nil
}
