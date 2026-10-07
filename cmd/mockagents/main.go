package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/mockagents/mockagents/internal/cli"
	"github.com/spf13/cobra"
)

// version is stamped at release time with -ldflags "-X main.version=...".
// A binary built by `go install …@vX.Y.Z` has no ldflags, so it falls back to
// the module version Go records in the build info; it used to report "dev",
// which made the install-path monitor fail every day (review O-06).
var version = resolveVersion("dev")

func resolveVersion(stamped string) string {
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}
	return stamped
}

var rootCmd = &cobra.Command{
	Use:   "mockagents",
	Short: "MockAgents — simulate, test, and validate AI agent integrations",
	Long: `MockAgents is an open-source platform for simulating, testing, and
validating AI agent integrations. Define mock agents with configurable
behaviors, tool responses, latency profiles, and failure modes — without
calling real LLMs or burning tokens.`,
	Version: version,
	// Errors are printed once, by main. Without these cobra printed
	// "Error: X", the full usage text, and then main printed X again for
	// every runtime failure; usage is only useful for a usage mistake, which
	// the flag-error hook below still reports with a pointer to --help.
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if noColor {
			cli.DisableColor()
		}
	},
}

var noColor bool

// Process-level seams, replaced only by tests: notifySignals lets a test
// deliver the shutdown signal the serve loops wait for, and osExit lets it
// observe the exit code a command reports without ending the test binary.
var (
	notifySignals = signal.Notify
	osExit        = os.Exit
)

func init() {
	rootCmd.PersistentFlags().String("agents-dir", envOrDefault("MOCKAGENTS_AGENTS_DIR", "./agents"), "Directory containing agent definition files")
	rootCmd.PersistentFlags().String("log-level", envOrDefault("MOCKAGENTS_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")

	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage.", err, cmd.CommandPath())
	})

	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(logsCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
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
