package adapter

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/mockagents/mockagents/internal/engine"
)

// HeaderToolErrors lists the simulated tool calls whose fixture resolved to an
// error, as comma-separated `tool=code` pairs (each side query-escaped), so an
// SDK can assert on a tool error without access to the engine's internal
// response (2026-10-06 review K-04). Absent when no tool call errored.
const HeaderToolErrors = "X-Mockagents-Tool-Errors"

// maxToolErrorsHeader bounds the header value; the pair that would cross it
// and every pair after it are dropped.
const maxToolErrorsHeader = 1024

// setToolErrorsHeader sets HeaderToolErrors from the engine's tool results.
// Must be called before the body is written (works for JSON and SSE).
func setToolErrorsHeader(w http.ResponseWriter, resp *engine.Response) {
	if resp == nil {
		return
	}
	var b strings.Builder
	for _, tr := range resp.ToolResults {
		if !tr.IsError {
			continue
		}
		code := ""
		if tr.Error != nil {
			code = tr.Error.Code
		}
		pair := url.QueryEscape(tr.ToolName) + "=" + url.QueryEscape(code)
		if b.Len() > 0 {
			pair = "," + pair
		}
		if b.Len()+len(pair) > maxToolErrorsHeader {
			break
		}
		b.WriteString(pair)
	}
	if b.Len() > 0 {
		w.Header().Set(HeaderToolErrors, b.String())
	}
}
