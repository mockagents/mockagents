package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProbeModel covers the streaming model probe (F-LH-008): only a
// top-level "model" string is returned; nested or value-position "model"
// strings and non-objects yield "".
func TestProbeModel(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"model":"gpt-4o","choices":[]}`, "gpt-4o"},
		{`{"id":"x","model":"claude","usage":{"model":"nested"}}`, "claude"},
		{`{"id":"model","model":"real"}`, "real"}, // "model" as a value must not fool it
		{`{"choices":[{"model":"nested-only"}]}`, ""},
		{`{"usage":{"prompt_tokens":1}}`, ""},
		{`{"model":123}`, ""},
		{`{}`, ""},
		{`[1,2,3]`, ""},
		{`not json`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := probeModel([]byte(c.in)); got != c.want {
			t.Errorf("probeModel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestStatusWriter_DoubleWriteHeader covers F-MW-005: the first WriteHeader
// wins and a second is ignored (no clobbered status, no stdlib warning).
func TestStatusWriter_DoubleWriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	sw.WriteHeader(http.StatusTeapot)
	sw.WriteHeader(http.StatusInternalServerError)
	if sw.status != http.StatusTeapot {
		t.Errorf("captured status = %d, want %d (first call wins)", sw.status, http.StatusTeapot)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("underlying status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}

// TestValidateHandler_EmptyYAMLWrapper covers F-VL-003: a JSON wrapper with an
// explicit empty "yaml" is a 400, while a JSON doc with no "yaml" key falls
// through to validation (not a 400 from the wrapper check).
func TestValidateHandler_EmptyYAMLWrapper(t *testing.T) {
	h := NewValidateHandler()

	t.Run("explicit empty yaml -> 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(`{"yaml":""}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no yaml key -> validated as raw doc (200)", func(t *testing.T) {
		// A JSON object with no "yaml" key is treated as a raw document and
		// handed to ValidateBytes, which returns 200 with a report (errors
		// array), not a 400 from the wrapper guard.
		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(`{"apiVersion":"mockagents/v1"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})
}
