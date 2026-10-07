package recording

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// fuzzChunks splits data into StreamEvents at fuzzer-chosen boundaries, the
// way serveStreaming captures whatever each upstream Read returned.
func fuzzChunks(data string, sizes []byte) []StreamEvent {
	var out []StreamEvent
	for i := 0; len(data) > 0; i++ {
		n := 1
		if len(sizes) > 0 {
			n = int(sizes[i%len(sizes)])%48 + 1
		}
		n = min(n, len(data))
		out = append(out, StreamEvent{DelayMs: int64(i), Data: data[:n]})
		data = data[n:]
	}
	return out
}

func frameData(frames []StreamEvent) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = f.Data
	}
	return out
}

// FuzzAssembleSSEFrames checks the recorder's SSE frame reassembly — the
// reader that turns arbitrarily chunked upstream bytes into complete frames
// before redaction.
//
// Invariants:
//   - no panic;
//   - chunk-boundary independence: the same bytes as one chunk or split at
//     arbitrary boundaries produce the same frames and the same
//     success/failure outcome;
//   - lossless: on success the frames concatenate back to the input, and
//     each frame ends with a blank-line terminator.
func FuzzAssembleSSEFrames(f *testing.F) {
	seeds := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: [DONE]\n\n",
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		"data: {\"candidates\":[]}\r\n\r\ndata: {\"ok\":true}\n\n",
		"data: {\"a\":1}\n\ndata: {\"b\":2}\r\n\r\n",
		"\r\n\r\n\n",
		"data: incomplete",
		"",
	}
	for _, s := range seeds {
		f.Add(s, []byte{0}, uint16(0))
		f.Add(s, []byte{2, 9, 0, 40}, uint16(16))
	}
	f.Fuzz(func(t *testing.T, data string, sizes []byte, limit uint16) {
		maxBytes := maxRecordedSSEFrameBytes
		if limit > 0 {
			maxBytes = int(limit%256) + 1 // small limits exercise the size checks
		}
		whole, wholeErr := assembleSSEFrames([]StreamEvent{{Data: data}}, maxBytes)
		chunked, chunkedErr := assembleSSEFrames(fuzzChunks(data, sizes), maxBytes)
		if (wholeErr == nil) != (chunkedErr == nil) {
			t.Fatalf("chunking changed the outcome for %q: whole err=%v, chunked err=%v", data, wholeErr, chunkedErr)
		}
		if wholeErr != nil {
			return
		}
		if w, c := frameData(whole), frameData(chunked); !reflect.DeepEqual(w, c) {
			t.Fatalf("chunking changed the frames for %q:\nwhole:   %q\nchunked: %q", data, w, c)
		}
		if got := strings.Join(frameData(whole), ""); got != data {
			t.Fatalf("frames are not lossless: got %q want %q", got, data)
		}
		for _, fr := range whole {
			if !strings.HasSuffix(fr.Data, "\n\n") && !strings.HasSuffix(fr.Data, "\r\n\r\n") {
				t.Fatalf("frame without terminator: %q", fr.Data)
			}
		}
	})
}

const fuzzAlnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// fuzzSecret derives a deterministic credential of a known provider shape
// from the fuzzer's seed. Every shape is one the Redactor is documented to
// mask: storage.SanitizeBody's sk-/key-/Bearer prefixes plus the built-in
// credential patterns.
func fuzzSecret(kind uint8, seed uint64) string {
	rng := rand.New(rand.NewPCG(seed, ^seed))
	gen := func(alphabet string, n int) string {
		var b strings.Builder
		for range n {
			b.WriteByte(alphabet[rng.IntN(len(alphabet))])
		}
		return b.String()
	}
	const upperDigits = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	switch kind % 10 {
	case 0:
		return "sk-" + gen(fuzzAlnum, 40)
	case 1:
		return "sk-ant-api03-" + gen(fuzzAlnum, 40)
	case 2:
		return "key-" + gen(fuzzAlnum, 32)
	case 3:
		return "Bearer " + gen(fuzzAlnum, 40)
	case 4:
		return "AKIA" + gen(upperDigits, 16)
	case 5:
		return "ghp_" + gen(fuzzAlnum, 36)
	case 6:
		return "gho_" + gen(fuzzAlnum, 36)
	case 7:
		return "xoxb-" + gen(fuzzAlnum, 24)
	case 8:
		return "AIza" + gen(fuzzAlnum, 35)
	default:
		return "eyJ" + gen(fuzzAlnum, 20) + "." + gen(fuzzAlnum, 30) + "." + gen(fuzzAlnum, 30)
	}
}

