package recording

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
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
	// Redactor, when non-nil, masks secrets in each recorded interaction before
	// it is appended to the cassette (R-03). It never touches the response
	// forwarded to the client.
	Redactor *Redactor
	// BodyTimeout bounds how long a NON-streaming upstream response body may
	// take to arrive once headers are in. Zero uses DefaultBodyTimeout;
	// negative disables the bound. SSE responses are exempt — a recording
	// session legitimately holds a stream open for as long as the client wants
	// (audit M-33: a single Client.Timeout cut every long stream at 60s).
	BodyTimeout time.Duration
	// SkipRecordOnError, when true, suppresses Cassette.Append for upstream
	// responses with status >= 400 (the client still receives the error). This
	// keeps a record-on-miss fallback (R-01) from caching a transient 429/500 as
	// the canonical recorded response. The standalone `record` command leaves it
	// false so an explicitly-recorded session captures errors too.
	SkipRecordOnError bool
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
		// No Client.Timeout: it covers the whole exchange INCLUDING the body,
		// so it killed every SSE recording at 60s (audit M-33). Each phase is
		// bounded separately instead — connect, TLS, and response headers on
		// the transport, the non-streaming body read in ServeHTTP.
		Client: &http.Client{Transport: NewProxyTransport()},
	}, nil
}

// Per-phase upstream timeouts (audit M-33). These bound the phases that can
// hang against an unresponsive upstream without capping a legitimately long
// streamed response.
const (
	// DefaultDialTimeout bounds TCP connect.
	DefaultDialTimeout = 10 * time.Second
	// DefaultTLSHandshakeTimeout bounds the TLS handshake.
	DefaultTLSHandshakeTimeout = 10 * time.Second
	// DefaultResponseHeaderTimeout bounds the wait for the upstream's response
	// headers. It is generous because a slow model can take a while to produce
	// the first byte, but unlike a whole-request timeout it stops counting once
	// the stream starts.
	DefaultResponseHeaderTimeout = 120 * time.Second
	// DefaultBodyTimeout bounds reading a NON-streaming response body.
	DefaultBodyTimeout = 120 * time.Second
	// DefaultIdleConnTimeout bounds how long a pooled connection stays open.
	DefaultIdleConnTimeout = 90 * time.Second
)

// NewProxyTransport builds the http.Transport the recording proxy uses:
// bounded connect / TLS / response-header phases, unbounded body so streams
// can run as long as the client keeps reading.
func NewProxyTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{
		Timeout:   DefaultDialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	t.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	t.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	t.ExpectContinueTimeout = 1 * time.Second
	t.IdleConnTimeout = DefaultIdleConnTimeout
	return t
}

// bodyTimeout resolves the non-streaming body read bound.
func (p *Proxy) bodyTimeout() time.Duration {
	if p.BodyTimeout == 0 {
		return DefaultBodyTimeout
	}
	return p.BodyTimeout
}

