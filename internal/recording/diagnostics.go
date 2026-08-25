package recording

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"unicode/utf8"
)

// missResponse is the JSON body returned on a replay miss. It carries the
// request's hash plus, when the cassette is non-empty, the nearest recorded
// interaction and a field-level diff so a drifted request is debuggable.
type missResponse struct {
	Error   string        `json:"error"`
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Hash    string        `json:"hash"`
	Nearest *nearestMatch `json:"nearest,omitempty"`
}

type nearestMatch struct {
	Hash       string      `json:"hash"`
	Method     string      `json:"method"`
	Path       string      `json:"path"`
	Similarity float64     `json:"similarity"`
	Diff       []diffEntry `json:"diff"`
}

// diffEntry is one top-level field difference between the incoming request and
// the nearest recorded request. Kind is changed | missing_in_request |
// extra_in_request | truncated. Values are JSON fragments (truncated when long).
type diffEntry struct {
	Field         string          `json:"field"`
	Kind          string          `json:"kind"`
	CassetteValue json.RawMessage `json:"cassette_value"`
	RequestValue  json.RawMessage `json:"request_value"`
}

const (
	maxDiffEntries  = 25
	maxDiffValueLen = 200
)

// serveMissDiagnostics writes a structured response describing the miss and (when the
// cassette is non-empty) the nearest recorded interaction with a field diff.
func (rp *Replay) serveMissDiagnostics(w http.ResponseWriter, method, path, hash string, body []byte) {
	resp := missResponse{
		Error:   "no cassette match",
		Method:  method,
		Path:    path,
		Hash:    shortHash(hash),
		Nearest: nearestDiff(rp.Cassette, method, path, body),
	}
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusNotFound
	state := "miss"
	if rp.Strict {
		status = http.StatusServiceUnavailable
		state = "miss-strict"
	}
	w.Header().Set("X-Mockagents-Replay", state)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}

