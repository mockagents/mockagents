package main

import (
	"encoding/json"
	"github.com/mockagents/mockagents/internal/adapter"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

func TestSourceRouteCoverage(t *testing.T) {
	t.Chdir("../..")
	checkOnly = true
	defer func() { checkOnly = false }()
	require.NoError(t, run())
}
func TestCoverageRejectsUnmappedRoute(t *testing.T) {
	t.Chdir("../..")
	require.ErrorContains(t, checkCoverage([]route{{pattern: "POST /new-undocumented-route", file: "internal/server/server.go"}}), "missing from OpenAPI")
}
func TestRuntimeProviderPatternsAreInventoried(t *testing.T) {
	t.Chdir("../..")
	var routes []route
	for _, a := range adapter.DefaultRegistry(nil).Adapters() {
		for _, r := range a.Routes() {
			routes = append(routes, route{pattern: r.Pattern})
		}
	}
	// Runtime patterns must occur in either the OpenAPI or an exact exclusion.
	// The full source check above additionally verifies source files and stale entries.
	data, err := os.ReadFile("docs/api-spec.yaml")
	require.NoError(t, err)
	var spec struct{ Paths map[string]map[string]any }
	require.NoError(t, yaml.Unmarshal(data, &spec))
	data, err = os.ReadFile("docs/api-exclusions.json")
	require.NoError(t, err)
	var exclusions []exclusion
	require.NoError(t, json.Unmarshal(data, &exclusions))
	excluded := map[string]bool{}
	for _, e := range exclusions {
		excluded[e.Pattern] = true
	}
	for _, r := range routes {
		method, path, _ := strings.Cut(r.pattern, " ")
		_, ok := spec.Paths[path][strings.ToLower(method)]
		require.True(t, ok || excluded[r.pattern], r.pattern)
	}
}
