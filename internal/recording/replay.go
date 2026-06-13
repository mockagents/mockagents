package recording

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// Replay is an HTTP handler that serves previously recorded interactions
// from a Cassette. Unknown requests either fall through to Fallback (if
// set) or return 404.
type Replay struct {
	Cassette *Cassette
	// Fallback is invoked when no matching interaction is found. If nil,
	// replay returns a 404 with a descriptive error body.
	Fallback http.Handler
	// PreserveStreamDelays, when true, causes streaming replays to
	// honor the original DelayMs timestamps between chunks. Defaults
	// off so CI suites get deterministic fast replays; set to true
	// for demos where realistic pacing matters.
	PreserveStreamDelays bool

	// Matcher, when set and active, relaxes matching by ignoring a set of
	// top-level request body fields (R-02). Requests are then looked up by a
	// "match key" (ignored-fields stripped, then hashed) via a lazily-built
	// secondary index over the cassette. Nil/inactive ⇒ exact-hash matching.
	Matcher *Matcher

	// cursor tracks the per-key replay position so a multi-turn loop that
	// repeats the same request gets the recorded interactions in sequence
	// (R-04). Past the end of a sequence the last interaction repeats. Guarded
	// by mu. One Replay instance serves all requests, so the cursor persists
	// across the loop.
	mu     sync.Mutex
	cursor map[string]int

	// byMatchKey is the secondary index used when Matcher is active: match key
	// → interactions in insertion order (same semantics as Cassette.byHash).
	// Guarded by indexMu. builtLen records the cassette size the index was last
	// built at; it is rebuilt when the cassette grows (record-on-miss, R-01)
	// rather than only once, so newly-recorded interactions become matchable.
	indexMu    sync.Mutex
	builtLen   int
	byMatchKey map[string][]*Interaction
}

// NewReplay builds a Replay handler.
func NewReplay(c *Cassette) *Replay {
	return &Replay{Cassette: c, cursor: make(map[string]int)}
}

// next returns the interaction to serve for this request hash, advancing the
// per-hash cursor. The Nth identical request returns the Nth recorded
// interaction; requests past the end repeat the last one. Returns nil when the
// hash has no recorded interactions.
func (rp *Replay) next(hash string) *Interaction {
	return rp.advance(hash, rp.Cassette.LookupSequence(hash))
}

// nextFromMatchIndex is the matcher-active counterpart of next: it looks the
// request up by match key in the secondary index (rebuilt when the cassette has
// grown), advancing the per-key cursor with the same sequence/repeat-last
// semantics.
func (rp *Replay) nextFromMatchIndex(key string) *Interaction {
	rp.indexMu.Lock()
	if rp.byMatchKey == nil || rp.Cassette.Len() > rp.builtLen {
		rp.extendMatchIndex()
	}
	seq := rp.byMatchKey[key]
	rp.indexMu.Unlock()
	return rp.advance(key, seq)
}

// advance applies the shared R-04 cursor logic to a resolved sequence.
func (rp *Replay) advance(key string, seq []*Interaction) *Interaction {
	if len(seq) == 0 {
		return nil
	}
	rp.mu.Lock()
	if rp.cursor == nil {
		// Tolerate a directly-constructed Replay{Cassette: c} (zero-value
		// usable, like before R-04 added the cursor).
		rp.cursor = make(map[string]int)
	}
	idx := rp.cursor[key]
	if idx >= len(seq) {
		idx = len(seq) - 1 // repeat-last
	} else {
		rp.cursor[key] = idx + 1
	}
	rp.mu.Unlock()
	return seq[idx]
}

// extendMatchIndex incrementally keys the interactions appended since the last
// build into byMatchKey (only the [builtLen:] suffix), so record-on-miss growth
// costs O(delta) rather than O(n) per miss. Because Cassette.All() is
// insertion-ordered, this is a pure suffix-append to each key's sequence — the
// per-key cursor stays valid and a newly-seen key starts at 0. Caller holds
// indexMu.
func (rp *Replay) extendMatchIndex() {
	all := rp.Cassette.All()
	if rp.byMatchKey == nil {
		rp.byMatchKey = make(map[string][]*Interaction, len(all))
	}
	for _, it := range all[rp.builtLen:] {
		key := rp.Matcher.Key(it.Method, it.Path, it.RequestBody)
		rp.byMatchKey[key] = append(rp.byMatchKey[key], it)
	}
	rp.builtLen = len(all)
}

// ServeHTTP looks up the incoming request in the cassette and, on a hit,
// writes the recorded status, headers and body back to the client.
func (rp *Replay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := DrainBody(r)
	if err != nil {
		http.Error(w, "reading request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// The full-body hash is always computed: it keys the exact-match path and is
	// echoed in the miss diagnostics regardless of which matching mode is used.
	hash := HashRequest(r.Method, r.URL.Path, body)
	var it *Interaction
	if rp.Matcher.active() {
		it = rp.nextFromMatchIndex(rp.Matcher.Key(r.Method, r.URL.Path, body))
	} else {
		it = rp.next(hash)
	}
	if it == nil {
		if rp.Fallback != nil {
			// Reset the body so the fallback can read it.
			r.Body = io.NopCloser(bytes.NewReader(body))
			rp.Fallback.ServeHTTP(w, r)
			return
		}
		rp.serveMissDiagnostics(w, r.Method, r.URL.Path, hash, body)
		return
	}

	// Streaming hit: replay captured SSE chunks in order, optionally
	// honoring the original inter-chunk delays.
	if it.Streaming {
		rp.serveStreaming(r.Context(), w, it)
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

// serveStreaming writes a captured SSE interaction back to the client,
// flushing after every chunk so downstream consumers see the same
// incremental arrivals they would from a real LLM server.
func (rp *Replay) serveStreaming(ctx context.Context, w http.ResponseWriter, it *Interaction) {
	for k, v := range it.ResponseHeaders {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("X-Mockagents-Replay", "hit-streaming")
	status := it.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	start := time.Now()
	for _, ev := range it.StreamEvents {
		if rp.PreserveStreamDelays && ev.DelayMs > 0 {
			target := start.Add(time.Duration(ev.DelayMs) * time.Millisecond)
			if d := time.Until(target); d > 0 {
				// Honor the captured pacing, but bail out promptly if the client
				// disconnects or the server shuts down (don't block for the full
				// recorded delay).
				timer := time.NewTimer(d)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
		if _, err := w.Write([]byte(ev.Data)); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}
