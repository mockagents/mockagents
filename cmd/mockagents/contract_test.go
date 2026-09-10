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
