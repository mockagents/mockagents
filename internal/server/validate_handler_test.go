package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateHandler_RawYAMLValid(t *testing.T) {
	srv := httptest.NewServer(NewValidateHandler())
	defer srv.Close()

	body := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: hello
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        match:
          default: true
        response:
          content: "hi"
`
	resp, err := http.Post(srv.URL, "application/x-yaml", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.OK || out.Kind != "Agent" || len(out.Errors) != 0 {
		t.Errorf("out = %+v", out)
	}
}

func TestValidateHandler_RawYAMLInvalidReports200(t *testing.T) {
	// Validation failures are not HTTP failures — the handler returns
	// 200 with ok=false so the GUI can render the errors inline.
	srv := httptest.NewServer(NewValidateHandler())
	defer srv.Close()

	body := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: bad
spec:
  protocol: not-a-protocol
  behavior:
    scenarios:
      - name: default
        match:
          default: true
        response:
          content: "hi"
`
	resp, err := http.Post(srv.URL, "application/x-yaml", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.OK {
		t.Error("expected ok=false")
	}
	if len(out.Errors) == 0 {
		t.Error("expected errors")
	}
}

func TestValidateHandler_JSONWrapper(t *testing.T) {
	srv := httptest.NewServer(NewValidateHandler())
	defer srv.Close()

	payload := map[string]string{
		"yaml": `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: wrapped
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        match:
          default: true
        response:
          content: "hi"
`,
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Errorf("out = %+v", out)
	}
	if out.Kind != "Agent" {
		t.Errorf("kind = %q", out.Kind)
	}
}

func TestValidateHandler_MethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(NewValidateHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestValidateHandler_OversizedBody(t *testing.T) {
	// X-DOS-001: a body over the cap must be rejected with 413 rather than
	// read unbounded into memory.
	srv := httptest.NewServer(NewValidateHandler())
	defer srv.Close()

	big := strings.Repeat("x", maxValidateBodyBytes+1024)
	resp, err := http.Post(srv.URL, "application/x-yaml", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}
