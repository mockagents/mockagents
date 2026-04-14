package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
)

// LogHandlers holds dependencies for log query API handlers.
type LogHandlers struct {
	Store *storage.SQLiteStore
}

// ListLogs handles GET /api/v1/logs with optional filtering.
func (h *LogHandlers) ListLogs(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "logging is not enabled",
		})
		return
	}

	filter := storage.InteractionFilter{
		AgentName: r.URL.Query().Get("agent"),
		SessionID: r.URL.Query().Get("session_id"),
		Since:     r.URL.Query().Get("since"),
		Until:     r.URL.Query().Get("until"),
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid limit parameter",
			})
			return
		}
		filter.Limit = limit
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid offset parameter",
			})
			return
		}
		filter.Offset = offset
	}

	logs, err := h.Store.Query(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("querying logs: %s", err),
		})
		return
	}

	if logs == nil {
		logs = []storage.InteractionLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// GetLog handles GET /api/v1/logs/{id}.
func (h *LogHandlers) GetLog(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "logging is not enabled",
		})
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid log ID",
		})
		return
	}

	log, err := h.Store.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("fetching log: %s", err),
		})
		return
	}
	if log == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("log %d not found", id),
		})
		return
	}
	writeJSON(w, http.StatusOK, log)
}

// DeleteLogs handles DELETE /api/v1/logs.
func (h *LogHandlers) DeleteLogs(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "logging is not enabled",
		})
		return
	}

	count, err := h.Store.DeleteAll(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("deleting logs: %s", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted_count": count,
	})
}

// InteractionCapture is middleware that captures request/response data for logging.
func InteractionCapture(store *storage.SQLiteStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Only log protocol adapter and engine endpoints.
			path := r.URL.Path
			if !isLoggablePath(path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			cw := &captureWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(cw, r)

			// Async log to avoid blocking the response.
			go func() {
				entry := &storage.InteractionLog{
					Timestamp:      start.UTC().Format(time.RFC3339),
					RequestMethod:  r.Method,
					RequestPath:    path,
					ResponseStatus: cw.statusCode,
					LatencyMs:      time.Since(start).Milliseconds(),
					Streaming:      cw.Header().Get("Content-Type") == "text/event-stream",
				}

				// Extract agent/session from response headers or context.
				if reqID, ok := r.Context().Value(RequestIDKey).(string); ok {
					entry.SessionID = reqID
				}

				store.Log(r.Context(), entry)
			}()
		})
	}
}

func isLoggablePath(path string) bool {
	return path == "/v1/chat/completions" ||
		path == "/v1/messages" ||
		path == "/v1/engines/process"
}

// captureWriter wraps ResponseWriter to capture status code.
type captureWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *captureWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func init() {
	// Ensure json import is used (for writeJSON in handlers.go).
	_ = json.Marshal
}
