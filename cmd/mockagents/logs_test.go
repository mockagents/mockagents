package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-06 review C-06: `logs` ignored MOCKAGENTS_DATA_DIR and created an
// empty database in the current directory instead of reporting it missing.
func TestResolveLogsDB(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MOCKAGENTS_DATA_DIR", dataDir)

	_, err := resolveLogsDB("")
	require.Error(t, err, "a missing database is an error, not an empty result")
	assert.Contains(t, err.Error(), filepath.Join(dataDir, ".mockagents.db"))
	_, statErr := os.Stat(filepath.Join(dataDir, ".mockagents.db"))
	assert.True(t, os.IsNotExist(statErr), "resolving must not create the file")

	db := filepath.Join(dataDir, ".mockagents.db")
	require.NoError(t, os.WriteFile(db, nil, 0o644))
	got, err := resolveLogsDB("")
	require.NoError(t, err)
	assert.Equal(t, db, got, "the default honours MOCKAGENTS_DATA_DIR")

	explicit := filepath.Join(t.TempDir(), "other.db")
	require.NoError(t, os.WriteFile(explicit, nil, 0o644))
	got, err = resolveLogsDB(explicit)
	require.NoError(t, err)
	assert.Equal(t, explicit, got, "--db wins")
}
