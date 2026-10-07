// Package cli implements the `playground` command-line client: every API
// operation as a subcommand, a trace renderer, and `verify`, an end-to-end
// self-check that exercises every feature against a running playground.
package cli

import (
	"bufio"
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

// Client calls the playground API.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

// NewClient returns a client for base (e.g. http://127.0.0.1:7070).
func NewClient(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: 150 * time.Second}}
}

// APIError is a non-2xx API response.
type APIError struct {
	Status  int
	Code    string
	Message string
	Details any
	Body    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// Do sends a request; body may be nil, []byte (sent as-is) or any JSON value.
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body any, out any) (int, error) {
	u := c.Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	ctype := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
		ctype = "application/yaml"
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, err
	}
	if rd != nil {
		req.Header.Set("Content-Type", ctype)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode/100 != 2 {
		ae := &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
		var env struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Details any    `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &env) == nil {
			ae.Code, ae.Message, ae.Details = env.Error.Code, env.Error.Message, env.Error.Details
		}
		return resp.StatusCode, ae
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// Get is a GET returning decoded JSON.
func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) error {
	_, err := c.Do(ctx, http.MethodGet, path, q, nil, out)
	return err
}

// Post is a POST with a JSON body.
func (c *Client) Post(ctx context.Context, path string, q url.Values, body, out any) (int, error) {
	return c.Do(ctx, http.MethodPost, path, q, body, out)
}

// Stream POSTs and calls fn for each SSE event until the stream ends.
func (c *Client) Stream(ctx context.Context, path string, body any, fn func(event string, data json.RawMessage) bool) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := &http.Client{} // no timeout: the stream ends when the run settles
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return &APIError{Status: resp.StatusCode, Body: string(b)}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	var event string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if !fn(event, json.RawMessage(strings.TrimSpace(strings.TrimPrefix(line, "data:")))) {
				return nil
			}
		}
	}
	return sc.Err()
}

// Run is the subset of a run the CLI reads.
type Run struct {
	ID          string         `json:"id"`
	Workflow    string         `json:"workflow"`
	Status      string         `json:"status"`
	Phase       string         `json:"phase"`
	CurrentStep string         `json:"current_step"`
	Input       map[string]any `json:"input"`
	Output      any            `json:"output"`
	Error       *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Step    string `json:"step"`
	} `json:"error"`
	Steps []struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Kind       string   `json:"kind"`
		Status     string   `json:"status"`
		Agent      string   `json:"agent"`
		Tier       string   `json:"tier"`
		Model      string   `json:"model"`
		Attempts   int      `json:"attempts"`
		Retries    int      `json:"retries"`
		Fallback   bool     `json:"fallback"`
		Escalated  bool     `json:"escalated"`
		DurationMS int64    `json:"duration_ms"`
		Error      string   `json:"error"`
		Notes      []string `json:"notes"`
	} `json:"steps"`
	ReviewIDs []string `json:"review_ids"`
	Stats     struct {
		LLMCalls    int     `json:"llm_calls"`
		Attempts    int     `json:"attempts"`
		Retries     int     `json:"retries"`
		Fallbacks   int     `json:"fallbacks"`
		Escalations int     `json:"escalations"`
		ToolCalls   int     `json:"tool_calls"`
		CostUSD     float64 `json:"cost_usd"`
		SLMCalls    int     `json:"slm_calls"`
		LLMTierCall int     `json:"llm_tier_calls"`
	} `json:"stats"`
	DurationMS int64 `json:"duration_ms"`
}

// Terminal reports whether the run finished.
func (r *Run) Terminal() bool { return r.Status == "completed" || r.Status == "failed" }

// Waiting reports whether the run waits on a human.
func (r *Run) Waiting() bool { return r.Phase == "awaiting_review" || r.Phase == "awaiting_approval" }

// Review is the subset of a review item the CLI reads.
type Review struct {
	ID             string         `json:"id"`
	RunID          string         `json:"run_id"`
	Workflow       string         `json:"workflow"`
	Kind           string         `json:"kind"`
	Blocking       bool           `json:"blocking"`
	Title          string         `json:"title"`
	Agent          string         `json:"agent"`
	Content        string         `json:"content"`
	Status         string         `json:"status"`
	AllowedActions []string       `json:"allowed_actions"`
	Context        map[string]any `json:"context"`
}

// StartRun launches a workflow and waits up to wait for it to settle.
func (c *Client) StartRun(ctx context.Context, workflow string, input map[string]any, options map[string]any, wait time.Duration) (*Run, error) {
	q := url.Values{}
	if wait > 0 {
		q.Set("wait", fmt.Sprintf("%.0f", wait.Seconds()))
	}
	body := map[string]any{"input": input}
	if options != nil {
		body["options"] = options
	}
	var run Run
	_, err := c.Post(ctx, "/api/workflows/"+url.PathEscape(workflow)+"/runs", q, body, &run)
	return &run, err
}

// GetRun fetches a run.
func (c *Client) GetRun(ctx context.Context, id string) (*Run, error) {
	var run Run
	err := c.Get(ctx, "/api/runs/"+url.PathEscape(id), nil, &run)
	return &run, err
}

// WaitRun polls until the run is terminal or until(run) holds.
func (c *Client) WaitRun(ctx context.Context, id string, until func(*Run) bool, timeout time.Duration) (*Run, error) {
	deadline := time.Now().Add(timeout)
	for {
		run, err := c.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		if run.Terminal() || (until != nil && until(run)) {
			return run, nil
		}
		if time.Now().After(deadline) {
			return run, fmt.Errorf("timed out waiting for run %s (status %s, phase %s)", id, run.Status, run.Phase)
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// PendingReviews lists pending blocking reviews (optionally for one run).
func (c *Client) PendingReviews(ctx context.Context, runID string) ([]Review, error) {
	q := url.Values{"status": {"pending"}, "blocking": {"true"}}
	if runID != "" {
		q.Set("run_id", runID)
	}
	var out struct {
		Reviews []Review `json:"reviews"`
	}
	err := c.Get(ctx, "/api/reviews", q, &out)
	return out.Reviews, err
}

// Decide posts a review decision.
func (c *Client) Decide(ctx context.Context, id, action, comment, reviewer string) (*Review, error) {
	var out Review
	_, err := c.Post(ctx, "/api/reviews/"+url.PathEscape(id)+"/decision", nil,
		map[string]any{"action": action, "comment": comment, "reviewer": reviewer}, &out)
	return &out, err
}
