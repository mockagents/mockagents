package recording

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultCaptureHeaders is the whitelist of request/response headers the
// proxy persists into the cassette. Auth tokens are deliberately excluded
// so cassettes are safe to check in.
var DefaultCaptureHeaders = []string{
	"Content-Type",
	"Accept",
	"User-Agent",
	"Anthropic-Version",
	"OpenAI-Organization",
	"X-Request-Id",
}

// Proxy is an HTTP handler that forwards incoming requests to an upstream
// base URL, writes the exchange to a cassette, and returns the upstream
// response verbatim. Streaming is not supported in v1: requests with
// `"stream": true` in their body are forwarded but the response is
// buffered in memory, so cassettes are always JSON.
type Proxy struct {
	Upstream *url.URL
	Cassette *Cassette
	Client   *http.Client
	// UpstreamAPIKey, when non-empty, replaces any incoming Authorization
	// header with "Bearer <key>" on forwarded requests. Useful for routing
	// recordings through a dedicated budget key.
	UpstreamAPIKey string
}

// NewProxy builds a Proxy against the given upstream base URL.
func NewProxy(upstream string, cassette *Cassette) (*Proxy, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		Upstream: u,
		Cassette: cassette,
		Client:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// ServeHTTP forwards the incoming request, captures the response, and
// writes both to the cassette before returning.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := DrainBody(r)
	if err != nil {
		http.Error(w, "failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	target := *p.Upstream
	target.Path = singleJoin(p.Upstream.Path, r.URL.Path)
	target.RawQuery = r.URL.RawQuery

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "building upstream request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	copyHeaders(proxyReq.Header, r.Header)
	if p.UpstreamAPIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+p.UpstreamAPIKey)
	}

	upstreamResp, err := p.Client.Do(proxyReq)
	if err != nil {
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	respBody, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		http.Error(w, "reading upstream response: "+err.Error(), http.StatusBadGateway)
		return
	}

	it := &Interaction{
		Method:          r.Method,
		Path:            r.URL.Path,
		RequestHeaders:  CaptureHeaders(r.Header, DefaultCaptureHeaders),
		RequestBody:     body,
		ResponseStatus:  upstreamResp.StatusCode,
		ResponseHeaders: CaptureHeaders(upstreamResp.Header, DefaultCaptureHeaders),
		ResponseBody:    respBody,
	}
	if err := p.Cassette.Append(it); err != nil {
		// Log via http.Error would overwrite headers; write the response
		// first and surface the cassette error as a trailing header.
		w.Header().Set("X-Mockagents-Record-Error", err.Error())
	}

	copyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(respBody)
}

// copyHeaders duplicates src into dst, dropping hop-by-hop headers.
func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

var hopByHop = map[string]bool{
	"Connection":          true,
	"Proxy-Connection":    true,
	"Keep-Alive":          true,
	"Transfer-Encoding":   true,
	"Te":                  true,
	"Trailer":             true,
	"Upgrade":             true,
	"Proxy-Authorization": true,
}

func isHopByHop(h string) bool {
	return hopByHop[http.CanonicalHeaderKey(h)]
}

// singleJoin concatenates two URL path segments without producing duplicate
// slashes. Handles the common (prefix, "") case where the upstream URL
// already encodes the target path.
func singleJoin(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return strings.TrimSuffix(a, "/") + "/" + strings.TrimPrefix(b, "/")
}
