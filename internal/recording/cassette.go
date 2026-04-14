// Package recording implements record-and-playback for the MockAgents
// HTTP surface. A cassette is an append-only JSON-lines file holding
// Interaction records; the in-memory Cassette type indexes records by
// request hash so replay lookups are O(1).
package recording

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

// Interaction is a single captured request/response pair.
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
}

// Cassette is an in-memory collection of Interaction records indexed by
// request hash. Cassettes are loaded from a .jsonl file and appended to
// via Append, which rewrites the file (cassettes are small).
type Cassette struct {
	Path string

	mu           sync.RWMutex
	interactions []*Interaction
	byHash       map[string]*Interaction
}

// New creates an empty in-memory cassette. Path may be empty if the
// cassette is only used for unit tests.
func New(path string) *Cassette {
	return &Cassette{
		Path:   path,
		byHash: make(map[string]*Interaction),
	}
}

// Load reads a JSON-lines cassette file from disk. A missing file is not
// an error: it produces an empty cassette pinned to that path so subsequent
// Appends will create it.
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
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var it Interaction
		if err := json.Unmarshal(line, &it); err != nil {
			return nil, fmt.Errorf("parsing cassette line: %w", err)
		}
		c.interactions = append(c.interactions, &it)
		c.byHash[it.Hash] = &it
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading cassette: %w", err)
	}
	return c, nil
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
		it.Hash = HashRequest(it.Method, it.Path, it.RequestBody)
	}
	if it.RecordedAt.IsZero() {
		it.RecordedAt = time.Now().UTC()
	}

	c.mu.Lock()
	c.interactions = append(c.interactions, it)
	c.byHash[it.Hash] = it
	snapshot := append([]*Interaction(nil), c.interactions...)
	c.mu.Unlock()

	if c.Path == "" {
		return nil
	}
	return writeCassette(c.Path, snapshot)
}

// Lookup returns the interaction matching the given request hash, or nil.
func (c *Cassette) Lookup(hash string) *Interaction {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.byHash[hash]
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
	tmp, err := os.CreateTemp("", "cassette-*.jsonl")
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
