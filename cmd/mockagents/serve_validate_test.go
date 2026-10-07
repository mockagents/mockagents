package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mockagents/mockagents/internal/config"
)

// `mockagents mcp` and `mockagents a2a` used to serve a definition without
// running its validator (2026-10-06 review C-02), so a document `validate`
// rejected was served and could panic net/http on every request.

func loadMutatedExample(t *testing.T, file, from, to string) *config.Documents {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", file))
	require.NoError(t, err)
	mutated := strings.Replace(string(data), from, to, 1)
	require.NotEqual(t, string(data), mutated, "mutation must apply")
	path := filepath.Join(t.TempDir(), file)
	require.NoError(t, os.WriteFile(path, []byte(mutated), 0o644))
	docs, err := config.LoadDocumentFile(path)
	require.NoError(t, err)
	return docs
}

func TestSelectA2AServer_RefusesAnInvalidDefinition(t *testing.T) {
	docs := loadMutatedExample(t, "a2a-server.yaml", "name: weather-a2a", "name: Weather A2A")
	_, err := selectA2AServer(docs, "dir")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is invalid")

	valid := loadMutatedExample(t, "a2a-server.yaml", "name: weather-a2a", "name: weather-a2a-2")
	def, err := selectA2AServer(valid, "dir")
	require.NoError(t, err)
	assert.Equal(t, "weather-a2a-2", def.Metadata.Name)
}

func TestSelectMCPServer_RefusesAnInvalidDefinition(t *testing.T) {
	docs := loadMutatedExample(t, "weather-mcp.yaml", "name: weather-mcp", "name: Weather MCP")
	_, err := selectMCPServer(docs, "dir")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is invalid")

	valid := loadMutatedExample(t, "weather-mcp.yaml", "name: weather-mcp", "name: weather-mcp-2")
	def, err := selectMCPServer(valid, "dir")
	require.NoError(t, err)
	assert.Equal(t, "weather-mcp-2", def.Metadata.Name)
}
