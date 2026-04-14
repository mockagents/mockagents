package recording

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// Replay is an HTTP handler that serves previously recorded interactions
// from a Cassette. Unknown requests either fall through to Fallback (if
// set) or return 404.
type Replay struct {
	Cassette *Cassette
	// Fallback is invoked when no matching interaction is found. If nil,
	// replay returns a 404 with a descriptive error body.
	Fallback http.Handler
}

// NewReplay builds a Replay handler.
func NewReplay(c *Cassette) *Replay {
	return &Replay{Cassette: c}
}

// ServeHTTP looks up the incoming request in the cassette and, on a hit,
// writes the recorded status, headers and body back to the client.
func (rp *Replay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := DrainBody(r)
	if err != nil {
		http.Error(w, "reading request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	hash := HashRequest(r.Method, r.URL.Path, body)
	it := rp.Cassette.Lookup(hash)
	if it == nil {
		if rp.Fallback != nil {
			// Reset the body so the fallback can read it.
			r.Body = io.NopCloser(bytes.NewReader(body))
			rp.Fallback.ServeHTTP(w, r)
			return
		}
		http.Error(w,
			fmt.Sprintf("no cassette match for %s %s (hash=%s)", r.Method, r.URL.Path, hash[:12]),
			http.StatusNotFound)
		return
	}

	for k, v := range it.ResponseHeaders {
		w.Header().Set(k, v)
	}
	w.Header().Set("X-Mockagents-Replay", "hit")
	status := it.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(it.ResponseBody)
}
