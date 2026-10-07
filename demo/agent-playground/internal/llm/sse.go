package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// sseEvent is one Server-Sent Event frame.
type sseEvent struct {
	Event string
	Data  string
}

// readSSE calls fn for every event in r until EOF or fn returns stop=true.
// It tolerates CRLF and LF line endings.
func readSSE(r io.Reader, fn func(ev sseEvent) (stop bool, err error)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var ev sseEvent
	var data []string
	flush := func() (bool, error) {
		if len(data) == 0 && ev.Event == "" {
			return false, nil
		}
		ev.Data = strings.Join(data, "\n")
		stop, err := fn(ev)
		ev = sseEvent{}
		data = data[:0]
		return stop, err
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
			if stop, err := flush(); stop || err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// comment / keep-alive
		case strings.HasPrefix(line, "event:"):
			ev.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	_, err := flush()
	return err
}

// postJSON sends a JSON POST and returns the response when it is 2xx. A
// non-2xx response is decoded into an *APIError via decodeErr.
func postJSON(ctx context.Context, c *http.Client, provider, url string, headers map[string]string, body any,
	decodeErr func(status int, h http.Header, body []byte) *APIError) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: encode request: %w", provider, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", provider, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, wrapDoErr(ctx, provider, err)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		apiErr := decodeErr(resp.StatusCode, resp.Header, raw)
		apiErr.Provider = provider
		apiErr.Status = resp.StatusCode
		apiErr.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		apiErr.Headers = interestingHeaders(resp.Header)
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(raw))
			if apiErr.Message == "" {
				apiErr.Message = http.StatusText(resp.StatusCode)
			}
		}
		return nil, apiErr
	}
	return resp, nil
}

// readJSON decodes a whole (non-streaming) response body.
func readJSON(ctx context.Context, provider string, resp *http.Response, v any) error {
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return wrapReadErr(ctx, provider, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		// A 2xx with an unparseable body is what a corrupting proxy produces.
		return &TransportError{Provider: provider, Err: fmt.Errorf("decode response: %w", err)}
	}
	return nil
}
