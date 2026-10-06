package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/spf13/cobra"
)

// Exit codes (the CI contract — pinned by validate_test.go):
//
//	0 - every document is valid
//	1 - validation or load errors, or no documents were found
//	2 - usage error: a path that does not exist, an unknown --format
const (
	exitValid   = 0
	exitInvalid = 1
	exitUsage   = 2
)

var validateCmd = &cobra.Command{
	Use:   "validate [file|directory...]",
	Short: "Validate agent definition files",
	Long: `Validate one or more agent definition files (YAML or JSON) against
the MockAgents schema. Reports all errors with file path, line number,
field path, and actionable suggestions.

If no arguments are given, validates files in the --agents-dir directory.

Exit codes: 0 all valid; 1 errors found, or no documents found (unless
--allow-empty); 2 a path does not exist or the flags are invalid.

--format json writes exactly one JSON document to stdout:
  {"valid": bool, "files": N, "errors": [...], "load_errors": [...], "warnings": [...]}`,
	RunE: runValidate,
}

var (
	outputFormat string
	strictMode   bool
	allowEmpty   bool
)

func init() {
	validateCmd.Flags().StringVar(&outputFormat, "format", "text", "Output format: text or json")
	validateCmd.Flags().BoolVar(&strictMode, "strict", false, "Treat warnings as errors")
	validateCmd.Flags().BoolVar(&allowEmpty, "allow-empty", false, "Succeed when no documents are found")
}

// validateOptions are the inputs of one validate run.
type validateOptions struct {
	Paths      []string
	Format     string
	Strict     bool
	AllowEmpty bool
}

// validateReport is the --format json output, and the data the text output is
// rendered from.
type validateReport struct {
	Valid      bool                      `json:"valid"`
	Files      int                       `json:"files"`
	Errors     []*config.ValidationError `json:"errors"`
	LoadErrors []string                  `json:"load_errors"`
	Warnings   []*config.ValidationError `json:"warnings"`
}

func runValidate(cmd *cobra.Command, args []string) error {
	paths := args
	if len(paths) == 0 {
		agentsDir, _ := cmd.Flags().GetString("agents-dir")
		paths = []string{agentsDir}
	}
	code := executeValidate(validateOptions{
		Paths: paths, Format: outputFormat, Strict: strictMode, AllowEmpty: allowEmpty,
	}, os.Stdout, os.Stderr)
	if code != exitValid {
		os.Exit(code)
	}
	return nil
}

// executeValidate runs a validation and writes its report, returning the exit
// code. In JSON mode stdout carries exactly one JSON document and nothing
// else; in text mode diagnostics go to stderr and the success line to stdout.
func executeValidate(opts validateOptions, stdout, stderr io.Writer) int {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format != "text" && format != "json" {
		fmt.Fprintf(stderr, "Error: unknown --format %q (want text or json)\n", opts.Format)
		return exitUsage
	}

	docs, loadErrs, usageErrs := loadValidateInputs(opts.Paths)
	if len(usageErrs) > 0 {
		if format == "json" {
			writeValidateJSON(stdout, &validateReport{LoadErrors: errorStrings(usageErrs)})
		} else {
			for _, err := range usageErrs {
				fmt.Fprintln(stderr, "Error:", err)
			}
		}
		return exitUsage
	}

	errs, warnings := validateDocuments(docs)
	if opts.Strict && len(warnings) > 0 {
		errs = append(errs, warnings...)
		warnings = nil
	}

	files := docs.Count() + len(loadErrs)
	if files == 0 && !opts.AllowEmpty {
		loadErrs = append(loadErrs, fmt.Errorf(
			"no MockAgents documents found under %s (pass --allow-empty if that is expected)",
			strings.Join(opts.Paths, ", ")))
	}

	report := &validateReport{
		Valid:      len(errs) == 0 && len(loadErrs) == 0,
		Files:      files,
		Errors:     errs,
		LoadErrors: errorStrings(loadErrs),
		Warnings:   warnings,
	}

	if format == "json" {
		writeValidateJSON(stdout, report)
	} else {
		for _, err := range loadErrs {
			fmt.Fprintln(stderr, "Error:", err)
		}
		if len(errs) > 0 {
			fmt.Fprintln(stderr, config.FormatErrors(errs, config.ErrorFormatText))
		}
		for _, w := range warnings {
			fmt.Fprintln(stderr, "Warning:", w.Error())
		}
		fmt.Fprintln(stderr, config.FormatSummary(files, len(errs)+len(loadErrs)))
		if report.Valid {
			fmt.Fprintln(stdout, "All agent definitions are valid.")
		}
	}

	if !report.Valid {
		return exitInvalid
	}
	return exitValid
}

