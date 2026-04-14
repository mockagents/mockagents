package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_ExampleFiles(t *testing.T) {
	examplesDir := filepath.Join("..", "..", "examples")
	results, errs := LoadDir(examplesDir)
	require.Empty(t, errs, "example files should parse without errors")
	require.GreaterOrEqual(t, len(results), 2, "expected at least 2 example files")

	v := &Validator{}
	for _, result := range results {
		ApplyDefaults(result.Definition)
		validationErrs := v.Validate(result.Definition, result.FilePath, result.Node)
		assert.Nil(t, validationErrs, "example file %s should be valid, got: %v", result.FilePath, validationErrs)
	}
}

func TestIntegration_LoadValidateDirectory(t *testing.T) {
	dir := t.TempDir()

	writeFileAt(t, dir, "agent1.yaml", `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: agent-one
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Agent one"
`)
	writeFileAt(t, dir, "agent2.yaml", `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: agent-two
spec:
  protocol: anthropic-messages
  model: claude-3-opus
  behavior:
    scenarios:
      - name: default
        response:
          content: "Agent two"
`)

	results, errs := LoadDir(dir)
	require.Empty(t, errs)
	require.Len(t, results, 2)

	v := &Validator{}
	for _, result := range results {
		ApplyDefaults(result.Definition)
		assert.Nil(t, v.Validate(result.Definition, result.FilePath, result.Node))
	}
}

func TestIntegration_DefaultModelApplied(t *testing.T) {
	path := writeTestFile(t, "no-model.yaml", `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: no-model
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hi"
`)
	result, err := LoadFile(path)
	require.NoError(t, err)

	ApplyDefaults(result.Definition)
	assert.Equal(t, "mock-agent", result.Definition.Spec.Model)

	v := &Validator{}
	assert.Nil(t, v.Validate(result.Definition, result.FilePath, result.Node))
}
