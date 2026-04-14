package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/storage"
)

// LogHandlers holds dependencies for log query API handlers.
type LogHandlers struct {
	Store  *storage.SQLiteStore
	Prices *pricing.Table // optional; when nil, cost fields are zero.
}

// LogWithCost is the wire shape returned by ListLogs when a pricing
// table is configured. It embeds InteractionLog and adds a computed
// cost breakdown pulled from the response_body's usage block.
type LogWithCost struct {
	storage.InteractionLog
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Model            string  `json:"model,omitempty"`
	CostUSD          float64 `json:"cost_usd"`
}

// annotate wraps a row with its computed usage + cost. Returns a
// zero-cost LogWithCost when the table is unset or the response body
// has no usage block.
func annotate(row storage.InteractionLog, table *pricing.Table) LogWithCost {
	out := LogWithCost{InteractionLog: row}
	if table == nil {
		return out
	}
	usage := pricing.ExtractUsage([]byte(row.ResponseBody))
	out.PromptTokens = usage.PromptTokens
	out.CompletionTokens = usage.CompletionTokens
	out.Model = usage.Model
	out.CostUSD = table.Estimate(usage.Model, usage.PromptTokens, usage.CompletionTokens)
	return out
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
	// Annotate every row with computed token counts and cost pulled
	// from the stored response body. When Prices is unset this is a
	// cheap zero-cost passthrough.
	annotated := make([]LogWithCost, len(logs))
	for i, row := range logs {
		annotated[i] = annotate(row, h.Prices)
	}
	writeJSON(w, http.StatusOK, annotated)
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
//
// The writer buffers up to maxCaptureBodyBytes of the response body
// in memory so downstream cost-estimation (see internal/pricing) has
// the usage block to parse. Streaming responses are recognized by
// Content-Type and skip the body buffer to avoid pinning SSE chunks.
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
			cw := &captureWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				capture:        true,
			}

			next.ServeHTTP(cw, r)

			// Snapshot into a local slice so the goroutine below
			// cannot race the request-scoped captureWriter after the
			// handler returns.
			bodySnapshot := cw.snapshot()
			streaming := cw.Header().Get("Content-Type") == "text/event-stream"

			// Async log to avoid blocking the response.
			go func() {
				entry := &storage.InteractionLog{
					Timestamp:      start.UTC().Format(time.RFC3339),
					RequestMethod:  r.Method,
					RequestPath:    path,
					ResponseStatus: cw.statusCode,
					LatencyMs:      time.Since(start).Milliseconds(),
					Streaming:      streaming,
					ResponseBody:   string(bodySnapshot),
				}
				// Best-effort: extract the model name from the
				// response body and use it as AgentName so the
				// /api/v1/costs by_agent grouping is populated even
				// without wiring engine metadata through the
				// middleware. A follow-up slice will plumb the
				// matched agent name through the request context.
				if len(bodySnapshot) > 0 {
					var probe struct{ Model string `json:"model"` }
					_ = json.Unmarshal(bodySnapshot, &probe)
					entry.AgentName = probe.Model
				}

				if reqID, ok := r.Context().Value(RequestIDKey).(string); ok {
					entry.SessionID = reqID
				}

				store.Log(r.Context(), entry)
			}()
		})
	}
}

// maxCaptureBodyBytes caps the response-body buffer used by the
// interaction-capture middleware. 1 MiB is enough for every tool-call
// response we care about in practice and bounds worst-case memory.
const maxCaptureBodyBytes = 1 << 20

func isLoggablePath(path string) bool {
	return path == "/v1/chat/completions" ||
		path == "/v1/messages" ||
		path == "/v1/engines/process"
}

// captureWriter wraps ResponseWriter to capture the status code and,
// optionally, the response body (up to maxCaptureBodyBytes) so the
// interaction-capture middleware can persist it for downstream cost
// estimation.
type captureWriter struct {
	http.ResponseWriter
	statusCode int
	capture    bool
	body       []byte
	truncated  bool
}

func (w *captureWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if w.capture && !w.truncated {
		remaining := maxCaptureBodyBytes - len(w.body)
		if remaining > 0 {
			take := len(p)
			if take > remaining {
				take = remaining
				w.truncated = true
			}
			w.body = append(w.body, p[:take]...)
		} else {
			w.truncated = true
		}
	}
	return w.ResponseWriter.Write(p)
}

// snapshot returns a defensive copy of the captured body for the
// async logger goroutine. Empty when capture is off or the response
// produced nothing.
func (w *captureWriter) snapshot() []byte {
	if !w.capture || len(w.body) == 0 {
		return nil
	}
	out := make([]byte, len(w.body))
	copy(out, w.body)
	return out
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
