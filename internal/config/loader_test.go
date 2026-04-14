package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFile_ValidYAML(t *testing.T) {
	path := writeTestFile(t, "valid.yaml", `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	result, err := LoadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "mockagents/v1", result.Definition.APIVersion)
	assert.Equal(t, "Agent", result.Definition.Kind)
	assert.Equal(t, "test-agent", result.Definition.Metadata.Name)
	assert.Equal(t, "openai-chat-completions", result.Definition.Spec.Protocol)
	assert.Len(t, result.Definition.Spec.Behavior.Scenarios, 1)
	assert.NotNil(t, result.Node)
}

func TestLoadFile_ValidJSON(t *testing.T) {
	path := writeTestFile(t, "valid.json", `{
  "apiVersion": "mockagents/v1",
  "kind": "Agent",
  "metadata": { "name": "json-agent" },
  "spec": {
    "protocol": "anthropic-messages",
    "behavior": {
      "scenarios": [{ "name": "default", "response": { "content": "Hi" } }]
    }
  }
}`)
	result, err := LoadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "json-agent", result.Definition.Metadata.Name)
	assert.Equal(t, "anthropic-messages", result.Definition.Spec.Protocol)
}

func TestLoadFile_EmptyFile(t *testing.T) {
	path := writeTestFile(t, "empty.yaml", "")
	_, err := LoadFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoadFile_MalformedYAML(t *testing.T) {
	path := writeTestFile(t, "bad.yaml", `
apiVersion: mockagents/v1
  bad indentation: here
`)
	_, err := LoadFile(path)
	require.Error(t, err)
	var pe *ParseError
	assert.ErrorAs(t, err, &pe)
}

func TestLoadFile_NonexistentFile(t *testing.T) {
	_, err := LoadFile("/nonexistent/path.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading")
}

func TestLoadFile_WithTools(t *testing.T) {
	path := writeTestFile(t, "tools.yaml", `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: tool-agent
spec:
  protocol: openai-chat-completions
  tools:
    - name: search
      description: Search for items
      parameters:
        type: object
        properties:
          query: { type: string }
      responses:
        - default: true
          response:
            results: []
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hi"
`)
	result, err := LoadFile(path)
	require.NoError(t, err)
	require.Len(t, result.Definition.Spec.Tools, 1)
	assert.Equal(t, "search", result.Definition.Spec.Tools[0].Name)
	assert.NotEmpty(t, result.Definition.Spec.Tools[0].Parameters)
}

func TestLoadDir_MixedFiles(t *testing.T) {
	dir := t.TempDir()

	writeFileAt(t, dir, "good.yaml", `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: good-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
`)
	writeFileAt(t, dir, "bad.yaml", `invalid: yaml: [`)
	writeFileAt(t, dir, "readme.txt", "not a config file")

	results, errs := LoadDir(dir)
	assert.Len(t, results, 1)
	assert.Len(t, errs, 1)
}

func TestLoadDir_Nonexistent(t *testing.T) {
	results, errs := LoadDir("/nonexistent/dir")
	assert.Nil(t, results)
	assert.Len(t, errs, 1)
}

func TestLoadDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	results, errs := LoadDir(dir)
	assert.Len(t, results, 0)
	assert.Len(t, errs, 0)
}

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	return writeFileAt(t, dir, name, content)
}

func writeFileAt(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
