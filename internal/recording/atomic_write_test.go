package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteCassette_TempFileLivesBesideTarget is the audit M-29 guard: the
// atomic write must stage its temp file in the cassette's own directory and
// leave nothing behind. Staging in os.TempDir and renaming across
// filesystems fails with EXDEV on most container images (tmpfs /tmp), which
// made every recording fail.
func TestWriteCassette_TempFileLivesBesideTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cassette.jsonl")
	c := New(path)
	for i := 0; i < 3; i++ {
		if err := c.Append(&Interaction{
			Method:         "POST",
			Path:           "/v1/chat/completions",
			RequestBody:    json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
			ResponseStatus: 200,
			ResponseBody:   json.RawMessage(`{"ok":true}`),
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "cassette.jsonl" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory should hold only the cassette, got %v", names)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Len() != 3 {
		t.Fatalf("reloaded %d interactions, want 3", reloaded.Len())
	}

	// A directly-driven writeCassette into a nested directory stages next to
	// the target too (the path the proxy takes).
	nested := filepath.Join(dir, "sub", "c.jsonl")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeCassette(nested, reloaded.interactions); err != nil {
		t.Fatalf("writeCassette: %v", err)
	}
	entries, _ = os.ReadDir(filepath.Dir(nested))
	if len(entries) != 1 {
		t.Fatalf("nested dir should hold only c.jsonl, got %d entries", len(entries))
	}
}
