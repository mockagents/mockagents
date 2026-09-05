package server

import (
	"os"
	"path/filepath"
	"testing"
)

// U3-6: the catalog's two new columns. "Does this survive a restart" is not a
// question a count of agents can answer, and three outcomes are genuinely
// different: backed by a file, never had one, or had one that is now gone.
func TestSummarizePersistence(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.yaml")
	if err := os.WriteFile(present, []byte("kind: Agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("no source path is runtime-only", func(t *testing.T) {
		kind, file := summarizePersistence("")
		if kind != agentRuntimeOnly || file != "" {
			t.Errorf("got (%q, %q), want (%q, \"\")", kind, file, agentRuntimeOnly)
		}
	})

	t.Run("a present file is persisted, named by base name only", func(t *testing.T) {
		kind, file := summarizePersistence(present)
		if kind != agentPersistedToFile {
			t.Errorf("kind = %q, want %q", kind, agentPersistedToFile)
		}
		// The absolute path is server-side detail; a client has no use for it.
		if file != "present.yaml" {
			t.Errorf("file = %q, want the base name", file)
		}
	})

	// The case worth having: a tracked path whose file was deleted out of band
	// still serves from memory. Reporting it as persisted would promise a
	// restart it will not survive.
	t.Run("a tracked file that is gone is neither persisted nor runtime-only", func(t *testing.T) {
		kind, file := summarizePersistence(filepath.Join(dir, "vanished.yaml"))
		if kind != agentFileMissing {
			t.Errorf("kind = %q, want %q", kind, agentFileMissing)
		}
		if file != "vanished.yaml" {
			t.Errorf("file = %q, want the base name", file)
		}
	})
}
