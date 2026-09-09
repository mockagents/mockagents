// Package recording implements record-and-playback for the MockAgents
// HTTP surface. A cassette is an append-only JSON-lines file holding
// Interaction records; the in-memory Cassette type indexes records by
// request hash so replay lookups are O(1).
package recording

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// Interaction is a single captured request/response pair.
//
// Streaming SSE responses are stored in StreamEvents (each chunk with
// a millisecond offset from the start of the response) instead of
// ResponseBody. Non-streaming JSON responses keep the old shape. A
// cassette can contain a mix of both.
type Interaction struct {
	RecordedAt      time.Time         `json:"recorded_at"`
	Hash            string            `json:"hash"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	RequestBody     json.RawMessage   `json:"request_body,omitempty"`
	ResponseStatus  int               `json:"response_status"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ResponseBody    json.RawMessage   `json:"response_body,omitempty"`
	// RequestBodyEncoding / ResponseBodyEncoding describe how a body that is
	// NOT valid JSON was wrapped for storage: "text" (JSON string) or "base64"
	// (JSON string of base64). Empty means the body is verbatim JSON, so every
	// cassette recorded before audit M-28 keeps its exact shape.
	RequestBodyEncoding  string `json:"request_body_encoding,omitempty"`
	ResponseBodyEncoding string `json:"response_body_encoding,omitempty"`
	// Streaming is true for captured Server-Sent Events responses.
	// When set, ResponseBody is empty and StreamEvents holds the
	// ordered chunks that were pushed to the client.
	Streaming    bool          `json:"streaming,omitempty"`
	StreamEvents []StreamEvent `json:"stream_events,omitempty"`
}

// StreamEvent is one chunk of a captured SSE response. Data holds the
// raw bytes exactly as the upstream server sent them (including
// `data: ...\n\n` framing). DelayMs is the offset in milliseconds
// from the start of the response, so replay can optionally re-honor
// the original pacing.
type StreamEvent struct {
	DelayMs int64  `json:"delay_ms"`
	Data    string `json:"data"`
}

// Cassette is an in-memory collection of Interaction records indexed by
// request hash. Cassettes are loaded from a .jsonl file and appended to
// via Append, which writes ONE line through an O_APPEND open/write/close
// (audit M-26): the old whole-file rewrite was O(n^2) over a recording session
// and ran outside the mutex, so two concurrent recorders could rename
// stale snapshots over each other and silently drop interactions.
type Cassette struct {
	Path string

	mu           sync.RWMutex
	interactions []*Interaction
	// byHash indexes interactions by request hash, in insertion order. A hash
	// can map to MULTIPLE interactions (a multi-turn loop replays them in
	// sequence — R-04); single-interaction hashes keep a one-element slice.
	byHash map[string][]*Interaction

	// writeMu serializes ALL disk writes and guards the three fields below, so
	// exactly one goroutine appends at a time and the file order matches the
	// in-memory order.
	writeMu sync.Mutex
	// flushed is the number of leading interactions already on disk. Each
	// writer persists interactions[flushed:] and advances it, so a concurrent
	// writer that already covered a record never writes it twice.
	flushed int
	// rewrite forces the next write to rebuild the whole file atomically
	// instead of appending — set when Load skipped a torn trailing line, whose
	// partial bytes would otherwise be concatenated with the next record.
	rewrite bool
}

// Body encodings for Interaction.RequestBodyEncoding / ResponseBodyEncoding.
// The zero value ("") means the body is valid JSON stored verbatim, which is
// what every cassette written before audit M-28 contains.
const (
	// BodyEncodingJSON stores the body verbatim as JSON.
	BodyEncodingJSON = ""
	// BodyEncodingText stores a non-JSON but valid-UTF-8 body as a JSON string.
	BodyEncodingText = "text"
	// BodyEncodingBase64 stores a body with non-UTF-8 bytes as base64 text.
	BodyEncodingBase64 = "base64"
)

// EncodeBody prepares an arbitrary body for storage in a cassette and returns
// the stored value plus its encoding. A non-JSON body (an HTML error page, a
// proxy's plain-text 502, a gzip fragment) used to be assigned straight to a
// json.RawMessage field, which made the whole interaction un-encodable and
// failed the cassette write (audit M-28). Wrapping it keeps every line valid
// JSON, and DecodeBody restores the original bytes byte-for-byte.
func EncodeBody(b []byte) (json.RawMessage, string) {
	if len(b) == 0 {
		return nil, BodyEncodingJSON
	}
	if json.Valid(b) {
		return json.RawMessage(b), BodyEncodingJSON
	}
	if utf8.Valid(b) {
		return json.RawMessage(strconv.Quote(string(b))), BodyEncodingText
	}
	return json.RawMessage(strconv.Quote(base64.StdEncoding.EncodeToString(b))), BodyEncodingBase64
}

// DecodeBody restores the bytes EncodeBody stored. An unknown encoding, or a
// value that does not decode, falls back to the raw stored bytes so an
// unreadable body degrades to "served as-is" rather than to an empty response.
func DecodeBody(raw json.RawMessage, encoding string) []byte {
	if len(raw) == 0 {
		return nil
	}
	switch encoding {
	case BodyEncodingText, BodyEncodingBase64:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return raw
		}
		if encoding == BodyEncodingText {
			return []byte(s)
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return raw
		}
		return decoded
	default:
		return raw
	}
}

