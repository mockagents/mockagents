package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/pricing"
	"github.com/mockagents/mockagents/internal/storage"
)

// LogHandlers holds dependencies for log query API handlers.
type LogHandlers struct {
	Store  *storage.SQLiteStore
	Prices *pricing.Table // optional; when nil, cost fields are zero.
	// Broadcaster is set when live-stream subscriptions are enabled.
	// Nil disables StreamLogs; the server only mounts the route when
	// this field is non-nil.
	Broadcaster *LogBroadcaster
	// StreamHeartbeat overrides the SSE keepalive cadence. Zero uses
	// the default 15s. Tests may set a short duration.
	StreamHeartbeat time.Duration
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

	since, ok := parseTimestampParam(w, r.URL.Query().Get("since"), "since")
	if !ok {
		return
	}
	until, ok := parseTimestampParam(w, r.URL.Query().Get("until"), "until")
	if !ok {
		return
	}
	filter := storage.InteractionFilter{
		AgentName: r.URL.Query().Get("agent"),
		SessionID: r.URL.Query().Get("session_id"),
		Since:     since,
		Until:     until,
	}
	if tenantID := callerTenantID(r); tenantID != "" {
		filter.TenantID = tenantID
		filter.FilterTenantID = true
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, ok := parseBoundedInt(w, limitStr, "limit", 1, maxListLimit)
		if !ok {
			return
		}
		filter.Limit = limit
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, ok := parseBoundedInt(w, offsetStr, "offset", 0, 0)
		if !ok {
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
	if tenantID := callerTenantID(r); tenantID != "" && log.TenantID != tenantID {
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

	var (
		count int64
		err   error
	)
	if tenantID := callerTenantID(r); tenantID != "" {
		count, err = h.Store.DeleteForTenant(r.Context(), tenantID)
	} else {
		count, err = h.Store.DeleteAll(r.Context())
	}
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

// captureWriterPool recycles captureWriter instances across requests.
// Each request acquires a writer, uses it for the duration of the
// handler chain, snapshots the body (which produces a defensive copy
// for the async worker), and releases the writer back to the pool
// before returning. The body slice's backing array is reused across
// acquisitions via `body[:0]`, which is the primary win on the hot
// path: a 1 KiB response no longer allocates a fresh slice per
// request under sustained load.
//
// Safe because:
//  1. The captured body is only ever read via snapshot(), which
//     copies bytes into a new slice the worker owns.
//  2. Release clears ResponseWriter to nil before returning to the
//     pool, so a stale pointer can never escape.
//  3. append(body[:0], ...) preserves the backing array across Put
//     cycles without retaining references to the old request.
var captureWriterPool = sync.Pool{
	New: func() any { return &captureWriter{} },
}

// acquireCaptureWriter gets a captureWriter from the pool and binds
// it to the given ResponseWriter. The returned writer has its body
// reset to an empty slice that shares any previously allocated
// backing array.
func acquireCaptureWriter(w http.ResponseWriter) *captureWriter {
	cw := captureWriterPool.Get().(*captureWriter)
	cw.ResponseWriter = w
	cw.statusCode = http.StatusOK
	cw.capture = true
	cw.truncated = false
	cw.streaming = false
	cw.sniffed = false
	cw.body = cw.body[:0]
	return cw
}

// releaseCaptureWriter returns a captureWriter to the pool. Clearing
// the embedded ResponseWriter is essential — leaving a stale
// reference would pin the outer http.ResponseWriter (and whatever
// connection state it wraps) in memory for the lifetime of the pool
// entry.
func releaseCaptureWriter(cw *captureWriter) {
	cw.ResponseWriter = nil
	// Drop the backing array if it grew pathologically large so the
	// pool does not turn into a per-process memory high-water mark.
	// The normal case (small responses) keeps the slice for reuse.
	if cap(cw.body) > maxCaptureBodyBytes/4 {
		cw.body = nil
	}
	captureWriterPool.Put(cw)
}

// InteractionCapture is middleware that captures request/response data for logging.
//
// The writer buffers up to maxCaptureBodyBytes of the response body
// in memory so downstream cost-estimation (see internal/pricing) has
// the usage block to parse. Streaming responses are recognized by
// Content-Type and skip the body buffer to avoid pinning SSE chunks.
//
// Persistence is delegated to a LogWorker: the middleware builds the
// entry on the request goroutine and submits it to a bounded queue
// drained by a fixed worker pool. The old "spawn one goroutine per
// request" pattern caused unbounded fan-out under load (~54 % cum GC
// at 1.7 M ops/sec in the baseline profile). Submit is non-blocking;
// overflow increments the worker's Dropped counter.
//
// The captureWriter itself is pooled via captureWriterPool so each
// request reuses both the struct and the body slice's backing array.
func InteractionCapture(worker *LogWorker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if worker == nil || worker.store == nil {
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
			cw := acquireCaptureWriter(w)
			defer releaseCaptureWriter(cw)

			// Attach a mutable RequestMeta to the context so the
			// adapter handlers can stamp the matched agent name and
			// model after engine resolution. Reading it back after
			// ServeHTTP returns gives an authoritative answer without
			// having to reparse the response body.
			r, meta := engine.WithRequestMeta(r)

			next.ServeHTTP(cw, r)

			// One independent copy of the body for the async worker; the
			// captureWriter is released and reused right after this block, so
			// the worker must not alias its pooled buffer.
			respBody := cw.bodyString()
			// cw.streaming was set by the writer the moment it sniffed an
			// SSE Content-Type; for streams capture was disabled so respBody
			// is empty by design (F-LH-001 / X-SSE-001).
			streaming := cw.streaming
			agentName := meta.AgentName
			// If the engine never ran (validation error, chaos 429 before
			// resolve, etc.) fall back to a body probe so by_agent grouping
			// still captures something useful. Probe cw.body directly — it is
			// still live (release is deferred) — to avoid re-materializing it.
			if agentName == "" && respBody != "" {
				agentName = probeModel(cw.body)
			}

			entry := &storage.InteractionLog{
				Timestamp:      start.UTC().Format(time.RFC3339),
				TenantID:       engine.TenantIDFromContext(r.Context()),
				RequestMethod:  r.Method,
				RequestPath:    path,
				ResponseStatus: cw.statusCode,
				LatencyMs:      time.Since(start).Milliseconds(),
				Streaming:      streaming,
				ResponseBody:   respBody,
				AgentName:      agentName,
			}
			// Protocol-adapter interactions have no application-level session,
			// so SessionID carries the per-request id here; the /api/v1/logs
			// `session_id` filter therefore doubles as a request-id filter for
			// these rows (F-LH-007).
			if reqID, ok := r.Context().Value(RequestIDKey).(string); ok {
				entry.SessionID = reqID
			}
			// Submit is non-blocking and drops on a full queue. The drop is
			// already metered (worker.Metrics().Dropped); surface it at debug
			// so an operator tailing logs can correlate a gap (F-LH-005).
			if !worker.Submit(entry) {
				worker.logger.Debug("interaction log dropped (queue full)",
					"path", path, "status", entry.ResponseStatus)
			}
		})
	}
}

// probeModel scans a response body for a top-level "model" string field and
// returns as soon as it finds one, so the engine-didn't-resolve fallback
// doesn't fully re-parse a body that can be up to maxCaptureBodyBytes just to
// read one field (F-LH-008). Returns "" when the body isn't a JSON object or
// has no top-level model. Nested "model" keys (inside choices/usage/etc.) are
// ignored — only the top-level field is the request's model.
func probeModel(body []byte) string {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return "" // not a JSON object
	}
	// Read top-level key/value pairs. dec.Token() yields the key (a string)
	// then the value; object/array values are skipped wholesale so nested
	// "model" keys (e.g. inside choices/usage) are ignored.
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return ""
		}
		key, _ := keyTok.(string)
		valTok, err := dec.Token()
		if err != nil {
			return ""
		}
		if d, ok := valTok.(json.Delim); ok && (d == '{' || d == '[') {
			if err := skipJSONValue(dec); err != nil {
				return ""
			}
			continue
		}
		if key == "model" {
			if s, ok := valTok.(string); ok {
				return s
			}
			return ""
		}
	}
	return ""
}

