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

	var allResults []*config.LoadResult
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
			results, errs := config.LoadDir(absPath)
			allResults = append(allResults, results...)
			allLoadErrors = append(allLoadErrors, errs...)
		} else {
			result, err := config.LoadFile(absPath)
			if err != nil {
				allLoadErrors = append(allLoadErrors, err)
			} else {
				allResults = append(allResults, result)
			}
		}
	}

	var allValidationErrors []*config.ValidationError
	validator := &config.Validator{}

	for _, result := range allResults {
		config.ApplyDefaults(result.Definition)
		if errList := validator.Validate(result.Definition, result.FilePath, result.Node); errList != nil {
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

	totalFiles := len(allResults) + len(allLoadErrors)
	hasErrors := len(allLoadErrors) > 0 || len(allValidationErrors) > 0

	// Print load errors.
	for _, err := range allLoadErrors {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}

	// Print validation errors.
	if len(allValidationErrors) > 0 {
		fmt.Fprintln(os.Stderr, config.FormatErrors(allValidationErrors, format))
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
