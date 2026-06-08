package adapter

import (
	"net/http"

	"github.com/mockagents/mockagents/internal/engine"
)

// HeaderHallucination advertises that a response is a planted hallucination
// fixture (FB-02), so negative tests and harnesses can detect it without
// parsing the body.
const HeaderHallucination = "X-Mockagents-Hallucination"

// setHallucinationHeader sets HeaderHallucination to the fixture type (or
// "true") when the matched scenario response is a hallucination fixture. Must
// be called before the body is written (works for JSON and SSE).
func setHallucinationHeader(w http.ResponseWriter, resp *engine.Response) {
	if resp == nil || resp.Hallucination == nil {
		return
	}
	t := resp.Hallucination.Type
	if t == "" {
		t = "true"
	}
	w.Header().Set(HeaderHallucination, t)
}