// skipJSONValue consumes tokens until the composite value whose opening
// delimiter was just read is fully closed.
func skipJSONValue(dec *json.Decoder) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
	}
	return nil
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
	// streaming is set once the final Content-Type is sniffed and turns
	// out to be an SSE stream; capture is disabled in that case so the
	// stream's chunks are never buffered/pinned (F-LH-001). sniffed guards
	// the one-shot detection so it runs at most once per request.
	streaming bool
	sniffed   bool
}

func (w *captureWriter) WriteHeader(code int) {
	w.statusCode = code
	// The Content-Type is final by the time WriteHeader runs, so this is
	// the right moment to decide whether to buffer the body.
	w.detectStreaming()
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(p []byte) (int, error) {
	// A handler that writes without an explicit WriteHeader implies 200;
	// sniff here too so SSE started that way is still recognized before
	// the first chunk is buffered.
	w.detectStreaming()
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
// bodyString returns an independent, immutable copy of the captured body for
// the async log worker. `string(w.body)` copies the pooled bytes exactly once
// into a string the worker owns, so the captureWriter can be released and
// reused immediately afterward. This replaces the old snapshot()+string()
// double copy (PERF-09): one response-size allocation per loggable request
// instead of two. Empty when capture is off (e.g. SSE) or nothing was written.
func (w *captureWriter) bodyString() string {
	if !w.capture || len(w.body) == 0 {
		return ""
	}
	return string(w.body)
}

// detectStreaming inspects the final Content-Type exactly once. When the
// response is an SSE stream it records that fact and disables body capture
// so the stream's chunks are neither buffered nor pinned in memory
// (F-LH-001 / X-SSE-001). Matching tolerates a charset (or any) parameter
// via mime.ParseMediaType (F-LH-004).
func (w *captureWriter) detectStreaming() {
	if w.sniffed {
		return
	}
	w.sniffed = true
	if isEventStream(w.Header().Get("Content-Type")) {
		w.streaming = true
		w.capture = false
	}
}

// isEventStream reports whether a Content-Type header value denotes an SSE
// stream, ignoring any media-type parameters (e.g.
// "text/event-stream; charset=utf-8"). Falls back to a prefix check when
// the header is malformed enough that mime.ParseMediaType rejects it.
func isEventStream(contentType string) bool {
	if contentType == "" {
		return false
	}
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		return mediaType == "text/event-stream"
	}
	return strings.HasPrefix(contentType, "text/event-stream")
}

func (w *captureWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// StreamLogs implements GET /api/v1/logs/stream. It opens a long-lived
// Server-Sent Events response, subscribes to the LogBroadcaster, and
// writes each newly-persisted interaction log as one SSE frame:
//
//	event: log
//	data: {"id":42,"agent_name":"echo",...}
//
// A `:heartbeat\n\n` comment is emitted every 15s (configurable via
// StreamHeartbeat) so idle proxies don't reap the connection. When
// the client disconnects or the request context is cancelled the
// handler tears down the subscription and returns cleanly.
//
// Each frame is annotated with pricing data when a Prices table is
// configured, matching ListLogs' wire shape so the GUI can share its
// row-rendering code with the static log list.
// StreamMetrics implements GET /api/v1/logs/stream/metrics. Returns
// a JSON snapshot of every active /api/v1/logs/stream subscriber so
// operators can answer "is anyone currently falling behind?" — the
// cumulative per-tab drop count from §2.44 is per-subscription;
// this endpoint aggregates across every connection the server
// currently holds.
//
// Returns 503 when the broadcaster is nil (logging disabled) so
// clients get a clear error rather than an empty snapshot that
// could be misread as "no drops anywhere".
func (h *LogHandlers) StreamMetrics(w http.ResponseWriter, r *http.Request) {
	if h.Broadcaster == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "live feed disabled",
		})
		return
	}
	snap := h.Broadcaster.Snapshot()
	writeJSON(w, http.StatusOK, snap)
}

