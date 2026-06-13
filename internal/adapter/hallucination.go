package adapter

import (
	"net/http"
	"strconv"

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

// HeaderImageCount advertises how many image content parts a vision request
// carried (A-05), so a vision test can assert on multimodal handling without
// parsing the request echo.
const HeaderImageCount = "X-Mockagents-Image-Count"

// setImageCountHeader sets HeaderImageCount when the request carried images.
// No-op (header absent) for text-only requests. Must be called before the body
// is written (works for JSON and SSE).
func setImageCountHeader(w http.ResponseWriter, imageCount int) {
	if imageCount <= 0 {
		return
	}
	w.Header().Set(HeaderImageCount, strconv.Itoa(imageCount))
}
