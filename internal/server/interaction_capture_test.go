package server

import (
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
	handler := InteractionCapture(worker)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := InteractionCapture(worker)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := InteractionCapture(worker)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// TestInteractionCapture_SSENotBuffered proves that an SSE response is
// flagged as streaming and its chunks are NOT buffered into the captured
// body (F-LH-001 / X-SSE-001), even when the Content-Type carries a charset
// parameter (F-LH-004). The chunks must still reach the client unchanged.
func TestInteractionCapture_SSENotBuffered(t *testing.T) {
	worker, store := newTestWorker(t)

	const chunk = "data: {\"delta\":\"hello\"}\n\n"
	handler := InteractionCapture(worker)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
