package cli

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTemplates_ScaffoldAndValidate is the FB-01 acceptance check: every
// shipped template must scaffold and every agent + test suite it produces must
// pass the real validators — so `mockagents init --template <name>` always
// yields a project that validates and runs.
func TestTemplates_ScaffoldAndValidate(t *testing.T) {
	templates := ListTemplates()
	require.GreaterOrEqual(t, len(templates), 4, "FB-01 acceptance: ship at least 4 packs")

	for _, tmpl := range templates {
		tmpl := tmpl
		t.Run(tmpl.Name, func(t *testing.T) {
			dir := t.TempDir()
			res, err := ScaffoldWithResult(ScaffoldOptions{
				ProjectName: tmpl.Name,
				TargetDir:   dir,
				Template:    tmpl.Name,
			})
			require.NoError(t, err)
			assert.Equal(t, tmpl.Name, res.Template)
			require.NotEmpty(t, res.Agents, "template must ship at least one agent")
			require.NotEmpty(t, res.Tests, "template must ship at least one test suite")

			// Every agent file validates.
			validator := &config.Validator{}
			for _, rel := range res.Agents {
				lr, err := config.LoadFile(filepath.Join(dir, rel))
				require.NoError(t, err, "load %s", rel)
				if errs := validator.Validate(lr.Definition, rel, lr.Node); errs != nil {
					t.Errorf("agent %s failed validation: %v", rel, errs)
				}
			}

			// Every test suite parses and validates.
			for _, rel := range res.Tests {
				ts, err := config.LoadTestSuiteFile(filepath.Join(dir, rel))
				require.NoError(t, err, "load %s", rel)
				if errs := config.ValidateTestSuite(ts.Definition, rel, ts.Node); errs != nil {
					t.Errorf("suite %s failed validation: %v", rel, errs)
				}
			}
		})
	}
}

// TestTemplates_RunSuites is the real FB-01 acceptance guard: it scaffolds
// every pack, loads its agents into an engine, and EXECUTES the bundled
// TestSuite through the runner — so a pack whose suite assertions no longer
// match its agent (renamed scenario, changed canned content, scenario-order
// shadowing, tool-arg drift) fails CI instead of shipping broken.
func TestTemplates_RunSuites(t *testing.T) {
	for _, tmpl := range ListTemplates() {
		tmpl := tmpl
		t.Run(tmpl.Name, func(t *testing.T) {
			dir := t.TempDir()
			res, err := ScaffoldWithResult(ScaffoldOptions{
				ProjectName: tmpl.Name,
				TargetDir:   dir,
				Template:    tmpl.Name,
			})
			require.NoError(t, err)

			reg := engine.NewAgentRegistry()
			for _, rel := range res.Agents {
				lr, err := config.LoadFile(filepath.Join(dir, rel))
				require.NoError(t, err, "load %s", rel)
				reg.Register(lr.Definition)
			}
			eng := engine.NewEngine(reg, state.NewMemoryStore(state.DefaultSessionTTL),
				slog.New(slog.NewTextHandler(io.Discard, nil)))
			r := runner.New(eng, nil)

			require.NotEmpty(t, res.Tests)
			for _, rel := range res.Tests {
				ts, err := config.LoadTestSuiteFile(filepath.Join(dir, rel))
				require.NoError(t, err, "load %s", rel)
				sr, err := r.RunSuite(ts.Definition)
				require.NoError(t, err, "run %s", rel)
				for _, c := range sr.Cases {
					if !c.Passed {
						t.Errorf("%s / case %q failed: %v", rel, c.Name, c.Failures)
					}
				}
				assert.Equal(t, len(ts.Definition.Spec.Cases), sr.Passed,
					"every case in %s should pass", rel)
				assert.Zero(t, sr.Failed, "no case in %s should fail", rel)
			}
		})
	}
}

