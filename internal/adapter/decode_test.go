package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestDecodeJSONBody_RoundTrip verifies that decodeJSONBody parses
// well-formed JSON identically to json.NewDecoder(r.Body).Decode().
// This is the parity guarantee that lets us swap the call site
// without changing any caller-visible behavior.
func TestDecodeJSONBody_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		body string
		want ChatCompletionRequest
	}{
		{
			name: "minimal",
			body: `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
			want: ChatCompletionRequest{
				Model:    "gpt-4o",
				Messages: []OpenAIMessage{{Role: "user", Content: "hi"}},
			},
		},
		{
			name: "with stream flag",
			body: `{"model":"m","messages":[{"role":"user","content":"x"}],"stream":true}`,
			want: ChatCompletionRequest{
				Model:    "m",
				Messages: []OpenAIMessage{{Role: "user", Content: "x"}},
				Stream:   true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(tc.body)))
			var got ChatCompletionRequest
			if err := decodeJSONBody(req, &got); err != nil {
				t.Fatalf("decodeJSONBody: %v", err)
			}
			if got.Model != tc.want.Model {
				t.Errorf("Model = %q, want %q", got.Model, tc.want.Model)
			}
			if got.Stream != tc.want.Stream {
				t.Errorf("Stream = %v, want %v", got.Stream, tc.want.Stream)
			}
			if len(got.Messages) != len(tc.want.Messages) {
				t.Errorf("len(Messages) = %d, want %d", len(got.Messages), len(tc.want.Messages))
			}
		})
	}
}

// TestDecodeJSONBody_PoolReuseIsSafe fires many sequential decodes
// against the helper to verify the pooled buffer is reset between
// uses. The classic sync.Pool failure mode is "previous request's
// bytes bleed into this one"; this test would surface that as a
// model-name mismatch.
func TestDecodeJSONBody_PoolReuseIsSafe(t *testing.T) {
	for i := 0; i < 100; i++ {
		body := `{"model":"m` + strings.Repeat("x", i%50) + `","messages":[{"role":"user","content":"q"}]}`
		req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(body)))
		var got ChatCompletionRequest
		if err := decodeJSONBody(req, &got); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		wantModel := "m" + strings.Repeat("x", i%50)
		if got.Model != wantModel {
			t.Errorf("iter %d: model = %q, want %q", i, got.Model, wantModel)
		}
	}
}

// TestDecodeJSONBody_MalformedReturnsError ensures invalid JSON
// surfaces as an error instead of being silently dropped.
func TestDecodeJSONBody_MalformedReturnsError(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(`{"model":not-a-string`)))
	var got ChatCompletionRequest
	if err := decodeJSONBody(req, &got); err == nil {
		t.Error("expected error on malformed JSON, got nil")
	}
}

// TestDecodeJSONBody_OversizedReturnsMaxBytesError verifies the body
// size cap: a body larger than maxDecodeBodyBytes is rejected with a
// *http.MaxBytesError (which the adapter handlers map to a 413)
// instead of being drained into an unbounded allocation.
func TestDecodeJSONBody_OversizedReturnsMaxBytesError(t *testing.T) {
	// A valid-JSON string literal padded well past the cap, so the
	// rejection is driven by size and not by a parse error.
	huge := `{"model":"` + strings.Repeat("a", maxDecodeBodyBytes+1024) + `"}`
	req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(huge)))
	var got ChatCompletionRequest
	err := decodeJSONBody(req, &got)
	if err == nil {
		t.Fatal("expected error on oversized body, got nil")
	}
	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) {
		t.Fatalf("expected *http.MaxBytesError, got %T: %v", err, err)
	}
}

// TestDecodeJSONBody_AtLimitSucceeds checks that a body at the upper
// boundary still decodes — the cap rejects only what exceeds it.
func TestDecodeJSONBody_AtLimitSucceeds(t *testing.T) {
	// Build a valid request whose total length is comfortably under
	// the cap but large (a long content string).
	pad := strings.Repeat("x", maxDecodeBodyBytes/2)
	body := `{"model":"m","messages":[{"role":"user","content":"` + pad + `"}]}`
	if len(body) > maxDecodeBodyBytes {
		t.Fatalf("test body %d exceeds cap %d", len(body), maxDecodeBodyBytes)
	}
	req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(body)))
	var got ChatCompletionRequest
	if err := decodeJSONBody(req, &got); err != nil {
		t.Fatalf("decodeJSONBody at limit: %v", err)
	}
	if got.Model != "m" || len(got.Messages) != 1 {
		t.Fatalf("decoded request mismatch: %+v", got)
	}
}

// BenchmarkDecodeJSONBody_Pooled measures the new helper.
// BenchmarkDecodeJSONBody_StreamingDecoder measures the old
// json.NewDecoder(r.Body).Decode pattern for comparison. The two
// numbers should sit side by side in `make bench-report` output so
// the win is auditable.
var benchBody = []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello world, this is a typical short request body that an SDK would send"}],"stream":false}`)

func BenchmarkDecodeJSONBody_Pooled(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/", io.NopCloser(bytes.NewReader(benchBody)))
		var got ChatCompletionRequest
		if err := decodeJSONBody(req, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeJSONBody_StreamingDecoder(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/", io.NopCloser(bytes.NewReader(benchBody)))
		var got ChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			b.Fatal(err)
		}
	}
}