// RequestBodyBytes returns the original request body bytes, decoding the
// wrapper EncodeBody applied to a non-JSON body.
func (i *Interaction) RequestBodyBytes() []byte {
	return DecodeBody(i.RequestBody, i.RequestBodyEncoding)
}

// ResponseBodyBytes returns the original response body bytes, decoding the
// wrapper EncodeBody applied to a non-JSON body.
func (i *Interaction) ResponseBodyBytes() []byte {
	return DecodeBody(i.ResponseBody, i.ResponseBodyEncoding)
}

// MaxCassetteLine bounds a single serialized interaction line on both the read
// side (Load's scanner) and the import write side (the VCR decompression cap and
// the OpenAI JSONL line buffer), so an importer can never produce a line larger
// than Load can read back.
const MaxCassetteLine = 32 << 20 // 32 MiB

// New creates an empty in-memory cassette. Path may be empty if the
// cassette is only used for unit tests.
func New(path string) *Cassette {
	return &Cassette{
		Path:   path,
		byHash: make(map[string][]*Interaction),
	}
}

// Load reads a JSON-lines cassette file from disk. A missing file is not
// an error: it produces an empty cassette pinned to that path so subsequent
// Appends will create it.
//
// A recorder killed mid-write leaves a torn FINAL line. That line is skipped
// with a warning and every interaction recorded before it is returned intact —
// one interrupted session must not cost the whole cassette. An unparseable
// line anywhere before the end is real corruption and still fails hard.
func Load(path string) (*Cassette, error) {
	c := New(path)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, fmt.Errorf("opening cassette %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxCassetteLine)
	// A parse failure is held back until we know whether another interaction
	// follows it, because only the trailing line can be a torn write.
	var (
		tornErr  error
		tornLine int
		lineNo   int
	)
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if tornErr != nil {
			return nil, fmt.Errorf("parsing cassette %s line %d: %w", path, tornLine, tornErr)
		}
		var it Interaction
		if err := json.Unmarshal(line, &it); err != nil {
			tornErr, tornLine = err, lineNo
			continue
		}
		c.interactions = append(c.interactions, &it)
		c.byHash[it.Hash] = append(c.byHash[it.Hash], c.interactions[len(c.interactions)-1])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading cassette: %w", err)
	}
	if tornErr != nil {
		slog.Warn("cassette ends in an unparseable line, most likely a recording interrupted mid-write; skipping it and keeping the interactions before it",
			"path", path, "line", tornLine, "kept", len(c.interactions), "error", tornErr)
	}
	// Everything just read is already on disk, so appends start after it. A
	// torn tail or a missing final newline can't be appended to safely — the
	// next record would be concatenated onto a partial line — so the first
	// write rebuilds the file atomically instead.
	c.flushed = len(c.interactions)
	c.rewrite = tornErr != nil || !endsWithNewline(f)
	return c, nil
}

// endsWithNewline reports whether the file's last byte is '\n'. An empty file
// counts as terminated: there is no partial line to append to.
func endsWithNewline(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if info.Size() == 0 {
		return true
	}
	var b [1]byte
	if _, err := f.ReadAt(b[:], info.Size()-1); err != nil {
		return false
	}
	return b[0] == '\n'
}

// Len returns the number of interactions in the cassette.
func (c *Cassette) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.interactions)
}

// Append adds an interaction and flushes the cassette to disk when Path
// is set. The hash is assigned by Append if blank.
func (c *Cassette) Append(it *Interaction) error {
	if it.Hash == "" {
		it.Hash = HashRequest(it.Method, it.Path, it.RequestBodyBytes())
	}
	if it.RecordedAt.IsZero() {
		it.RecordedAt = time.Now().UTC()
	}

	c.mu.Lock()
	c.interactions = append(c.interactions, it)
	c.byHash[it.Hash] = append(c.byHash[it.Hash], it)
	snapshot := append([]*Interaction(nil), c.interactions...)
	c.mu.Unlock()

	if c.Path == "" {
		return nil
	}
	return c.flush(snapshot)
}

// AppendAll adds many interactions and writes the cassette to disk ONCE. Hashes
// and timestamps are assigned exactly as Append does. This is the path bulk
// imports must use — calling Append N times would rewrite the whole file N times
// (O(n^2)). With an empty Path the interactions are only indexed in memory.
func (c *Cassette) AppendAll(interactions []*Interaction) error {
	now := time.Now().UTC()
	c.mu.Lock()
	for _, it := range interactions {
		if it.Hash == "" {
			it.Hash = HashRequest(it.Method, it.Path, it.RequestBodyBytes())
		}
		if it.RecordedAt.IsZero() {
			it.RecordedAt = now
		}
		c.interactions = append(c.interactions, it)
		c.byHash[it.Hash] = append(c.byHash[it.Hash], it)
	}
	snapshot := append([]*Interaction(nil), c.interactions...)
	c.mu.Unlock()

	if c.Path == "" {
		return nil
	}
	return c.flush(snapshot)
}

