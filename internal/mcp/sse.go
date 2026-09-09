package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EventStreamHandler is the server→client half of the v0.3 bidirectional
// transport. GET /mcp/events opens a long-lived SSE stream that carries
// server-initiated JSON-RPC requests (sampling/createMessage, roots/list)
// and notifications. Clients POST their replies to /mcp/response, which
// the paired ResponseHandler routes back to the SendRequest caller.
//
// Frame format:
//
//	event: request
//	data: {"jsonrpc":"2.0","id":"1","method":"sampling/createMessage","params":{...}}
//
//	event: notification
//	data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}
//
// Heartbeats (`:ping\n\n`) are written every 15s so load balancers and
// HTTP/2 middleboxes don't reap an idle connection.
type EventStreamHandler struct {
	Server *Server
	// HeartbeatInterval overrides the default 15-second keepalive.
	// Zero uses the default. Tests may set this to a short duration.
	HeartbeatInterval time.Duration
	// SubscribeBuffer overrides the subscriber channel capacity.
	// Zero uses the bidirectional default (16).
	SubscribeBuffer int
}

// NewEventStreamHandler returns a ready-to-mount SSE handler.
func NewEventStreamHandler(s *Server) *EventStreamHandler {
	return &EventStreamHandler{Server: s}
}

// ServeHTTP implements http.Handler.
func (h *EventStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := h.Server.bi.Subscribe(h.SubscribeBuffer)
	defer cancel()

	heartbeat := h.HeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := io.WriteString(w, ":heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				// Another subscriber stole the stream. Exit cleanly.
				return
			}
			if err := writeSSEMessage(w, msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEMessage formats and writes a single OutboundMessage as an SSE
// frame. Returns the first write error encountered.
func writeSSEMessage(w io.Writer, msg *OutboundMessage) error {
	var (
		eventName string
		payload   any
	)
	switch msg.Kind {
	case OutboundRequest:
		eventName = "request"
		payload = msg.Request
	case OutboundNotification:
		eventName = "notification"
		// Notifications are spec'd as JSON-RPC requests with no id.
		// Wrap the Notification into that envelope so the client can
		// parse it with the same Request decoder.
		payload = map[string]any{
			"jsonrpc": "2.0",
			"method":  msg.Notification.Method,
			"params":  msg.Notification.Params,
		}
	default:
		return fmt.Errorf("mcp: unknown outbound kind %q", msg.Kind)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, data); err != nil {
		return err
	}
	return nil
}

// ResponseHandler is the client→server half of the bidirectional
// transport. POST /mcp/response with a JSON-RPC response body; the
// handler calls Server.DeliverResponse to unblock the matching
// SendRequest caller.
type ResponseHandler struct {
	Server *Server
}

// NewResponseHandler returns a ready-to-mount response handler.
func NewResponseHandler(s *Server) *ResponseHandler {
	return &ResponseHandler{Server: s}
}

// ServeHTTP implements http.Handler.
func (h *ResponseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Same cap as every other MCP entry point (audit M-24).
	r.Body = http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
	var resp Response
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), readCapStatus(err))
		return
	}
	if resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
	}
	if err := h.Server.DeliverResponse(&resp); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// SendRequestHandler is an admin endpoint (POST /mcp/sample,
// /mcp/roots) that tests drive from outside the process to trigger a
// server-initiated JSON-RPC request. The body is the `params` object
// for the wrapped method, the timeout is read from the
// `X-MCP-Timeout-Ms` header (default 5s), and the response body is
// the raw JSON-RPC Response that DeliverResponse routed back.
//
// One handler = one method. Mount with Method set to
// "sampling/createMessage", "roots/list", or any other server-
// initiated method.
type SendRequestHandler struct {
	Server         *Server
	Method         string
	DefaultTimeout time.Duration
}

// maxAdminRequestTimeout caps how long an admin trigger may wait for the
// client's reply, whatever X-MCP-Timeout-Ms asks for.
const maxAdminRequestTimeout = 60 * time.Second

// NewSendRequestHandler returns a handler bound to the given method.
func NewSendRequestHandler(s *Server, method string) *SendRequestHandler {
	return &SendRequestHandler{Server: s, Method: method}
}

// ServeHTTP implements http.Handler.
func (h *SendRequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	timeout := h.DefaultTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if hdr := r.Header.Get("X-MCP-Timeout-Ms"); hdr != "" {
		if v, err := time.ParseDuration(hdr + "ms"); err == nil && v > 0 {
			timeout = v
		}
	}
	// A client-chosen timeout used to be unbounded: X-MCP-Timeout-Ms of a
	// few billion parked a goroutine and a pending-request entry for the
	// life of the process (audit M-24).
	if timeout > maxAdminRequestTimeout {
		timeout = maxAdminRequestTimeout
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
	var params map[string]any
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			http.Error(w, "invalid params JSON: "+err.Error(), readCapStatus(err))
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	resp, err := h.Server.SendRequest(ctx, h.Method, params)
	if err != nil {
		if ctx.Err() != nil {
			http.Error(w, "timeout waiting for client response", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
