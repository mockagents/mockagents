package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mockagents/mockagents/internal/drift"
	"github.com/spf13/cobra"
)

var errCriticalDrift = errors.New("critical provider drift detected")

var (
	driftSDKPath      string
	driftProviderPath string
	driftMockPath     string
	driftOperation    string
	driftAdapter      string
	driftFormat       string
	driftOutput       string
	driftIgnorePaths  []string
)

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Compare scrubbed SDK, live-provider, and MockAgents JSON shapes",
	Long: `Performs an offline three-way response-shape comparison. Artifact
collection is explicit and separate: this command never reads credentials or
makes network calls. Critical type/nullability/missing-field drift exits nonzero.`,
	Args: cobra.NoArgs,
	RunE: runDrift,
}

func init() {
	driftCmd.Flags().StringVar(&driftSDKPath, "sdk", "", "Scrubbed SDK/type JSON artifact (required)")
	driftCmd.Flags().StringVar(&driftProviderPath, "provider", "", "Scrubbed live-provider JSON artifact (required)")
	driftCmd.Flags().StringVar(&driftMockPath, "mock", "", "MockAgents JSON artifact (required)")
	driftCmd.Flags().StringVar(&driftOperation, "operation", "", "Operation label, e.g. openai.chat.completions (required)")
	driftCmd.Flags().StringVar(&driftAdapter, "adapter", "", "Adapter source file responsible for fixes")
	driftCmd.Flags().StringVar(&driftFormat, "format", "markdown", "Output format: markdown, json, sarif, or junit")
	driftCmd.Flags().StringVarP(&driftOutput, "output", "o", "", "Write report to a file (default: stdout)")
	driftCmd.Flags().StringArrayVar(&driftIgnorePaths, "ignore-path", nil, "Volatile JSON path to exclude, including descendants (repeatable, e.g. $.created)")
	_ = driftCmd.MarkFlagRequired("sdk")
	_ = driftCmd.MarkFlagRequired("provider")
	_ = driftCmd.MarkFlagRequired("mock")
	_ = driftCmd.MarkFlagRequired("operation")
	rootCmd.AddCommand(driftCmd)
}

func runDrift(cmd *cobra.Command, _ []string) error {
	load := func(label, path string) (map[string]drift.Shape, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s artifact %s: %w", label, path, err)
		}
		shape, err := drift.Extract(data)
		if err != nil {
			return nil, fmt.Errorf("%s artifact %s: %w", label, path, err)
		}
		return shape, nil
	}
	sdk, err := load("SDK", driftSDKPath)
	if err != nil {
		return err
	}
	provider, err := load("provider", driftProviderPath)
	if err != nil {
		return err
	}
	mock, err := load("mock", driftMockPath)
	if err != nil {
		return err
	}
	sdk, err = drift.IgnorePaths(sdk, driftIgnorePaths)
	if err != nil {
		return fmt.Errorf("SDK artifact: %w", err)
	}
	provider, err = drift.IgnorePaths(provider, driftIgnorePaths)
	if err != nil {
		return fmt.Errorf("provider artifact: %w", err)
	}
	mock, err = drift.IgnorePaths(mock, driftIgnorePaths)
	if err != nil {
		return fmt.Errorf("mock artifact: %w", err)
	}
	report := drift.Compare(driftOperation, sdk, provider, mock)
	report.Adapter = driftAdapter
	var output []byte
	switch driftFormat {
	case "json":
		output, err = json.MarshalIndent(report, "", " ")
		output = append(output, '\n')
	case "markdown":
		output = []byte(renderDriftMarkdown(report))
	case "sarif":
		output, err = drift.SARIF(report)
	case "junit":
		output, err = drift.JUnit(report)
	default:
		return fmt.Errorf("unsupported format %q (want markdown, json, sarif, or junit)", driftFormat)
	}
	if err != nil {
		return err
	}
	if driftOutput == "" {
		_, err = cmd.OutOrStdout().Write(output)
	} else {
		err = os.WriteFile(driftOutput, output, 0o644)
	}
	if err != nil {
		return err
	}
	if report.HasCritical() {
		return errCriticalDrift
	}
	return nil
}

func renderDriftMarkdown(report drift.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Provider drift: %s\n\n", report.Operation)
	if report.Adapter != "" {
		fmt.Fprintf(&b, "Adapter: `%s`\n\n", report.Adapter)
	}
	if len(report.Findings) == 0 {
		b.WriteString("No drift detected.\n")
		return b.String()
	}
	b.WriteString("| Severity | JSON path | Rule |\n|---|---|---|\n")
	for _, finding := range report.Findings {
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", finding.Severity, finding.Path, finding.Rule)
	}
	return b.String()
}
