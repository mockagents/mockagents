package recording

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
)

// A minimal vcrpy cassette: one POST to chat/completions.
const vcrSimple = `
interactions:
- request:
    method: POST
    uri: https://api.openai.com/v1/chat/completions?beta=1
    body: '{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}'
    headers:
      Content-Type: [application/json]
      Authorization: [Bearer sk-secret]
  response:
    status: {code: 200, message: OK}
    headers:
      Content-Type: [application/json]
    body: {string: '{"id":"chatcmpl-1","choices":[{"message":{"content":"hello from vcr"}}]}'}
version: 1
`

// PLAIN_B64 / GZIP_B64 generated once (see below) and embedded as literals.
const (
	plainBodyB64 = "eyJjaG9pY2VzIjpbeyJtZXNzYWdlIjp7ImNvbnRlbnQiOiJmcm9tIGJhc2U2NCJ9fV19"
	gzipBodyB64  = "H4sIAAAAAAAC/6tWSs7Iz0xOLVayiq5Wyk0tLk5MT1WyqlZKzs8rSc0rUbJSSivKz1VIr8osUKqtja0FAHERKygxAAAA"
)

func TestVCRImportThenReplay(t *testing.T) {
	interactions, res, err := ImportVCR(strings.NewReader(vcrSimple), ImportVCROpts{})
	if err != nil {
		t.Fatalf("ImportVCR: %v", err)
	}
	if res.Imported != 1 || res.Skipped != 0 {
		t.Fatalf("imported=%d skipped=%d, want 1/0 (%v)", res.Imported, res.Skipped, res.SkipReasons)
	}
	// Credential header must be stripped.
	if _, ok := interactions[0].RequestHeaders["Authorization"]; ok {
		t.Error("Authorization header must be dropped on import")
	}
	// Path must be the URL path only (query dropped).
	if interactions[0].Path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", interactions[0].Path)
	}

	cass := New("")
	if err := cass.AppendAll(interactions); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewReplay(cass))
	defer srv.Close()

	// Acceptance: the imported vcrpy cassette replays through MockAgents.
	resp, body := post(t, srv.URL, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 hit; body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Mockagents-Replay") != "hit" {
		t.Errorf("expected replay hit, got %q", resp.Header.Get("X-Mockagents-Replay"))
	}
	if !strings.Contains(body, "hello from vcr") {
		t.Errorf("replayed body = %s", body)
	}
}

func TestVCRImportBase64Body(t *testing.T) {
	yaml := `
interactions:
- request:
    method: POST
    uri: https://api.openai.com/v1/chat/completions
    body: '{"model":"x"}'
  response:
    status: {code: 200}
    body: {base64_string: '` + plainBodyB64 + `'}
`
	its, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
	if !strings.Contains(string(its[0].ResponseBody), "from base64") {
		t.Errorf("base64 body not decoded: %s", its[0].ResponseBody)
	}
}

func TestVCRImportGzippedBase64Body(t *testing.T) {
	yaml := `
interactions:
- request:
    method: POST
    uri: https://api.openai.com/v1/chat/completions
    body: '{"model":"x"}'
  response:
    status: {code: 200}
    body: {base64_string: '` + gzipBodyB64 + `'}
`
	its, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
	if !strings.Contains(string(its[0].ResponseBody), "from gzip") {
		t.Errorf("gzip body not decompressed: %s", its[0].ResponseBody)
	}
}

