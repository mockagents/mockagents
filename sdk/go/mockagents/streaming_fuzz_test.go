package mockagents

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

// chunkedReader serves data in fuzzer-chosen chunk sizes, simulating the
// arbitrary TCP/HTTP chunk boundaries a real SSE response arrives in.
type chunkedReader struct {
	data  []byte
	sizes []byte
	i     int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := 1
	if len(r.sizes) > 0 {
		n = int(r.sizes[r.i%len(r.sizes)])%32 + 1
		r.i++
	}
	n = min(n, len(p), len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

// sseParseResult is everything the SDK derives from an SSE body: the raw
// frame tokens, the parsed frames, and the events RawEventStream surfaces
// (re-encoded as JSON, which sorts map keys, so results compare stably).
type sseParseResult struct {
	Tokens []string
	Frames []sseFrame
	Events []string
	Err    bool
}

func parseSSEStream(t *testing.T, newReader func() io.Reader) sseParseResult {
	t.Helper()
	var res sseParseResult
	sc := newSSEScanner(newReader())
	for sc.Scan() {
		tok := sc.Text()
		res.Tokens = append(res.Tokens, tok)
		if fr := parseSSEFrame(tok); fr != nil {
			res.Frames = append(res.Frames, *fr)
		}
	}
	res.Err = sc.Err() != nil

	// The same body through the iterator the SDK hands to callers.
	r := newReader()
	stream := &RawEventStream{body: io.NopCloser(r), scanner: newSSEScanner(r)}
	defer stream.Close()
	for stream.Next() {
		b, err := json.Marshal(stream.Value())
		if err != nil {
			t.Fatalf("re-encoding event: %v", err)
		}
		res.Events = append(res.Events, string(b))
	}
	return res
}

// FuzzSSEStream feeds arbitrary bytes through the SDK's SSE frame splitter
// (splitSSEFrames via newSSEScanner), frame parser (parseSSEFrame), and the
// RawEventStream iterator.
//
// Invariants:
//   - nothing panics;
//   - chunk-boundary independence: the same body delivered whole and
//     delivered in arbitrary fuzzer-chosen chunks yields the same frame
//     tokens, the same parsed frames, and the same surfaced events. A real
//     HTTP body arrives in unpredictable chunks, so any difference means
//     events are dropped or merged depending on network timing.
func FuzzSSEStream(f *testing.F) {
	seeds := []string{
		// OpenAI chat completions stream.
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		// Anthropic messages stream.
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		// Gemini alt=sse uses CRLF framing.
		"data: {\"candidates\":[]}\r\n\r\ndata: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\r\n\r\n",
		// Comments, multi-line data, no space after colon, unterminated tail.
		": keep-alive\n\ndata:{\"a\":\ndata: 1}\n\ndata: {\"tail\":true}",
		// Mixed line endings.
		"data: {\"a\":1}\n\ndata: {\"b\":2}\r\n\r\n",
		"\n\n\r\n\r\n",
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s), []byte{0})
		f.Add([]byte(s), []byte{3, 7, 1, 30})
	}
	f.Fuzz(func(t *testing.T, data, sizes []byte) {
		whole := parseSSEStream(t, func() io.Reader { return bytes.NewReader(data) })
		chunked := parseSSEStream(t, func() io.Reader { return &chunkedReader{data: data, sizes: sizes} })
		if !reflect.DeepEqual(whole, chunked) {
			t.Fatalf("chunk boundaries changed the parse of %q (sizes %v):\nwhole:   %+v\nchunked: %+v", data, sizes, whole, chunked)
		}
	})
}