// FuzzRedactorApply records an interaction carrying a credential and checks
// the cassette copy after Redactor.Apply. The credential is embedded, between
// two arbitrary fuzzer strings, as a JSON string value inside a fuzzed JSON
// request document, in a plain-text response body, in a captured header,
// and in a JSON payload of a chunked SSE stream.
//
// Invariants:
//   - Apply never panics and never errors on a well-framed stream;
//   - the credential never appears verbatim in any recorded surface;
//   - redaction is structure-preserving: a JSON body stays valid JSON and
//     the stream keeps its frame count and terminators.
func FuzzRedactorApply(f *testing.F) {
	docs := []string{
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"claude-3-opus","max_tokens":64,"system":"x","messages":[]}`,
		`{"contents":[{"parts":[{"text":"hello"}]}],"n":12345678901234567890}`,
		`[1,"two",{"three":null}]`,
		`"just a string"`,
		`not json`,
		``,
	}
	for i, d := range docs {
		f.Add([]byte(d), "token=", "; rest", uint8(i), uint64(i), []byte{5, 17, 1})
	}
	f.Add([]byte(`{"a":{"b":[{"c":"d"}]}}`), "AKIA", "", uint8(4), uint64(99), []byte{0})
	f.Add([]byte(`{}`), "sk-", "ghp_", uint8(5), uint64(7), []byte{3})
	f.Add([]byte(`{}`), "\\\"<&>", "\n\r\t\x00\xff", uint8(9), uint64(1), []byte{40})

	r, err := NewRedactor(nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, doc []byte, before, after string, kind uint8, seed uint64, sizes []byte) {
		secret := fuzzSecret(kind, seed)
		if bytes.Contains(doc, []byte(secret)) {
			t.Skip() // the fuzzer reproduced the secret itself; not an injection
		}
		// The credential starts a word: "sk-" and "key-" are only masked at a
		// word boundary, so prose such as "turkey-dinner" survives (P-14).
		if n := len(before); n > 0 {
			if c := before[n-1]; (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				before += " "
			}
		}
		value := before + secret + after

		// Request body: the credential as a string value inside the fuzzed
		// JSON document (wrapped when the document is not an object).
		var obj map[string]any
		if json.Unmarshal(doc, &obj) != nil || obj == nil {
			obj = map[string]any{}
			if json.Valid(doc) {
				obj["original"] = json.RawMessage(doc)
			}
		}
		obj["api_key"] = value
		obj["nested"] = map[string]any{"list": []any{value, 1, true, nil}}
		reqBody, err := json.Marshal(obj)
		if err != nil {
			t.Skip()
		}

		// SSE: two data frames whose JSON payload carries the credential,
		// LF- or CRLF-framed, then the sentinel; chunked arbitrarily.
		payload, err := json.Marshal(map[string]any{"delta": map[string]any{"text": value}})
		if err != nil {
			t.Fatal(err)
		}
		nl := "\n"
		if kind&0x80 != 0 {
			nl = "\r\n"
		}
		stream := "event: delta" + nl + "data: " + string(payload) + nl + nl +
			"data: " + string(payload) + nl + nl + "data: [DONE]" + nl + nl

		// Response body: a non-JSON (plain-text) body stored the way the
		// proxy stores it.
		respBody, _ := EncodeBody([]byte("plain: " + value))

		it := &Interaction{
			RequestBody:     json.RawMessage(reqBody),
			ResponseBody:    respBody,
			RequestHeaders:  map[string]string{"Authorization": value},
			ResponseHeaders: map[string]string{"X-Echo": value},
			Streaming:       true,
			StreamEvents:    fuzzChunks(stream, sizes),
		}
		if err := r.Apply(it); err != nil {
			t.Fatalf("Apply on a well-framed stream: %v", err)
		}

		if bytes.Contains(it.RequestBody, []byte(secret)) {
			t.Fatalf("secret survived in request body: %s", it.RequestBody)
		}
		if !json.Valid(it.RequestBody) {
			t.Fatalf("request body no longer valid JSON: %s", it.RequestBody)
		}
		if bytes.Contains(it.ResponseBody, []byte(secret)) {
			t.Fatalf("secret survived in response body: %s", it.ResponseBody)
		}
		for k, v := range it.RequestHeaders {
			if strings.Contains(v, secret) {
				t.Fatalf("secret survived in request header %s: %q", k, v)
			}
		}
		for k, v := range it.ResponseHeaders {
			if strings.Contains(v, secret) {
				t.Fatalf("secret survived in response header %s: %q", k, v)
			}
		}
		if len(it.StreamEvents) != 3 {
			t.Fatalf("stream frame count changed: got %d want 3", len(it.StreamEvents))
		}
		var joined strings.Builder
		for _, ev := range it.StreamEvents {
			if !strings.HasSuffix(ev.Data, nl+nl) {
				t.Fatalf("redacted frame lost its terminator: %q", ev.Data)
			}
			joined.WriteString(ev.Data)
		}
		if strings.Contains(joined.String(), secret) {
			t.Fatalf("secret survived in stream: %q", joined.String())
		}
	})
}
