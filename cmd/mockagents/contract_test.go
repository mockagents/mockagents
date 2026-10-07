package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadContractRejectsIncompleteJSON(t *testing.T) {
	for _, body := range []string{`{}`, `{"name":"agent"}`, `{"name":"agent","protocol":"openai-chat-completions","tools":[{},{}]}`} {
		path := filepath.Join(t.TempDir(), "contract.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadContract(path); err == nil || !strings.Contains(err.Error(), "invalid contract JSON") {
			t.Fatalf("loadContract(%s) error = %v, want invalid contract JSON", body, err)
		}
	}
}

func TestLoadContractAcceptsMinimalMeaningfulJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, []byte(`{"name":"agent","protocol":"openai-chat-completions"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadContract(path); err != nil {
		t.Fatalf("loadContract error = %v", err)
	}
}

// 2026-10-06 review P-18 / C-21: a misspelled contract key decoded as an empty
// list, and a JSON agent definition was mistaken for a contract.
func TestLoadContract_StrictJSONAndAgentJSON(t *testing.T) {
	dir := t.TempDir()
	typo := filepath.Join(dir, "typo.json")
	if err := os.WriteFile(typo, []byte(`{"name":"agent","protocol":"openai-chat-completions","tool":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadContract(typo); err == nil || !strings.Contains(err.Error(), `unknown field "tool"`) {
		t.Fatalf("misspelled contract key: err = %v, want unknown field", err)
	}

	agent := filepath.Join(dir, "agent.json")
	body := `{"apiVersion":"mockagents/v1","kind":"Agent","metadata":{"name":"json-agent"},"spec":{"protocol":"openai-chat-completions","model":"m","behavior":{"scenarios":[{"name":"d","response":{"content":"x"}}]}}}`
	if err := os.WriteFile(agent, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadContract(agent)
	if err != nil {
		t.Fatalf("JSON agent definition: %v", err)
	}
	if c.Name != "json-agent" {
		t.Fatalf("contract name = %q, want json-agent", c.Name)
	}

	invalid := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("apiVersion: mockagents/v1\nkind: Agent\nmetadata:\n  name: a\nspec:\n  protocol: openai-chat-completions\n  behaviour: {}\n  behavior:\n    scenarios:\n      - name: d\n        response:\n          content: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadContract(invalid); err == nil || !strings.Contains(err.Error(), "not a valid agent definition") {
		t.Fatalf("invalid agent: err = %v", err)
	}
}
