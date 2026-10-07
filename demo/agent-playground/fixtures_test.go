package playground

import (
	"io"
	"log/slog"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/runner"
)

// TestFixturesValidateAndContractSuitesPass is `mockagents validate` plus
// `mockagents test --agents-dir mockagents mock-tests/` as a Go test, so the
// repository's `go test ./...` (and CI) pins the playground's fixtures.
func TestFixturesValidateAndContractSuitesPass(t *testing.T) {
	docs, errs := config.LoadAllDocuments("mockagents")
	if len(errs) > 0 {
		t.Fatalf("loading fixtures: %v", errs)
	}
	if len(docs.Agents) != 25 || len(docs.Pipelines) != 1 {
		t.Fatalf("expected 25 agents and 1 pipeline, got %d and %d", len(docs.Agents), len(docs.Pipelines))
	}
	agents := engine.NewAgentRegistry()
	for _, r := range docs.Agents {
		config.ApplyDefaults(r.Definition)
		if verr := (&config.Validator{}).Validate(r.Definition, r.FilePath, r.Node); verr != nil {
			t.Fatalf("%s: %v", r.FilePath, verr)
		}
		agents.Register(r.Definition)
	}
	pipelines := engine.NewPipelineRegistry()
	for _, r := range docs.Pipelines {
		if verr := config.ValidatePipeline(r.Definition, r.FilePath, r.Node); verr != nil {
			t.Fatalf("%s: %v", r.FilePath, verr)
		}
		pipelines.Register(r.Definition)
	}

	suites, errs := config.LoadAllDocuments("mock-tests")
	if len(errs) > 0 || len(suites.TestSuites) == 0 {
		t.Fatalf("loading suites: %v (%d suites)", errs, len(suites.TestSuites))
	}
	eng := engine.NewEngine(agents, state.NewMemoryStore(state.DefaultSessionTTL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	run := runner.New(eng, pipelines)
	for _, s := range suites.TestSuites {
		if verr := config.ValidateTestSuite(s.Definition, s.FilePath, s.Node); verr != nil {
			t.Fatalf("%s: %v", s.FilePath, verr)
		}
		res, err := run.RunSuite(s.Definition)
		if err != nil {
			t.Fatalf("%s: %v", s.Definition.Metadata.Name, err)
		}
		for _, c := range res.Cases {
			if !c.Passed {
				t.Errorf("%s / %s: %v %s", res.SuiteName, c.Name, c.Failures, c.ErrMessage)
			}
		}
	}
}
