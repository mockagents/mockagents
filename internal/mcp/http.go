package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// maxMCPBodyBytes caps a single JSON-RPC request body on the HTTP transport so a
// hostile client can't drive an unbounded io.ReadAll allocation. The standalone
// `mockagents mcp --transport http` server applies no MaxBodySize middleware, so
// the cap lives in the handler itself and protects every mount point (SEC-01).
const maxMCPBodyBytes = 1 << 20 // 1 MiB

// readCapStatus maps a body-read error to an HTTP status: 413 when the
// MaxBytesReader cap was exceeded, 400 otherwise.
func readCapStatus(err error) int {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// HTTPHandler wraps a Server in an http.Handler that accepts a single
// JSON-RPC request per POST and writes the JSON-RPC response. Notifications
// return 204 No Content as per the MCP Streamable HTTP convention.
//
// v0.2 additions:
//   - Any pending server-emitted notifications (queued via
//     Server.EmitNotification) are surfaced in the response headers as
//     `X-MCP-Pending-Notifications: N` and as a JSON array body when
//     the inbound request was itself a notification.
//   - The companion NotifyHandler exposes a small admin endpoint
//     (POST /mcp/notify) so test harnesses can drive the queue from
//     outside the server process.
type HTTPHandler struct {
	Server *Server
}

// NewHTTPHandler returns a ready-to-mount handler.
func NewHTTPHandler(s *Server) *HTTPHandler {
	return &HTTPHandler{Server: s}
}

// ServeHTTP implements http.Handler.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading request: "+err.Error(), readCapStatus(err))
		return
	}
	defer r.Body.Close()

	out, err := h.Server.HandleBytes(body)
	if err != nil {
		http.Error(w, "handler error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Drain any notifications the request handler emitted (or that an
	// out-of-band POST /mcp/notify queued earlier) so clients never
	// have to poll a separate endpoint.
	notifications := h.Server.DrainNotifications()
	if len(notifications) > 0 {
		w.Header().Set("X-MCP-Pending-Notifications", itoa(len(notifications)))
	}

	if out == nil {
		// Notification request: write any queued server notifications
		// as a JSON array body, otherwise 204.
		if len(notifications) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(notifications)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if len(notifications) > 0 {
		// Multipart-ish bundling: response first, then a server-sent
		// `notifications` envelope appended after a record separator.
		// Clients that want notifications opt in by parsing the
		// X-MCP-Pending-Notifications header. For everyone else the
		// leading bytes are still a valid JSON-RPC response.
		envelope := struct {
			Response      json.RawMessage `json:"response"`
			Notifications []*Notification `json:"notifications"`
		}{
			Response:      out,
			Notifications: notifications,
		}
		_ = json.NewEncoder(w).Encode(envelope)
		return
	}
	_, _ = w.Write(out)
}

// itoa is a tiny no-alloc int->string for header values that avoids
// pulling in strconv just for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// NotifyHandler is a small admin endpoint that lets test harnesses
// enqueue a server-emitted notification from outside the process.
// POST a JSON object of the form `{"method": "...", "params": {...}}`
// and the next ServeHTTP call (or DrainNotifications consumer) will
// see it in the queue.
type NotifyHandler struct {
	Server *Server
}

// NewNotifyHandler builds the admin endpoint bound to the same Server.
func NewNotifyHandler(s *Server) *NotifyHandler {
	return &NotifyHandler{Server: s}
}

// ServeHTTP implements http.Handler.
func (h *NotifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
	var body Notification
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid notification: "+err.Error(), readCapStatus(err))
		return
	}
	if body.Method == "" {
		http.Error(w, "method is required", http.StatusBadRequest)
		return
	}
	h.Server.EmitNotification(body.Method, body.Params)
	w.WriteHeader(http.StatusAccepted)
}
