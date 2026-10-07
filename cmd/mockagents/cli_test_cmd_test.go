package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `mockagents test`: exit 0 when every case passes, 1 on any assertion
// failure, 2 on a load/config error (a RunE error).

func suiteYAML(name, target, expect string) string {
	return "apiVersion: mockagents/v1\nkind: TestSuite\nmetadata:\n  name: " + name +
		"\nspec:\n  target:\n    agent: " + target +
		"\n  cases:\n    - name: says-hi\n      steps:\n        - role: user\n          content: hello\n" +
		"      assertions:\n        - type: response_contains\n          value: " + expect + "\n"
}

func TestCLITest_PassFailAndFormats(t *testing.T) {
	agents := writeTree(t, map[string]string{"alpha.yaml": validAgentYAML})
	suites := writeTree(t, map[string]string{
		"pass.yaml": suiteYAML("pass-suite", "alpha", "hi"),
		"fail.yaml": suiteYAML("fail-suite", "alpha", "goodbye"),
	})
	pass, fail := filepath.Join(suites, "pass.yaml"), filepath.Join(suites, "fail.yaml")

	t.Run("passing suite exits 0", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, pass)
		require.NoError(t, res.Err, res.Stderr)
		assert.Equal(t, 0, res.Code)
		assert.Contains(t, res.Stdout, "Suite: pass-suite")
		assert.Contains(t, res.Stdout, "PASS  says-hi")
		assert.Contains(t, res.Stdout, "Total: 1 passed, 0 failed")
	})
	t.Run("failing suite exits 1 and names the case on stdout", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, fail)
		assert.Equal(t, 1, res.Code)
		assert.Contains(t, res.Stdout, "FAIL  says-hi")
		assert.Contains(t, res.Stdout, "goodbye", "the failure detail is printed under the case")
		assert.Contains(t, res.Stdout, "Total: 0 passed, 1 failed")
	})
	t.Run("json format", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, "--format", "json", pass, fail)
		assert.Equal(t, 1, res.Code)
		var results []struct {
			SuiteName string `json:"suite_name"`
			Passed    int    `json:"passed"`
			Failed    int    `json:"failed"`
		}
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &results), res.Stdout)
		require.Len(t, results, 2)
		assert.Equal(t, 1, results[0].Passed+results[1].Passed)
		assert.Equal(t, 1, results[0].Failed+results[1].Failed)
	})
	t.Run("junit format", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, "--format", "junit", pass)
		assert.Equal(t, 0, res.Code, res.Stderr)
		assert.Contains(t, res.Stdout, "<testsuites")
		assert.Contains(t, res.Stdout, `name="says-hi"`)
	})
	t.Run("unknown format is an error before anything runs", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, "--format", "tap", pass)
		assert.Equal(t, 2, res.Code)
		assert.ErrorContains(t, res.Err, `unknown test output format "tap"`)
		assert.Empty(t, res.Stdout)
	})
	t.Run("glob argument is expanded by the command", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, filepath.Join(suites, "p*.yaml"))
		assert.Equal(t, 0, res.Code, res.Stderr)
		assert.Contains(t, res.Stdout, "pass-suite")
		assert.NotContains(t, res.Stdout, "fail-suite")

		res = runCLI(t, "test", "--agents-dir", agents, filepath.Join(suites, "zzz*.yaml"))
		assert.Equal(t, 2, res.Code)
		assert.ErrorContains(t, res.Err, "no test suite files match")
	})
	t.Run("a directory argument runs every suite in it", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, suites)
		assert.Equal(t, 1, res.Code)
		assert.Contains(t, res.Stdout, "pass-suite")
		assert.Contains(t, res.Stdout, "fail-suite")
	})
	t.Run("--suites-dir", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, "--suites-dir", suites)
		assert.Equal(t, 1, res.Code)
		assert.Contains(t, res.Stdout, "Total: 1 passed, 1 failed")
	})
	t.Run("missing suite file", func(t *testing.T) {
		res := runCLI(t, "test", "--agents-dir", agents, filepath.Join(suites, "absent.yaml"))
		assert.Equal(t, 2, res.Code)
		assert.Error(t, res.Err)
	})
}

func TestCLITest_DefaultsToSuitesInAgentsDir(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"alpha.yaml": validAgentYAML,
		"suite.yaml": suiteYAML("inline-suite", "alpha", "hi"),
	})
	res := runCLI(t, "test", "--agents-dir", dir)
	require.NoError(t, res.Err, res.Stderr)
	assert.Contains(t, res.Stdout, "inline-suite")
	assert.Contains(t, res.Stdout, "Total: 1 passed, 0 failed")
}

func TestCLITest_ConfigErrors(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		args  func(dir string) []string
		want  string
	}{
		{
			name:  "no agents",
			files: map[string]string{"notes.txt": "x"},
			want:  "no agents found",
		},
		{
			name:  "a definition that fails to load",
			files: map[string]string{"alpha.yaml": validAgentYAML, "broken.yaml": "kind: Agent\nmetadata: [\n"},
			want:  "loading definitions",
		},
		{
			name:  "an invalid agent",
			files: map[string]string{"alpha.yaml": strings.Replace(validAgentYAML, "name: alpha", "name: Not Valid", 1)},
			want:  "validating agent",
		},
		{
			name:  "a suite targeting an unknown agent",
			files: map[string]string{"alpha.yaml": validAgentYAML, "suite.yaml": suiteYAML("s", "ghost", "hi")},
			want:  "ghost",
		},
		{
			name:  "no suites anywhere",
			files: map[string]string{"alpha.yaml": validAgentYAML},
			want:  "no test suites found",
		},
		{
			name:  "an explicit suite targeting an unknown agent",
			files: map[string]string{"alpha.yaml": validAgentYAML},
			args: func(string) []string {
				other := writeTree(t, map[string]string{"s.yaml": suiteYAML("s", "ghost", "hi")})
				return []string{filepath.Join(other, "s.yaml")}
			},
			want: "validating selected test references",
		},
		{
			name: "an invalid pipeline",
			files: map[string]string{
				"alpha.yaml": validAgentYAML,
				"p.yaml":     "apiVersion: mockagents/v1\nkind: Pipeline\nmetadata:\n  name: p\nspec:\n  agents: []\n",
			},
			want: "validating pipeline",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTree(t, tc.files)
			args := []string{"test", "--agents-dir", dir}
			if tc.args != nil {
				args = append(args, tc.args(dir)...)
			}
			res := runCLI(t, args...)
			assert.Equal(t, 2, res.Code, "stdout: %s", res.Stdout)
			assert.ErrorContains(t, res.Err, tc.want)
		})
	}
}

func TestLoadSuitesFrom(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.yaml": suiteYAML("a", "alpha", "hi"),
		"b.yaml": suiteYAML("b", "alpha", "hi"),
	})
	all, err := loadSuitesFrom(dir)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	one, err := loadSuitesFrom(filepath.Join(dir, "a.yaml"))
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, "a", one[0].Definition.Metadata.Name)

	broken := writeTree(t, map[string]string{"x.yaml": "kind: TestSuite\nmetadata: [\n"})
	_, err = loadSuitesFrom(broken)
	assert.ErrorContains(t, err, "loading suites")
	_, err = loadSuitesFrom(filepath.Join(broken, "x.yaml"))
	assert.Error(t, err)
}
