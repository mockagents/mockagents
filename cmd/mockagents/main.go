package main

import (
	"fmt"
	"os"

	"github.com/mockagents/mockagents/internal/cli"
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "mockagents",
	Short: "MockAgents — simulate, test, and validate AI agent integrations",
	Long: `MockAgents is an open-source platform for simulating, testing, and
validating AI agent integrations. Define mock agents with configurable
behaviors, tool responses, latency profiles, and failure modes — without
calling real LLMs or burning tokens.`,
	Version: version,
}

var noColor bool

func init() {
	rootCmd.PersistentFlags().String("agents-dir", envOrDefault("MOCKAGENTS_AGENTS_DIR", "./agents"), "Directory containing agent definition files")
	rootCmd.PersistentFlags().String("log-level", envOrDefault("MOCKAGENTS_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")

	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(logsCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

// envOrDefault returns the environment variable value or a default.
func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// printSuccess prints a colored success message.
func printSuccess(msg string) {
	cli.PrintSuccess(msg)
}
