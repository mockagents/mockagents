package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestScrubRemovesVolatileAndThirdPartyValues(t *testing.T) {
	input := `{"query":"mock agents","answer":"summary","images":["https://image"],"results":[{"title":"title","url":"https://example","content":"body","raw_content":"raw","score":0.9,"published_date":"2026-08-25"}],"response_time":1.23,"request_id":"live-id"}`
	var output bytes.Buffer
	if err := scrub(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	result := got["results"].([]any)[0].(map[string]any)
	if got["request_id"] != "<redacted>" || got["response_time"] != float64(0) || got["answer"] != "<redacted>" || result["content"] != "<redacted>" {
		t.Fatalf("response=%v", got)
	}
}

func TestScrubRejectsNonSearchResponse(t *testing.T) {
	if err := scrub(strings.NewReader(`{"detail":{"error":"unauthorized"}}`), &bytes.Buffer{}); err == nil {
		t.Fatal("missing results accepted")
	}
}
