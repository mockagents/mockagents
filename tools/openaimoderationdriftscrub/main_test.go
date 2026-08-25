package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestScrubRedactsRequestID(t *testing.T) {
	input := `{"id":"modr-live","model":"omni-moderation-latest","results":[{"flagged":false}]}`
	var output bytes.Buffer
	if err := scrub(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "<redacted>" || got["model"] != "omni-moderation-latest" {
		t.Fatalf("response=%v", got)
	}
}

func TestScrubRejectsNonModerationResponse(t *testing.T) {
	if err := scrub(strings.NewReader(`{"error":{"message":"unauthorized"}}`), &bytes.Buffer{}); err == nil {
		t.Fatal("missing results accepted")
	}
}
