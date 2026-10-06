package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
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

// A UTF-8 BOM made encoding/json reject an otherwise valid .json file (C-15).
func TestLoadFile_JSONWithBOM(t *testing.T) {
	body := `{"apiVersion":"mockagents/v1","kind":"Agent","metadata":{"name":"bom"},"spec":{"protocol":"openai-chat-completions","model":"m","behavior":{"scenarios":[{"name":"d","response":{"content":"x"}}]}}}`
	path := filepath.Join(t.TempDir(), "bom.json")
	require.NoError(t, os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, body...), 0o644))
	res, err := LoadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "bom", res.Definition.Metadata.Name)
	assert.Empty(t, ValidateBytes(append([]byte{0xEF, 0xBB, 0xBF}, body...)).Errors)
}

func agentWith(behaviorExtra, scenario, tools string) string {
	return `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: bounds
spec:
  protocol: openai-chat-completions
  model: m
` + tools + `  behavior:
` + behaviorExtra + `    scenarios:
` + scenario
}

const plainScenario = `      - name: d
        response:
          content: "hi"
`

func errorFields(report *ValidateReport) []string {
	var fields []string
	for _, e := range report.Errors {
		fields = append(fields, e.Field)
	}
	return fields
}

// Streaming timing is bounded like chaos and negatives are rejected (C-08).
func TestValidate_StreamingBounds(t *testing.T) {
	doc := agentWith(`    streaming:
      enabled: true
      ttft_ms: 3600000
      jitter_ms: -5
      chunk_size: -3
      tokens_per_sec: -1
      truncate_after_chunks: -1
`, plainScenario, "")
	fields := errorFields(ValidateBytes([]byte(doc)))
	for _, want := range []string{
		"spec.behavior.streaming.ttft_ms", "spec.behavior.streaming.jitter_ms",
		"spec.behavior.streaming.chunk_size", "spec.behavior.streaming.tokens_per_sec",
		"spec.behavior.streaming.truncate_after_chunks",
	} {
		assert.Contains(t, fields, want)
	}
}

// NaN rates are rejected (C-19): "rate < 0 || rate > 1" let NaN through.
func TestValidate_NaNRates(t *testing.T) {
	doc := agentWith(`    chaos:
      errors:
        rate: .nan
      connection:
        mode: reset
        rate: .nan
`, plainScenario, "")
	fields := errorFields(ValidateBytes([]byte(doc)))
	assert.Contains(t, fields, "spec.behavior.chaos.errors.rate")
	assert.Contains(t, fields, "spec.behavior.chaos.connection.rate")
}

// A malformed template or an unknown template function is a validation
// error, not a 500 on every request (C-09).
func TestValidate_ResponseTemplatesAreParsed(t *testing.T) {
	for name, content := range map[string]string{
		"unclosed action":  `Hello {{ uuid `,
		"unknown function": `Hello {{ not_a_function }}`,
	} {
		doc := agentWith("", "      - name: d\n        response:\n          content: \""+content+"\"\n", "")
		fields := errorFields(ValidateBytes([]byte(doc)))
		assert.Contains(t, fields, "spec.behavior.scenarios.0.response.content", name)
	}
	ok := agentWith("", "      - name: d\n        response:\n          content: \"Hi {{ fake_name }} {{ .TurnNumber }} {{ .Timestamp }}\"\n", "")
	assert.Empty(t, ValidateBytes([]byte(ok)).Errors)
}

// Tool response rules must return something, errors need code and message,
// and tool names are capped at 64 characters (C-19).
func TestValidate_ToolResponseRulesAndNameLength(t *testing.T) {
	tools := `  tools:
    - name: ` + strings.Repeat("a", 65) + `
    - name: lookup
      responses:
        - match: {id: "1"}
        - default: true
          error: {code: NOT_FOUND}
        - default: true
          response: {ok: true}
          error: {code: E, message: m}
`
	fields := errorFields(ValidateBytes([]byte(agentWith("", plainScenario, tools))))
	for _, want := range []string{
		"spec.tools.0.name", "spec.tools.1.responses.0", "spec.tools.1.responses.1.error", "spec.tools.1.responses.2",
	} {
		assert.Contains(t, fields, want)
	}
}

// validate warns when two agents in a tenant claim one model (C-13): only the
// lexicographically smallest name answers requests for it.
func TestLintDocuments_SharedModel(t *testing.T) {
	mk := func(name, tenant, model string) *LoadResult {
		return &LoadResult{FilePath: name + ".yaml", Definition: &types.AgentDefinition{
			Metadata: types.Metadata{Name: name, TenantID: tenant},
			Spec:     types.AgentSpec{Model: model},
		}}
	}
	warnings := LintDocuments(&Documents{Agents: []*LoadResult{
		mk("zeta", "", "gpt-4o"), mk("alpha", "", "gpt-4o"),
		mk("solo", "", "other"), mk("beta", "ten_x", "gpt-4o"),
	}})
	require.Len(t, warnings, 1)
	assert.Equal(t, "zeta.yaml", warnings[0].File)
	assert.Contains(t, warnings[0].Message, `agent "alpha"`)
}
