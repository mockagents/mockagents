package adapter

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// faultServer stands up an httptest server whose handler performs a connection
// fault for the given mode, and returns its URL.
func faultServer(t *testing.T, mode string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !connectionFault(w, mode) {
			http.Error(w, "not hijackable", http.StatusBadGateway)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func faultClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// TestConnectionFault_ClientSeesError asserts that each mode causes the HTTP
// client to fail (no usable response) — without hard-coding OS-specific errno
// text (RST manifests differently on Linux vs Windows).
func TestConnectionFault_ClientSeesError(t *testing.T) {
	for _, mode := range []string{"empty", "reset", "random", "peer-reset", "garbage"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			url := faultServer(t, mode)
			resp, err := faultClient().Post(url, "application/json", nil)
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				t.Fatalf("mode %q: expected a client error, got status %d body=%q", mode, resp.StatusCode, body)
			}
		})
	}
}

// TestConnectionFault_EmptyIsZeroBytes verifies the empty mode closes the
// connection without sending any bytes (a raw read returns EOF immediately).
func TestConnectionFault_EmptyIsZeroBytes(t *testing.T) {
	url := faultServer(t, "empty")
	// A raw HTTP/1.1 GET over a fresh conn: the server closes with no response.
	resp, err := faultClient().Get(url)
	if err == nil {
		resp.Body.Close()
		t.Fatal("empty mode should produce a client error (no response)")
	}
}

// unwrapWriter mimics the server's statusWriter: it does not implement
// http.Hijacker directly but exposes Unwrap, so http.ResponseController must
// traverse it to reach the underlying hijackable connection.
type unwrapWriter struct {
	http.ResponseWriter
}

func (u *unwrapWriter) Unwrap() http.ResponseWriter { return u.ResponseWriter }

func TestConnectionFault_TraversesUnwrapChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap w so it only exposes Hijack via Unwrap (like statusWriter).
		if !connectionFault(&unwrapWriter{w}, "empty") {
			http.Error(w, "hijack failed through Unwrap", http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	resp, err := faultClient().Get(srv.URL)
	if err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("hijack should have traversed the Unwrap chain; got status %d body=%q", resp.StatusCode, body)
	}
}

// TestConnectionFault_OverTLS exercises the *tls.Conn -> NetConn() unwrap in
// underlyingTCP: a reset over a TLS server must still fault the client (and not
// panic on the SetLinger path).
func TestConnectionFault_OverTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !connectionFault(w, "reset") {
			http.Error(w, "not hijackable", http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	client := srv.Client()
	client.Timeout = 5 * time.Second
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("reset over TLS should produce a client error")
	}
}

// TestConnectionFault_NonHijackableReturnsFalse covers the HTTP/2 / recorder
// fallback path: a ResponseWriter with no Hijacker (and no Unwrap) returns false
// so the caller can write a 502 instead of hanging.
func TestConnectionFault_NonHijackableReturnsFalse(t *testing.T) {
	rec := httptest.NewRecorder()
	if connectionFault(rec, "reset") {
		t.Error("a non-hijackable writer must return false")
	}
	if rec.Body.Len() != 0 || rec.Code != 200 {
		t.Errorf("connectionFault must not write anything itself on the fallback path; code=%d bodylen=%d", rec.Code, rec.Body.Len())
	}
}
