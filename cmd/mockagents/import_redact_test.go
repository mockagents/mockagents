package main

// #39: `mockagents record` could mask secrets before they reached a cassette,
// but the import subcommands could not — and re-recording is not an option when
// the source is someone else's vcrpy file or an exported OpenAI log. These
// drive the commands end to end and read the written cassette back.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/recording"
	"github.com/spf13/cobra"
)

const (
	fakeOpenAIKey = "sk-proj-abcdefghijklmnopqrstuvwxyz0123456789"
	fakeGitHubPAT = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	licencePlate  = "PROJECT-ORION"
)

// storedCompletionsFixture is a one-line OpenAI stored-completions export whose
// request carries a key and whose response carries a token and a codename.
func storedCompletionsFixture(t *testing.T) string {
	t.Helper()
	line := `{"id":"chatcmpl-1","model":"gpt-4o",` +
		`"messages":[{"role":"user","content":"use ` + fakeOpenAIKey + ` to call the api"}],` +
		`"choices":[{"message":{"content":"done, and ` + fakeGitHubPAT + ` for ` + licencePlate + `"}}]}`
	path := filepath.Join(t.TempDir(), "stored.jsonl")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

// runImport executes the import command with the given args, restoring the
// package-level flag state afterwards — cobra does not reset it between runs.
func runImport(t *testing.T, args ...string) string {
	t.Helper()
	cassette := filepath.Join(t.TempDir(), "out.jsonl")
	prevCassette, prevAll := importCassette, importAll
	prevRedact, prevPatterns := importRedact, importRedactPatterns
	t.Cleanup(func() {
		importCassette, importAll = prevCassette, prevAll
		importRedact, importRedactPatterns = prevRedact, prevPatterns
		_ = importOpenAICmd.Flags().Set("redact", "false")
		_ = importVCRCmd.Flags().Set("redact", "false")
		rootCmd.SetArgs(nil)
	})

	// Execute from the root: cobra walks up to it anyway, so running a
	// subcommand directly just prints the root help.
	rootCmd.SetArgs(append(append([]string{"import"}, args...), "--cassette", cassette))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("import %v: %v", args, err)
	}
	return cassette
}

func readCassette(t *testing.T, path string) (*recording.Cassette, string) {
	t.Helper()
	cass, err := recording.Load(path)
	if err != nil {
		t.Fatalf("loading cassette: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cassette: %v", err)
	}
	return cass, string(raw)
}

func TestImportOpenAI_RedactMasksSecrets(t *testing.T) {
	fixture := storedCompletionsFixture(t)

	cassette := runImport(t, "openai-stored-completions", fixture, "--redact")

	cass, raw := readCassette(t, cassette)
	if strings.Contains(raw, fakeOpenAIKey) {
		t.Error("the OpenAI key survived --redact")
	}
	if strings.Contains(raw, fakeGitHubPAT) {
		t.Error("the GitHub token survived --redact")
	}
	// Structure has to survive: a cassette that no longer parses is worse than
	// one with a secret in it, because nothing will tell you.
	if cass.Len() != 1 {
		t.Fatalf("expected 1 interaction after redaction, got %d", cass.Len())
	}
	it := cass.All()[0]
	if it.Path != "/v1/chat/completions" || it.Method != "POST" {
		t.Errorf("routing lost: %s %s", it.Method, it.Path)
	}
	var req map[string]any
	if err := json.Unmarshal(it.RequestBody, &req); err != nil {
		t.Fatalf("request body is no longer valid JSON: %v", err)
	}
	if req["model"] != "gpt-4o" {
		t.Errorf("model lost through redaction: %v", req["model"])
	}
}

func TestImportOpenAI_WithoutRedactKeepsSecrets(t *testing.T) {
	// The flag has to actually be the thing doing the work — otherwise the
	// test above could pass for the wrong reason.
	fixture := storedCompletionsFixture(t)

	cassette := runImport(t, "openai-stored-completions", fixture)

	_, raw := readCassette(t, cassette)
	if !strings.Contains(raw, fakeOpenAIKey) {
		t.Error("expected the unredacted import to keep the key verbatim")
	}
}

