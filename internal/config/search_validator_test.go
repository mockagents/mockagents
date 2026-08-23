package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAndValidateSearchService(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "search.yaml")
	yaml := "apiVersion: mockagents/v1\nkind: SearchService\nmetadata:\n  name: web-search\nspec:\n  provider: tavily\n  scenarios:\n    - name: docs\n      match:\n        query_contains: mockagents\n      response:\n        results:\n          - title: Docs\n            url: https://example.test/docs\n            content: offline\n            score: 0.9\n  faults:\n    partial_results:\n      max_results: 1\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	docs, errs := LoadAllDocuments(dir)
	require.Empty(t, errs)
	require.Len(t, docs.SearchServices, 1)
	require.Nil(t, ValidateSearchService(docs.SearchServices[0].Definition, path, docs.SearchServices[0].Node))
	report := ValidateBytes([]byte(yaml))
	require.Equal(t, "SearchService", report.Kind)
	require.Empty(t, report.Errors)
}

func TestValidateSearchServiceRejectsUnsafeFixture(t *testing.T) {
	report := ValidateBytes([]byte("apiVersion: mockagents/v1\nkind: SearchService\nmetadata:\n  name: search\nspec:\n  provider: tavily\n  scenarios:\n    - name: broken\n      match:\n        query_regex: '['\n      response: {}\n  faults:\n    status_code: 200\n    latency_ms: 60001\n"))
	require.Len(t, report.Errors, 3)
}
