package streaming

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter handles writing Server-Sent Events to an HTTP response.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSEWriter. Returns an error if the ResponseWriter
// does not support flushing (required for SSE).
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported: ResponseWriter does not implement http.Flusher")
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteData writes a data-only SSE event: "data: {json}\n\n"
func (s *SSEWriter) WriteData(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling SSE data: %w", err)
	}
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
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, data); err != nil {
		return fmt.Errorf("writing SSE event: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// WriteRaw writes a raw SSE line: "data: {raw}\n\n"
func (s *SSEWriter) WriteRaw(raw string) error {
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", raw); err != nil {
		return fmt.Errorf("writing raw SSE: %w", err)
	}
	s.flusher.Flush()
	return nil
}
