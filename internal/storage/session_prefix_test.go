package storage

import (
	"context"
	"testing"
)

// §9.3: a pipeline run scopes each node's session as
// "<session>::<pipeline>::<node>", so the id an operator submitted matches
// nothing under equality. Without a prefix filter, "show me what this run did"
// has no answer, and the console had to tell people to search by time instead.
func TestQuery_SessionPrefix(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	for _, sess := range []string{
		"gui-run-abc::support-triage::router",
		"gui-run-abc::support-triage::summarizer",
		"gui-run-xyz::support-triage::router",
		"gui-run-abc-but-different",
	} {
		if err := store.Log(ctx, &InteractionLog{AgentName: "a", SessionID: sess}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("matches every node of one run", func(t *testing.T) {
		got, err := store.Query(ctx, InteractionFilter{SessionPrefix: "gui-run-abc::"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d rows, want the run's 2 nodes", len(got))
		}
	})

	t.Run("an exact id still wins over a prefix", func(t *testing.T) {
		got, err := store.Query(ctx, InteractionFilter{
			SessionID:     "gui-run-xyz::support-triage::router",
			SessionPrefix: "gui-run-abc::",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].SessionID != "gui-run-xyz::support-triage::router" {
			t.Fatalf("exact id should take precedence, got %+v", got)
		}
	})

	// The reason for ESCAPE: "_" is a single-character wildcard in LIKE, and it
	// appears in session ids more often than anyone expects. Left unescaped,
	// this prefix would also match "gui-runXabc" and quietly return another
	// run's rows.
	t.Run("underscores are literal, not wildcards", func(t *testing.T) {
		if err := store.Log(ctx, &InteractionLog{AgentName: "a", SessionID: "gui_run::p::n"}); err != nil {
			t.Fatal(err)
		}
		if err := store.Log(ctx, &InteractionLog{AgentName: "a", SessionID: "guiXrun::p::n"}); err != nil {
			t.Fatal(err)
		}
		got, err := store.Query(ctx, InteractionFilter{SessionPrefix: "gui_run::"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].SessionID != "gui_run::p::n" {
			t.Fatalf("underscore must be literal, got %+v", got)
		}
	})

	t.Run("a percent sign is literal too", func(t *testing.T) {
		if err := store.Log(ctx, &InteractionLog{AgentName: "a", SessionID: "100%::p::n"}); err != nil {
			t.Fatal(err)
		}
		got, err := store.Query(ctx, InteractionFilter{SessionPrefix: "100%::"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("percent must be literal, got %d rows", len(got))
		}
	})
}
