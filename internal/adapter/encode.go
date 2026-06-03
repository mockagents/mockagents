package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
)

// respEncoder bundles a bytes.Buffer with a json.Encoder bound to it so
// writeJSON reuses BOTH the scratch buffer and the encoder across responses —
// the out-bound symmetric partner of decodeBufPool (PERF-04). The previous
// `json.NewEncoder(w).Encode(v)` allocated a fresh encoder per response and
// reflect-encoded directly into the socket, interleaving encode with Write
// syscalls. json.Encoder.Encode appends a trailing newline, so buffering then
// doing a single Write leaves the wire bytes identical.
type respEncoder struct {
	buf *bytes.Buffer
	enc *json.Encoder
}

var respEncPool = sync.Pool{
	New: func() any {
		b := new(bytes.Buffer)
		return &respEncoder{buf: b, enc: json.NewEncoder(b)}
	},
}

// writeJSON serializes v into a pooled buffer and writes it in one Write.
func writeJSON(w http.ResponseWriter, status int, v any) {
	re := respEncPool.Get().(*respEncoder)
	re.buf.Reset()
	defer func() {
		// Don't let a pathologically large response keep the pooled buffer as a
		// permanent memory high-water mark (mirrors decodeBufPool's guard).
		if re.buf.Cap() <= maxPooledBodyBufBytes {
			respEncPool.Put(re)
		}
	}()
	// These response shapes always marshal; the error is ignored exactly as the
	// previous json.NewEncoder(w).Encode(v) did.
	_ = re.enc.Encode(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(re.buf.Bytes())
}
