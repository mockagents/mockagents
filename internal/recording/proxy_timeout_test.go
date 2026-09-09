package recording

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProxyHasNoWholeRequestTimeout is the audit M-33 guard. A single
// http.Client.Timeout covers the response BODY too, so every recorded SSE
// stream was cut at 60 seconds. Each phase must be bounded on the transport
// instead, leaving the body unbounded.
func TestProxyHasNoWholeRequestTimeout(t *testing.T) {
	p, err := NewProxy("https://api.openai.com", New(""))
	if err != nil {
		t.Fatal(err)
	}
	if p.Client.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want 0 (it would cap streamed bodies)", p.Client.Timeout)
	}
	tr, ok := p.Client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", p.Client.Transport)
	}
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, DefaultResponseHeaderTimeout)
	}
	if tr.TLSHandshakeTimeout != DefaultTLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", tr.TLSHandshakeTimeout, DefaultTLSHandshakeTimeout)
	}
	if tr.IdleConnTimeout != DefaultIdleConnTimeout {
		t.Errorf("IdleConnTimeout = %v, want %v", tr.IdleConnTimeout, DefaultIdleConnTimeout)
	}
}

// TestProxyStreamOutlivesBodyTimeout: a stream that takes longer than the
// non-streaming body bound must still be recorded and relayed in full.
func TestProxyStreamOutlivesBodyTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		for i := 0; i < 3; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(40 * time.Millisecond):
			}
			io.WriteString(w, "data: chunk\n\n")
			flusher.Flush()
		}
		io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	p, err := NewProxy(upstream.URL, New(path))
	if err != nil {
		t.Fatal(err)
	}
	// Far shorter than the stream: it must NOT apply to an SSE response.
	p.BodyTimeout = 20 * time.Millisecond

	rec := httptest.NewServer(p)
	defer rec.Close()

	resp, err := http.Post(rec.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading stream: %v", err)
	}
	if got := strings.Count(string(body), "data: chunk"); got != 3 {
		t.Errorf("received %d chunks, want 3 (body:\n%s)", got, body)
	}
	if !strings.Contains(string(body), "[DONE]") {
		t.Errorf("stream was cut before [DONE]:\n%s", body)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Len() != 1 {
		t.Fatalf("recorded %d interactions, want 1", reloaded.Len())
	}
	if !reloaded.All()[0].Streaming {
		t.Errorf("interaction should be marked streaming")
	}
}

// TestProxyBodyTimeoutBoundsNonStreamingRead: an upstream that sends headers
// and then stalls must not hang the recorder forever.
func TestProxyBodyTimeoutBoundsNonStreamingRead(t *testing.T) {
	released := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-released:
		case <-time.After(5 * time.Second):
		}
	}))
	defer upstream.Close()
	defer close(released)

	p, err := NewProxy(upstream.URL, New(""))
	if err != nil {
		t.Fatal(err)
	}
	p.BodyTimeout = 50 * time.Millisecond

	rec := httptest.NewServer(p)
	defer rec.Close()

	start := time.Now()
	resp, err := http.Post(rec.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-4o"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("stalled body read took %v, want it bounded near the 50ms body timeout", elapsed)
	}
}

// TestProxyRecordsNonJSONResponse is the audit M-28 end-to-end guard: an HTML
// error page from the upstream used to be assigned straight to a
// json.RawMessage, producing a cassette line that could not be encoded — the
// write failed and the interaction was lost. It must now be stored wrapped and
// replayed byte-for-byte.
func TestProxyRecordsNonJSONResponse(t *testing.T) {
	const page = "<html><body>502 Bad Gateway</body></html>"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, page)
	}))
	defer upstream.Close()

	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	p, err := NewProxy(upstream.URL, New(path))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewServer(p)
	defer rec.Close()

	const reqBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(rec.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || string(got) != page {
		t.Fatalf("client got %d %q, want 502 %q", resp.StatusCode, got, page)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Len() != 1 {
		t.Fatalf("recorded %d interactions, want 1 — a non-JSON body must not lose the record", reloaded.Len())
	}
	it := reloaded.All()[0]
	if it.ResponseBodyEncoding != BodyEncodingText {
		t.Errorf("response encoding = %q, want %q", it.ResponseBodyEncoding, BodyEncodingText)
	}
	if string(it.ResponseBodyBytes()) != page {
		t.Errorf("stored body = %q, want %q", it.ResponseBodyBytes(), page)
	}

	// And replay serves the original bytes back, not the JSON wrapper.
	rp := httptest.NewServer(NewReplay(reloaded))
	defer rp.Close()
	replayed, err := http.Post(rp.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("replay post: %v", err)
	}
	defer replayed.Body.Close()
	rbody, _ := io.ReadAll(replayed.Body)
	if replayed.StatusCode != http.StatusBadGateway {
		t.Errorf("replay status = %d, want 502", replayed.StatusCode)
	}
	if string(rbody) != page {
		t.Errorf("replayed body = %q, want %q", rbody, page)
	}
}
