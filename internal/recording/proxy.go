package recording

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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
// response verbatim. Streaming SSE responses are tee'd: each chunk is
// flushed to the client as it arrives and simultaneously appended to the
// captured Interaction.StreamEvents list, with DelayMs recording the
// offset from the start of the response so Replay can optionally
// re-honor the pacing.
type Proxy struct {
	Upstream *url.URL
	Cassette *Cassette
	Client   *http.Client
	// UpstreamAPIKey, when non-empty, replaces any incoming Authorization
	// header with "Bearer <key>" on forwarded requests. Useful for routing
	// recordings through a dedicated budget key.
	UpstreamAPIKey string
}

// isSSE reports whether a Content-Type value indicates a Server-Sent
// Events stream.
func isSSE(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")
}

// NewProxy builds a Proxy against the given upstream base URL.
func NewProxy(upstream string, cassette *Cassette) (*Proxy, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	// Only http(s) upstreams with a host (SEC-06): reject file://, gopher://,
	// and schemeless/hostless values so a recording can never be pointed at a
	// non-network target or a bare path.
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("recording: upstream must be an http(s) URL with a host, got %q", upstream)
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
	// path.Clean strips any "../" traversal segments from the incoming path
	// before it is appended to the operator's upstream base (SEC-06). The host
	// and scheme are always the operator's; this just keeps a request from
	// smuggling traversal sequences into the forwarded upstream path.
	target.Path = singleJoin(p.Upstream.Path, path.Clean(r.URL.Path))
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

	// Streaming path: tee each chunk into the cassette while flushing
	// to the client. We intentionally do not buffer the whole body —
	// clients that consume SSE expect incremental arrivals.
	if isSSE(upstreamResp.Header.Get("Content-Type")) {
		p.serveStreaming(w, r, body, upstreamResp)
		return
	}

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

// serveStreaming copies SSE chunks from upstreamResp to the client and
// captures each chunk as an Interaction.StreamEvent. Headers are
// flushed before any chunk so clients that rely on Content-Type (every
// SSE client) see it before the first event. Append happens after the
// stream finishes so a half-closed upstream still produces a usable
// (partial) cassette entry.
func (p *Proxy) serveStreaming(w http.ResponseWriter, r *http.Request, reqBody []byte, upstreamResp *http.Response) {
	copyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	var events []StreamEvent
	start := time.Now()
	buf := make([]byte, 4096)
	for {
		n, rerr := upstreamResp.Body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if _, werr := w.Write(chunk); werr != nil {
				// Client disconnected; stop streaming but still save
				// what we captured so the partial recording is useful.
				events = append(events, StreamEvent{
					DelayMs: time.Since(start).Milliseconds(),
					Data:    string(chunk),
				})
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
			events = append(events, StreamEvent{
				DelayMs: time.Since(start).Milliseconds(),
				Data:    string(chunk),
			})
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			// Surface the upstream read error as a trailing header on
			// the cassette entry; the client has already seen whatever
			// bytes we managed to forward.
			w.Header().Set("X-Mockagents-Upstream-Error", rerr.Error())
			break
		}
	}

	it := &Interaction{
		Method:          r.Method,
		Path:            r.URL.Path,
		RequestHeaders:  CaptureHeaders(r.Header, DefaultCaptureHeaders),
		RequestBody:     reqBody,
		ResponseStatus:  upstreamResp.StatusCode,
		ResponseHeaders: CaptureHeaders(upstreamResp.Header, DefaultCaptureHeaders),
		Streaming:       true,
		StreamEvents:    events,
	}
	if err := p.Cassette.Append(it); err != nil {
		w.Header().Set("X-Mockagents-Record-Error", err.Error())
	}
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
