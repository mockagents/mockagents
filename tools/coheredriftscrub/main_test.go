package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestScrubRemovesVolatileAndBillingFields(t *testing.T) {
	input := `{"id":"live-request-id","results":[{"index":0,"relevance_score":0.9}],"meta":{"api_version":{"version":"2"},"billed_units":{"search_units":1},"tokens":{"input_tokens":12}}}`
	var output bytes.Buffer
	if err := scrub(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "<redacted>" {
		t.Fatalf("id=%v", got["id"])
	}
	meta := got["meta"].(map[string]any)
	if meta["billed_units"] != nil || meta["tokens"] != nil || meta["api_version"] == nil {
		t.Fatalf("meta=%v", meta)
	}
}

func TestScrubRejectsNonRerankResponse(t *testing.T) {
	if err := scrub(strings.NewReader(`{"message":"unauthorized"}`), &bytes.Buffer{}); err == nil {
		t.Fatal("missing results accepted")
	}
}
