package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/contract"
	"github.com/spf13/cobra"
)

var contractCmd = &cobra.Command{
	Use:   "contract",
	Short: "Extract or diff agent contracts",
	Long: `Agent definitions double as contracts. "contract extract" writes the
canonical public surface (protocol, tools with input schemas, scenarios,
streaming) as JSON so it can be version-controlled. "contract diff" compares
two extracted contracts and flags breaking changes — useful in CI to fail
the build when an agent refactor silently breaks a consumer.`,
}

var contractExtractCmd = &cobra.Command{
	Use:   "extract <agent.yaml>",
	Short: "Extract a canonical contract JSON document from an agent definition",
	Args:  cobra.ExactArgs(1),
	RunE:  runContractExtract,
}

var contractDiffCmd = &cobra.Command{
	Use:   "diff <old> <new>",
	Short: "Diff two contracts (JSON or agent YAML) and report changes",
	Long: `Compares two contracts and prints each change with a severity label.
Exits 0 when no breaking changes are present and 1 otherwise, which makes
it safe to drop into a CI pipeline. Both arguments can be agent YAML files
or already-extracted contract JSON files.`,
	Args: cobra.ExactArgs(2),
	RunE: runContractDiff,
}

var (
	contractOutput     string
	contractDiffFormat string
)

func init() {
	contractExtractCmd.Flags().StringVarP(&contractOutput, "output", "o", "", "Write contract JSON to this file (default: stdout)")
	contractDiffCmd.Flags().StringVar(&contractDiffFormat, "format", "text", "Output format: text or json")
	contractCmd.AddCommand(contractExtractCmd, contractDiffCmd)
	rootCmd.AddCommand(contractCmd)
}

// loadContract handles both agent YAML and already-extracted contract JSON.
// We dispatch by sniffing the first character: JSON contracts start with '{'.
func loadContract(path string) (*contract.Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	// Skip leading whitespace.
	i := 0
	for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r' || data[i] == '\t') {
		i++
	}
	if i < len(data) && data[i] == '{' {
		var c contract.Contract
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("%s: invalid contract JSON: %w", path, err)
		}
		return &c, nil
	}
	// Otherwise treat as agent YAML.
	result, err := config.LoadFile(path)
	if err != nil {
		return nil, err
	}
	config.ApplyDefaults(result.Definition)
	return contract.Extract(result.Definition), nil
}

func runContractExtract(cmd *cobra.Command, args []string) error {
	c, err := loadContract(args[0])
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if contractOutput == "" {
		fmt.Println(string(out))
		return nil
	}
	return os.WriteFile(contractOutput, append(out, '\n'), 0o644)
}

func runContractDiff(cmd *cobra.Command, args []string) error {
	oldC, err := loadContract(args[0])
	if err != nil {
		return err
	}
	newC, err := loadContract(args[1])
	if err != nil {
		return err
	}
	changes := contract.Diff(oldC, newC)

	switch contractDiffFormat {
	case "json":
		out, _ := json.MarshalIndent(changes, "", "  ")
		fmt.Println(string(out))
	default:
		if len(changes) == 0 {
			fmt.Println("No changes detected.")
		} else {
			for _, c := range changes {
				fmt.Printf("  [%s] %s: %s\n", c.Severity, c.Path, c.Message)
			}
		}
	}

	if contract.HasBreaking(changes) {
		fmt.Fprintln(os.Stderr, "\nbreaking changes detected")
		os.Exit(1)
	}
	return nil
}
