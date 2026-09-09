package streaming

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// StreamWriteWindow is how far into the future each SSE frame pushes the
// connection's write deadline. http.Server.WriteTimeout is armed once, when
// the request starts, so a paced stream (load-target physics, chaos latency)
// longer than the server's 60s default was severed mid-frame with no
// terminal event — indistinguishable from the truncation fault the product
// injects on purpose (audit M-16). Resetting per frame turns the timeout
// into an inter-frame idle bound instead: a stream only dies if it goes
// silent for this long.
const StreamWriteWindow = 60 * time.Second

// SSEWriter handles writing Server-Sent Events to an HTTP response.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	rc      *http.ResponseController
}

// NewSSEWriter creates an SSEWriter. Returns an error if the ResponseWriter
// does not support flushing (required for SSE).
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported: ResponseWriter does not implement http.Flusher")
	}

	s := &SSEWriter{w: w, flusher: flusher, rc: http.NewResponseController(w)}
	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	s.bumpDeadline()
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return s, nil
}

// bumpDeadline extends the connection write deadline by StreamWriteWindow.
// Writers that cannot set one (recorders, some middleware) return
// ErrNotSupported, which is deliberately ignored: the stream then simply
// keeps the server's global timeout, as before.
func (s *SSEWriter) bumpDeadline() {
	_ = s.rc.SetWriteDeadline(time.Now().Add(StreamWriteWindow))
}

// WriteData writes a data-only SSE event: "data: {json}\n\n"
func (s *SSEWriter) WriteData(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling SSE data: %w", err)
	}
	s.bumpDeadline()
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("writing SSE data: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// WriteEvent writes a named SSE event: "event: {name}\ndata: {json}\n\n"
func (s *SSEWriter) WriteEvent(eventType string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling SSE event: %w", err)
	}
	s.bumpDeadline()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
		return fmt.Errorf("writing SSE event: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// WriteRaw writes a raw SSE line: "data: {raw}\n\n"
func (s *SSEWriter) WriteRaw(raw string) error {
	s.bumpDeadline()
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", raw); err != nil {
		return fmt.Errorf("writing raw SSE: %w", err)
	}
	s.flusher.Flush()
	return nil
}
