package main

import (
	"fmt"

	"github.com/mockagents/mockagents/internal/cli"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Scaffold a new MockAgents project",
	Long: `Create a new MockAgents project directory with sample agent definitions,
test files, and a project configuration file.

If no project name is given, scaffolds in the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

var forceInit bool

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "Overwrite existing files")
}

func runInit(cmd *cobra.Command, args []string) error {
	projectName := "."
	if len(args) > 0 {
		projectName = args[0]
	}

	opts := cli.ScaffoldOptions{
		ProjectName: projectName,
		TargetDir:   projectName,
		Force:       forceInit,
	}

	absDir, err := cli.Scaffold(opts)
	if err != nil {
		return err
	}

	printSuccess(fmt.Sprintf("Project scaffolded at %s", absDir))
	fmt.Println("\nNext steps:")
	fmt.Println("  1. cd", projectName)
	fmt.Println("  2. mockagents validate")
	fmt.Println("  3. mockagents start")
	return nil
}
