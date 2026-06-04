package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/storage"
)

// TestSkipAuth_ExactMatchOnly is the SEC-03 guard: the auth-exemption predicate
// must match the open routes EXACTLY, so a future route mounted under one of the
// prefixes (e.g. /v1/models-internal) does not silently inherit anonymous access.
func TestSkipAuth_ExactMatchOnly(t *testing.T) {
	exempt := []string{
		"/api/v1/health",
		"/v1/chat/completions",
		"/v1/messages",
		"/v1/models",
		"/v1/engines/process",
	}
	for _, p := range exempt {
		if !skipAuth(httptest.NewRequest(http.MethodGet, p, nil)) {
			t.Errorf("skipAuth(%q) = false, want true (open route)", p)
		}
	}

	notExempt := []string{
		"/v1/models-internal",
		"/v1/messages/extra",
		"/v1/chat/completions/x",
		"/v1/engines/secret",
		"/v1/engines/",
		"/api/v1/healthz",
		"/api/v1/agents",
		"/api/v1/tenants",
	}
	for _, p := range notExempt {
		if skipAuth(httptest.NewRequest(http.MethodGet, p, nil)) {
			t.Errorf("skipAuth(%q) = true, want false (a prefix must not auto-exempt)", p)
		}
	}
}

// TestParseBoundedInt_OffsetClamped is the SEC-04 guard: an absurd offset is
// clamped to maxListOffset instead of passing through to force a deep scan.
func TestParseBoundedInt_OffsetClamped(t *testing.T) {
	rec := httptest.NewRecorder()
	got, ok := parseBoundedInt(rec, "999999999", "offset", 0, maxListOffset)
	if !ok {
		t.Fatalf("parseBoundedInt ok = false, want true (clamp, not reject)")
	}
	if got != maxListOffset {
		t.Errorf("clamped offset = %d, want %d", got, maxListOffset)
	}
	// A negative offset is still rejected with a 400.
	rec2 := httptest.NewRecorder()
	if _, ok := parseBoundedInt(rec2, "-1", "offset", 0, maxListOffset); ok {
		t.Error("parseBoundedInt(-1) ok = true, want false")
	}
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("negative offset status = %d, want 400", rec2.Code)
	}
}

// TestListLogs_HidesStoreErrors is the SEC-02 guard: a store/driver error must
// surface to the client as the generic {"error":"internal error"} envelope, not
// the raw SQLite error string (F-TN-006 consistency).
func TestListLogs_HidesStoreErrors(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	// Close the store so the subsequent Query fails with a driver error.
	store.Close()

	h := &LogHandlers{Store: store}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "internal error") {
		t.Errorf("body = %q, want the generic envelope", body)
	}
	// The raw driver/SQL detail must NOT leak to the client.
	for _, leak := range []string{"sql:", "sqlite", "interaction_logs", "database is closed"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("response body leaks internal detail %q: %s", leak, body)
		}
	}
}
