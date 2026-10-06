package config

import (
	"strings"
	"testing"
)

func TestValidateBytes_ValidAgent(t *testing.T) {
	yaml := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: hello
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: default
        response:
          content: "hi"
`
	r := ValidateBytes([]byte(yaml))
	if r.Kind != "Agent" {
		t.Errorf("kind = %q", r.Kind)
	}
	if len(r.Errors) != 0 {
		t.Errorf("unexpected errors: %+v", r.Errors)
	}
}

func TestValidateBytes_InvalidProtocol(t *testing.T) {
	yaml := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: bad
spec:
  protocol: not-a-protocol
  behavior:
    scenarios:
      - name: default
        response:
          content: "hi"
`
	r := ValidateBytes([]byte(yaml))
	if len(r.Errors) == 0 {
		t.Fatal("expected protocol error")
	}
	var found bool
	for _, e := range r.Errors {
		if strings.Contains(e.Message, "protocol") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no error mentions protocol: %+v", r.Errors)
	}
}

func TestValidateBytes_YAMLParseError(t *testing.T) {
	// Mis-indented value that yaml.v3 will choke on.
	yaml := `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: broken
  extra:
  - unclosed: [1, 2
`
	r := ValidateBytes([]byte(yaml))
	if len(r.Errors) == 0 {
		t.Fatal("expected parse error")
	}
	if r.Errors[0].Field != "document" {
		t.Errorf("field = %q", r.Errors[0].Field)
	}
}

func TestValidateBytes_Empty(t *testing.T) {
	r := ValidateBytes([]byte("   \n  \t  "))
	if len(r.Errors) == 0 {
		t.Fatal("expected empty error")
	}
	if !strings.Contains(r.Errors[0].Message, "empty") {
		t.Errorf("message = %q", r.Errors[0].Message)
	}
}

func TestValidateBytes_UnknownKind(t *testing.T) {
	yaml := `apiVersion: mockagents/v1
kind: Weasel
metadata:
  name: huh
`
	r := ValidateBytes([]byte(yaml))
	if len(r.Errors) == 0 {
		t.Fatal("expected unknown-kind error")
	}
	if r.Kind != "Weasel" {
		t.Errorf("kind = %q", r.Kind)
	}
	if !strings.Contains(r.Errors[0].Message, "unknown kind") {
		t.Errorf("message = %q", r.Errors[0].Message)
	}
}

func TestValidateBytes_PipelineKind(t *testing.T) {
	yaml := `apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: research
spec:
  topology: sequential
  agents:
    - id: writer
      ref: summary-writer
    - id: reviewer
      ref: fact-checker
`
	r := ValidateBytes([]byte(yaml))
	if r.Kind != "Pipeline" {
		t.Errorf("kind = %q", r.Kind)
	}
	if len(r.Errors) != 0 {
		t.Errorf("unexpected errors: %+v", r.Errors)
	}
}

func TestValidateBytes_JSONInput(t *testing.T) {
	body := `{"apiVersion":"mockagents/v1","kind":"Agent","metadata":{"name":"json-agent"},"spec":{"protocol":"openai-chat-completions","model":"gpt-4o","behavior":{"scenarios":[{"name":"default","response":{"content":"hi"}}]}}}`
	r := ValidateBytes([]byte(body))
	if r.Kind != "Agent" {
		t.Errorf("kind = %q", r.Kind)
	}
	if len(r.Errors) != 0 {
		t.Errorf("unexpected errors: %+v", r.Errors)
	}
}
