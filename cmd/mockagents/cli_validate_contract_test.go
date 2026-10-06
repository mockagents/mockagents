package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These drive `validate` and `contract` through rootCmd so the flag wiring
// and the process exit code (osExit) are covered, not just the helpers.

// lintWarningAgentYAML is valid but carries a raw_arguments tool call on a
// non-OpenAI protocol, which Lint reports as a warning.
const lintWarningAgentYAML = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: linty
spec:
  protocol: anthropic-messages
  model: model-linty
  tools:
    - name: lookup
      description: look something up
      parameters:
        type: object
  behavior:
    scenarios:
      - name: default
        response:
          tool_calls:
            - name: lookup
              raw_arguments: '{"q":"x"}'
`

func TestCLIValidate_ExitCodes(t *testing.T) {
	good := writeTree(t, map[string]string{"a.yaml": validAgentYAML})
	bad := writeTree(t, map[string]string{"a.yaml": strings.Replace(validAgentYAML, "name: alpha", "name: Not Valid", 1)})

	t.Run("valid tree exits 0", func(t *testing.T) {
		res := runCLI(t, "validate", good)
		require.NoError(t, res.Err)
		assert.Equal(t, 0, res.Code)
		assert.Contains(t, res.Stdout, "All agent definitions are valid.")
	})
	t.Run("invalid tree exits 1 with diagnostics on stderr", func(t *testing.T) {
		res := runCLI(t, "validate", bad)
		assert.Equal(t, exitInvalid, res.Code)
		assert.NotContains(t, res.Stdout, "valid.")
		assert.Contains(t, res.Stderr, "metadata.name")
	})
	t.Run("missing path exits 2", func(t *testing.T) {
		res := runCLI(t, "validate", filepath.Join(good, "nope"))
		assert.Equal(t, exitUsage, res.Code)
		assert.Contains(t, res.Stderr, "nope")
	})
	t.Run("no args validates --agents-dir", func(t *testing.T) {
		res := runCLI(t, "validate", "--agents-dir", bad)
		assert.Equal(t, exitInvalid, res.Code)
		res = runCLI(t, "validate", "--agents-dir", good)
		assert.Equal(t, 0, res.Code, res.Stderr)
	})
	t.Run("json format is one document", func(t *testing.T) {
		res := runCLI(t, "validate", "--format", "json", bad)
		assert.Equal(t, exitInvalid, res.Code)
		var report validateReport
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &report), res.Stdout)
		assert.False(t, report.Valid)
		assert.Equal(t, 1, report.Files)
	})
	t.Run("usage error in json mode is still a json document", func(t *testing.T) {
		res := runCLI(t, "validate", "--format", "json", filepath.Join(good, "nope"))
		assert.Equal(t, exitUsage, res.Code)
		var report validateReport
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &report), res.Stdout)
		require.Len(t, report.LoadErrors, 1)
		assert.Contains(t, report.LoadErrors[0], "nope")
	})
}

func TestCLIValidate_StrictUpgradesWarnings(t *testing.T) {
	dir := writeTree(t, map[string]string{"linty.yaml": lintWarningAgentYAML})

	res := runCLI(t, "validate", dir)
	require.Equal(t, 0, res.Code, "a lint warning alone must not fail validation: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "Warning:")
	assert.Contains(t, res.Stderr, "raw_arguments")

	res = runCLI(t, "validate", "--strict", dir)
	assert.Equal(t, exitInvalid, res.Code, "--strict turns the warning into an error")
	assert.NotContains(t, res.Stderr, "Warning:")
	assert.Contains(t, res.Stderr, "raw_arguments")
}

func TestCLIValidate_AllKindsAreValidated(t *testing.T) {
	// The shipped examples include MCP, A2A, vector and test-suite documents;
	// each kind's validator must run (a broken MCP server fails the tree).
	dir := writeTree(t, map[string]string{
		"a.yaml": validAgentYAML,
		"mcp.yaml": "apiVersion: mockagents/v1\nkind: MCPServer\nmetadata:\n  name: Bad Name\n" +
			"spec:\n  capabilities:\n    tools: true\n",
	})
	res := runCLI(t, "validate", dir)
	assert.Equal(t, exitInvalid, res.Code)
	assert.Contains(t, res.Stderr, "metadata.name")
}

const contractAgentYAML = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: contract-bot
spec:
  protocol: openai-chat-completions
  model: contract-model
  tools:
    - name: get_weather
      description: Weather lookup
      parameters:
        type: object
        properties:
          city:
            type: string
        required: [city]
    - name: get_time
      description: Clock
      parameters:
        type: object
  behavior:
    scenarios:
      - name: default
        response:
          content: hi
`

