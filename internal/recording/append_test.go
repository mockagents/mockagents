package recording

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// countLines returns the number of non-empty lines in a cassette file.
func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	n := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			n++
		}
	}
	return n
}

func testInteraction(i int) *Interaction {
	return &Interaction{
		Method:         "POST",
		Path:           "/v1/chat/completions",
		RequestBody:    json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
		ResponseStatus: 200,
		ResponseBody:   json.RawMessage(fmt.Sprintf(`{"a":%d}`, i)),
	}
}

// TestAppendConcurrentWritersKeepEveryInteraction is the audit M-26 guard.
// Append used to snapshot under the lock and then rewrite the WHOLE file
// outside it, so two concurrent recorders raced on os.Rename and the loser's
// snapshot silently replaced the winner's — interactions vanished.
func TestAppendConcurrentWritersKeepEveryInteraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)

	const writers = 32
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := c.Append(testInteraction(i)); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Append: %v", err)
	}

	if got := countLines(t, path); got != writers {
		t.Errorf("cassette has %d lines, want %d", got, writers)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Len() != writers {
		t.Fatalf("reloaded %d interactions, want %d", reloaded.Len(), writers)
	}
	// Every distinct request must be present exactly once.
	for i := 0; i < writers; i++ {
		h := HashRequest("POST", "/v1/chat/completions", []byte(fmt.Sprintf(`{"i":%d}`, i)))
		if got := len(reloaded.LookupSequence(h)); got != 1 {
			t.Errorf("interaction %d recorded %d times, want 1", i, got)
		}
	}
}

// TestAppendDoesNotRewriteEarlierLines proves the append is a single-line
// O_APPEND write rather than a whole-file rewrite: a pre-existing line keeps
// bytes (here an unknown field) that a rewrite through the Interaction struct
// would drop.
func TestAppendDoesNotRewriteEarlierLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	existing := `{"recorded_at":"2026-01-01T00:00:00Z","hash":"abc","method":"POST","path":"/v1/chat/completions","request_body":{"i":0},"response_status":200,"response_body":{"a":0},"custom_marker":"keep"}` + "\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := c.Append(testInteraction(1)); err != nil {
		t.Fatalf("append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"custom_marker":"keep"`) {
		t.Errorf("the pre-existing line was rewritten; file:\n%s", data)
	}
	if got := countLines(t, path); got != 2 {
		t.Errorf("cassette has %d lines, want 2", got)
	}
}

// TestAppendAfterTornTailRewritesFile: a recorder killed mid-write leaves a
// partial final line. Appending to it would glue the next record onto that
// fragment, so the first write must rebuild the file atomically instead.
func TestAppendAfterTornTailRewritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	good := `{"recorded_at":"2026-01-01T00:00:00Z","hash":"abc","method":"POST","path":"/v1/chat/completions","request_body":{"i":0},"response_status":200,"response_body":{"a":0}}` + "\n"
	if err := os.WriteFile(path, []byte(good+`{"method":"POST","path":"/v1/ch`), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Len() != 1 {
		t.Fatalf("loaded %d interactions, want 1 (torn tail skipped)", c.Len())
	}
	if err := c.Append(testInteraction(1)); err != nil {
		t.Fatalf("append: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Len() != 2 {
		t.Fatalf("reloaded %d interactions, want 2", reloaded.Len())
	}
	if got := countLines(t, path); got != 2 {
		t.Errorf("cassette has %d lines, want 2", got)
	}
}

// TestAppendAfterMissingFinalNewlineRewritesFile covers the other unterminated
// case: the last line parses fine but has no trailing newline.
func TestAppendAfterMissingFinalNewlineRewritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	line := `{"recorded_at":"2026-01-01T00:00:00Z","hash":"abc","method":"POST","path":"/v1/chat/completions","request_body":{"i":0},"response_status":200,"response_body":{"a":0}}`
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := c.Append(testInteraction(1)); err != nil {
		t.Fatalf("append: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Len() != 2 {
		t.Fatalf("reloaded %d interactions, want 2", reloaded.Len())
	}
}

// TestAppendDropsUnencodableInteraction is the audit M-28 blast-radius guard:
// one interaction that cannot be marshalled must not stop the rest of the
// recording session from being written.
func TestAppendDropsUnencodableInteraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)

	poisoned := &Interaction{
		Method:         "POST",
		Path:           "/v1/chat/completions",
		RequestBody:    json.RawMessage(`{not json`),
		ResponseStatus: 200,
	}
	if err := c.Append(poisoned); err != nil {
		t.Fatalf("a poisoned interaction should be dropped, not returned as an error: %v", err)
	}
	if err := c.Append(testInteraction(7)); err != nil {
		t.Fatalf("append after a dropped interaction: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Len() != 1 {
		t.Fatalf("reloaded %d interactions, want 1 (the good one)", reloaded.Len())
	}
	if !strings.Contains(string(reloaded.All()[0].ResponseBody), `"a":7`) {
		t.Errorf("wrong interaction survived: %s", reloaded.All()[0].ResponseBody)
	}
}

// TestAppendAllThenAppendKeepsOrder mixes the bulk and single-record paths,
// which share the flush cursor.
func TestAppendAllThenAppendKeepsOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cassette.jsonl")
	c := New(path)
	if err := c.AppendAll([]*Interaction{testInteraction(0), testInteraction(1)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Append(testInteraction(2)); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Len() != 3 {
		t.Fatalf("reloaded %d interactions, want 3", reloaded.Len())
	}
	for i, it := range reloaded.All() {
		want := fmt.Sprintf(`{"a":%d}`, i)
		if string(it.ResponseBody) != want {
			t.Errorf("interaction %d body = %s, want %s", i, it.ResponseBody, want)
		}
	}
}

// TestEncodeDecodeBodyRoundTrip covers the M-28 body wrapper: any byte string
// must survive storage and come back identical.
func TestEncodeDecodeBodyRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		body     []byte
		encoding string
	}{
		{"json object", []byte(`{"a":1}`), BodyEncodingJSON},
		{"json string", []byte(`"already a json string"`), BodyEncodingJSON},
		{"html error page", []byte("<html><body>502 Bad Gateway</body></html>"), BodyEncodingText},
		{"plain text", []byte("upstream unavailable"), BodyEncodingText},
		{"invalid utf-8", []byte{0xff, 0xfe, 0x00, 0x41}, BodyEncodingBase64},
		{"empty", nil, BodyEncodingJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, enc := EncodeBody(tc.body)
			if enc != tc.encoding {
				t.Fatalf("encoding = %q, want %q", enc, tc.encoding)
			}
			if len(raw) > 0 && !json.Valid(raw) {
				t.Fatalf("stored value is not valid JSON: %s", raw)
			}
			got := DecodeBody(raw, enc)
			if !bytes.Equal(got, tc.body) {
				t.Fatalf("round trip = %q, want %q", got, tc.body)
			}
		})
	}
}
