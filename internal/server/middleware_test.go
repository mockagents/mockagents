package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRequestID_GeneratesID(t *testing.T) {
	handler := RequestContext(dummyHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-Id")
	assert.NotEmpty(t, id)
	assert.True(t, strings.HasPrefix(id, "req-"))
}

func TestRequestID_PreservesExisting(t *testing.T) {
	handler := RequestContext(dummyHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-Id", "custom-id-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, "custom-id-123", rec.Header().Get("X-Request-Id"))
}

func TestCORS_SetsHeaders(t *testing.T) {
	handler := CORS(nil)(dummyHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
}

func TestCORS_OptionsPreflightReturns204(t *testing.T) {
	handler := CORS(nil)(dummyHandler())
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// TestCORS_ConfigurableOrigins covers F-MW-001: with an explicit allowlist
// only listed origins are echoed; a "*" entry (or nil) restores the wildcard.
func TestCORS_ConfigurableOrigins(t *testing.T) {
	serve := func(origins []string, reqOrigin string) *httptest.ResponseRecorder {
		h := CORS(origins)(dummyHandler())
		req := httptest.NewRequest("GET", "/test", nil)
		if reqOrigin != "" {
			req.Header.Set("Origin", reqOrigin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Allowlisted origin is echoed (not "*") and carries Vary: Origin.
	rec := serve([]string{"https://app.example.com"}, "https://app.example.com")
	assert.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Vary"), "Origin")

	// A non-listed origin gets NO Allow-Origin header (browser blocks it).
	rec = serve([]string{"https://app.example.com"}, "https://evil.example.com")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))

	// "*" in the list keeps the wildcard regardless of Origin.
	rec = serve([]string{"*"}, "https://anything.example.com")
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestRecovery_CatchesPanic(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	handler := Recovery(testLogger())(panicHandler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")
}

func TestMaxBodySize_LimitsBody(t *testing.T) {
	handler := MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		_, err := r.Body.Read(buf)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	largeBody := strings.NewReader(strings.Repeat("x", 100))
	req := httptest.NewRequest("POST", "/test", largeBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestExtractAPIKey_BearerToken(t *testing.T) {
	var extractedKey string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extractedKey = apiKeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestContext(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer sk-test-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, "sk-test-123", extractedKey)
}

func TestExtractAPIKey_NoHeader(t *testing.T) {
	var extractedKey string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extractedKey = apiKeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestContext(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Empty(t, extractedKey)
}

// BenchmarkRequestContext documents the PERF-06 win: the merged middleware
// stamps the request id and extracts the bearer key in a single pass, storing
// both under one context entry. Compared with the former
// RequestID(ExtractAPIKey(...)) chain (10 allocs/op, 841 B/op on this same
// auth'd request) it does one shallow Request copy and one context node instead
// of two. generateRequestID's crypto/rand+Sprintf allocs are common to both and
// are PERF-07's separate target.
func BenchmarkRequestContext(b *testing.B) {
	noop := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := RequestContext(noop)
	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-test-123")
	rec := httptest.NewRecorder()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rec, req)
	}
}

// BenchmarkGenerateRequestID tracks the PERF-07 id-generation cost.
func BenchmarkGenerateRequestID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = generateRequestID()
	}
}

func TestStructuredLogger_LogsRequest(t *testing.T) {
	handler := StructuredLogger(testLogger())(dummyHandler())
	req := httptest.NewRequest("GET", "/test-path", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStatusWriter_CapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	sw.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, sw.status)
}
