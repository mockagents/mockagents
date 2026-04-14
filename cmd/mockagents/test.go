package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mockagents/mockagents/internal/cli"
	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/engine/state"
	"github.com/mockagents/mockagents/internal/runner"
	"github.com/spf13/cobra"
)

// Exit codes:
//   0 - all test cases passed
//   1 - one or more assertion failures
//   2 - load/config error

var testCmd = &cobra.Command{
	Use:   "test [file|directory]",
	Short: "Run TestSuite YAML files against loaded agents and pipelines",
	Long: `Load agent and pipeline definitions from --agents-dir, then execute
every TestSuite file passed as an argument (or every TestSuite found inside
the argument directory). Results are reported per case with failure details.

A TestSuite targets either an agent (target.agent: <name>) or a pipeline
(target.pipeline: <name>). Supported assertion types:
  - tool_call (tool + arguments)
  - response_contains (value)
  - scenario_matched (value)
  - latency_ms_lt (max_ms)`,
	RunE: runTest,
}

var (
	testFormat string
	testSuites string
)

func init() {
	testCmd.Flags().StringVar(&testFormat, "format", "text", "Output format: text, json, or junit")
	testCmd.Flags().StringVar(&testSuites, "suites-dir", "", "Directory containing TestSuite YAML files (defaults to --agents-dir)")
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	docs, loadErrs := config.LoadAllDocuments(agentsDir)
	for _, e := range loadErrs {
		fmt.Fprintln(os.Stderr, "load error:", e)
	}
	if len(docs.Agents) == 0 {
		return fmt.Errorf("no agents found in %q", agentsDir)
	}

	agentReg := engine.NewAgentRegistry()
	validator := &config.Validator{}
	for _, r := range docs.Agents {
		config.ApplyDefaults(r.Definition)
		if errList := validator.Validate(r.Definition, r.FilePath, r.Node); errList != nil {
			fmt.Fprintln(os.Stderr, "skipping invalid agent:", errList.Error())
			continue
		}
		agentReg.Register(r.Definition)
	}
	if agentReg.Count() == 0 {
		return fmt.Errorf("no valid agents loaded from %q", agentsDir)
	}

	pipelineReg := engine.NewPipelineRegistry()
	for _, r := range docs.Pipelines {
		pipelineReg.Register(r.Definition)
	}

	store := state.NewMemoryStore(state.DefaultSessionTTL)
	eng := engine.NewEngine(agentReg, store, logger)
	run := runner.New(eng, pipelineReg)

	// Resolve suite paths: explicit args > --suites-dir > agents-dir (already loaded docs).
	var suites []*config.TestSuiteLoadResult
	switch {
	case len(args) > 0:
		for _, p := range args {
			loaded, err := loadSuitesFrom(p)
			if err != nil {
				return err
			}
			suites = append(suites, loaded...)
		}
	case testSuites != "":
		loaded, err := loadSuitesFrom(testSuites)
		if err != nil {
			return err
		}
		suites = append(suites, loaded...)
	default:
		suites = docs.TestSuites
	}

	if len(suites) == 0 {
		return fmt.Errorf("no test suites found")
	}

	var allResults []*runner.SuiteResult
	totalFailed := 0
	for _, s := range suites {
		res, err := run.RunSuite(s.Definition)
		if err != nil {
			fmt.Fprintf(os.Stderr, "suite %q: %s\n", s.FilePath, err)
			totalFailed++
			continue
		}
		allResults = append(allResults, res)
		totalFailed += res.Failed
	}

	switch testFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(allResults)
	case "junit":
		if err := runner.WriteJUnit(os.Stdout, allResults); err != nil {
			return fmt.Errorf("writing junit xml: %w", err)
		}
	default:
		printTextResults(allResults)
	}

	if totalFailed > 0 {
		os.Exit(1)
	}
	return nil
}

func loadSuitesFrom(path string) ([]*config.TestSuiteLoadResult, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		docs, errs := config.LoadAllDocuments(abs)
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "load error:", e)
		}
		return docs.TestSuites, nil
	}
	r, err := config.LoadTestSuiteFile(abs)
	if err != nil {
		return nil, err
	}
	return []*config.TestSuiteLoadResult{r}, nil
}

func printTextResults(results []*runner.SuiteResult) {
	totalPassed, totalFailed := 0, 0
	for _, sr := range results {
		fmt.Printf("\nSuite: %s (%s)\n", sr.SuiteName, sr.Target)
		for _, c := range sr.Cases {
			if c.Passed {
				cli.PrintSuccess(fmt.Sprintf("  PASS  %s (%s)", c.Name, c.Latency))
			} else {
				cli.PrintError(fmt.Sprintf("  FAIL  %s (%s)", c.Name, c.Latency))
				for _, f := range c.Failures {
					fmt.Printf("        - %s\n", f)
				}
			}
		}
		fmt.Printf("  %d passed, %d failed in %s\n", sr.Passed, sr.Failed, sr.Latency)
		totalPassed += sr.Passed
		totalFailed += sr.Failed
	}
	fmt.Printf("\nTotal: %d passed, %d failed\n", totalPassed, totalFailed)
}
