package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Path Traversal Prevention ---

func TestSecurity_PathTraversal_DotDot(t *testing.T) {
	dir := t.TempDir()
	// Create a file outside the agents directory.
	secretFile := filepath.Join(dir, "secret.yaml")
	require.NoError(t, os.WriteFile(secretFile, []byte("secret: data"), 0644))

	// Create agents subdir.
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0755))

	// LoadDir should only load from the specified directory, not parent.
	results, errs := LoadDir(agentsDir)
	assert.Empty(t, results)
	assert.Empty(t, errs)
}

func TestSecurity_PathTraversal_AbsolutePath(t *testing.T) {
	// LoadFile with an absolute path should still work (it's explicit).
	_, err := LoadFile("/nonexistent/../../etc/passwd")
	assert.Error(t, err) // File doesn't exist, but it shouldn't crash.
}

// --- YAML Bomb Prevention ---

func TestSecurity_YAMLBomb_LargeFile(t *testing.T) {
	dir := t.TempDir()
	// Create a very large YAML file (> 1MB of repeated content).
	largePath := filepath.Join(dir, "large.yaml")
	content := "apiVersion: mockagents/v1\nkind: Agent\nmetadata:\n  name: large-agent\n  description: " +
		strings.Repeat("A", 2*1024*1024) // 2MB
	require.NoError(t, os.WriteFile(largePath, []byte(content), 0644))

	// Should still parse (we don't restrict size in loader, but it shouldn't crash).
	result, err := LoadFile(largePath)
	if err == nil {
		assert.NotNil(t, result)
	}
	// The important thing is it doesn't panic or hang.
}

func TestSecurity_YAMLBomb_DeeplyNested(t *testing.T) {
	dir := t.TempDir()
	// Create deeply nested YAML.
	nested := "a:\n"
	for i := 0; i < 100; i++ {
		nested += strings.Repeat(" ", (i+1)*2) + "b:\n"
	}
	nested += strings.Repeat(" ", 202) + "c: value\n"
	path := filepath.Join(dir, "nested.yaml")
	require.NoError(t, os.WriteFile(path, []byte(nested), 0644))

	// Should not panic on deeply nested YAML.
	_, err := LoadFile(path)
	// Error is acceptable (invalid agent format), panic is not.
	_ = err
}

func TestSecurity_YAMLBomb_RecursiveAnchor(t *testing.T) {
	dir := t.TempDir()
	// YAML with anchors/aliases (not a true bomb, but tests the parser).
	content := `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: &name anchor-agent
  description: *name
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
`
	path := filepath.Join(dir, "anchor.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	result, err := LoadFile(path)
	require.NoError(t, err)
	// Alias should resolve to the anchor value.
	assert.Equal(t, "anchor-agent", result.Definition.Metadata.Description)
}

// --- Empty/Malicious Input ---

func TestSecurity_EmptyYAML(t *testing.T) {
	path := writeTestFile(t, "empty.yaml", "")
	_, err := LoadFile(path)
	assert.Error(t, err)
}

func TestSecurity_NullBytesInYAML(t *testing.T) {
	path := writeTestFile(t, "null.yaml", "apiVersion: mock\x00agents/v1\nkind: Agent\n")
	_, err := LoadFile(path)
	// Should either parse or error, but not panic.
	_ = err
}

func TestSecurity_BinaryFileAsYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.yaml")
	require.NoError(t, os.WriteFile(path, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, 0644))

	_, err := LoadFile(path)
	assert.Error(t, err)
}

// --- Validation Edge Cases ---

func TestSecurity_ExtremelyLongAgentName(t *testing.T) {
	longName := strings.Repeat("a", 1000)
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: `+longName+`
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
`)
	require.NotNil(t, errs)
	assertHasError(t, errs, "metadata.name", "exceeds 63")
}

func TestSecurity_SpecialCharsInAgentName(t *testing.T) {
	// Names that are valid YAML strings but should fail name validation.
	for _, name := range []string{"../etc/passwd", "My Agent", "UPPERCASE", "has spaces"} {
		t.Run(name, func(t *testing.T) {
			errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: "`+name+`"
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
`)
			require.NotNil(t, errs, "name %q should fail validation", name)
		})
	}
}

func TestSecurity_ToolNameInjection(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: safe-agent
spec:
  protocol: openai-chat-completions
  tools:
    - name: "'; DROP TABLE tools; --"
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
`)
	require.NotNil(t, errs)
	assertHasError(t, errs, "spec.tools.0.name", "snake_case")
}