// nearestDiff finds the recorded interaction closest to the incoming request by
// top-level field overlap and returns it plus a bounded field diff. Only
// interactions on the SAME method+path are considered — reporting a "nearest"
// on a different endpoint would surface a misleading similarity (e.g. 1.0 for an
// unrelated path). Returns nil when no recording exists for this endpoint.
func nearestDiff(c *Cassette, method, path string, body []byte) *nearestMatch {
	var candidates []*Interaction
	for _, it := range c.All() {
		if it.Method == method && it.Path == path {
			candidates = append(candidates, it)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	reqMap, reqOK := bodyMap(body)
	best := -1.0
	var bestIt *Interaction
	var bestMap map[string]any
	var bestOK bool
	for _, it := range candidates {
		candMap, candOK := bodyMap(it.RequestBody)
		// Tie-break on earliest insertion order: strict ">" keeps the first.
		if s := scoreBody(reqMap, reqOK, body, candMap, candOK, it.RequestBody); s > best {
			best, bestIt, bestMap, bestOK = s, it, candMap, candOK
		}
	}
	if bestIt == nil {
		return nil
	}
	return &nearestMatch{
		Hash:       shortHash(bestIt.Hash),
		Method:     bestIt.Method,
		Path:       bestIt.Path,
		Similarity: round2(best),
		// Reuse the winner's already-decoded map — no second parse.
		Diff: buildDiff(reqMap, reqOK, body, bestMap, bestOK, bestIt.RequestBody),
	}
}

// scoreBody is the Jaccard similarity of matching top-level key/value pairs in
// [0,1]. Non-object bodies score 1.0 when canonically equal, else 0.0. The
// candidate map is decoded by the caller and passed in to avoid re-parsing.
func scoreBody(reqMap map[string]any, reqOK bool, reqBody []byte, candMap map[string]any, candOK bool, candBody []byte) float64 {
	if !reqOK || !candOK {
		if bytes.Equal(canonicalize(reqBody), canonicalize(candBody)) {
			return 1.0
		}
		return 0.0
	}
	union := make(map[string]struct{}, len(reqMap)+len(candMap))
	for k := range reqMap {
		union[k] = struct{}{}
	}
	for k := range candMap {
		union[k] = struct{}{}
	}
	if len(union) == 0 {
		return 1.0
	}
	matching := 0
	for k := range union {
		rv, rok := reqMap[k]
		cv, cok := candMap[k]
		if rok && cok && equalJSON(rv, cv) {
			matching++
		}
	}
	return float64(matching) / float64(len(union))
}

// buildDiff lists the top-level field differences, grouped changed → missing →
// extra (each alphabetical), bounded to maxDiffEntries with a truncation
// sentinel. The candidate map is decoded by the caller and passed in.
func buildDiff(reqMap map[string]any, reqOK bool, reqBody []byte, candMap map[string]any, candOK bool, candBody []byte) []diffEntry {
	if !reqOK || !candOK {
		if bytes.Equal(canonicalize(reqBody), canonicalize(candBody)) {
			return nil
		}
		return []diffEntry{{
			Field:         "(body)",
			Kind:          "changed",
			CassetteValue: diffValue(rawAny(candBody)),
			RequestValue:  diffValue(rawAny(reqBody)),
		}}
	}

	keys := make(map[string]struct{}, len(reqMap)+len(candMap))
	for k := range reqMap {
		keys[k] = struct{}{}
	}
	for k := range candMap {
		keys[k] = struct{}{}
	}

	var changed, missing, extra []diffEntry
	for k := range keys {
		rv, rok := reqMap[k]
		cv, cok := candMap[k]
		switch {
		case rok && cok:
			if !equalJSON(rv, cv) {
				changed = append(changed, diffEntry{Field: k, Kind: "changed", CassetteValue: diffValue(cv), RequestValue: diffValue(rv)})
			}
		case cok && !rok:
			missing = append(missing, diffEntry{Field: k, Kind: "missing_in_request", CassetteValue: diffValue(cv), RequestValue: json.RawMessage("null")})
		case rok && !cok:
			extra = append(extra, diffEntry{Field: k, Kind: "extra_in_request", CassetteValue: json.RawMessage("null"), RequestValue: diffValue(rv)})
		}
	}
	sortByField(changed)
	sortByField(missing)
	sortByField(extra)

	out := make([]diffEntry, 0, len(changed)+len(missing)+len(extra))
	out = append(out, changed...)
	out = append(out, missing...)
	out = append(out, extra...)
	if len(out) > maxDiffEntries {
		omitted := len(out) - maxDiffEntries
		out = out[:maxDiffEntries:maxDiffEntries]
		out = append(out, diffEntry{
			Field:         fmt.Sprintf("(+%d more fields omitted)", omitted),
			Kind:          "truncated",
			CassetteValue: json.RawMessage("null"),
			RequestValue:  json.RawMessage("null"),
		})
	}
	return out
}

// diffValue renders a decoded JSON value as a JSON fragment, truncating long
// values to a JSON string so the 404 body stays readable.
func diffValue(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	if len(b) <= maxDiffValueLen {
		return json.RawMessage(b)
	}
	cut := maxDiffValueLen - 3
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	truncated := string(b[:cut]) + "…"
	s, err := json.Marshal(truncated)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}

// bodyMap decodes a JSON-object body into a map. The bool is false for an empty,
// non-JSON, or non-object body. Numbers decode to float64 (NOT json.Number) so
// the diff's notion of equality matches the matcher's: HashRequest also uses
// float64, treating 1, 1.0, and 1e0 as the same value. Using json.Number here
// would flag numerically-equal-but-differently-spelled numbers as spurious diffs.
func bodyMap(body []byte) (map[string]any, bool) {
	if len(body) == 0 {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// rawAny decodes a body to any; a non-JSON body becomes its raw string so
// diffValue can still render it.
func rawAny(body []byte) any {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	return v
}

// canonicalize returns a stable byte form of a body for equality comparison,
// using the same float64 number normalization as HashRequest.
func canonicalize(body []byte) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}
	return canonicalJSON(v)
}

func equalJSON(a, b any) bool {
	return bytes.Equal(canonicalJSON(a), canonicalJSON(b))
}

func sortByField(d []diffEntry) {
	sort.Slice(d, func(i, j int) bool { return d[i].Field < d[j].Field })
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
