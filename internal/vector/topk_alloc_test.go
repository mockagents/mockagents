package vector

import (
	"fmt"
	"testing"
)

// storeWithPoints builds a collection of n points, each carrying a metadata
// map, so the query path has something to clone.
func storeWithPoints(t testing.TB, n int) *Store {
	t.Helper()
	s := &Store{}
	if err := s.CreateCollection("docs", 2, Cosine); err != nil {
		t.Fatalf("create: %v", err)
	}
	points := make([]Point, 0, n)
	for i := 0; i < n; i++ {
		points = append(points, Point{
			ID:     fmt.Sprintf("p%d", i),
			Vector: []float64{float64(i % 7), float64(i % 11)},
			Metadata: map[string]any{
				"title":   fmt.Sprintf("doc %d", i),
				"section": fmt.Sprintf("s%d", i%20),
				"rank":    i,
			},
		})
	}
	if err := s.Upsert("docs", points); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	return s
}

// TestQueryClonesOnlyTheSurvivingMetadata is the audit M-21 guard. Metadata
// used to be cloned for every candidate before the ranking truncated to TopK,
// so a 10k-point collection allocated 10k maps to return 3. Measured through
// allocations rather than by inspecting internals: cloning per candidate makes
// the count scale with the collection, cloning after top-k makes it scale with
// TopK.
func TestQueryClonesOnlyTheSurvivingMetadata(t *testing.T) {
	small := storeWithPoints(t, 100)
	large := storeWithPoints(t, 10_000)
	query := Query{Vector: []float64{1, 1}, TopK: 3}

	smallAllocs := testing.AllocsPerRun(20, func() {
		if _, err := small.Query("docs", query); err != nil {
			t.Fatal(err)
		}
	})
	largeAllocs := testing.AllocsPerRun(20, func() {
		if _, err := large.Query("docs", query); err != nil {
			t.Fatal(err)
		}
	})

	// The scan itself still walks every point, so the counts are not equal —
	// but the per-candidate metadata clone (3 allocations per map here) must
	// be gone. If it were still there, the 100x bigger collection would cost
	// tens of thousands more allocations rather than a few thousand.
	if largeAllocs > smallAllocs*10 {
		t.Errorf("allocations scale with collection size, not TopK: %.0f for 100 points vs %.0f for 10000 — metadata is still cloned per candidate",
			smallAllocs, largeAllocs)
	}
}

// TestQueryReturnsClonedMetadata: the caller must not receive the store's
// live map. Mutating a result must never corrupt the stored point.
func TestQueryReturnsClonedMetadata(t *testing.T) {
	s := storeWithPoints(t, 5)
	matches, err := s.Query("docs", Query{Vector: []float64{1, 1}, TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	matches[0].Metadata["title"] = "MUTATED"

	again, err := s.Query("docs", Query{Vector: []float64{1, 1}, TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Metadata["title"] == "MUTATED" {
		t.Error("a caller's mutation reached the stored point; the metadata was handed out by reference")
	}
}

// TestQueryRankingUnchanged: the ranking and tie-break must be identical to
// the pre-refactor behavior — highest score first, ties broken by id.
func TestQueryRankingUnchanged(t *testing.T) {
	s := &Store{}
	if err := s.CreateCollection("docs", 2, Cosine); err != nil {
		t.Fatal(err)
	}
	// b and c are identical vectors, so they tie and must order by id.
	if err := s.Upsert("docs", []Point{
		{ID: "c", Vector: []float64{1, 0}, Metadata: map[string]any{"n": 3}},
		{ID: "b", Vector: []float64{1, 0}, Metadata: map[string]any{"n": 2}},
		{ID: "a", Vector: []float64{0, 1}, Metadata: map[string]any{"n": 1}},
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := s.Query("docs", Query{Vector: []float64{1, 0}, TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{matches[0].ID, matches[1].ID, matches[2].ID}
	want := []string{"b", "c", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranking = %v, want %v", got, want)
		}
	}
	// And every surviving match still carries its metadata.
	for _, m := range matches {
		if len(m.Metadata) == 0 {
			t.Errorf("match %s lost its metadata", m.ID)
		}
	}
}
