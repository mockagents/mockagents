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
	driftSDKHeaders   string
	driftProvHeaders  string
	driftMockHeaders  string
	driftSDKEnums     string
	driftProvEnums    string
	driftMockEnums    string
	driftSDKEvents    string
	driftProvEvents   string
	driftMockEvents   string
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
	driftCmd.Flags().StringVar(&driftSDKHeaders, "sdk-headers", "", "Scrubbed SDK/type header JSON artifact (requires all header flags)")
	driftCmd.Flags().StringVar(&driftProvHeaders, "provider-headers", "", "Scrubbed live-provider header JSON artifact (requires all header flags)")
	driftCmd.Flags().StringVar(&driftMockHeaders, "mock-headers", "", "MockAgents header JSON artifact (requires all header flags)")
	driftCmd.Flags().StringVar(&driftSDKEnums, "sdk-enums", "", "SDK enum inventory JSON artifact (requires all enum flags)")
	driftCmd.Flags().StringVar(&driftProvEnums, "provider-enums", "", "Live-provider enum inventory JSON artifact (requires all enum flags)")
	driftCmd.Flags().StringVar(&driftMockEnums, "mock-enums", "", "MockAgents enum inventory JSON artifact (requires all enum flags)")
	driftCmd.Flags().StringVar(&driftSDKEvents, "sdk-events", "", "SDK stream event sequence JSON artifact (requires all event flags)")
	driftCmd.Flags().StringVar(&driftProvEvents, "provider-events", "", "Live-provider stream event sequence JSON artifact (requires all event flags)")
	driftCmd.Flags().StringVar(&driftMockEvents, "mock-events", "", "MockAgents stream event sequence JSON artifact (requires all event flags)")
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
	headerPaths := []string{driftSDKHeaders, driftProvHeaders, driftMockHeaders}
	headerCount := 0
	for _, path := range headerPaths {
		if path != "" {
			headerCount++
		}
	}
	if headerCount != 0 && headerCount != len(headerPaths) {
		return errors.New("--sdk-headers, --provider-headers, and --mock-headers must be provided together")
	}
	if headerCount == len(headerPaths) {
		loadHeaders := func(label, path string) (map[string]drift.Shape, error) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("reading %s header artifact %s: %w", label, path, readErr)
			}
			shape, extractErr := drift.ExtractHeaders(data)
			if extractErr != nil {
				return nil, fmt.Errorf("%s header artifact %s: %w", label, path, extractErr)
			}
			return shape, nil
		}
		sdkHeaders, loadErr := loadHeaders("SDK", driftSDKHeaders)
		if loadErr != nil {
			return loadErr
		}
		providerHeaders, loadErr := loadHeaders("provider", driftProvHeaders)
		if loadErr != nil {
			return loadErr
		}
		mockHeaders, loadErr := loadHeaders("mock", driftMockHeaders)
		if loadErr != nil {
			return loadErr
		}
		sdk = drift.MergeShapes(sdk, sdkHeaders)
		provider = drift.MergeShapes(provider, providerHeaders)
		mock = drift.MergeShapes(mock, mockHeaders)
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
	enumPaths := []string{driftSDKEnums, driftProvEnums, driftMockEnums}
	enumCount := 0
	for _, path := range enumPaths {
		if path != "" {
			enumCount++
		}
	}
	if enumCount != 0 && enumCount != len(enumPaths) {
		return errors.New("--sdk-enums, --provider-enums, and --mock-enums must be provided together")
	}
	if enumCount == len(enumPaths) {
		loadEnums := func(label, path string) (drift.EnumSet, error) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("reading %s enum artifact %s: %w", label, path, readErr)
			}
			values, extractErr := drift.ExtractEnums(data)
			if extractErr != nil {
				return nil, fmt.Errorf("%s enum artifact %s: %w", label, path, extractErr)
			}
			return drift.IgnoreEnumPaths(values, driftIgnorePaths)
		}
		sdkEnums, loadErr := loadEnums("SDK", driftSDKEnums)
		if loadErr != nil {
			return loadErr
		}
		providerEnums, loadErr := loadEnums("provider", driftProvEnums)
		if loadErr != nil {
			return loadErr
		}
		mockEnums, loadErr := loadEnums("mock", driftMockEnums)
		if loadErr != nil {
			return loadErr
		}
		report = drift.MergeFindings(report, drift.CompareEnums(driftOperation, sdkEnums, providerEnums, mockEnums))
	}
	eventPaths := []string{driftSDKEvents, driftProvEvents, driftMockEvents}
	eventCount := 0
	for _, path := range eventPaths {
		if path != "" {
			eventCount++
		}
	}
	if eventCount != 0 && eventCount != len(eventPaths) {
		return errors.New("--sdk-events, --provider-events, and --mock-events must be provided together")
	}
	if eventCount == len(eventPaths) && !containsString(driftIgnorePaths, "$events") && !containsString(driftIgnorePaths, "$") {
		loadEvents := func(label, path string) ([]string, error) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("reading %s event artifact %s: %w", label, path, readErr)
			}
			events, extractErr := drift.ExtractEvents(data)
			if extractErr != nil {
				return nil, fmt.Errorf("%s event artifact %s: %w", label, path, extractErr)
			}
			return events, nil
		}
		sdkEvents, loadErr := loadEvents("SDK", driftSDKEvents)
		if loadErr != nil {
			return loadErr
		}
		providerEvents, loadErr := loadEvents("provider", driftProvEvents)
		if loadErr != nil {
			return loadErr
		}
		mockEvents, loadErr := loadEvents("mock", driftMockEvents)
		if loadErr != nil {
			return loadErr
		}
		report = drift.MergeFindings(report, drift.CompareEvents(driftOperation, sdkEvents, providerEvents, mockEvents))
	}
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
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
	b.WriteString("| Severity | JSON path | Rule | Values |\n|---|---|---|---|\n")
	for _, finding := range report.Findings {
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", finding.Severity, finding.Path, finding.Rule, drift.FindingValueSummary(finding))
	}
	return b.String()
}
