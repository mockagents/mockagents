// Package mockctl talks to the mockagents management API: health, the agent
// catalog, interaction logs, cost rollups, native pipelines, agent reloads
// (to re-arm stateful chaos) and runtime fixture authoring. It uses the
// in-repo Go SDK (sdk/go/mockagents) where the SDK covers an endpoint, and
// plain HTTP for the rest.
package mockctl

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

	mockagents "github.com/mockagents/mockagents/sdk/go/mockagents"
)

// Client is a mockagents management-API client.
type Client struct {
	base   string
	apiKey string
	sdk    *mockagents.Client
	http   *http.Client
}

// New returns a client for the server at baseURL.
func New(baseURL, apiKey string) *Client {
	base := strings.TrimRight(baseURL, "/")
	hc := &http.Client{Timeout: 30 * time.Second, Transport: bearer{key: apiKey, next: http.DefaultTransport}}
	return &Client{
		base:   base,
		apiKey: apiKey,
		sdk:    mockagents.NewClient(mockagents.ClientOptions{BaseURL: base, HTTPClient: hc}),
		http:   hc,
	}
}

// bearer adds "Authorization: Bearer <key>" (multi-tenant mockagents servers
// require an API key on the management API).
type bearer struct {
	key  string
	next http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.key != "" && r.Header.Get("Authorization") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.key)
	}
	return b.next.RoundTrip(r)
}

// BaseURL returns the server URL.
func (c *Client) BaseURL() string { return c.base }

// Health calls GET /api/v1/health (via the Go SDK).
func (c *Client) Health(ctx context.Context) (map[string]any, error) { return c.sdk.Health(ctx) }

// Agents calls GET /api/v1/agents (via the Go SDK).
func (c *Client) Agents(ctx context.Context) ([]mockagents.AgentSummary, error) {
	return c.sdk.ListAgents(ctx)
}

// Reload calls POST /api/v1/agents/{name}/reload (via the Go SDK). Reloading
// re-registers the definition, which resets its chaos fail_first counters.
func (c *Client) Reload(ctx context.Context, name string) error {
	_, err := c.sdk.ReloadAgent(ctx, name)
	return err
}

// Arm reloads every named agent.
func (c *Client) Arm(ctx context.Context, names []string) error {
	for _, n := range names {
		if err := c.Reload(ctx, n); err != nil {
			return fmt.Errorf("reload %s: %w", n, err)
		}
	}
	return nil
}

// Error is a non-2xx management API response.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string { return fmt.Sprintf("mockagents: HTTP %d: %s", e.Status, e.Body) }

// Response is a raw management-API response.
type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

// Do performs a raw request. contentType defaults to application/json when
// body is non-nil; accept, when set, is sent as the Accept header (the
// mockagents agent endpoint returns canonical YAML for application/yaml).
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte, contentType, accept string) (*Response, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return &Response{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: raw}, err
}

// JSON performs a request and decodes a 2xx JSON response into out.
func (c *Client) JSON(ctx context.Context, method, path string, query url.Values, body []byte, contentType string, out any) error {
	resp, err := c.Do(ctx, method, path, query, body, contentType, "")
	if err != nil {
		return err
	}
	if resp.Status/100 != 2 {
		return &Error{Status: resp.Status, Body: strings.TrimSpace(string(resp.Body))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Body, out)
}