func TestCLIContractExtract(t *testing.T) {
	dir := writeTree(t, map[string]string{"agent.yaml": contractAgentYAML})
	agent := filepath.Join(dir, "agent.yaml")

	res := runCLI(t, "contract", "extract", agent)
	require.NoError(t, res.Err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &doc), res.Stdout)
	assert.Contains(t, res.Stdout, "get_weather")
	assert.Contains(t, res.Stdout, "get_time")

	out := filepath.Join(dir, "contract.json")
	res = runCLI(t, "contract", "extract", agent, "-o", out)
	require.NoError(t, res.Err)
	assert.Empty(t, strings.TrimSpace(res.Stdout), "-o writes the file, not stdout")
	written, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(written), "}\n"), "file ends with a newline")
	assert.JSONEq(t, strings.TrimSpace(runCLI(t, "contract", "extract", agent).Stdout), string(written))

	// The extracted JSON round-trips: diffing it against its own source is clean.
	res = runCLI(t, "contract", "diff", out, agent)
	require.NoError(t, res.Err)
	assert.Equal(t, 0, res.Code)
	assert.Contains(t, res.Stdout, "No changes detected.")

	res = runCLI(t, "contract", "extract", filepath.Join(dir, "missing.yaml"))
	assert.Equal(t, 2, res.Code)
	assert.ErrorContains(t, res.Err, "missing.yaml")
}

func TestCLIContractDiff(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"old.yaml": contractAgentYAML,
		// Removing a tool is breaking for any consumer that calls it.
		"removed.yaml": strings.Replace(contractAgentYAML,
			"    - name: get_time\n      description: Clock\n      parameters:\n        type: object\n", "", 1),
		// A new tool is additive.
		"added.yaml": strings.Replace(contractAgentYAML, "  behavior:",
			"    - name: get_news\n      description: News\n      parameters:\n        type: object\n  behavior:", 1),
		"invalid.yaml": strings.Replace(contractAgentYAML, "  behavior:", "  behaviour: {}\n  behavior:", 1),
	})
	p := func(n string) string { return filepath.Join(dir, n) }

	t.Run("breaking change exits 1", func(t *testing.T) {
		res := runCLI(t, "contract", "diff", p("old.yaml"), p("removed.yaml"))
		assert.Equal(t, 1, res.Code)
		assert.Contains(t, res.Stdout, "get_time")
		assert.Contains(t, res.Stdout, "[breaking]")
		assert.Contains(t, res.Stderr, "breaking changes detected")
	})
	t.Run("additive change exits 0", func(t *testing.T) {
		res := runCLI(t, "contract", "diff", p("old.yaml"), p("added.yaml"))
		require.NoError(t, res.Err)
		assert.Equal(t, 0, res.Code)
		assert.Contains(t, res.Stdout, "get_news")
		assert.NotContains(t, res.Stdout, "[breaking]")
	})
	t.Run("json format", func(t *testing.T) {
		res := runCLI(t, "contract", "diff", "--format", "json", p("old.yaml"), p("removed.yaml"))
		assert.Equal(t, 1, res.Code)
		var changes []map[string]any
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &changes), res.Stdout)
		require.NotEmpty(t, changes)
		assert.Equal(t, "breaking", changes[0]["severity"])
	})
	t.Run("an invalid side is an error, not a pass", func(t *testing.T) {
		res := runCLI(t, "contract", "diff", p("old.yaml"), p("invalid.yaml"))
		assert.Equal(t, 2, res.Code)
		assert.ErrorContains(t, res.Err, "not a valid agent definition")
		res = runCLI(t, "contract", "diff", p("invalid.yaml"), p("old.yaml"))
		assert.Equal(t, 2, res.Code)
	})
	t.Run("wrong arity is a usage error", func(t *testing.T) {
		res := runCLI(t, "contract", "diff", p("old.yaml"))
		assert.Equal(t, 2, res.Code)
		assert.ErrorContains(t, res.Err, "accepts 2 arg(s)")
	})
	t.Run("unknown flag points at --help", func(t *testing.T) {
		res := runCLI(t, "contract", "diff", "--bogus", p("old.yaml"), p("added.yaml"))
		assert.Equal(t, 2, res.Code)
		assert.ErrorContains(t, res.Err, "unknown flag: --bogus")
		assert.ErrorContains(t, res.Err, "Run 'mockagents contract diff --help' for usage.")
	})
}

func TestCLIContractDiff_UnknownFormatIsAUsageError(t *testing.T) {
	dir := writeTree(t, map[string]string{"a.yaml": contractAgentYAML})
	a := filepath.Join(dir, "a.yaml")
	res := runCLI(t, "contract", "diff", "--format", "yaml", a, a)
	assert.Equal(t, 2, res.Code)
	assert.EqualError(t, res.Err, `unknown --format "yaml" (want text or json)`)
	assert.Empty(t, res.Stdout, "nothing is compared or printed")
}
