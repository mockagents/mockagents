package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCostsHandler_TimestampValidation covers F-CO-004: a malformed
// since/until is a 400, a valid RFC3339 still succeeds.
func TestCostsHandler_TimestampValidation(t *testing.T) {
	h := &CostsHandlers{Store: newTestStore(t)}
	for _, q := range []string{"since=not-a-time", "until=2024-13-99", "since=1719000000"} {
		rec := httptest.NewRecorder()
		h.ListCosts(rec, httptest.NewRequest("GET", "/api/v1/costs?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", q, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ListCosts(rec, httptest.NewRequest("GET", "/api/v1/costs?since=2024-01-01T00:00:00Z&until=2024-02-01T00:00:00Z", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("valid RFC3339 window: status = %d, want 200", rec.Code)
	}
}

// TestLogHandlers_TimestampValidation covers F-LH-011: same validation on the
// /api/v1/logs list endpoint.
func TestLogHandlers_TimestampValidation(t *testing.T) {
	h := &LogHandlers{Store: newLogHandlerTestStore(t)}
	for _, q := range []string{"since=garbage", "until=yesterday"} {
		rec := httptest.NewRecorder()
		h.ListLogs(rec, httptest.NewRequest("GET", "/api/v1/logs?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", q, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ListLogs(rec, httptest.NewRequest("GET", "/api/v1/logs?since=2026-05-30T12:00:00Z", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("valid RFC3339 since: status = %d, want 200", rec.Code)
	}
}
