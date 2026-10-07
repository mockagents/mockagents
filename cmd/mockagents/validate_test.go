package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The validate exit-code and output contract is what CI users rely on. Every
// case here is one that used to report success (or the wrong code): the
// 2026-10-06 quality review, findings C-01, C-04, C-10, C-11, C-12, C-16, C-23.

const validAgentYAML = `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: alpha
spec:
  protocol: openai-chat-completions
  model: model-alpha
  behavior:
    scenarios:
      - name: default
        response:
          content: "hi"
`

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

func pipelineYAML(name string) string {
	return "apiVersion: mockagents/v1\nkind: Pipeline\nmetadata:\n  name: " + name +
		"\nspec:\n  agents:\n    - id: a\n      ref: alpha\n"
}

func TestValidate_ExitCodeContract(t *testing.T) {
	cases := []struct {
		name       string
		files      map[string]string
		args       func(dir string) []string
		opts       validateOptions
		wantCode   int
		wantStderr string
	}{
		{
			name:     "valid tree",
			files:    map[string]string{"a.yaml": validAgentYAML},
			wantCode: exitValid,
		},
		{
			name:       "unknown field is an error",
			files:      map[string]string{"a.yaml": strings.Replace(validAgentYAML, "  behavior:", "  behaviour: {}\n  behavior:", 1)},
			wantCode:   exitInvalid,
			wantStderr: `unknown field "behaviour"`,
		},
		{
			name:       "second document is an error",
			files:      map[string]string{"a.yaml": validAgentYAML + "---\nkind: Agent\nmetadata:\n  name: BROKEN NAME\n"},
			wantCode:   exitInvalid,
			wantStderr: "a second YAML document starts here",
		},
		{
			name:       "empty directory is an error",
			files:      map[string]string{"notes.txt": "not ours"},
			wantCode:   exitInvalid,
			wantStderr: "no MockAgents documents found",
		},
		{
			name:     "empty directory with --allow-empty",
			files:    map[string]string{"notes.txt": "not ours"},
			opts:     validateOptions{AllowEmpty: true},
			wantCode: exitValid,
		},
		{
			name:       "missing path is a usage error",
			files:      map[string]string{},
			args:       func(dir string) []string { return []string{filepath.Join(dir, "does-not-exist")} },
			wantCode:   exitUsage,
			wantStderr: "does-not-exist",
		},
		{
			name:       "unknown format is a usage error",
			files:      map[string]string{"a.yaml": validAgentYAML},
			opts:       validateOptions{Format: "yaml"},
			wantCode:   exitUsage,
			wantStderr: "unknown --format",
		},
		{
			name: "duplicate pipeline names",
			files: map[string]string{
				"a.yaml":  validAgentYAML,
				"p1.yaml": pipelineYAML("same-pipe"),
				"p2.yaml": pipelineYAML("same-pipe"),
			},
			wantCode:   exitInvalid,
			wantStderr: `pipeline name "same-pipe" is already used`,
		},
		{
			name:  "a file named twice is not its own duplicate",
			files: map[string]string{"a.yaml": validAgentYAML},
			args: func(dir string) []string {
				return []string{dir, filepath.Join(dir, "a.yaml")}
			},
			wantCode: exitValid,
		},
		{
			name: "single non-agent file reports its own decode error",
			files: map[string]string{
				"p.yaml": "apiVersion: mockagents/v1\nkind: Pipeline\nmetadata:\n  name: p\nspec:\n  agents: should-be-a-list\n",
			},
			args:       func(dir string) []string { return []string{filepath.Join(dir, "p.yaml")} },
			wantCode:   exitInvalid,
			wantStderr: "cannot unmarshal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, tc.files)
			opts := tc.opts
			if opts.Format == "" {
				opts.Format = "text"
			}
			opts.Paths = []string{dir}
			if tc.args != nil {
				opts.Paths = tc.args(dir)
			}
			var stdout, stderr bytes.Buffer
			code := executeValidate(opts, &stdout, &stderr)
			assert.Equal(t, tc.wantCode, code, "stderr: %s", stderr.String())
			if tc.wantStderr != "" {
				assert.Contains(t, stderr.String(), tc.wantStderr)
			}
			if code == exitValid {
				assert.Contains(t, stdout.String(), "All agent definitions are valid.")
			} else {
				assert.NotContains(t, stdout.String(), "valid.", "a failing run must not print the success line")
			}
		})
	}
}

// --format json writes one parseable document to stdout and nothing to it in
// any other form, for success and every failure class.
func TestValidate_JSONOutputIsOneDocumentOnStdout(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"good.yaml":   validAgentYAML,
		"bad.yaml":    strings.Replace(validAgentYAML, "name: alpha", "name: Bad Name", 1),
		"broken.json": "{not json",
	})
	var stdout, stderr bytes.Buffer
	code := executeValidate(validateOptions{Paths: []string{dir}, Format: "json"}, &stdout, &stderr)
	require.Equal(t, exitInvalid, code)

	var report validateReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report), "stdout must be exactly one JSON document: %q", stdout.String())
	assert.False(t, report.Valid)
	assert.Equal(t, 3, report.Files)
	assert.NotEmpty(t, report.Errors)
	require.Len(t, report.LoadErrors, 1)
	assert.Contains(t, report.LoadErrors[0], "broken.json")
	assert.Empty(t, stderr.String(), "JSON mode keeps diagnostics in the document")

	stdout.Reset()
	code = executeValidate(validateOptions{Paths: []string{filepath.Join(dir, "good.yaml")}, Format: "json"}, &stdout, &stderr)
	require.Equal(t, exitValid, code)
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	assert.True(t, report.Valid)
	assert.NotNil(t, report.Errors, "empty lists serialise as [] not null")
}

// The shipped examples must stay valid under the stricter checks.
func TestValidate_ShippedExamplesAndTemplates(t *testing.T) {
	for _, dir := range []string{
		filepath.Join("..", "..", "examples"),
		filepath.Join("..", "..", "internal", "cli", "templates"),
	} {
		var stdout, stderr bytes.Buffer
		code := executeValidate(validateOptions{Paths: []string{dir}, Format: "text"}, &stdout, &stderr)
		assert.Equal(t, exitValid, code, "%s: %s", dir, stderr.String())
	}
}
