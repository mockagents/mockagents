package mockagents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout is the per-request timeout applied when ClientOptions
// doesn't override it.
const DefaultTimeout = 30 * time.Second

// ClientOptions configures a Client.
type ClientOptions struct {
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client is an HTTP client for the MockAgents server.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a Client for the given base URL. A nil or empty
// ClientOptions defaults to http://localhost:8080 with a 30s timeout.
func NewClient(opts ClientOptions) *Client {
	base := opts.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	base = strings.TrimRight(base, "/")
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{baseURL: base, httpClient: hc}
}

// BaseURL returns the server URL the client is configured against.
func (c *Client) BaseURL() string { return c.baseURL }

// ChatOptions tune a single Chat call.
type ChatOptions struct {
	Model       string
	SessionID   string
	Tools       []any
	ToolChoice  any
	Temperature *float64
	MaxTokens   *int
	Extra       map[string]any
}

// Chat calls the OpenAI Chat Completions endpoint with the given messages.
func (c *Client) Chat(ctx context.Context, messages []ChatMessage, opts ChatOptions) (*ChatResponse, error) {
	model := opts.Model
	if model == "" {
		model = "gpt-4o"
	}
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	if opts.Tools != nil {
		payload["tools"] = opts.Tools
	}
	if opts.ToolChoice != nil {
		payload["tool_choice"] = opts.ToolChoice
	}
	if opts.Temperature != nil {
		payload["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		payload["max_tokens"] = *opts.MaxTokens
	}
	for k, v := range opts.Extra {
		payload[k] = v
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if opts.SessionID != "" {
		headers["X-Session-Id"] = opts.SessionID
	}

	start := time.Now()
	status, body, err := c.do(ctx, http.MethodPost, "/v1/chat/completions", headers, payload)
	latency := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return nil, err
	}
	return parseOpenAIResponse(body, status, latency)
}

// MessageOptions tune a single Message call (Anthropic protocol).
type MessageOptions struct {
	Model     string
	SessionID string
	System    string
	MaxTokens int
	Tools     []any
	Extra     map[string]any
}

// Message calls the Anthropic Messages endpoint with the given messages.
func (c *Client) Message(ctx context.Context, messages []ChatMessage, opts MessageOptions) (*ChatResponse, error) {
	model := opts.Model
	if model == "" {
		model = "claude-3-5-sonnet-latest"
	}
	max := opts.MaxTokens
	if max == 0 {
		max = 1024
	}
	payload := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": max,
		"stream":     false,
	}
	if opts.System != "" {
		payload["system"] = opts.System
	}
	if opts.Tools != nil {
		payload["tools"] = opts.Tools
	}
	for k, v := range opts.Extra {
		payload[k] = v
	}

	headers := map[string]string{
		"Content-Type":      "application/json",
		"X-Api-Key":         "mock-api-key",
		"Anthropic-Version": "2023-06-01",
	}
	if opts.SessionID != "" {
		headers["X-Session-Id"] = opts.SessionID
	}

	start := time.Now()
	status, body, err := c.do(ctx, http.MethodPost, "/v1/messages", headers, payload)
	latency := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return nil, err
	}
	return parseAnthropicResponse(body, status, latency)
}

// Health probes /api/v1/health and returns the parsed JSON body.
func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	_, body, err := c.do(ctx, http.MethodGet, "/api/v1/health", nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAgents returns the summaries for every loaded agent.
func (c *Client) ListAgents(ctx context.Context) ([]AgentSummary, error) {
	_, body, err := c.do(ctx, http.MethodGet, "/api/v1/agents", nil, nil)
	if err != nil {
		return nil, err
	}
	var out []AgentSummary
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAgent returns the full definition for a single agent as a raw map.
func (c *Client) GetAgent(ctx context.Context, name string) (map[string]any, error) {
	_, body, err := c.do(ctx, http.MethodGet, "/api/v1/agents/"+url.PathEscape(name), nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReloadAgent triggers a hot reload of the named agent.
func (c *Client) ReloadAgent(ctx context.Context, name string) (map[string]any, error) {
	_, body, err := c.do(ctx, http.MethodPost, "/api/v1/agents/"+url.PathEscape(name)+"/reload", nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// do performs a single HTTP request and returns the raw body bytes,
// unwrapping non-2xx statuses into *HTTPError.
func (c *Client) do(ctx context.Context, method, path string, headers map[string]string, payload any) (int, json.RawMessage, error) {
	var reader io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal payload: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, nil, &HTTPError{Status: resp.StatusCode, Body: string(body)}
	}
	return resp.StatusCode, body, nil
}
