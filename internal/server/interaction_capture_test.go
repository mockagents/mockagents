package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/storage"
)

// TestCaptureWriterPool_ReusedCleanly exercises the sync.Pool
// optimization by firing 50 serial requests through the middleware
// and asserting that no request "bleeds" body bytes from a previous
// one. The middleware assigns each handler a distinct body; the
// stored rows must match 1:1 with no cross-contamination.
//
// This is the regression guard for the pool design — the most
// common sync.Pool mistake is forgetting to reset a buffer on
// acquire, which produces exactly the "last request's tail shows
// up in this request" bug pattern.
func TestCaptureWriterPool_ReusedCleanly(t *testing.T) {
	// Use a generously sized worker so this test's 50 rapid-fire
	// submits cannot overflow the queue. The default newTestWorker
	// (queue=16, 2 workers) is sized for the simple 1-request
	// tests; here we want every request to reach the store so we
	// can assert the pool doesn't bleed state between requests.
	store := newTestStore(t)
	worker := NewLogWorker(store, nil, LogWorkerConfig{Workers: 4, QueueSize: 256})
	t.Cleanup(func() { worker.Shutdown(2 * time.Second) })

	var counter atomic.Int32
	handler := InteractionCapture(worker, LogBodyFull)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := counter.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Valid JSON with a variable-length pad field, so any
		// slice-cap reuse bug would surface as truncation (invalid
		// JSON) or tail bleed (wrong model embedded in pad).
		body := `{"model":"m` + strconv.Itoa(int(id)) + `","pad":"` +
			strings.Repeat("x", int(id)) + `"}`
		_, _ = w.Write([]byte(body))
	}))

	const requests = 50
	for i := 0; i < requests; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if !waitForLog(t, store, requests, 2*time.Second) {
		t.Fatalf("expected %d logs, got %d", requests, mustCount(t, store))
	}

	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != requests {
		t.Fatalf("rows = %d, want %d", len(rows), requests)
	}

	// Each row should carry exactly the body that handler wrote for
	// its own request, identified by the embedded mN model field.
	seen := make(map[string]bool, requests)
	for _, row := range rows {
		// Model name extracted by the body-probe fallback; sanity-
		// check it matches a row whose body ends with that marker.
		model := row.AgentName
		if model == "" {
			t.Errorf("row %d has empty AgentName — probe failed", row.ID)
			continue
		}
		if !strings.Contains(row.ResponseBody, `"model":"`+model+`"`) {
			t.Errorf("row %d: body %q does not match model %q",
				row.ID, row.ResponseBody, model)
		}
		if seen[model] {
			t.Errorf("duplicate model %q — pool leaked state between requests", model)
		}
		seen[model] = true
	}
	if len(seen) != requests {
		t.Errorf("distinct models = %d, want %d", len(seen), requests)
	}
}

func mustCount(t *testing.T, store *storage.SQLiteStore) int {
	t.Helper()
	n, err := store.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	return int(n)
}

// newTestWorker builds a LogWorker backed by a fresh in-memory store,
// with the cleanup hook wired to drain on test exit. Returns both so
// tests can still query the underlying store directly.
func newTestWorker(t *testing.T) (*LogWorker, *storage.SQLiteStore) {
	t.Helper()
	store := newTestStore(t)
	worker := NewLogWorker(store, nil, LogWorkerConfig{Workers: 2, QueueSize: 16})
	t.Cleanup(func() { worker.Shutdown(time.Second) })
	return worker, store
}

// TestInteractionCapture_AgentNameFromContext verifies that the
// middleware prefers RequestMeta.AgentName (set by the adapter after
// ProcessRequest resolves an agent) over the legacy body-probe
// fallback.
func TestInteractionCapture_AgentNameFromContext(t *testing.T) {
	worker, store := newTestWorker(t)

	handler := InteractionCapture(worker, LogBodyFull)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
			meta.AgentName = "weather-bot"
			meta.Model = "gpt-4o"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Body intentionally has a *different* model than what meta
		// carries, so the assertion below proves the meta wins.
		_, _ = w.Write([]byte(`{"model":"some-other-model","choices":[]}`))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// The worker writes the log asynchronously; poll briefly.
	if !waitForLog(t, store, 1, time.Second) {
		t.Fatal("log never appeared")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].AgentName != "weather-bot" {
		t.Fatalf("expected AgentName=weather-bot from ctx meta, got %q", rows[0].AgentName)
	}
}

