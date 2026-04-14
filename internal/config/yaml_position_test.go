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
