package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Directory scanning recurses (so agents can be organized into subdirectories),
// but a subdirectory may belong to a project that merely contains agents. These
// tests pin both halves of that contract.

const nestedAgentYAML = `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: nested-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "ok"
`

// writeNested writes content at dir/relPath, creating parent directories.
func writeNested(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func collected(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := listDocumentPaths(dir)
	require.NoError(t, err)
	rel := make([]string, 0, len(paths))
	for _, p := range paths {
		r, err := filepath.Rel(dir, p)
		require.NoError(t, err)
		rel = append(rel, filepath.ToSlash(r))
	}
	return rel
}

func TestListDocumentPaths_RecursesIntoSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "top.yaml", nestedAgentYAML)
	writeNested(t, dir, "agents/nested.yaml", nestedAgentYAML)
	writeNested(t, dir, "agents/deeper/still.yaml", nestedAgentYAML)

	assert.Equal(t, []string{
		"agents/deeper/still.yaml",
		"agents/nested.yaml",
		"top.yaml",
	}, collected(t, dir))
}

func TestListDocumentPaths_SkipsDependencyTrees(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "top.yaml", nestedAgentYAML)
	// A real, valid document inside a dependency tree is still skipped: the
	// point is that these directories are not ours to read, not that their
	// contents happen to be invalid.
	writeNested(t, dir, "node_modules/pkg/agent.yaml", nestedAgentYAML)
	writeNested(t, dir, "node_modules/pkg/.travis.yml", "language: node_js\n")
	writeNested(t, dir, "venv/lib/thing.yaml", nestedAgentYAML)
	writeNested(t, dir, "dist/build-output.yaml", nestedAgentYAML)

	assert.Equal(t, []string{"top.yaml"}, collected(t, dir))
}

func TestListDocumentPaths_SkipsDotDirectories(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "top.yaml", nestedAgentYAML)
	writeNested(t, dir, ".git/config.yaml", nestedAgentYAML)
	writeNested(t, dir, ".github/workflows/ci.yml", "name: CI\n")

	assert.Equal(t, []string{"top.yaml"}, collected(t, dir))
}

func TestListDocumentPaths_IgnoresNestedNonDocuments(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "top.yaml", nestedAgentYAML)
	// The case that motivated the rule: examples/frameworks/typescript keeps a
	// package.json next to the recipes, and .json is a document extension.
	writeNested(t, dir, "typescript/package.json", `{"name":"recipes","version":"1.0.0"}`)
	writeNested(t, dir, "typescript/tsconfig.json", `{"compilerOptions":{}}`)
	writeNested(t, dir, "compose/docker-compose.yml", "services:\n  web:\n    image: nginx\n")

	assert.Equal(t, []string{"top.yaml"}, collected(t, dir))
}

func TestListDocumentPaths_TopLevelNonDocumentIsStillCollected(t *testing.T) {
	dir := t.TempDir()
	// The top level of an agents directory is ours by convention, so a stray
	// document-extension file there is a mistake worth reporting — unchanged
	// from before recursion existed. Contrast TestListDocumentPaths_
	// IgnoresNestedNonDocuments, where the same file one level down is skipped.
	writeNested(t, dir, "docker-compose.yml", "services:\n  web:\n    image: nginx\n")

	assert.Equal(t, []string{"docker-compose.yml"}, collected(t, dir))

	// LoadDir itself does not error: a kind-less file decodes as an empty Agent.
	// The rejection comes from the validator, which is what `mockagents
	// validate` runs and where the user actually sees it.
	results, errs := LoadDir(dir)
	assert.Empty(t, errs)
	require.Len(t, results, 1, "the stray file still reaches the loader")

	ApplyDefaults(results[0].Definition)
	errList := (&Validator{}).Validate(results[0].Definition, results[0].FilePath, results[0].Node)
	require.NotNil(t, errList, "validation should reject a non-document at the top level")
	assert.NotEmpty(t, errList.Errors)
}

func TestListDocumentPaths_NestedMalformedDocumentIsSurfaced(t *testing.T) {
	dir := t.TempDir()
	// A nested file that cannot be parsed is collected rather than skipped: a
	// real document with a syntax error must fail loudly, not vanish.
	writeNested(t, dir, "agents/broken.yaml", "invalid: yaml: [")

	assert.Equal(t, []string{"agents/broken.yaml"}, collected(t, dir))
}

func TestListDocumentPaths_NestedDocumentWithBadAPIVersionIsSurfaced(t *testing.T) {
	dir := t.TempDir()
	// Parses fine, no apiVersion, but declares a kind we own — ours and broken,
	// which deserves an error rather than a silent skip.
	writeNested(t, dir, "agents/typo.yaml", "kind: Agent\nmetadata:\n  name: typo\n")

	assert.Equal(t, []string{"agents/typo.yaml"}, collected(t, dir))
}

func TestListDocumentPaths_SkipsEmptyNestedFile(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "top.yaml", nestedAgentYAML)
	writeNested(t, dir, "agents/empty.yaml", "")

	assert.Equal(t, []string{"top.yaml"}, collected(t, dir))
}

func TestListDocumentPaths_SkipsOversizedNestedFile(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "top.yaml", nestedAgentYAML)
	// Stands in for a package-lock.json: reading it only to reject it would
	// make every scan pay for it.
	writeNested(t, dir, "js/huge.json", `{"a":"`+strings.Repeat("x", maxNestedDocumentSize)+`"}`)

	assert.Equal(t, []string{"top.yaml"}, collected(t, dir))
}

func TestLoadAllDocuments_LoadsNestedAgent(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "agents/nested.yaml", nestedAgentYAML)
	writeNested(t, dir, "node_modules/pkg/package.json", `{"name":"x"}`)

	docs, errs := LoadAllDocuments(dir)
	require.Empty(t, errs)
	require.Len(t, docs.Agents, 1)
	assert.Equal(t, "nested-agent", docs.Agents[0].Definition.Metadata.Name)
}

func TestLoadAllDocumentsLoadsNestedVectorCollection(t *testing.T) {
	dir := t.TempDir()
	writeNested(t, dir, "fixtures/vectors.yaml", "apiVersion: mockagents/v1\nkind: VectorCollection\nmetadata:\n  name: docs\nspec:\n  dimension: 1\n  metric: cosine\n  points:\n    - id: one\n      vector: [1]\n")
	docs, errs := LoadAllDocuments(dir)
	require.Empty(t, errs)
	require.Len(t, docs.Vectors, 1)
	assert.Equal(t, "docs", docs.Vectors[0].Definition.Metadata.Name)
}
