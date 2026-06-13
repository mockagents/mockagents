package main

import (
	"fmt"
	"os"

	"github.com/mockagents/mockagents/internal/recording"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import an existing cassette or stored completions into a MockAgents cassette",
	Long: `Convert recordings captured by other tools into a MockAgents cassette that
"mockagents replay" can serve. Supports vcrpy YAML cassettes and OpenAI
stored-completions JSONL exports.`,
}

var importVCRCmd = &cobra.Command{
	Use:   "vcr <input-file>",
	Short: "Import a vcrpy YAML cassette",
	Long: `Import a vcrpy (Python) YAML cassette into a MockAgents cassette.

By default only POST requests to known LLM endpoints (/v1/chat/completions,
/v1/messages, /v1/embeddings, /v1/responses, /v1/moderations) are imported; pass
--all to import every interaction. base64 and gzip'd response bodies are decoded,
and credential-bearing headers (Authorization, Cookie, X-Api-Key, x-goog-api-key,
bearer/token/secret headers, ...) are dropped. Note: secrets embedded in request
or response BODIES are NOT redacted — review the cassette before committing (or
re-record through "mockagents record --redact").`,
	Args: cobra.ExactArgs(1),
	RunE: runImportVCR,
}

var importOpenAICmd = &cobra.Command{
	Use:   "openai-stored-completions <input-file>",
	Short: "Import an OpenAI stored-completions JSONL export",
	Long: `Import an OpenAI stored-completions JSONL file (one JSON object per line) into a
MockAgents cassette. Each line must be either an envelope
{"request": {...}, "response": {...}} or a flat stored ChatCompletion that
carries its input under "input" or "messages". Unrecognized lines are skipped
with a reason; all imported interactions target POST /v1/chat/completions.`,
	Args: cobra.ExactArgs(1),
	RunE: runImportOpenAI,
}

var (
	importCassette string
	importAll      bool
)

func init() {
	importVCRCmd.Flags().StringVarP(&importCassette, "cassette", "o", "cassette.jsonl", "Output cassette path")
	importVCRCmd.Flags().BoolVar(&importAll, "all", false, "Import every interaction, not just POSTs to known LLM paths")
	importOpenAICmd.Flags().StringVarP(&importCassette, "cassette", "o", "cassette.jsonl", "Output cassette path")
	importCmd.AddCommand(importVCRCmd)
	importCmd.AddCommand(importOpenAICmd)
	rootCmd.AddCommand(importCmd)
}

func runImportVCR(cmd *cobra.Command, args []string) error {
	f, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("opening input: %w", err)
	}
	defer f.Close()

	interactions, res, err := recording.ImportVCR(f, recording.ImportVCROpts{AllInteractions: importAll})
	if err != nil {
		return err
	}
	return writeImported(interactions, res)
}

func runImportOpenAI(cmd *cobra.Command, args []string) error {
	f, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("opening input: %w", err)
	}
	defer f.Close()

	interactions, res, err := recording.ImportOpenAIStored(f)
	if err != nil {
		return err
	}
	return writeImported(interactions, res)
}

// writeImported reports skips, persists the interactions, and prints a summary.
func writeImported(interactions []*recording.Interaction, res recording.ImportResult) error {
	for _, r := range res.SkipReasons {
		fmt.Fprintln(os.Stderr, "skip:", r)
	}
	if res.Imported == 0 {
		fmt.Printf("no interactions imported (%d skipped)\n", res.Skipped)
		return nil
	}
	cass, err := recording.Load(importCassette)
	if err != nil {
		return fmt.Errorf("loading output cassette: %w", err)
	}
	if err := cass.AppendAll(interactions); err != nil {
		return fmt.Errorf("writing cassette: %w", err)
	}
	if res.Skipped > 0 {
		fmt.Printf("imported %d interactions (%d skipped) → %s\n", res.Imported, res.Skipped, importCassette)
	} else {
		fmt.Printf("imported %d interactions → %s\n", res.Imported, importCassette)
	}
	return nil
}
