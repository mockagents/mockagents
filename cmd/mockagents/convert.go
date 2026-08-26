package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mockagents/mockagents/internal/conversion"
	"github.com/spf13/cobra"
)

var convertCmd = &cobra.Command{
	Use:   "convert",
	Short: "Convert fixtures from another mock framework",
}

var convertAIMockCmd = &cobra.Command{
	Use:   "aimock <fixture-file>",
	Short: "Convert AIMock JSON fixtures to a MockAgents agent",
	Long: `Convert the safely representable AIMock fixture subset into one MockAgents
Agent YAML file. userMessage and turnIndex matchers, text/refusal responses, and
toolCalls are preserved. Fixtures using AIMock-only matchers are skipped with a
warning instead of being broadened into unsafe catch-alls.`,
	Args: cobra.ExactArgs(1),
	RunE: runConvertAIMock,
}

var (
	convertOutput   string
	convertName     string
	convertProtocol string
	convertModel    string
	convertForce    bool
)

func init() {
	convertAIMockCmd.Flags().StringVarP(&convertOutput, "output", "o", "aimock-agent.yaml", "Output Agent YAML path, or - for stdout")
	convertAIMockCmd.Flags().StringVar(&convertName, "name", "", "Agent name (default: sanitized input filename)")
	convertAIMockCmd.Flags().StringVar(&convertProtocol, "protocol", "openai-chat-completions", "MockAgents protocol")
	convertAIMockCmd.Flags().StringVar(&convertModel, "model", "mock-agent", "Model reported by the converted agent")
	convertAIMockCmd.Flags().BoolVar(&convertForce, "force", false, "Overwrite an existing output file")
	convertCmd.AddCommand(convertAIMockCmd)
	rootCmd.AddCommand(convertCmd)
}

var nonNameChars = regexp.MustCompile(`[^a-z0-9]+`)

func aimockAgentName(input string) string {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)))
	name = strings.Trim(nonNameChars.ReplaceAllString(name, "-"), "-")
	if name == "" {
		return "aimock-agent"
	}
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	return name
}

func runConvertAIMock(cmd *cobra.Command, args []string) error {
	input, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("opening AIMock fixtures: %w", err)
	}
	defer input.Close()
	name := convertName
	if name == "" {
		name = aimockAgentName(args[0])
	}
	data, result, err := conversion.ConvertAIMock(input, conversion.AIMockOptions{Name: name, Protocol: convertProtocol, Model: convertModel})
	for _, warning := range result.Warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "skip:", warning)
	}
	if err != nil {
		return err
	}
	if convertOutput == "-" {
		if _, err := cmd.OutOrStdout().Write(data); err != nil {
			return err
		}
	} else if convertForce {
		if err := os.WriteFile(convertOutput, data, 0o644); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	} else {
		file, err := os.OpenFile(convertOutput, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("creating output (use --force to overwrite): %w", err)
		}
		if _, err = file.Write(data); err != nil {
			_ = file.Close()
			return fmt.Errorf("writing output: %w", err)
		}
		if err = file.Close(); err != nil {
			return fmt.Errorf("closing output: %w", err)
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "converted %d AIMock fixtures", result.Imported)
	if result.Skipped > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), " (%d skipped)", result.Skipped)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), " -> %s\n", convertOutput)
	return nil
}