func TestVCRImportFilterAndAllFlag(t *testing.T) {
	yaml := `
interactions:
- request:
    method: GET
    uri: https://api.openai.com/healthz
    body:
  response:
    status: {code: 200}
    body: {string: 'ok'}
- request:
    method: POST
    uri: https://api.openai.com/v1/chat/completions
    body: '{"model":"x"}'
  response:
    status: {code: 200}
    body: {string: '{"ok":true}'}
`
	// Default: GET /healthz skipped, the POST kept.
	its, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Skipped != 1 {
		t.Fatalf("default imported=%d skipped=%d, want 1/1", res.Imported, res.Skipped)
	}
	if its[0].Path != "/v1/chat/completions" {
		t.Errorf("kept the wrong interaction: %s", its[0].Path)
	}
	// --all: both imported.
	_, resAll, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{AllInteractions: true})
	if err != nil {
		t.Fatal(err)
	}
	if resAll.Imported != 2 || resAll.Skipped != 0 {
		t.Fatalf("--all imported=%d skipped=%d, want 2/0", resAll.Imported, resAll.Skipped)
	}
}

func TestVCRImportDuplicateHashOrder(t *testing.T) {
	one := `
interactions:
- request: {method: POST, uri: https://api.openai.com/v1/chat/completions, body: '{"m":1}'}
  response: {status: {code: 200}, body: {string: '{"turn":1}'}}
- request: {method: POST, uri: https://api.openai.com/v1/chat/completions, body: '{"m":1}'}
  response: {status: {code: 200}, body: {string: '{"turn":2}'}}
`
	its, res, err := ImportVCR(strings.NewReader(one), ImportVCROpts{})
	if err != nil || res.Imported != 2 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
	cass := New("")
	if err := cass.AppendAll(its); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewReplay(cass))
	defer srv.Close()
	// R-04 sequencing: identical requests replay in recorded order.
	_, b1 := post(t, srv.URL, `{"m":1}`)
	_, b2 := post(t, srv.URL, `{"m":1}`)
	if !strings.Contains(b1, `"turn":1`) || !strings.Contains(b2, `"turn":2`) {
		t.Errorf("sequence not preserved: b1=%s b2=%s", b1, b2)
	}
}

func TestVCRImportEmptyFile(t *testing.T) {
	its, res, err := ImportVCR(strings.NewReader(""), ImportVCROpts{})
	if err != nil {
		t.Fatalf("empty file must not error: %v", err)
	}
	if len(its) != 0 || res.Imported != 0 {
		t.Errorf("empty file should import nothing, got %d", res.Imported)
	}
}

func TestVCRImportMalformedYAML(t *testing.T) {
	if _, _, err := ImportVCR(strings.NewReader("\tnot: [valid: yaml"), ImportVCROpts{}); err == nil {
		t.Error("malformed YAML must return a hard error")
	}
}

func TestVCRImportNullBody(t *testing.T) {
	yaml := `
interactions:
- request: {method: POST, uri: https://api.openai.com/v1/chat/completions, body: }
  response: {status: {code: 200}, body: {string: '{"ok":true}'}}
`
	its, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
	if its[0].RequestBody != nil {
		t.Errorf("null body should yield nil RequestBody, got %s", its[0].RequestBody)
	}
}

