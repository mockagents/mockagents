// Package llm holds the playground's provider clients. They are thin,
// stdlib-only implementations of the OpenAI Chat Completions, Anthropic
// Messages and Google Gemini generateContent wire formats.
//
// Nothing here knows about mockagents. Point BaseURL at api.openai.com or
// api.anthropic.com and the same code talks to the real providers. Pointing
// it at a mockagents server is the whole "mock" integration, which is the
// point of the demo.
package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Message is one provider-neutral conversation turn.
type Message struct {
	// Role is "user", "assistant" or "tool".
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	// ToolCalls are the calls an assistant turn requested.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID links a "tool" turn to the call it answers.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolName is the tool a "tool" turn answers (Gemini/Anthropic bookkeeping).
	ToolName string `json:"tool_name,omitempty"`
	// IsError marks a tool result that reports a failure.
	IsError bool `json:"is_error,omitempty"`
}

// ToolCall is a model-requested tool invocation. Arguments is the raw JSON
// string exactly as the model produced it: possibly malformed, which the
// agent runtime validates before any tool runs.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec declares a tool to the model.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Request is a provider-neutral completion request.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	Temperature *float64
	MaxTokens   int
	// SessionID is sent as X-Session-Id. Real providers ignore it; mockagents
	// uses it to advance turn numbers across an agent's tool loop.
	SessionID string
	// Stream requests SSE delivery; OnDelta receives each text delta.
	Stream  bool
	OnDelta func(text string)
}

// Usage is the token accounting a provider reports.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is a provider-neutral completion.
type Response struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// FinishReason is normalized to stop | length | tool_calls | content_filter.
	FinishReason string `json:"finish_reason"`
	Refusal      string `json:"refusal,omitempty"`
	Usage        Usage  `json:"usage"`
	Model        string `json:"model"`
	Streamed     bool   `json:"streamed"`
	// Headers keeps the diagnostically interesting response headers:
	// every X-Mockagents-* header (fixture/chaos metadata) and x-request-id.
	Headers map[string]string `json:"headers,omitempty"`
}

// Provider completes requests against one wire format.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req *Request) (*Response, error)
}

// Options configures a provider client.
type Options struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func (o Options) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	// No client-level timeout: every call is bounded by its context, which
	// carries the per-attempt timeout chosen by the retry policy.
	return &http.Client{}
}

func (o Options) url(path string) string {
	return strings.TrimRight(o.BaseURL, "/") + path
}

// ---------------------------------------------------------------------------
// Errors and their retry classification.
// ---------------------------------------------------------------------------