// TestInteractionCapture_BodyProbeFallback verifies that when the
// handler never fills in RequestMeta (e.g. validation failed before
// ProcessRequest ran) the middleware still captures the model name
// from the body, preserving the old behavior as a fallback.
func TestInteractionCapture_BodyProbeFallback(t *testing.T) {
	worker, store := newTestWorker(t)

	handler := InteractionCapture(worker, LogBodyFull)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately do NOT touch engine.RequestMetaFromContext.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"model":"gpt-4o-mini","error":"bad request"}`))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !waitForLog(t, store, 1, time.Second) {
		t.Fatal("log never appeared")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].AgentName != "gpt-4o-mini" {
		t.Fatalf("expected fallback AgentName=gpt-4o-mini, got %q", rows[0].AgentName)
	}
}

// TestInteractionCapture_FidelityFromMeta verifies the middleware records the
// full set of metadata the adapter stamps onto RequestMeta — protocol,
// resolved session id, scenario, tool-call count — plus the captured request
// body, so the log/cost dashboards have real context (REF-04).
func TestInteractionCapture_FidelityFromMeta(t *testing.T) {
	worker, store := newTestWorker(t)

	const reqBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	handler := InteractionCapture(worker, LogBodyFull)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body like the real adapter so the tee captures it.
		_, _ = io.ReadAll(r.Body)
		if meta := engine.RequestMetaFromContext(r.Context()); meta != nil {
			meta.Protocol = "openai-chat-completions"
			meta.SessionID = "sess-xyz"
			meta.AgentName = "weather-bot"
			meta.Model = "gpt-4o"
			meta.ScenarioName = "lookup"
			meta.ToolCallsCount = 2
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"gpt-4o","choices":[]}`))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !waitForLog(t, store, 1, time.Second) {
		t.Fatal("log never appeared")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Protocol != "openai-chat-completions" {
		t.Errorf("Protocol = %q, want openai-chat-completions", row.Protocol)
	}
	if row.SessionID != "sess-xyz" {
		t.Errorf("SessionID = %q, want sess-xyz (real session, not request id)", row.SessionID)
	}
	if row.ScenarioName != "lookup" {
		t.Errorf("ScenarioName = %q, want lookup", row.ScenarioName)
	}
	if row.ToolCallsCount != 2 {
		t.Errorf("ToolCallsCount = %d, want 2", row.ToolCallsCount)
	}
	if row.RequestBody != reqBody {
		t.Errorf("RequestBody = %q, want %q", row.RequestBody, reqBody)
	}
	if row.Error != "" {
		t.Errorf("Error = %q, want empty on success", row.Error)
	}
	if row.Truncated {
		t.Error("Truncated = true, want false for a small body")
	}
}

// TestInteractionCapture_RequestBodyTruncated proves an over-cap request body
// is clipped to maxCaptureBodyBytes and flags Truncated, while the handler
// still reads the full payload through the tee (REF-04).
func TestInteractionCapture_RequestBodyTruncated(t *testing.T) {
	worker, store := newTestWorker(t)

	big := strings.Repeat("a", maxCaptureBodyBytes+1024)
	var readN int
	handler := InteractionCapture(worker, LogBodyFull)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		readN = len(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"m"}`))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(big))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readN != len(big) {
		t.Fatalf("handler read %d bytes, want full %d — tee disturbed the read path", readN, len(big))
	}
	if !waitForLog(t, store, 1, time.Second) {
		t.Fatal("log never appeared")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	row := rows[0]
	if !row.Truncated {
		t.Error("Truncated = false, want true for an over-cap request body")
	}
	if len(row.RequestBody) != maxCaptureBodyBytes {
		t.Errorf("stored RequestBody len = %d, want clipped to %d", len(row.RequestBody), maxCaptureBodyBytes)
	}
}

// TestInteractionCapture_LogBodyNoneSkipsRequestBody verifies LogBodyNone
// drops both bodies and never flags truncation (REF-04 + SEC-05).
func TestInteractionCapture_LogBodyNoneSkipsRequestBody(t *testing.T) {
	worker, store := newTestWorker(t)

	handler := InteractionCapture(worker, LogBodyNone)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"m"}`))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"m","secret":"x"}`))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !waitForLog(t, store, 1, time.Second) {
		t.Fatal("log never appeared")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	row := rows[0]
	if row.RequestBody != "" {
		t.Errorf("RequestBody = %q, want empty under LogBodyNone", row.RequestBody)
	}
	if row.ResponseBody != "" {
		t.Errorf("ResponseBody = %q, want empty under LogBodyNone", row.ResponseBody)
	}
	if row.Truncated {
		t.Error("Truncated = true, want false under LogBodyNone (nothing stored)")
	}
}

// TestInteractionCapture_SSENotBuffered proves that an SSE response is
// flagged as streaming and its chunks are NOT buffered into the captured
// body (F-LH-001 / X-SSE-001), even when the Content-Type carries a charset
// parameter (F-LH-004). The chunks must still reach the client unchanged.
func TestInteractionCapture_SSENotBuffered(t *testing.T) {
	worker, store := newTestWorker(t)

	const chunk = "data: {\"delta\":\"hello\"}\n\n"
	handler := InteractionCapture(worker, LogBodyFull)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// charset param exercises the tolerant media-type match.
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 3; i++ {
			_, _ = w.Write([]byte(chunk))
		}
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// The stream must pass through to the client untouched.
	if got := rec.Body.String(); got != strings.Repeat(chunk, 3) {
		t.Fatalf("client did not receive the full stream: %q", got)
	}

	if !waitForLog(t, store, 1, time.Second) {
		t.Fatal("log never appeared")
	}
	rows, err := store.Query(t.Context(), storage.InteractionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if !rows[0].Streaming {
		t.Error("SSE response was not flagged Streaming")
	}
	if rows[0].ResponseBody != "" {
		t.Errorf("SSE body was buffered; want empty, got %q", rows[0].ResponseBody)
	}
}

// TestIsEventStream pins the tolerant Content-Type matcher (F-LH-004).
func TestIsEventStream(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"text/event-stream", true},
		{"text/event-stream; charset=utf-8", true},
		{"text/event-stream;charset=utf-8", true},
		{"Text/Event-Stream", true}, // media types are case-insensitive
		{"application/json", false},
		{"application/json; charset=utf-8", false},
		{"", false},
		{"text/event-stream extra garbage", true}, // prefix fallback path
	}
	for _, c := range cases {
		if got := isEventStream(c.ct); got != c.want {
			t.Errorf("isEventStream(%q) = %v, want %v", c.ct, got, c.want)
		}
	}
}

func newTestStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "capture_test.db")
	s, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func waitForLog(t *testing.T, store *storage.SQLiteStore, want int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := store.Count(t.Context())
		if err == nil && int(n) >= want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
