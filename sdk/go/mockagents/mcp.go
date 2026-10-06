package mockagents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// McpEvent is one parsed SSE frame from GET /mcp/events. Kind is the
// SSE event: line — the MockAgents server emits "request" for
// server-initiated JSON-RPC requests and "notification" for fire-and-
// forget notifications. Payload is the decoded JSON-RPC envelope.
//
// Mirrors the Python McpEvent dataclass and the TypeScript McpEvent
// interface one-for-one so example code translates across all three
// SDKs.
type McpEvent struct {
	Kind    string
	Payload JSONRPCEnvelope
}

// JSONRPCEnvelope is the JSON-RPC 2.0 envelope for a server-initiated
// request or notification. ID is non-nil on requests and nil on
// notifications. Params is always decoded as a generic map when
// present; handlers can type-assert as needed.
type JSONRPCEnvelope struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  map[string]any  `json:"params,omitempty"`
}

// JSONRPCError is the error object in a JSON-RPC 2.0 response. Code
// follows the spec's reserved ranges (e.g. -32601 method-not-found,
// -32603 internal error).
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// IsRequest reports whether the event carries a JSON-RPC id — i.e.
// whether the client is expected to POST a matching response.
func (e *McpEvent) IsRequest() bool {
	return e != nil && e.Kind == "request" && len(e.Payload.ID) > 0 && string(e.Payload.ID) != "null"
}

// IsNotification reports whether the event is a fire-and-forget
// notification (no id, no reply expected).
func (e *McpEvent) IsNotification() bool {
	return !e.IsRequest()
}

// Params returns the event's params as a generic map, or an empty
// map when the server omitted the field. Always non-nil so handlers
// can use “params["x"]“ without first checking for nil.
func (e *McpEvent) Params() map[string]any {
	if e == nil || e.Payload.Params == nil {
		return map[string]any{}
	}
	return e.Payload.Params
}

// McpRequestHandler is the signature every handler in a
// DispatchRequest handlers map must satisfy. Receives the parsed
// params map and returns the result map that will be POSTed back as
// the JSON-RPC reply. Return a non-nil error to have the client post
// a -32603 internal error and re-surface the original error to the
// caller.
type McpRequestHandler func(ctx context.Context, params map[string]any) (map[string]any, error)

// McpClientOptions tunes an McpClient. All fields are optional.
type McpClientOptions struct {
	BaseURL    string
	HTTPClient *http.Client
}

// McpClient is the Go SDK facade over the MockAgents MCP bidirectional
// transport. One client owns an *http.Client and exposes three entry
// points matching the Python and TypeScript SDKs:
//
//   - Connect(ctx) opens a long-lived GET /mcp/events SSE
//     subscription and returns an McpEventStream iterator.
//   - SendResponse(ctx, id, result, error) POSTs a JSON-RPC reply to
//     /mcp/response.
//   - DispatchRequest(ctx, event, handlers) routes one event through
//     a method->handler map, auto-posts the result, and re-surfaces
//     any handler error so tests still see the failure.
//
// Use via NewMcpClient — the zero value is not usable.
type McpClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMcpClient returns an McpClient configured against the given base
// URL. A nil or empty options struct defaults to http://localhost:8080
// with the stdlib default http.Client (no timeout, so long-lived SSE
// subscriptions work correctly).
func NewMcpClient(opts McpClientOptions) *McpClient {
	base := opts.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	return &McpClient{
		baseURL:    trimTrailingSlash(base),
		httpClient: hc,
	}
}

// BaseURL returns the URL the client is configured against.
func (c *McpClient) BaseURL() string { return c.baseURL }

// Connect opens a subscription to GET /mcp/events and returns a
// scanner-style stream iterator. The returned McpEventStream holds
// the underlying HTTP response open; callers must call Close when
// done to release the connection back to the pool.
//
// The passed context bounds the entire subscription — cancelling it
// aborts the request and the next Next call returns false with
// ctx.Err().
func (c *McpClient) Connect(ctx context.Context) (*McpEventStream, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.baseURL+"/mcp/events", nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	// Drop any client-level timeout — SSE is long-lived by design.
	httpClient := c.httpClient
	if httpClient.Timeout > 0 {
		cloned := *httpClient
		cloned.Timeout = 0
		httpClient = &cloned
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		return nil, &HTTPError{Status: resp.StatusCode, Body: string(body)}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	scanner.Split(splitSSEFrames)
	return &McpEventStream{
		body:    resp.Body,
		scanner: scanner,
		cancel:  cancel,
	}, nil
}

// SendResponse POSTs a JSON-RPC reply to /mcp/response. Exactly one
// of result/jsonrpcErr must be non-nil; passing both or neither is a
// programmer error and returns a validation error without hitting the
// network. The server routes the reply to the matching SendRequest
// caller by id — an unknown id comes back as 404 (wrapped in
// *HTTPError here).
func (c *McpClient) SendResponse(ctx context.Context, id json.RawMessage, result map[string]any, jsonrpcErr *JSONRPCError) error {
	if (result == nil) == (jsonrpcErr == nil) {
		return fmt.Errorf("mockagents: SendResponse needs exactly one of result or error")
	}
	if len(id) == 0 {
		return fmt.Errorf("mockagents: SendResponse requires a non-empty id")
	}
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
	}
	if result != nil {
		body["result"] = result
	} else {
		body["error"] = jsonrpcErr
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/mcp/response", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return &HTTPError{Status: resp.StatusCode, Body: string(respBody)}
	}
	return nil
}