// TestScaffold_ForceReplacesTemplateTree verifies that re-scaffolding a
// different template with --force replaces the agents/ tree rather than
// merging stale files from the previous template.
func TestScaffold_ForceReplacesTemplateTree(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")

	_, err := ScaffoldWithResult(ScaffoldOptions{ProjectName: "proj", TargetDir: proj, Template: "customer-support"})
	require.NoError(t, err)
	assertFileExists(t, filepath.Join(proj, "agents", "support-agent.yaml"))

	// A different template without --force conflicts on the managed files.
	_, err = ScaffoldWithResult(ScaffoldOptions{ProjectName: "proj", TargetDir: proj, Template: "basic"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// With --force the agents/ tree is replaced — no stale support-agent.yaml.
	_, err = ScaffoldWithResult(ScaffoldOptions{ProjectName: "proj", TargetDir: proj, Template: "basic", Force: true})
	require.NoError(t, err)
	assertFileExists(t, filepath.Join(proj, "agents", "example-agent.yaml"))
	_, statErr := os.Stat(filepath.Join(proj, "agents", "support-agent.yaml"))
	assert.True(t, os.IsNotExist(statErr), "stale support-agent.yaml should be removed by --force")

	results, errs := config.LoadDir(filepath.Join(proj, "agents"))
	assert.Empty(t, errs)
	assert.Len(t, results, 1, "agents dir should hold exactly the basic template's one agent")
}

// TestScaffold_ForceIdempotent verifies re-running --force over prior output
// reproduces a byte-identical, still-valid project.
func TestScaffold_ForceIdempotent(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")

	r1, err := ScaffoldWithResult(ScaffoldOptions{ProjectName: "proj", TargetDir: proj, Template: "rag"})
	require.NoError(t, err)
	require.NotEmpty(t, r1.Agents)
	first := readFile(t, filepath.Join(proj, r1.Agents[0]))

	r2, err := ScaffoldWithResult(ScaffoldOptions{ProjectName: "proj", TargetDir: proj, Template: "rag", Force: true})
	require.NoError(t, err)
	assert.Equal(t, r1.Agents, r2.Agents)
	assert.Equal(t, first, readFile(t, filepath.Join(proj, r2.Agents[0])), "force re-scaffold should be idempotent")
}

func TestScaffold_UnknownTemplate(t *testing.T) {
	dir := t.TempDir()
	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "x",
		TargetDir:   dir,
		Template:    "does-not-exist",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown template")
}

func TestScaffold_TemplateSelectsFiles(t *testing.T) {
	dir := t.TempDir()
	res, err := ScaffoldWithResult(ScaffoldOptions{
		ProjectName: "support",
		TargetDir:   dir,
		Template:    "customer-support",
	})
	require.NoError(t, err)
	assertFileExists(t, filepath.Join(dir, "agents", "support-agent.yaml"))
	assertFileExists(t, filepath.Join(dir, "tests", "support-suite.yaml"))
	assertFileExists(t, filepath.Join(dir, "README.md"))
	assertFileExists(t, filepath.Join(dir, ".mockagents.yaml"))

	readme := readFile(t, filepath.Join(dir, "README.md"))
	assert.Contains(t, readme, "customer-support")
	assert.Contains(t, readme, "support-suite.yaml")
	_ = res
}

func TestScaffold_DefaultTemplateIsBasic(t *testing.T) {
	dir := t.TempDir()
	res, err := ScaffoldWithResult(ScaffoldOptions{
		ProjectName: "p",
		TargetDir:   dir,
		// Template intentionally empty.
	})
	require.NoError(t, err)
	assert.Equal(t, DefaultTemplate, res.Template)
	assertFileExists(t, filepath.Join(dir, "agents", "example-agent.yaml"))
}

func TestListTemplates_HasExpectedPacks(t *testing.T) {
	names := map[string]bool{}
	for _, tmpl := range ListTemplates() {
		names[tmpl.Name] = true
		assert.NotEmpty(t, tmpl.Description, "%s needs a description", tmpl.Name)
	}
	for _, want := range []string{"basic", "customer-support", "rag", "coding-agent", "planner"} {
		assert.True(t, names[want], "expected template %q", want)
	}
}
