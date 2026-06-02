package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mockagents/mockagents/internal/engine"
)

// TestIsNotFound covers F-SV-002/F-SV-003: a wrapped ErrAgentNotFound (the
// engine wraps it with %w) must route to 404, while an unrelated error whose
// text merely mentions "agent not found" must NOT — the fragile substring
// match was removed.
func TestIsNotFound(t *testing.T) {
	if !isNotFound(fmt.Errorf("process: %w (available: [a b])", engine.ErrAgentNotFound)) {
		t.Error("wrapped ErrAgentNotFound should be recognized as not-found")
	}
	if !isNotFound(engine.ErrAgentNotFound) {
		t.Error("bare ErrAgentNotFound should be recognized")
	}
	if isNotFound(errors.New("db down: agent not found in cache shard")) {
		t.Error("substring-only match must no longer count as not-found (F-SV-003)")
	}
	if isNotFound(engine.ErrEmptyMessage) {
		t.Error("a different sentinel must not be not-found")
	}
	if isNotFound(nil) {
		t.Error("nil is not not-found")
	}
}

// TestIsTransientMissing covers F-WT-005: file-gone detection now keys on
// errors.Is(fs.ErrNotExist) instead of locale-fragile message substrings.
func TestIsTransientMissing(t *testing.T) {
	if isTransientMissing(nil) {
		t.Error("nil is not transient-missing")
	}
	// The exact error path the watcher sees: config.LoadFile wraps
	// os.ReadFile's *PathError with %w.
	_, readErr := os.ReadFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if readErr == nil {
		t.Fatal("expected a read error for a missing file")
	}
	if !isTransientMissing(fmt.Errorf("reading x: %w", readErr)) {
		t.Error("wrapped missing-file error should be transient-missing")
	}
	// fs.ErrNotExist.Error() is "file does not exist" — it contains neither
	// "no such file" nor "cannot find the file", so the OLD substring code
	// would have missed it. This is the regression witness for the swap.
	if !isTransientMissing(fs.ErrNotExist) {
		t.Error("bare fs.ErrNotExist should be transient-missing")
	}
	if isTransientMissing(errors.New("permission denied")) {
		t.Error("an unrelated error must not be treated as a deletion")
	}
}
