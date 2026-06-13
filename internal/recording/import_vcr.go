package recording

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxDecompressedBody caps the size of a gunzipped vcr body. It is tied to
// MaxCassetteLine so a decompressed body can never exceed what Load can read
// back (a too-large body is skipped per-interaction rather than producing an
// unreadable cassette), and it also bounds decompression bombs.
const maxDecompressedBody = MaxCassetteLine

// llmPaths is the default set of endpoints the importer keeps — the request
// shapes MockAgents actually replays. Use AllInteractions to import everything.
var llmPaths = map[string]bool{
	"/v1/chat/completions": true,
	"/v1/messages":         true,
	"/v1/embeddings":       true,
	"/v1/responses":        true,
	"/v1/moderations":      true,
}

// sensitiveHeaderSubstrings / sensitiveHeaderExact decide which headers are
// dropped from imported interactions so a cassette converted from someone's
// vcrpy recording never carries a live credential. Substring matching catches
// provider-specific names like x-goog-api-key, x-amz-security-token, and
// x-auth-token that an exact allowlist would miss.
var sensitiveHeaderSubstrings = []string{"auth", "api-key", "api_key", "apikey", "token", "secret", "cookie", "password"}

var sensitiveHeaderExact = map[string]bool{
	"openai-organization": true,
	"openai-project":      true,
}

func isSensitiveHeader(name string) bool {
	l := strings.ToLower(name)
	if sensitiveHeaderExact[l] {
		return true
	}
	for _, s := range sensitiveHeaderSubstrings {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// ImportVCROpts controls VCR import behavior.
type ImportVCROpts struct {
	// AllInteractions imports every interaction, not just POSTs to known LLM
	// paths.
	AllInteractions bool
}

// ImportResult summarizes an import run for the CLI to report.
type ImportResult struct {
	Imported    int
	Skipped     int
	SkipReasons []string
}

// vcr* mirror the vcrpy YAML cassette serialization.
type vcrCassette struct {
	Interactions []vcrInteraction `yaml:"interactions"`
}

type vcrInteraction struct {
	Request  vcrRequest  `yaml:"request"`
	Response vcrResponse `yaml:"response"`
}

type vcrRequest struct {
	Method  string              `yaml:"method"`
	URI     string              `yaml:"uri"`
	Body    vcrBody             `yaml:"body"`
	Headers map[string][]string `yaml:"headers"`
}

type vcrResponse struct {
	Status  vcrStatus           `yaml:"status"`
	Headers map[string][]string `yaml:"headers"`
	Body    vcrBody             `yaml:"body"`
}

type vcrStatus struct {
	Code    int    `yaml:"code"`
	Message string `yaml:"message"`
}

// vcrBody decodes the three vcrpy body shapes: a plain scalar string, a
// {string: ...} map, or a {base64_string: ...} map (which may be gzip'd).
type vcrBody struct {
	Raw     string // decoded, decompressed payload
	Present bool
	err     error // a fatal decode error (e.g. decompression bomb)
}

func (b *vcrBody) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		// `body: '...'` or `body:` (null tag → empty).
		if node.Tag == "!!null" {
			return nil
		}
		b.Raw, b.Present = node.Value, true
		return nil
	case yaml.MappingNode:
		// Decode once into a generic map: it is either a {string|base64_string}
		// wrapper, or an inline parsed-JSON body (vcrpy's JSON serializer renders
		// the request body as a YAML mapping). Any decode failure is a
		// per-interaction skip (b.err), never a parse-wide abort.
		var m map[string]any
		if err := node.Decode(&m); err != nil {
			b.err = fmt.Errorf("decoding body mapping: %w", err)
			return nil
		}
		if v, ok := m["string"]; ok {
			s, ok := v.(string)
			if !ok {
				b.err = fmt.Errorf("body.string is not a scalar")
				return nil
			}
			b.Raw, b.Present = s, true
			return nil
		}
		if v, ok := m["base64_string"]; ok {
			s, ok := v.(string)
			if !ok {
				b.err = fmt.Errorf("body.base64_string is not a scalar")
				return nil
			}
			raw, err := decodeBase64Body(s)
			if err != nil {
				b.err = err
				return nil
			}
			b.Raw, b.Present = raw, true
			return nil
		}
		// Inline parsed-JSON body: re-encode to JSON so it imports and (after
		// HashRequest canonicalization) hash-matches a client sending the same
		// body.
		jb, err := json.Marshal(m)
		if err != nil {
			b.err = fmt.Errorf("re-encoding inline body to JSON: %w", err)
			return nil
		}
		b.Raw, b.Present = string(jb), true
		return nil
	default:
		return nil
	}
}