func TestImportOpenAI_RedactPatternImpliesRedact(t *testing.T) {
	// A codename is not a credential shape, so only a custom pattern catches
	// it — and passing one alone must be enough, as it is on record.
	fixture := storedCompletionsFixture(t)

	cassette := runImport(t, "openai-stored-completions", fixture, "--redact-pattern", licencePlate)

	_, raw := readCassette(t, cassette)
	if strings.Contains(raw, licencePlate) {
		t.Error("--redact-pattern alone did not mask the pattern")
	}
	if strings.Contains(raw, fakeOpenAIKey) {
		t.Error("--redact-pattern should imply --redact, so built-ins apply too")
	}
}

func TestImportOpenAI_BadRedactPatternFails(t *testing.T) {
	fixture := storedCompletionsFixture(t)
	cassette := filepath.Join(t.TempDir(), "out.jsonl")
	t.Cleanup(func() {
		importRedact, importRedactPatterns = false, nil
		_ = importOpenAICmd.Flags().Set("redact", "false")
		rootCmd.SetArgs(nil)
	})

	rootCmd.SetArgs([]string{
		"import", "openai-stored-completions", fixture,
		"--cassette", cassette, "--redact-pattern", "([unclosed",
	})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("an uncompilable pattern should fail the import, not be ignored")
	}
	if _, err := os.Stat(cassette); err == nil {
		t.Error("no cassette should be written when the pattern is rejected")
	}
}

// vcrFixture is a minimal vcrpy cassette whose bodies carry secrets.
func vcrFixture(t *testing.T) string {
	t.Helper()
	yaml := `
interactions:
- request:
    method: POST
    uri: https://api.openai.com/v1/chat/completions
    body: '{"model":"gpt-4o","messages":[{"role":"user","content":"use ` + fakeOpenAIKey + `"}]}'
    headers:
      Content-Type: [application/json]
  response:
    status: {code: 200, message: OK}
    headers:
      Content-Type: [application/json]
    body: {string: '{"id":"chatcmpl-1","choices":[{"message":{"content":"ok ` + fakeGitHubPAT + `"}}]}'}
version: 1
`
	path := filepath.Join(t.TempDir(), "cassette.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestImportVCR_RedactMasksSecrets(t *testing.T) {
	fixture := vcrFixture(t)

	cassette := runImport(t, "vcr", fixture, "--redact")

	cass, raw := readCassette(t, cassette)
	if strings.Contains(raw, fakeOpenAIKey) {
		t.Error("the OpenAI key survived --redact")
	}
	if strings.Contains(raw, fakeGitHubPAT) {
		t.Error("the GitHub token survived --redact")
	}
	if cass.Len() != 1 {
		t.Fatalf("expected 1 interaction, got %d", cass.Len())
	}
	it := cass.All()[0]
	if it.Path != "/v1/chat/completions" || it.ResponseStatus != 200 {
		t.Errorf("structure lost: %s status=%d", it.Path, it.ResponseStatus)
	}
	var resp map[string]any
	if err := json.Unmarshal(it.ResponseBody, &resp); err != nil {
		t.Fatalf("response body is no longer valid JSON: %v", err)
	}
	if resp["id"] != "chatcmpl-1" {
		t.Errorf("response id lost through redaction: %v", resp["id"])
	}
}

func TestImportVCR_WithoutRedactKeepsSecrets(t *testing.T) {
	fixture := vcrFixture(t)

	cassette := runImport(t, "vcr", fixture)

	_, raw := readCassette(t, cassette)
	if !strings.Contains(raw, fakeOpenAIKey) {
		t.Error("expected the unredacted import to keep the key verbatim")
	}
}

// Both subcommands must offer the flags — the issue is that one path had them
// and the other did not.
func TestImportSubcommandsBothOfferRedactFlags(t *testing.T) {
	for _, cmd := range []*cobra.Command{importVCRCmd, importOpenAICmd} {
		for _, name := range []string{"redact", "redact-pattern"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("%q has no --%s flag", cmd.Name(), name)
			}
		}
	}
}

// The help text used to send the user away to re-record, which is not possible
// when the source is someone else's cassette.
func TestImportHelpNoLongerSaysReRecord(t *testing.T) {
	for _, cmd := range []*cobra.Command{importVCRCmd, importOpenAICmd} {
		if strings.Contains(cmd.Long, "re-record") {
			t.Errorf("%q still tells the user to re-record instead of redacting", cmd.Name())
		}
		if !strings.Contains(cmd.Long, "--redact") {
			t.Errorf("%q does not mention --redact", cmd.Name())
		}
	}
}
