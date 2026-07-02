package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/mockagents/mockagents/internal/cli"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Scaffold a new MockAgents project",
	Long: `Create a new MockAgents project directory with sample agent definitions,
test files, and a project configuration file.

Pick a starter pack with --template (see --list-templates). If no project name
is given, scaffolds in the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

var (
	forceInit     bool
	initTemplate  string
	listTemplates bool
)

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "Overwrite existing files (for a named project dir, replaces its agents/ and tests/)")
	initCmd.Flags().StringVarP(&initTemplate, "template", "t", cli.DefaultTemplate, "Starter pack to scaffold (see --list-templates)")
	initCmd.Flags().BoolVar(&listTemplates, "list-templates", false, "List available starter packs and exit")
}

func runInit(cmd *cobra.Command, args []string) error {
	if listTemplates {
		printTemplates()
		return nil
	}

	projectName := "."
	if len(args) > 0 {
		projectName = args[0]
	}

	res, err := cli.ScaffoldWithResult(cli.ScaffoldOptions{
		ProjectName: projectName,
		TargetDir:   projectName,
		Force:       forceInit,
		Template:    initTemplate,
	})
	if err != nil {
		return err
	}

	printSuccess(fmt.Sprintf("Project scaffolded at %s (template: %s)", res.Dir, res.Template))
	fmt.Println("\nNext steps:")
	if projectName != "." {
		fmt.Println("  1. cd", projectName)
	}
	fmt.Println("  - mockagents validate agents")
	fmt.Println("  - mockagents start --agents-dir agents")
	for _, t := range res.Tests {
		fmt.Printf("  - mockagents test --agents-dir agents %s\n", t)
	}
	return nil
}

func printTemplates() {
	fmt.Println("Available templates:")
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, t := range cli.ListTemplates() {
		fmt.Fprintf(w, "  %s\t%s\n", t.Name, t.Description)
	}
	_ = w.Flush()
	fmt.Println("\nUsage: mockagents init my-project --template <name>")
}