// APIError is a non-2xx provider response, decoded from the provider's own
// error envelope.
type APIError struct {
	Provider   string            `json:"provider"`
	Status     int               `json:"status"`
	Type       string            `json:"type,omitempty"`
	Message    string            `json:"message"`
	RetryAfter time.Duration     `json:"retry_after,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

func (e *APIError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("%s: HTTP %d %s: %s", e.Provider, e.Status, e.Type, e.Message)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, e.Message)
}

// TransportError wraps a failure below HTTP: connection reset, EOF,
// refused connection or a garbled response.
type TransportError struct {
	Provider string
	Err      error
}

func (e *TransportError) Error() string { return fmt.Sprintf("%s: transport: %v", e.Provider, e.Err) }
func (e *TransportError) Unwrap() error { return e.Err }

// StreamError is an SSE stream that ended without its terminal frame or
// carried an unparseable frame. The partial text is kept for diagnostics.
type StreamError struct {
	Provider string
	Reason   string
	Partial  string
}

func (e *StreamError) Error() string {
	return fmt.Sprintf("%s: stream integrity: %s (received %d chars)", e.Provider, e.Reason, len(e.Partial))
}

// Classify reports whether err is worth retrying, a short machine-readable
// reason, and any server-provided Retry-After hint.
//
// Retryable: 408, 409, 425, 429, 5xx (incl. Anthropic's 529), transport
// failures, broken streams, and an attempt that hit its own timeout while the
// parent context is still alive. Everything else (400/401/403/404/422,
// cancellation) is permanent.
func Classify(err error) (retryable bool, reason string, retryAfter time.Duration) {
	if err == nil {
		return false, "", 0
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == http.StatusTooManyRequests:
			return true, "rate_limited", apiErr.RetryAfter
		case apiErr.Status == http.StatusRequestTimeout, apiErr.Status == http.StatusConflict, apiErr.Status == http.StatusTooEarly:
			return true, fmt.Sprintf("http_%d", apiErr.Status), apiErr.RetryAfter
		case apiErr.Status >= 500:
			return true, fmt.Sprintf("http_%d", apiErr.Status), apiErr.RetryAfter
		default:
			return false, fmt.Sprintf("http_%d", apiErr.Status), 0
		}
	}
	var streamErr *StreamError
	if errors.As(err, &streamErr) {
		return true, "stream_integrity", 0
	}
	if errors.Is(err, errAttemptTimeout) {
		return true, "attempt_timeout", 0
	}
	if errors.Is(err, context.Canceled) {
		return false, "cancelled", 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false, "deadline_exceeded", 0
	}
	var transportErr *TransportError
	if errors.As(err, &transportErr) {
		return true, "transport", 0
	}
	return false, "unknown", 0
}

// errAttemptTimeout marks a per-attempt timeout (as opposed to the caller's
// own deadline). WithAttemptTimeout produces it.
var errAttemptTimeout = errors.New("attempt timed out")

// ErrAttemptTimeout is exported for callers that want errors.Is checks.
var ErrAttemptTimeout = errAttemptTimeout

// WithAttemptTimeout derives a context for one attempt. If the attempt's own
// timer fires first, the context's cause is ErrAttemptTimeout, which Classify
// treats as retryable. Expiry of the parent's deadline stays permanent.
func WithAttemptTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeoutCause(parent, d, errAttemptTimeout)
}

// wrapDoErr converts an http.Client.Do error into a typed error, preserving
// the attempt-timeout cause.
func wrapDoErr(ctx context.Context, provider string, err error) error {
	if cause := context.Cause(ctx); cause != nil && errors.Is(cause, errAttemptTimeout) {
		return fmt.Errorf("%s: %w", provider, errAttemptTimeout)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &TransportError{Provider: provider, Err: err}
}

// wrapReadErr classifies a failure while reading a response body.
func wrapReadErr(ctx context.Context, provider string, err error) error {
	if cause := context.Cause(ctx); cause != nil && errors.Is(cause, errAttemptTimeout) {
		return fmt.Errorf("%s: %w", provider, errAttemptTimeout)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || isNetErr(err) {
		return &TransportError{Provider: provider, Err: err}
	}
	return &TransportError{Provider: provider, Err: err}
}

func isNetErr(err error) bool {
	var ne net.Error
	return errors.As(err, &ne)
}

// parseRetryAfter reads a Retry-After header (seconds or HTTP date).
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs >= 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// interestingHeaders extracts X-Mockagents-* and request-id headers.
func interestingHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-mockagents-") || lk == "x-request-id" || lk == "request-id" || lk == "retry-after" {
			out[k] = strings.Join(v, ", ")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeFinish maps provider finish reasons onto the neutral set.
func normalizeFinish(reason string) string {
	switch strings.ToLower(reason) {
	case "stop", "end_turn", "stop_sequence", "finish_reason_stop":
		return "stop"
	case "length", "max_tokens", "finish_reason_max_tokens":
		return "length"
	case "tool_calls", "tool_use", "function_call":
		return "tool_calls"
	case "content_filter", "safety", "recitation", "refusal":
		return "content_filter"
	case "":
		return ""
	default:
		return strings.ToLower(reason)
	}
}
