package recording

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewProxy_RejectsNonHTTPUpstream is part of the SEC-06 guard: a recording
// upstream must be an http(s) URL with a host — file://, schemeless, and other
// schemes are rejected so a recording can't target a non-network resource.
func TestNewProxy_RejectsNonHTTPUpstream(t *testing.T) {
	cas := New("")
	bad := []string{
		"file:///etc/passwd",
		"gopher://x/",
		"/no/scheme",
		"ftp://h/",
		"",
		"https://", // no host
	}
	for _, u := range bad {
		if _, err := NewProxy(u, cas); err == nil {
			t.Errorf("NewProxy(%q) err = nil, want error", u)
		}
	}
	for _, u := range []string{"http://up.example/v1", "https://api.example"} {
		if _, err := NewProxy(u, cas); err != nil {
			t.Errorf("NewProxy(%q) err = %v, want nil", u, err)
		}
	}
}

// TestProxy_CleansTraversalInForwardedPath is the other half of SEC-06: a
// request path with ../ traversal segments must be cleaned before it is appended
// to the operator's upstream base, so it can't smuggle traversal upstream.
func TestProxy_CleansTraversalInForwardedPath(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := NewProxy(upstream.URL+"/base", New(""))
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/../../../etc/passwd", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if gotPath == "" {
		t.Fatal("upstream never received the request")
	}
	if strings.Contains(gotPath, "..") {
		t.Errorf("forwarded path %q still contains traversal segments", gotPath)
	}
}