// flush persists every interaction in snapshot that is not on disk yet. It is
// the ONLY writer: writeMu serializes callers, so concurrent Appends append in
// memory order and a caller whose records another goroutine already wrote does
// nothing. An interaction that cannot be encoded is dropped with a warning
// rather than aborting the write (audit M-28) — one poisoned record must not
// stop the rest of a recording session from being saved.
func (c *Cassette) flush(snapshot []*Interaction) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.flushed >= len(snapshot) {
		return nil
	}

	// A torn trailing line from a killed recorder can't be appended to: rebuild
	// the file atomically once, then resume appending.
	if c.rewrite {
		if err := writeCassette(c.Path, snapshot); err != nil {
			return err
		}
		c.rewrite = false
		c.flushed = len(snapshot)
		return nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	pending := snapshot[c.flushed:]
	for _, it := range pending {
		start := buf.Len()
		if err := enc.Encode(it); err != nil {
			buf.Truncate(start)
			slog.Warn("cassette interaction could not be encoded; dropping it and keeping the rest of the recording",
				"path", c.Path, "method", it.Method, "request_path", it.Path, "error", err)
		}
	}
	// Advance regardless: a dropped record is gone, and holding it back would
	// make every later flush retry the same failure.
	c.flushed = len(snapshot)
	if buf.Len() == 0 {
		return nil
	}

	// The handle is opened per flush rather than held for the cassette's
	// lifetime: one batch is a single open/write/close, and nothing keeps the
	// file locked between recordings (on Windows an open handle blocks any
	// rename or delete of the cassette).
	f, err := os.OpenFile(c.Path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Lookup returns the FIRST interaction recorded for the given request hash, or
// nil. (For multi-turn sequenced replay use LookupSequence.)
func (c *Cassette) Lookup(hash string) *Interaction {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if s := c.byHash[hash]; len(s) > 0 {
		return s[0]
	}
	return nil
}

// LookupSequence returns the interactions recorded for the given hash, in
// insertion order (nil when none). The returned SLICE is independent
// (reassigning its elements does not affect the cassette), but the *Interaction
// values are SHARED with the cassette and must be treated as READ-ONLY — they
// are written once at record time and only read on replay.
func (c *Cassette) LookupSequence(hash string) []*Interaction {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := c.byHash[hash]
	if len(s) == 0 {
		return nil
	}
	out := make([]*Interaction, len(s))
	copy(out, s)
	return out
}

// All returns a copy of the interactions in insertion order.
func (c *Cassette) All() []*Interaction {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Interaction, len(c.interactions))
	copy(out, c.interactions)
	return out
}

// HashRequest produces a stable key for a request. The body is
// canonicalized by re-encoding the JSON with sorted keys so semantically
// equivalent requests with different whitespace or key order match.
func HashRequest(method, path string, body []byte) string {
	var canonical []byte
	if len(body) > 0 {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			canonical = canonicalJSON(parsed)
		}
	}
	if canonical == nil {
		canonical = body
	}
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{'\n'})
	h.Write([]byte(path))
	h.Write([]byte{'\n'})
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON returns a stable byte representation of any JSON value
// by sorting object keys at every nesting level.
func canonicalJSON(v any) []byte {
	var buf bytes.Buffer
	writeCanonical(&buf, v)
	return buf.Bytes()
}

func writeCanonical(w *bytes.Buffer, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				w.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			w.Write(kb)
			w.WriteByte(':')
			writeCanonical(w, t[k])
		}
		w.WriteByte('}')
	case []any:
		w.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				w.WriteByte(',')
			}
			writeCanonical(w, item)
		}
		w.WriteByte(']')
	default:
		b, _ := json.Marshal(t)
		w.Write(b)
	}
}

// writeCassette persists interactions to disk as JSON lines.
func writeCassette(path string, interactions []*Interaction) error {
	// Same directory as the target: os.Rename across filesystems fails with
	// EXDEV, and most container images mount /tmp as tmpfs (audit M-29).
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cassette-*.jsonl.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	bw := bufio.NewWriter(tmp)
	enc := json.NewEncoder(bw)
	for _, it := range interactions {
		if err := enc.Encode(it); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// CaptureHeaders copies a whitelist of headers from r into a plain map so
// cassettes don't accidentally hoard Authorization or cookies.
func CaptureHeaders(h http.Header, whitelist []string) map[string]string {
	out := make(map[string]string)
	for _, k := range whitelist {
		if v := h.Get(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// DrainBody reads r.Body and returns the bytes, leaving a fresh ReadCloser
// behind so downstream handlers can re-read.
func DrainBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// MaxRequestBodyBytes caps a request body the proxy or replay server will
// read. DrainBody used to be a bare io.ReadAll, so one oversized POST
// allocated without bound (audit M-22); provider APIs reject bodies far
// below this anyway.
const MaxRequestBodyBytes = 10 << 20 // 10 MiB

// drainStatus maps a DrainBody error to an HTTP status: 413 when the cap
// tripped, 400 otherwise.
func drainStatus(err error) int {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