func TestVCRImportParsedJSONDictBodyThenReplay(t *testing.T) {
	// vcrpy's JSON serializer renders the request body as a YAML mapping, not a
	// scalar string. It must import AND replay (re-serialized to JSON, then
	// hash-matched against a client sending the equivalent JSON).
	yaml := `
interactions:
- request:
    method: POST
    uri: https://api.openai.com/v1/chat/completions
    body:
      model: gpt-4o
      messages:
      - role: user
        content: hi
      temperature: 0.7
  response:
    status: {code: 200}
    body: {string: '{"choices":[{"message":{"content":"dict body reply"}}]}'}
`
	its, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil {
		t.Fatalf("parsed-dict body must not hard-fail import: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("imported=%d skipped=%d (%v)", res.Imported, res.Skipped, res.SkipReasons)
	}
	cass := New("")
	if err := cass.AppendAll(its); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewReplay(cass))
	defer srv.Close()
	// Client sends equivalent JSON (different key order) — canonicalization matches.
	resp, body := post(t, srv.URL, `{"messages":[{"role":"user","content":"hi"}],"model":"gpt-4o","temperature":0.7}`)
	if resp.StatusCode != 200 || !strings.Contains(body, "dict body reply") {
		t.Fatalf("parsed-dict body did not replay: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestVCRImportDictWrapperNonScalarSkipsNotHardFails(t *testing.T) {
	// {string: <map>} is malformed; it must be a per-interaction skip, and a
	// following good interaction must still import.
	yaml := `
interactions:
- request: {method: POST, uri: https://api.openai.com/v1/chat/completions, body: {string: {nested: x}}}
  response: {status: {code: 200}, body: {string: '{}'}}
- request: {method: POST, uri: https://api.openai.com/v1/chat/completions, body: '{"m":1}'}
  response: {status: {code: 200}, body: {string: '{"ok":true}'}}
`
	_, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil {
		t.Fatalf("must not hard-fail: %v", err)
	}
	if res.Imported != 1 || res.Skipped != 1 {
		t.Fatalf("imported=%d skipped=%d, want 1/1 (%v)", res.Imported, res.Skipped, res.SkipReasons)
	}
}

func TestVCRImportBroadHeaderRedaction(t *testing.T) {
	yaml := `
interactions:
- request:
    method: POST
    uri: https://api.openai.com/v1/chat/completions
    body: '{"m":1}'
    headers:
      Content-Type: [application/json]
      X-Goog-Api-Key: [AIzaLEAK]
      X-Amz-Security-Token: [amztoken]
      Authentication: [Bearer leak]
      Openai-Organization: [org-123]
  response: {status: {code: 200}, body: {string: '{"ok":true}'}}
`
	its, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
	h := its[0].RequestHeaders
	for _, k := range []string{"X-Goog-Api-Key", "X-Amz-Security-Token", "Authentication", "Openai-Organization"} {
		if _, ok := h[k]; ok {
			t.Errorf("credential-bearing header %q must be dropped, got %q", k, h[k])
		}
	}
	if h["Content-Type"] != "application/json" {
		t.Errorf("non-sensitive header should be kept: %v", h)
	}
}

func TestVCRImportNonJSONRequestBodySkipped(t *testing.T) {
	yaml := `
interactions:
- request: {method: POST, uri: https://api.openai.com/v1/chat/completions, body: 'plain text not json'}
  response: {status: {code: 200}, body: {string: 'ok'}}
`
	_, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 || res.Skipped != 1 {
		t.Fatalf("non-JSON request body should skip: imported=%d skipped=%d", res.Imported, res.Skipped)
	}
	if len(res.SkipReasons) == 0 || !strings.Contains(res.SkipReasons[0], "non-JSON request") {
		t.Errorf("skip reason should explain: %v", res.SkipReasons)
	}
}

func TestVCRImportDecompBombRejected(t *testing.T) {
	// Build a gzip stream that decompresses past the cap, base64 it, feed it in.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	zeros := make([]byte, 1<<20)
	for i := 0; i < 65; i++ { // 65 MiB > 64 MiB cap
		_, _ = gz.Write(zeros)
	}
	_ = gz.Close()
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	yaml := `
interactions:
- request: {method: POST, uri: https://api.openai.com/v1/chat/completions, body: '{"m":1}'}
  response: {status: {code: 200}, body: {base64_string: '` + b64 + `'}}
`
	_, res, err := ImportVCR(strings.NewReader(yaml), ImportVCROpts{})
	if err != nil {
		t.Fatalf("a decomp bomb should be a per-interaction skip, not a hard error: %v", err)
	}
	if res.Imported != 0 || res.Skipped != 1 {
		t.Fatalf("bomb must be skipped: imported=%d skipped=%d", res.Imported, res.Skipped)
	}
	if len(res.SkipReasons) == 0 || !strings.Contains(res.SkipReasons[0], "bomb") {
		t.Errorf("skip reason should mention the bomb: %v", res.SkipReasons)
	}
}