// DispatchRequest routes one server-initiated request through a
// handler map and POSTs the matching response. Returns the handler's
// return value on success.
//
// Semantics match the Python and TypeScript SDKs exactly:
//
//   - If the event is not a request (IsRequest returns false), this
//     returns an error without touching the network.
//   - If no handler matches event.Payload.Method, the client POSTs a
//     JSON-RPC -32601 error and returns a non-nil error wrapping
//     the unknown method name.
//   - If the handler returns an error, the client POSTs a -32603
//     error carrying the error's string and returns the original
//     error so the caller still sees the failure.
func (c *McpClient) DispatchRequest(ctx context.Context, event *McpEvent, handlers map[string]McpRequestHandler) (map[string]any, error) {
	if !event.IsRequest() {
		return nil, fmt.Errorf("mockagents: DispatchRequest called on a non-request event")
	}
	method := event.Payload.Method
	handler, ok := handlers[method]
	if !ok {
		errBody := &JSONRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("method %q not handled", method),
		}
		_ = c.SendResponse(ctx, event.Payload.ID, nil, errBody)
		return nil, fmt.Errorf("mockagents: unknown method %q", method)
	}
	result, err := handler(ctx, event.Params())
	if err != nil {
		errBody := &JSONRPCError{
			Code:    -32603,
			Message: err.Error(),
		}
		_ = c.SendResponse(ctx, event.Payload.ID, nil, errBody)
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	if err := c.SendResponse(ctx, event.Payload.ID, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

// McpEventStream is a scanner-style iterator over parsed McpEvent
// frames. Use it like bufio.Scanner: call Next in a loop, read Value
// each iteration, then check Err. Close releases the HTTP body.
//
//	stream, err := client.Connect(ctx)
//	if err != nil { ... }
//	defer stream.Close()
//	for stream.Next() {
//	    ev := stream.Value()
//	    // ...
//	}
//	if err := stream.Err(); err != nil { ... }
type McpEventStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	cancel  context.CancelFunc
	done    bool
	value   *McpEvent
	err     error
}

// Next advances the stream. Returns true when Value holds a new
// event. After Next returns false, inspect Err to tell EOF from
// failure. Heartbeat comments and malformed frames are dropped
// silently so the iterator only surfaces valid events.
func (s *McpEventStream) Next() bool {
	if s == nil || s.done {
		return false
	}
	for s.scanner.Scan() {
		frame := parseSSEFrame(s.scanner.Text())
		if frame == nil {
			continue
		}
		var env JSONRPCEnvelope
		if err := json.Unmarshal([]byte(frame.Data), &env); err != nil {
			// Malformed data lines are dropped — matches the Python
			// and TypeScript helpers.
			continue
		}
		kind := frame.Event
		if kind == "" {
			kind = "message"
		}
		s.value = &McpEvent{Kind: kind, Payload: env}
		return true
	}
	if err := s.scanner.Err(); err != nil {
		s.err = err
	}
	s.done = true
	return false
}

// Value returns the event most recently surfaced by Next.
func (s *McpEventStream) Value() *McpEvent {
	if s == nil {
		return nil
	}
	return s.value
}

// Err returns the first error encountered while reading, or nil.
// Context cancellation counts as a read error.
func (s *McpEventStream) Err() error {
	if s == nil {
		return nil
	}
	return s.err
}

// Close releases the underlying response body and cancels the
// request context. Safe to call multiple times.
func (s *McpEventStream) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.body != nil {
		err := s.body.Close()
		s.body = nil
		return err
	}
	return nil
}

// trimTrailingSlash removes one trailing slash if present. Matches
// the behavior in client.go's NewClient so both clients accept
// "http://host" and "http://host/" interchangeably.
func trimTrailingSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}
