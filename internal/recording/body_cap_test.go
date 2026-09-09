package recording

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplay_OversizedBodyIs413 is the audit M-22 guard: a request body
// beyond MaxRequestBodyBytes is refused instead of read into memory.
func TestReplay_OversizedBodyIs413(t *testing.T) {
	cass := New(filepath.Join(t.TempDir(), "c.jsonl"))
	rp := NewReplay(cass)
	big := strings.Repeat("x", MaxRequestBodyBytes+1)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(big)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestProxy_OversizedBodyIs413(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for an oversized body")
	}))
	defer upstream.Close()
	cass := New(filepath.Join(t.TempDir(), "c.jsonl"))
	p, err := NewProxy(upstream.URL, cass)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", MaxRequestBodyBytes+1))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}
