package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type encPayload struct {
	A int      `json:"a"`
	B string   `json:"b"`
	C []string `json:"c"`
}

// TestWriteJSON_MatchesEncoderOutput is the PERF-04 correctness guard: the
// pooled writer must produce byte-for-byte the same response as the previous
// json.NewEncoder(w).Encode(v), including the trailing newline, status, and
// Content-Type.
func TestWriteJSON_MatchesEncoderOutput(t *testing.T) {
	v := encPayload{A: 7, B: "héllo", C: []string{"x", "y"}}

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, v)

	var want bytes.Buffer
	_ = json.NewEncoder(&want).Encode(v) // the old behavior, incl. trailing '\n'

	if got := rec.Body.String(); got != want.String() {
		t.Errorf("body = %q, want %q", got, want.String())
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// discardRW is a no-op ResponseWriter so the benchmark measures writeJSON's own
// cost, not a growing recorder buffer.
type discardRW struct{ h http.Header }

func (d *discardRW) Header() http.Header {
	if d.h == nil {
		d.h = http.Header{}
	}
	return d.h
}
func (d *discardRW) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardRW) WriteHeader(int)             {}

// BenchmarkWriteJSON exercises the pooled response encoder (PERF-04). After
// warmup the buffer+encoder are reused, so B/op reflects only the JSON content,
// not a fresh per-response encoder.
func BenchmarkWriteJSON(b *testing.B) {
	v := encPayload{A: 7, B: "hello world", C: []string{"alpha", "bravo", "charlie"}}
	w := &discardRW{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writeJSON(w, http.StatusOK, v)
	}
}
