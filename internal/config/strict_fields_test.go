package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression tests for the 2026-10-06 quality review, iteration 1: unknown
// keys and extra YAML documents used to be dropped silently, so a document
// could validate clean while the author's intent never happened.

const strictAgent = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: strict-agent
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    streming:
      enabled: true
    scenarios:
      - name: hello
        match:
          content_contains: hi
        response:
          content: "hello"
          metadata:
            anything: goes
`

func TestUnknownField_AgentNestedKeyIsAnErrorWithLineAndSuggestion(t *testing.T) {
	report := ValidateBytes([]byte(strictAgent))
	require.Len(t, report.Errors, 1, "errors: %v", report.Errors)
	e := report.Errors[0]
	assert.Equal(t, "spec.behavior.streming", e.Field)
	assert.Equal(t, 9, e.Line)
	assert.Contains(t, e.Message, `unknown field "streming"`)
	assert.Contains(t, e.Suggestion, `"streaming"`)
}

func TestUnknownField_FreeFormMapsAcceptAnyKey(t *testing.T) {
	doc := strings.Replace(strictAgent, "    streming:\n      enabled: true\n", "", 1)
	report := ValidateBytes([]byte(doc))
	assert.Empty(t, report.Errors, "response.metadata is free-form and must accept any key")
}

// P-01: misspelled TestSuite keys turned assertions into no-ops and the
// runner reported PASS.
func TestUnknownField_TestSuiteFalsePassShapesAreRejected(t *testing.T) {
	cases := map[string]struct {
		doc       string
		wantField string
	}{
		"args instead of arguments": {
			doc: `      assertions:
        - type: tool_call
          tool: lookup
          args:
            id: WRONG-ID
`,
			wantField: "spec.cases[0].assertions[0].args",
		},
		"assertion instead of assertions": {
			doc: `      assertion:
        - type: response_contains
          value: NOT-IN-THE-RESPONSE
`,
			wantField: "spec.cases[0].assertion",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			doc := `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: suite
spec:
  target:
    agent: some-agent
  cases:
    - name: c1
      steps:
        - role: user
          content: hi
` + tc.doc
			report := ValidateBytes([]byte(doc))
			var fields []string
			for _, e := range report.Errors {
				fields = append(fields, e.Field)
			}
			assert.Contains(t, fields, tc.wantField)
		})
	}
}

func TestTestSuite_CaseWithoutAssertionsIsRejected(t *testing.T) {
	doc := `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: suite
spec:
  target:
    agent: some-agent
  cases:
    - name: c1
      steps:
        - role: user
          content: hi
`
	report := ValidateBytes([]byte(doc))
	require.NotEmpty(t, report.Errors)
	assert.Equal(t, "spec.cases[0].assertions", report.Errors[0].Field)
}

// Every kind gets the check: append an unknown top-level key to one shipped
// example of each kind and expect it to be named.
func TestUnknownField_EveryKindRejectsAnUnknownTopLevelKey(t *testing.T) {
	examples := map[string]string{
		"Agent":            "access-denied-agent.yaml",
		"Pipeline":         "research-pipeline.yaml",
		"TestSuite":        "research-suite.yaml",
		"MCPServer":        "weather-mcp.yaml",
		"A2AServer":        "a2a-server.yaml",
		"VectorCollection": "rag-vector-collection.yaml",
	}
	for kind, file := range examples {
		t.Run(kind, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "examples", file))
			require.NoError(t, err)
			clean := ValidateBytes(data)
			require.Empty(t, clean.Errors, "the shipped example must be valid")
			require.Equal(t, kind, clean.Kind)

			broken := ValidateBytes(append(data, []byte("\nbogusTopLevelKey: 1\n")...))
			require.NotEmpty(t, broken.Errors, "an unknown key must fail validation")
			assert.Equal(t, "bogusTopLevelKey", broken.Errors[0].Field)
		})
	}
	t.Run("SearchService", func(t *testing.T) {
		doc := "apiVersion: mockagents/v1\nkind: SearchService\nmetadata:\n  name: search\nspec:\n  provider: tavily\n  providr: typo\n"
		report := ValidateBytes([]byte(doc))
		var fields []string
		for _, e := range report.Errors {
			fields = append(fields, e.Field)
		}
		assert.Contains(t, fields, "spec.providr")
	})
}

func TestUnknownField_JSONInput(t *testing.T) {
	body := `{"apiVersion":"mockagents/v1","kind":"Agent","metadata":{"name":"j"},"spec":{"protocol":"openai-chat-completions","model":"m","behaviour":{},"behavior":{"scenarios":[{"name":"d","response":{"content":"x"}}]}}}`
	report := ValidateBytes([]byte(body))
	require.Len(t, report.Errors, 1)
	assert.Equal(t, "spec.behaviour", report.Errors[0].Field)
	assert.Contains(t, report.Errors[0].Suggestion, `"behavior"`)
}

func TestUnknownAgentFields_ReportsOnlyUnknownKeys(t *testing.T) {
	assert.Len(t, UnknownAgentFields([]byte(strictAgent)), 1)
	assert.Empty(t, UnknownAgentFields([]byte("{{ not: [valid")), "syntax errors are the decoder's to report")
}

// C-01: only the first YAML document used to be decoded.
func TestMultiDocument_FileIsRejectedWithTheSeparatorLine(t *testing.T) {
	second := "---\napiVersion: mockagents/v1\nkind: Agent\nmetadata:\n  name: BROKEN NAME\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "two.yaml")
	clean := strings.Replace(strictAgent, "    streming:\n      enabled: true\n", "", 1)
	require.NoError(t, os.WriteFile(path, []byte(clean+second), 0o644))

	docs, errs := LoadAllDocuments(dir)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "line 17: a second YAML document starts here")
	assert.Empty(t, docs.Agents, "a rejected file contributes no documents")

	_, err := LoadFile(path)
	require.Error(t, err)

	report := ValidateBytes([]byte(clean + second))
	require.Len(t, report.Errors, 1)
	assert.Equal(t, 17, report.Errors[0].Line)
}

func TestMultiDocument_LeadingAndTrailingSeparatorsAreFine(t *testing.T) {
	clean := strings.Replace(strictAgent, "    streming:\n      enabled: true\n", "", 1)
	for name, doc := range map[string]string{
		"leading":  "---\n" + clean,
		"trailing": clean + "---\n",
		"comment":  clean + "---\n# nothing here\n",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, ValidateBytes([]byte(doc)).Errors)
		})
	}
}
