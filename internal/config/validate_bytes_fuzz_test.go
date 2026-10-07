package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FuzzValidateBytes drives the in-memory validator behind the GUI editor
// and POST /api/v1/config/validate with arbitrary bytes. That endpoint
// accepts caller-supplied YAML/JSON, so the parser + every kind-specific
// validator must hold up against hostile input.
//
// Invariants:
//   - ValidateBytes never panics and always returns a non-nil report;
//   - the report is deterministic: validating the same bytes twice yields
//     byte-identical JSON (same kind, same errors and warnings, same order);
//   - every reported error/warning is non-nil (the HTTP handler serialises
//     the slices straight to the wire).
func FuzzValidateBytes(f *testing.F) {
	// Seed with every shipped example document (agents, pipelines, test
	// suites, MCP/A2A servers, vector collections) so the fuzzer starts
	// from inputs that reach deep into each kind's validator.
	paths, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			f.Fatalf("reading seed %s: %v", p, err)
		}
		f.Add(data)
	}
	// Hand-picked edge shapes: empty, JSON input, unknown kind, aliases,
	// parse errors with line numbers.
	f.Add([]byte(""))
	f.Add([]byte("   \n\t"))
	f.Add([]byte(`{"apiVersion":"mockagents/v1","kind":"Agent","metadata":{"name":"j"},"spec":{"protocol":"openai-chat-completions","model":"gpt-4o"}}`))
	f.Add([]byte(`[1, 2, 3]`))
	f.Add([]byte(`{"kind": `))
	f.Add([]byte("kind: Nope\n"))
	f.Add([]byte("kind: Pipeline\nspec:\n  steps: [{name: a, depends_on: [a]}]\n"))
	f.Add([]byte("a: &x [1, *x]\n"))
	f.Add([]byte("kind: Agent\nmetadata:\n  name: x\n\tspec: {}\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		first := ValidateBytes(data)
		if first == nil {
			t.Fatal("ValidateBytes returned nil report")
		}
		for i, e := range first.Errors {
			if e == nil {
				t.Fatalf("Errors[%d] is nil", i)
			}
		}
		for i, w := range first.Warnings {
			if w == nil {
				t.Fatalf("Warnings[%d] is nil", i)
			}
		}

		second := ValidateBytes(data)
		a, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal first report: %v", err)
		}
		b, err := json.Marshal(second)
		if err != nil {
			t.Fatalf("marshal second report: %v", err)
		}
		if string(a) != string(b) {
			t.Fatalf("non-deterministic report for the same input:\nfirst:  %s\nsecond: %s", a, b)
		}
	})
}
