package mcp

import (
	"io"
	"net/http"
)

// HTTPHandler wraps a Server in an http.Handler that accepts a single
// JSON-RPC request per POST and writes the JSON-RPC response. Notifications
// return 204 No Content as per the MCP Streamable HTTP convention.
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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading request: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	out, err := h.Server.HandleBytes(body)
	if err != nil {
		http.Error(w, "handler error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if out == nil {
		// Notification: no response.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}
