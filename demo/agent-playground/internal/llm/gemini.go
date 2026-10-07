package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Gemini speaks the Google generateContent wire format
// (POST /v1beta/models/{model}:generateContent). The playground uses it
// text-only, for the SLM-tier ticket classifier; streaming requests fall
// back to one non-streaming call and a single delta.
type Gemini struct{ opts Options }

// NewGemini returns a Gemini-compatible client.
func NewGemini(opts Options) *Gemini { return &Gemini{opts: opts} }

// Name implements Provider.
func (p *Gemini) Name() string { return "gemini" }

type gmPart struct {
	Text string `json:"text,omitempty"`
}

type gmContent struct {
	Role  string   `json:"role,omitempty"`
	Parts []gmPart `json:"parts"`
}

type gmRequest struct {
	Contents          []gmContent `json:"contents"`
	SystemInstruction *gmContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  *struct {
		Temperature     *float64 `json:"temperature,omitempty"`
		MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	} `json:"generationConfig,omitempty"`
}

type gmResponse struct {
	Candidates []struct {
		Content      gmContent `json:"content"`
		FinishReason string    `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func decodeGeminiError(_ int, _ http.Header, body []byte) *APIError {
	var env struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	return &APIError{Type: env.Error.Status, Message: env.Error.Message}
}

// Complete implements Provider.
func (p *Gemini) Complete(ctx context.Context, req *Request) (*Response, error) {
	if len(req.Tools) > 0 {
		return nil, errNoTools
	}
	body := gmRequest{}
	if req.System != "" {
		body.SystemInstruction = &gmContent{Parts: []gmPart{{Text: req.System}}}
	}
	if req.Temperature != nil || req.MaxTokens > 0 {
		body.GenerationConfig = &struct {
			Temperature     *float64 `json:"temperature,omitempty"`
			MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
		}{Temperature: req.Temperature, MaxOutputTokens: req.MaxTokens}
	}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		if m.Role == "tool" || len(m.ToolCalls) > 0 {
			return nil, errNoTools
		}
		body.Contents = append(body.Contents, gmContent{Role: role, Parts: []gmPart{{Text: m.Content}}})
	}
	headers := map[string]string{"X-Session-Id": req.SessionID, "x-goog-api-key": p.opts.APIKey}
	path := fmt.Sprintf("/v1beta/models/%s:generateContent", url.PathEscape(req.Model))
	resp, err := postJSON(ctx, p.opts.client(), p.Name(), p.opts.url(path), headers, body, decodeGeminiError)
	if err != nil {
		return nil, err
	}
	var out gmResponse
	if err := readJSON(ctx, p.Name(), resp, &out); err != nil {
		return nil, err
	}
	if len(out.Candidates) == 0 {
		return nil, &TransportError{Provider: p.Name(), Err: fmt.Errorf("response has no candidates")}
	}
	var text strings.Builder
	for _, part := range out.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	res := &Response{
		Content:      text.String(),
		FinishReason: normalizeFinish(out.Candidates[0].FinishReason),
		Usage:        Usage{InputTokens: out.UsageMetadata.PromptTokenCount, OutputTokens: out.UsageMetadata.CandidatesTokenCount},
		Model:        req.Model,
		Headers:      interestingHeaders(resp.Header),
	}
	if req.Stream && req.OnDelta != nil && res.Content != "" {
		req.OnDelta(res.Content)
	}
	return res, nil
}