// loadValidateInputs loads every path. A path that cannot be accessed is a
// usage error (exit 2); a file that fails to parse is a load error (exit 1).
// A file reached twice — named directly and inside a named directory, or
// through two spellings of one path — is loaded once, so it is not reported
// as a duplicate of itself.
func loadValidateInputs(paths []string) (*config.Documents, []error, []error) {
	docs := &config.Documents{}
	var loadErrs, usageErrs []error
	seen := map[string]bool{}

	loadFile := func(path string) {
		key := fileIdentity(path)
		if seen[key] {
			return
		}
		seen[key] = true
		fileDocs, err := config.LoadDocumentFile(path)
		if err != nil {
			loadErrs = append(loadErrs, err)
			return
		}
		docs.Merge(fileDocs)
	}

	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			usageErrs = append(usageErrs, fmt.Errorf("resolving path %s: %w", p, err))
			continue
		}
		info, err := os.Stat(absPath)
		if err != nil {
			usageErrs = append(usageErrs, fmt.Errorf("accessing %s: %w", p, err))
			continue
		}
		if !info.IsDir() {
			loadFile(absPath)
			continue
		}
		files, err := config.ListDocumentPaths(absPath)
		if err != nil {
			loadErrs = append(loadErrs, err)
			continue
		}
		for _, f := range files {
			loadFile(f)
		}
	}
	return docs, loadErrs, usageErrs
}

// fileIdentity is the key two spellings of one file share: the cleaned
// absolute path, case-folded on the case-insensitive filesystems Windows and
// macOS use by default.
func fileIdentity(path string) string {
	p := filepath.Clean(path)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}

// validateDocuments runs every per-kind validator and the cross-document pass,
// returning errors and non-fatal lint warnings.
func validateDocuments(docs *config.Documents) ([]*config.ValidationError, []*config.ValidationError) {
	var errs, warnings []*config.ValidationError
	collect := func(l *config.ValidationErrorList) {
		if l != nil {
			errs = append(errs, l.Errors...)
		}
	}

	validator := &config.Validator{}
	for _, r := range docs.Agents {
		config.ApplyDefaults(r.Definition)
		collect(validator.Validate(r.Definition, r.FilePath, r.Node))
		warnings = append(warnings, validator.Lint(r.Definition, r.FilePath, r.Node)...)
	}
	for _, r := range docs.Pipelines {
		collect(config.ValidatePipeline(r.Definition, r.FilePath, r.Node))
	}
	for _, r := range docs.TestSuites {
		collect(config.ValidateTestSuite(r.Definition, r.FilePath, r.Node))
	}
	for _, r := range docs.MCPServers {
		collect(config.ValidateMCPServer(r.Definition, r.FilePath, r.Node))
	}
	for _, r := range docs.A2AServers {
		collect(config.ValidateA2AServer(r.Definition, r.FilePath, r.Node))
	}
	for _, r := range docs.Vectors {
		collect(config.ValidateVectorCollection(r.Definition, r.FilePath, r.Node))
	}
	for _, r := range docs.SearchServices {
		collect(config.ValidateSearchService(r.Definition, r.FilePath, r.Node))
	}

	// Cross-document checks: refs resolve, and no two documents of one kind
	// claim one name. Run even for an agents-only tree — a name collision
	// needs no pipelines at all.
	collect(config.ValidateDocuments(docs))
	warnings = append(warnings, config.LintDocuments(docs)...)
	return errs, warnings
}

func writeValidateJSON(w io.Writer, report *validateReport) {
	if report.Errors == nil {
		report.Errors = []*config.ValidationError{}
	}
	if report.Warnings == nil {
		report.Warnings = []*config.ValidationError{}
	}
	if report.LoadErrors == nil {
		report.LoadErrors = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

func errorStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		out = append(out, err.Error())
	}
	return out
}
