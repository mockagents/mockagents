package storage

import (
	"context"
	"testing"
)

// TestPruneToMaxRows is the SEC-05 retention guard: PruneToMaxRows keeps only
// the newest N rows, is a no-op when the table is already within bound or when
// max <= 0, and reports the number deleted.
func TestPruneToMaxRows(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		if err := store.Log(ctx, sampleLog("agent", "sess")); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	// max <= 0 is unlimited (no-op).
	if n, err := store.PruneToMaxRows(ctx, 0); err != nil || n != 0 {
		t.Fatalf("PruneToMaxRows(0) = (%d, %v), want (0, nil)", n, err)
	}

	// Trim to the newest 4 → 6 deleted.
	n, err := store.PruneToMaxRows(ctx, 4)
	if err != nil {
		t.Fatalf("PruneToMaxRows(4): %v", err)
	}
	if n != 6 {
		t.Errorf("deleted = %d, want 6", n)
	}

	rows, err := store.Query(ctx, InteractionFilter{Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("remaining = %d, want 4", len(rows))
	}
	// Query is ORDER BY id DESC, so the survivors must be the newest ids (7..10).
	for _, r := range rows {
		if r.ID <= 6 {
			t.Errorf("pruned the wrong rows: kept id %d (should keep only 7..10)", r.ID)
		}
	}

	// max >= remaining is a no-op.
	if n, err := store.PruneToMaxRows(ctx, 100); err != nil || n != 0 {
		t.Errorf("PruneToMaxRows(100) = (%d, %v), want (0, nil)", n, err)
	}
}