// decodeBase64Body base64-decodes a vcr body and gunzips it when it carries the
// gzip magic, capping the decompressed size.
func decodeBase64Body(enc string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		// +1 so we can detect an over-limit body rather than silently truncating.
		out, err := io.ReadAll(io.LimitReader(gz, maxDecompressedBody+1))
		if err != nil {
			return "", fmt.Errorf("gunzip: %w", err)
		}
		if len(out) > maxDecompressedBody {
			return "", fmt.Errorf("decompressed body exceeds %d bytes (possible decompression bomb)", maxDecompressedBody)
		}
		return string(out), nil
	}
	return string(data), nil
}

// ImportVCR parses a vcrpy YAML cassette and returns MockAgents interactions.
// Hashes are left blank for AppendAll to assign. A malformed cassette is a hard
// error; an individual interaction that cannot be imported is skipped with a
// reason.
func ImportVCR(r io.Reader, opts ImportVCROpts) ([]*Interaction, ImportResult, error) {
	var cass vcrCassette
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&cass); err != nil {
		if err == io.EOF {
			return nil, ImportResult{}, nil // empty file: nothing to import
		}
		return nil, ImportResult{}, fmt.Errorf("parsing vcr cassette: %w", err)
	}

	var out []*Interaction
	var res ImportResult
	for i, vi := range cass.Interactions {
		method := strings.ToUpper(strings.TrimSpace(vi.Request.Method))
		path, err := uriToPath(vi.Request.URI)
		if err != nil {
			res.Skipped++
			res.SkipReasons = append(res.SkipReasons, fmt.Sprintf("interaction %d: bad uri %q: %v", i, vi.Request.URI, err))
			continue
		}
		if vi.Request.Body.err != nil {
			res.Skipped++
			res.SkipReasons = append(res.SkipReasons, fmt.Sprintf("interaction %d: request body: %v", i, vi.Request.Body.err))
			continue
		}
		if vi.Response.Body.err != nil {
			res.Skipped++
			res.SkipReasons = append(res.SkipReasons, fmt.Sprintf("interaction %d: response body: %v", i, vi.Response.Body.err))
			continue
		}
		if !opts.AllInteractions && (method != "POST" || !llmPaths[path]) {
			res.Skipped++
			res.SkipReasons = append(res.SkipReasons, fmt.Sprintf("interaction %d: %s %s — not a POST to a known LLM path (use --all to include)", i, method, path))
			continue
		}
		// A non-JSON request body hashes over its raw bytes; storing it
		// JSON-string-wrapped (so the cassette stays loadable) would not match a
		// raw client send, producing a dead interaction. Skip it with a reason.
		if vi.Request.Body.Present && !json.Valid([]byte(vi.Request.Body.Raw)) {
			res.Skipped++
			res.SkipReasons = append(res.SkipReasons, fmt.Sprintf("interaction %d: non-JSON request body cannot be replay-matched — skipping", i))
			continue
		}

		out = append(out, &Interaction{
			Method:          method,
			Path:            path,
			RequestHeaders:  flattenHeaders(vi.Request.Headers),
			RequestBody:     bodyToRawMessage(vi.Request.Body),
			ResponseStatus:  statusOr(vi.Response.Status.Code, 200),
			ResponseHeaders: flattenHeaders(vi.Response.Headers),
			ResponseBody:    bodyToRawMessage(vi.Response.Body),
		})
		res.Imported++
	}
	return out, res, nil
}

// uriToPath extracts the URL path from a full vcr request URI. The query string
// is dropped because HashRequest (and replay) key on r.URL.Path only. The path
// must be absolute — a relative/scheme-less URI yields a path no real client
// (which always sends a leading-slash absolute path) could hash-match, so it is
// rejected (and skipped per-interaction).
func uriToPath(uri string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "", fmt.Errorf("non-absolute path %q (relative or scheme-less uri?)", u.Path)
	}
	return u.Path, nil
}

// flattenHeaders turns vcrpy's list-valued headers into single-valued ones and
// drops credential-bearing headers so an imported cassette is safe to commit.
func flattenHeaders(h map[string][]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if isSensitiveHeader(k) || len(vs) == 0 {
			continue
		}
		out[k] = vs[0]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// bodyToRawMessage stores a JSON body verbatim (so its hash matches a client
// sending the same body) and wraps a non-JSON body as a JSON string so the
// cassette file stays valid. Non-JSON request bodies therefore won't hash-match
// a raw client send — an accepted limitation for --all of non-LLM traffic.
func bodyToRawMessage(b vcrBody) json.RawMessage {
	if !b.Present || b.Raw == "" {
		return nil
	}
	if json.Valid([]byte(b.Raw)) {
		return json.RawMessage(b.Raw)
	}
	return json.RawMessage(strconv.Quote(b.Raw))
}

func statusOr(code, def int) int {
	if code == 0 {
		return def
	}
	return code
}
