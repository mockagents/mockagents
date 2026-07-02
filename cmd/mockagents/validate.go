package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/spf13/cobra"
)

// Exit codes:
//   0 - all agent definitions valid
//   1 - one or more validation errors found
//   2 - unexpected error (file not found, permission denied, etc.)

var validateCmd = &cobra.Command{
	Use:   "validate [file|directory...]",
	Short: "Validate agent definition files",
	Long: `Validate one or more agent definition files (YAML or JSON) against
the MockAgents schema. Reports all errors with file path, line number,
field path, and actionable suggestions.

If no arguments are given, validates files in the --agents-dir directory.`,
	RunE: runValidate,
}

var (
	outputFormat string
	strictMode   bool
)

func init() {
	validateCmd.Flags().StringVar(&outputFormat, "format", "text", "Output format: text or json")
	validateCmd.Flags().BoolVar(&strictMode, "strict", false, "Treat warnings as errors")
}

func runValidate(cmd *cobra.Command, args []string) error {
	paths := args
	if len(paths) == 0 {
		agentsDir, _ := cmd.Flags().GetString("agents-dir")
		paths = []string{agentsDir}
	}

	var allAgentResults []*config.LoadResult
	var allPipelineResults []*config.PipelineLoadResult
	var allTestSuiteResults []*config.TestSuiteLoadResult
	var allMCPServerResults []*config.MCPServerLoadResult
	var allA2AServerResults []*config.A2AServerLoadResult
	var allLoadErrors []error

	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			allLoadErrors = append(allLoadErrors, fmt.Errorf("resolving path %s: %w", p, err))
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			allLoadErrors = append(allLoadErrors, fmt.Errorf("accessing %s: %w", p, err))
			continue
		}

		if info.IsDir() {
			// LoadAllDocuments returns every kind — agents,
			// pipelines, testsuites, and mcp servers. We run the
			// matching validator against each bucket.
			docs, errs := config.LoadAllDocuments(absPath)
			if docs != nil {
				allAgentResults = append(allAgentResults, docs.Agents...)
				allPipelineResults = append(allPipelineResults, docs.Pipelines...)
				allTestSuiteResults = append(allTestSuiteResults, docs.TestSuites...)
				allMCPServerResults = append(allMCPServerResults, docs.MCPServers...)
				allA2AServerResults = append(allA2AServerResults, docs.A2AServers...)
			}
			allLoadErrors = append(allLoadErrors, errs...)
		} else {
			// Single-file mode: dispatch on kind by trying each
			// loader in turn. LoadFile only accepts Agents so we
			// fall back through the other three loaders before
			// surfacing the original error.
			result, err := config.LoadFile(absPath)
			if err == nil {
				allAgentResults = append(allAgentResults, result)
				continue
			}
			if pipelineResult, perr := config.LoadPipelineFile(absPath); perr == nil {
				allPipelineResults = append(allPipelineResults, pipelineResult)
				continue
			}
			if suiteResult, serr := config.LoadTestSuiteFile(absPath); serr == nil {
				allTestSuiteResults = append(allTestSuiteResults, suiteResult)
				continue
			}
			if mcpResult, merr := config.LoadMCPServerFile(absPath); merr == nil {
				allMCPServerResults = append(allMCPServerResults, mcpResult)
				continue
			}
			if a2aResult, aerr := config.LoadA2AServerFile(absPath); aerr == nil {
				allA2AServerResults = append(allA2AServerResults, a2aResult)
				continue
			}
			allLoadErrors = append(allLoadErrors, err)
		}
	}

	var allValidationErrors []*config.ValidationError
	var allWarnings []*config.ValidationError
	validator := &config.Validator{}

	for _, result := range allAgentResults {
		config.ApplyDefaults(result.Definition)
		if errList := validator.Validate(result.Definition, result.FilePath, result.Node); errList != nil {
			allValidationErrors = append(allValidationErrors, errList.Errors...)
		}
		// Non-fatal lint findings (round-11); --strict upgrades them.
		allWarnings = append(allWarnings, validator.Lint(result.Definition, result.FilePath, result.Node)...)
	}
	for _, result := range allPipelineResults {
		if errList := config.ValidatePipeline(result.Definition, result.FilePath, result.Node); errList != nil {
			allValidationErrors = append(allValidationErrors, errList.Errors...)
		}
	}
	for _, result := range allTestSuiteResults {
		if errList := config.ValidateTestSuite(result.Definition, result.FilePath, result.Node); errList != nil {
			allValidationErrors = append(allValidationErrors, errList.Errors...)
		}
	}
	for _, result := range allMCPServerResults {
		if errList := config.ValidateMCPServer(result.Definition, result.FilePath, result.Node); errList != nil {
			allValidationErrors = append(allValidationErrors, errList.Errors...)
		}
	}
	for _, result := range allA2AServerResults {
		if errList := config.ValidateA2AServer(result.Definition, result.FilePath, result.Node); errList != nil {
			allValidationErrors = append(allValidationErrors, errList.Errors...)
		}
	}

	// Cross-document reference checks: every pipeline's agent refs
	// and every testsuite's target must resolve against the other
	// documents in the same run. Skipped entirely when no pipelines
	// or testsuites were loaded — pure agent directories don't
	// need the extra pass.
	if len(allPipelineResults) > 0 || len(allTestSuiteResults) > 0 {
		crossDocs := &config.Documents{
			Agents:     allAgentResults,
			Pipelines:  allPipelineResults,
			TestSuites: allTestSuiteResults,
			MCPServers: allMCPServerResults,
			A2AServers: allA2AServerResults,
		}
		if errList := config.ValidateDocuments(crossDocs); errList != nil {
			allValidationErrors = append(allValidationErrors, errList.Errors...)
		}
	}

	// Determine output format.
	var format config.ErrorFormat
	switch strings.ToLower(outputFormat) {
	case "json":
		format = config.ErrorFormatJSON
	default:
		format = config.ErrorFormatText
	}

	// --strict upgrades lint warnings to errors.
	if strictMode && len(allWarnings) > 0 {
		allValidationErrors = append(allValidationErrors, allWarnings...)
		allWarnings = nil
	}

	totalFiles := len(allAgentResults) + len(allPipelineResults) +
		len(allTestSuiteResults) + len(allMCPServerResults) +
		len(allA2AServerResults) + len(allLoadErrors)
	hasErrors := len(allLoadErrors) > 0 || len(allValidationErrors) > 0

	// Print load errors.
	for _, err := range allLoadErrors {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}

	// Print validation errors, then non-fatal warnings.
	if len(allValidationErrors) > 0 {
		fmt.Fprintln(os.Stderr, config.FormatErrors(allValidationErrors, format))
	}
	for _, w := range allWarnings {
		fmt.Fprintln(os.Stderr, "Warning:", w.Error())
	}

	// Summary.
	totalErrors := len(allLoadErrors) + len(allValidationErrors)
	fmt.Fprintln(os.Stderr, config.FormatSummary(totalFiles, totalErrors))

	if hasErrors {
		os.Exit(1)
	}

	fmt.Println("All agent definitions are valid.")
	return nil
}
