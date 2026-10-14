package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func parseNode(t *testing.T, input string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(input), &doc))
	return &doc
}

func TestNodeAt_TopLevel(t *testing.T) {
	node := parseNode(t, `
apiVersion: mockagents/v1
kind: Agent
`)
	result := NodeAt(node, "apiVersion")
	require.NotNil(t, result)
	assert.Equal(t, "mockagents/v1", result.Value)
}

func TestNodeAt_Nested(t *testing.T) {
	node := parseNode(t, `
metadata:
  name: test-agent
  description: A test
`)
	result := NodeAt(node, "metadata.name")
	require.NotNil(t, result)
	assert.Equal(t, "test-agent", result.Value)
}

func TestNodeAt_ArrayIndex(t *testing.T) {
	node := parseNode(t, `
items:
  - name: first
  - name: second
`)
	result := NodeAt(node, "items.1.name")
	require.NotNil(t, result)
	assert.Equal(t, "second", result.Value)
}

// Validators build indexed paths with fmt "%s[%d]" (spec.agents[0].id,
// spec.cases[0].steps[1].role), not the dot form; NodeAt only split on "." so
// "agents[0]" never resolved and every such error reported 0:0.
func TestNodeAt_BracketIndex(t *testing.T) {
	node := parseNode(t, `
spec:
  items:
    - name: first
    - name: second
      steps:
        - role: user
        - role: system
  matrix:
    - [a, b]
    - [c, d]
`)
	for path, want := range map[string]string{
		"spec.items[1].name":          "second",
		"spec.items[1].steps[1].role": "system",
		"spec.items.1.steps.0.role":   "user", // dot form keeps working
		"spec.items[1].steps.1.role":  "system",
		"spec.matrix[1][0]":           "c",
	} {
		got := NodeAt(node, path)
		if assert.NotNil(t, got, path) {
			assert.Equal(t, want, got.Value, path)
		}
	}
	for _, path := range []string{"spec.items[2].name", "spec.items[x].name", "spec.items[-1]", "spec.items[1"} {
		assert.Nil(t, NodeAt(node, path), path)
	}
}

func TestNodeAt_InvalidPath(t *testing.T) {
	node := parseNode(t, `key: value`)
	assert.Nil(t, NodeAt(node, "nonexistent"))
	assert.Nil(t, NodeAt(node, "key.deep"))
}

func TestNodeAt_EmptyPath(t *testing.T) {
	node := parseNode(t, `key: value`)
	result := NodeAt(node, "")
	assert.NotNil(t, result)
}

func TestNodeAt_NilNode(t *testing.T) {
	assert.Nil(t, NodeAt(nil, "any"))
}

func TestLineColOf_ReturnsPosition(t *testing.T) {
	node := parseNode(t, `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test
`)
	line, col := LineColOf(node, "metadata.name")
	assert.Equal(t, 4, line)
	assert.Greater(t, col, 0)
}

func TestLineColOf_UnresolvableReturnsZero(t *testing.T) {
	node := parseNode(t, `key: value`)
	line, col := LineColOf(node, "missing.path")
	assert.Equal(t, 0, line)
	assert.Equal(t, 0, col)
}