func (h *LogHandlers) StreamLogs(w http.ResponseWriter, r *http.Request) {
	if h.Broadcaster == nil {
		http.Error(w, "live feed disabled", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	heartbeat := h.StreamHeartbeat
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}

	// SSE feeds are long-lived, so the server's global WriteTimeout would
	// otherwise sever them after WriteTimeout elapses (F-SV-004). Reset the
	// per-connection write deadline before every write to a window larger
	// than the heartbeat interval: a healthy stream keeps writing (at least a
	// heartbeat every `heartbeat`) and so stays open indefinitely, while a
	// genuinely stuck write (client stopped reading) still fails after the
	// window. SetWriteDeadline reaches the net.Conn through the middleware
	// wrappers' Unwrap methods; if the chain doesn't support it the call is a
	// harmless no-op and behavior reverts to the global timeout.
	rc := http.NewResponseController(w)
	writeWindow := heartbeat + 30*time.Second
	bumpDeadline := func() { _ = rc.SetWriteDeadline(time.Now().Add(writeWindow)) }
	bumpDeadline()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Subscribe scoped to the caller's tenant so another tenant's volume can't
	// fill this subscriber's buffer and starve it of its own rows (F-LH-003).
	// Empty in single-tenant mode = receive everything. The per-row check in
	// the loop below stays as defense-in-depth.
	sub, cancel := h.Broadcaster.SubscribeTenant(0, callerTenantID(r))
	defer cancel()

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	// lastDropped tracks the drop count we most recently surfaced
	// to the client. A slow subscriber that can't keep up will
	// see its dropped counter advance; the handler emits an
	// `event: dropped` frame at the top of every loop iteration
	// whenever the counter has moved, so the client learns about
	// backpressure regardless of whether it's a data tick, a
	// heartbeat, or just a context cancellation.
	var lastDropped uint64

	ctx := r.Context()
	for {
		// Refresh the write deadline each iteration so the next write (data,
		// heartbeat, or dropped frame) gets a full window (F-SV-004).
		bumpDeadline()
		if current := sub.Dropped(); current > lastDropped {
			newly := current - lastDropped
			payload := fmt.Sprintf(`{"count":%d,"new":%d}`, current, newly)
			if _, err := fmt.Fprintf(w, "event: dropped\ndata: %s\n\n", payload); err != nil {
				return
			}
			lastDropped = current
			flusher.Flush()
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ":heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case entry, ok := <-sub.C():
			if !ok {
				return
			}
			row := annotate(*entry, h.Prices)
			if tenantID := callerTenantID(r); tenantID != "" && row.TenantID != tenantID {
				continue
			}
			buf, err := json.Marshal(row)
			if err != nil {
				// Malformed rows are skipped rather than dropping
				// the whole stream. Extremely unlikely — the
				// InteractionLog shape is always JSON-safe.
				continue
			}
			if _, err := fmt.Fprintf(w, "event: log\ndata: %s\n\n", buf); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