// ServeHTTP forwards the incoming request, captures the response, and
// writes both to the cassette before returning.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	body, err := DrainBody(r)
	if err != nil {
		http.Error(w, "failed to read request body: "+err.Error(), drainStatus(err))
		return
	}

	target := *p.Upstream
	// path.Clean strips any "../" traversal segments from the incoming path
	// before it is appended to the operator's upstream base (SEC-06). The host
	// and scheme are always the operator's; this just keeps a request from
	// smuggling traversal sequences into the forwarded upstream path.
	target.Path = singleJoin(p.Upstream.Path, path.Clean(r.URL.Path))
	target.RawQuery = r.URL.RawQuery

	// One cancelable context for the whole upstream exchange: the client going
	// away cancels it, and the non-streaming path arms a body-read deadline on
	// it once the response type is known.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	proxyReq, err := http.NewRequestWithContext(ctx, r.Method, target.String(), bytes.NewReader(body))
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

	// Non-streaming: bound the body read. This is the phase a whole-request
	// timeout used to cover, and the only one where an upstream that stops
	// mid-body would otherwise hang the handler forever.
	if d := p.bodyTimeout(); d > 0 {
		timer := time.AfterFunc(d, cancel)
		defer timer.Stop()
	}
	respBody, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		http.Error(w, "reading upstream response: "+err.Error(), http.StatusBadGateway)
		return
	}

	// A non-JSON body (HTML error page, plain-text 502) is wrapped rather than
	// assigned straight to a json.RawMessage, which would have made the whole
	// interaction un-encodable and failed the cassette write (audit M-28).
	reqRaw, reqEnc := EncodeBody(body)
	respRaw, respEnc := EncodeBody(respBody)
	it := &Interaction{
		Method:               r.Method,
		Path:                 r.URL.Path,
		RequestHeaders:       CaptureHeaders(r.Header, DefaultCaptureHeaders),
		RequestBody:          reqRaw,
		RequestBodyEncoding:  reqEnc,
		ResponseStatus:       upstreamResp.StatusCode,
		ResponseHeaders:      CaptureHeaders(upstreamResp.Header, DefaultCaptureHeaders),
		ResponseBody:         respRaw,
		ResponseBodyEncoding: respEnc,
	}
	// Compute the hash from the ORIGINAL request body before any redaction or
	// encoding wrapper, so replay (which sees the un-redacted request) still
	// matches (R-03).
	it.Hash = HashRequest(it.Method, it.Path, body)
	if !p.skipRecording(it.ResponseStatus) {
		if p.Redactor != nil {
			if err := p.Redactor.Apply(it); err != nil {
				w.Header().Set("X-Mockagents-Record-Error", err.Error())
				goto respond
			}
		}
		if err := p.Cassette.Append(it); err != nil {
			// Log via http.Error would overwrite headers; write the response
			// first and surface the cassette error as a trailing header.
			w.Header().Set("X-Mockagents-Record-Error", err.Error())
		}
	}

respond:
	copyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(respBody)
}

// skipRecording reports whether an upstream response with the given status must
// not be appended to the cassette (R-01 record-on-miss must not cache a
// transient 4xx/5xx as the canonical recorded response).
func (p *Proxy) skipRecording(status int) bool {
	return p.SkipRecordOnError && status >= 400
}

// serveStreaming copies SSE chunks from upstreamResp to the client and
// captures each chunk as an Interaction.StreamEvent. Headers are
// flushed before any chunk so clients that rely on Content-Type (every
// SSE client) see it before the first event. Append happens after the
// stream finishes so a half-closed upstream still produces a usable
// (partial) cassette entry.
func (p *Proxy) serveStreaming(w http.ResponseWriter, r *http.Request, reqBody []byte, upstreamResp *http.Response) {
	copyHeaders(w.Header(), upstreamResp.Header)
	// The upstream may have buffered its SSE response and supplied a content
	// length. The proxy streams and may need trailers, so it must frame the
	// downstream response itself.
	w.Header().Del("Content-Length")
	// Streaming headers are committed before recording finishes. Declare the
	// recording error trailer up front so redaction/append failures remain
	// observable instead of being silently discarded after WriteHeader.
	if p.Redactor != nil {
		w.Header().Add("Trailer", "X-Mockagents-Record-Error")
	}
	w.WriteHeader(upstreamResp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	var events []StreamEvent
	var upstreamErrored bool
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
			upstreamErrored = true
			break
		}
	}

	reqRaw, reqEnc := EncodeBody(reqBody)
	it := &Interaction{
		Method:              r.Method,
		Path:                r.URL.Path,
		RequestHeaders:      CaptureHeaders(r.Header, DefaultCaptureHeaders),
		RequestBody:         reqRaw,
		RequestBodyEncoding: reqEnc,
		ResponseStatus:      upstreamResp.StatusCode,
		ResponseHeaders:     CaptureHeaders(upstreamResp.Header, DefaultCaptureHeaders),
		Streaming:           true,
		StreamEvents:        events,
	}
	it.Hash = HashRequest(it.Method, it.Path, reqBody)
	// A stream that broke mid-flight (upstream reset/crash) is a transient
	// failure, not a canonical response — on the record-on-miss path, skipping
	// it avoids permanently caching a truncated, [DONE]-less stream. The status
	// is 200 here, so skipRecording (status-only) can't catch it.
	if !p.skipRecording(it.ResponseStatus) && !(p.SkipRecordOnError && upstreamErrored) {
		if p.Redactor != nil {
			if err := p.Redactor.Apply(it); err != nil {
				w.Header().Set("X-Mockagents-Record-Error", err.Error())
				return
			}
		}
		if err := p.Cassette.Append(it); err != nil {
			w.Header().Set("X-Mockagents-Record-Error", err.Error())
		}
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
