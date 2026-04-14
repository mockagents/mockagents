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
	handler := RequestID(dummyHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-Id")
	assert.NotEmpty(t, id)
	assert.True(t, strings.HasPrefix(id, "req-"))
}

func TestRequestID_PreservesExisting(t *testing.T) {
	handler := RequestID(dummyHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-Id", "custom-id-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, "custom-id-123", rec.Header().Get("X-Request-Id"))
}

func TestCORS_SetsHeaders(t *testing.T) {
	handler := CORS(dummyHandler())
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
}

func TestCORS_OptionsPreflightReturns204(t *testing.T) {
	handler := CORS(dummyHandler())
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
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
		key, _ := r.Context().Value(APIKeyKey).(string)
		extractedKey = key
		w.WriteHeader(http.StatusOK)
	})
	handler := ExtractAPIKey(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer sk-test-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, "sk-test-123", extractedKey)
}

func TestExtractAPIKey_NoHeader(t *testing.T) {
	var extractedKey string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, _ := r.Context().Value(APIKeyKey).(string)
		extractedKey = key
		w.WriteHeader(http.StatusOK)
	})
	handler := ExtractAPIKey(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Empty(t, extractedKey)
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
