package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
)

// decodeBufPool recycles bytes.Buffer instances used to drain HTTP
// request bodies for JSON decoding. The previous code path created a
// fresh json.Decoder per request, which streams from r.Body into an
// internal scratch buffer that is allocated on every call. Reading
// the body into a pooled bytes.Buffer once and then handing the
// resulting slice to json.Unmarshal lets us reuse that scratch
// across requests — one fewer allocation and one fewer garbage
// scan on every adapter call.
//
// Sizing: the New func returns a buffer with no initial backing
// array; the first ReadFrom on a fresh buffer will allocate a
// 64-byte slice and grow exponentially, so steady-state requests
// quickly converge to a backing array large enough for the typical
// body and never reallocate again.
var decodeBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// maxPooledBodyBufBytes caps the size of buffers returned to the
// pool. A single oversized request (e.g. an attacker probing
// multipart limits) must not turn the pool into a permanent memory
// high-water mark.
const maxPooledBodyBufBytes = 1 << 20 // 1 MiB

// maxDecodeBodyBytes bounds how many bytes decodeJSONBody will read
// from a request body before rejecting it. Without this cap a single
// request with a huge (or never-ending) body would drain straight
// into the pooled buffer and allocate without limit — an
// unbounded-allocation DoS on every adapter route. The limit is
// generous enough for realistic LLM payloads (long histories, tool
// schemas, modestly sized base64 vision inputs) while keeping the
// worst-case allocation per request bounded. Oversized buffers are
// already dropped from the pool by maxPooledBodyBufBytes above.
const maxDecodeBodyBytes = 10 << 20 // 10 MiB

// decodeJSONBody drains r.Body into a pooled buffer and unmarshals
// it into v. Behavior matches json.NewDecoder(r.Body).Decode(v)
// exactly for the inputs we care about (no UseNumber, no
// DisallowUnknownFields, no streaming): both fully consume the body
// and return the same kind of *json.SyntaxError on malformed input.
//
// The body is size-capped at maxDecodeBodyBytes; a larger body makes
// the read return a *http.MaxBytesError, which callers map to a 413.
//
// The body must not be read again after this call returns.
func decodeJSONBody(r *http.Request, v any) error {
	buf := decodeBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() > maxPooledBodyBufBytes {
			// Drop the oversized backing array on the floor instead
			// of reusing it — let the GC reclaim the memory.
			return
		}
		decodeBufPool.Put(buf)
	}()

	// Passing a nil ResponseWriter is fine: the byte limit is still
	// enforced (ReadFrom returns *http.MaxBytesError past the cap);
	// only the optional "close the connection early" server signal
	// is skipped, which the per-route error response makes moot.
	limited := http.MaxBytesReader(nil, r.Body, maxDecodeBodyBytes)
	if _, err := buf.ReadFrom(limited); err != nil {
		return err
	}
	return json.Unmarshal(buf.Bytes(), v)
}
