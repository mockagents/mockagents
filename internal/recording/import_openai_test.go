package recording

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIImportEnvelopeShapeThenReplay(t *testing.T) {
	jsonl := `{"request":{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]},"response":{"id":"chatcmpl-1","choices":[{"message":{"content":"envelope reply"}}]}}`
	its, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v reasons=%v", res.Imported, err, res.SkipReasons)
	}
	if its[0].Path != "/v1/chat/completions" || its[0].Method != "POST" {
		t.Errorf("wrong method/path: %s %s", its[0].Method, its[0].Path)
	}

	cass := New("")
	if err := cass.AppendAll(its); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewReplay(cass))
	defer srv.Close()
	resp, body := post(t, srv.URL, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != 200 || !strings.Contains(body, "envelope reply") {
		t.Fatalf("envelope import did not replay: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestOpenAIImportFlatShapeWithInputRoutesToResponses(t *testing.T) {
	// A Responses-API stored completion (input, no messages) keeps the `input`
	// key and targets /v1/responses so it hash-matches the originating client.
	jsonl := `{"id":"resp-2","model":"gpt-4o","input":[{"role":"user","content":"flat"}],"temperature":0.7,"choices":[{"message":{"content":"flat reply"}}]}`
	its, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v reasons=%v", res.Imported, err, res.SkipReasons)
	}
	if its[0].Path != "/v1/responses" {
		t.Errorf("input shape should route to /v1/responses, got %s", its[0].Path)
	}
	var req map[string]any
	if err := json.Unmarshal(its[0].RequestBody, &req); err != nil {
		t.Fatalf("reconstructed request not JSON: %v", err)
	}
	if req["model"] != "gpt-4o" {
		t.Errorf("model not carried into request: %v", req["model"])
	}
	if _, ok := req["input"]; !ok {
		t.Errorf("input key must be preserved (not renamed to messages): %v", req)
	}
	if req["temperature"] != 0.7 {
		t.Errorf("sampling param not copied: %v", req["temperature"])
	}
}

func TestOpenAIImportFlatShapeMessagesRoutesToChat(t *testing.T) {
	jsonl := `{"model":"gpt-4o","messages":[{"role":"user","content":"hey"}],"choices":[{"message":{"content":"r"}}]}`
	its, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
	if its[0].Path != "/v1/chat/completions" {
		t.Errorf("messages shape should route to /v1/chat/completions, got %s", its[0].Path)
	}
}

func TestOpenAIImportFlatShapeWithMessages(t *testing.T) {
	jsonl := `{"model":"gpt-4o","messages":[{"role":"user","content":"hey"}],"choices":[{"message":{"content":"r"}}]}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Imported != 1 {
		t.Fatalf("imported=%d err=%v", res.Imported, err)
	}
}

func TestOpenAIImportMissingInputSkipped(t *testing.T) {
	// Flat shape with choices but no input/messages → cannot reconstruct.
	jsonl := `{"model":"gpt-4o","choices":[{"message":{"content":"r"}}]}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 || res.Skipped != 1 {
		t.Fatalf("missing-input line should be skipped: imported=%d skipped=%d", res.Imported, res.Skipped)
	}
}

func TestOpenAIImportUnrecognizedShapeSkipped(t *testing.T) {
	jsonl := `{"foo":"bar"}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Skipped != 1 {
		t.Fatalf("unrecognized line should skip: skipped=%d err=%v", res.Skipped, err)
	}
}

func TestOpenAIImportMalformedLineSkippedRestProcessed(t *testing.T) {
	jsonl := "not json at all\n" +
		`{"request":{"model":"x"},"response":{"choices":[]}}` + "\n"
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Skipped != 1 {
		t.Fatalf("bad line should skip, good line process: imported=%d skipped=%d", res.Imported, res.Skipped)
	}
}

func TestOpenAIImportEnvelopeMissingResponse(t *testing.T) {
	jsonl := `{"request":{"model":"x"}}`
	_, res, err := ImportOpenAIStored(strings.NewReader(jsonl))
	if err != nil || res.Skipped != 1 {
		t.Fatalf("envelope without response should skip: skipped=%d err=%v", res.Skipped, err)
	}
}

func TestAppendAllWritesOnceAndReloads(t *testing.T) {
	path := t.TempDir() + "/out.jsonl"
	cass := New(path)
	its := []*Interaction{
		{Method: "POST", Path: "/v1/chat/completions", RequestBody: json.RawMessage(`{"m":1}`), ResponseStatus: 200, ResponseBody: json.RawMessage(`{"a":1}`)},
		{Method: "POST", Path: "/v1/chat/completions", RequestBody: json.RawMessage(`{"m":2}`), ResponseStatus: 200, ResponseBody: json.RawMessage(`{"a":2}`)},
		{Method: "POST", Path: "/v1/chat/completions", RequestBody: json.RawMessage(`{"m":1}`), ResponseStatus: 200, ResponseBody: json.RawMessage(`{"a":3}`)},
	}
	if err := cass.AppendAll(its); err != nil {
		t.Fatal(err)
	}
	if cass.Len() != 3 {
		t.Fatalf("len = %d, want 3", cass.Len())
	}
	// Hashes were assigned; the two {"m":1} requests share a hash sequence.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Len() != 3 {
		t.Fatalf("reloaded len = %d, want 3", reloaded.Len())
	}
	seq := reloaded.LookupSequence(HashRequest("POST", "/v1/chat/completions", json.RawMessage(`{"m":1}`)))
	if len(seq) != 2 {
		t.Fatalf("duplicate-hash sequence len = %d, want 2", len(seq))
	}
	if !strings.Contains(string(seq[0].ResponseBody), `"a":1`) || !strings.Contains(string(seq[1].ResponseBody), `"a":3`) {
		t.Errorf("AppendAll did not preserve insertion order: %s / %s", seq[0].ResponseBody, seq[1].ResponseBody)
	}
}
