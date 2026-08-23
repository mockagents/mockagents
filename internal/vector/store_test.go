package vector

import (
	"errors"
	"reflect"
	"testing"
)

func seededStore(t *testing.T, metric Metric) *Store {
	t.Helper()
	s := &Store{}
	if err := s.CreateCollection("docs", 2, metric); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert("docs", []Point{
		{ID: "b", Vector: []float64{1, 0}, Metadata: map[string]any{"team": "blue"}},
		{ID: "a", Vector: []float64{1, 0}, Metadata: map[string]any{"team": "blue"}},
		{ID: "c", Vector: []float64{0, 1}, Metadata: map[string]any{"team": "red"}},
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestQueryStableTieBreakAndFilter(t *testing.T) {
	s := seededStore(t, Cosine)
	matches, err := s.Query("docs", Query{Vector: []float64{1, 0}, TopK: 3, Filter: map[string]any{"team": "blue"}})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{matches[0].ID, matches[1].ID}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("ids = %v", got)
	}
}

func TestQueryEmptyAndLowConfidence(t *testing.T) {
	s := seededStore(t, Cosine)
	min := 0.5
	matches, err := s.Query("docs", Query{Vector: []float64{-1, 0}, TopK: 3, MinScore: &min})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("matches = %+v, want empty", matches)
	}

	matches, err = s.Query("docs", Query{Vector: []float64{0.1, 0.9}, TopK: 3})
	if err != nil || len(matches) != 3 {
		t.Fatalf("low-confidence query = %+v, %v", matches, err)
	}
}

func TestQueryPartialResultsTruncateAfterStableRanking(t *testing.T) {
	s := seededStore(t, Cosine)
	limit := 1
	if err := s.SetPartialResultLimit("docs", &limit); err != nil {
		t.Fatal(err)
	}
	result, err := s.QueryWithInfo("docs", Query{Vector: []float64{1, 0}, TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Partial || len(result.Matches) != 1 || result.Matches[0].ID != "a" {
		t.Fatalf("result = %+v", result)
	}
	// The compatibility Query method observes the same fault.
	matches, err := s.Query("docs", Query{Vector: []float64{1, 0}, TopK: 3})
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %+v, err=%v", matches, err)
	}
}

func TestQueryPartialResultsDoesNotClaimPartialWhenNothingWasDropped(t *testing.T) {
	s := seededStore(t, Cosine)
	limit := 3
	if err := s.SetPartialResultLimit("docs", &limit); err != nil {
		t.Fatal(err)
	}
	result, err := s.QueryWithInfo("docs", Query{Vector: []float64{1, 0}, TopK: 1})
	if err != nil || result.Partial {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
}

func TestQueryPartialResultsSeedAndRequestOverride(t *testing.T) {
	s := seededStore(t, Cosine)
	limit := 1
	zero := 0.0
	if err := s.SetPartialResultPolicy("docs", &limit, 42, &zero); err != nil {
		t.Fatal(err)
	}
	base := Query{Vector: []float64{1, 0}, TopK: 3, RequestKey: "request-1"}
	result, err := s.QueryWithInfo("docs", base)
	if err != nil || result.Partial || len(result.Matches) != 3 {
		t.Fatalf("zero-rate result=%+v err=%v", result, err)
	}
	base.ForcedChaos = "partial"
	result, err = s.QueryWithInfo("docs", base)
	if err != nil || !result.Partial || len(result.Matches) != 1 || result.ChaosSource != "request-force" {
		t.Fatalf("forced result=%+v err=%v", result, err)
	}
	base.ForcedChaos = "off"
	result, err = s.QueryWithInfo("docs", base)
	if err != nil || result.Partial || len(result.Matches) != 3 {
		t.Fatalf("off result=%+v err=%v", result, err)
	}
}

func TestUpsertIsAtomicOnDimensionMismatch(t *testing.T) {
	s := seededStore(t, Dot)
	err := s.Upsert("docs", []Point{
		{ID: "good", Vector: []float64{1, 2}},
		{ID: "bad", Vector: []float64{1}},
	})
	if !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("error = %v", err)
	}
	points, err := s.Fetch("docs", []string{"good"})
	if err != nil || len(points) != 0 {
		t.Fatalf("partial mutation: points=%+v err=%v", points, err)
	}
}

func TestPendingCollectionLearnsDimensionAtomically(t *testing.T) {
	s := &Store{}
	if err := s.CreatePendingCollection("chroma", Cosine); err != nil {
		t.Fatal(err)
	}
	err := s.Upsert("chroma", []Point{{ID: "a", Vector: []float64{1, 0}}, {ID: "b", Vector: []float64{1}}})
	if !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("error=%v", err)
	}
	c, _ := s.Collection("chroma")
	if c.Dimension != 0 || c.PointCount != 0 {
		t.Fatalf("partial init=%+v", c)
	}
	if err := s.Upsert("chroma", []Point{{ID: "a", Vector: []float64{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	c, _ = s.Collection("chroma")
	if c.Dimension != 2 || c.PointCount != 1 {
		t.Fatalf("initialized=%+v", c)
	}
}

func TestDuplicateUpsertDeleteAndDefensiveCopies(t *testing.T) {
	s := seededStore(t, Euclidean)
	point := Point{ID: "a", Vector: []float64{2, 0}, Metadata: map[string]any{"version": 2}}
	if err := s.Upsert("docs", []Point{point}); err != nil {
		t.Fatal(err)
	}
	point.Vector[0] = 999
	fetched, err := s.Fetch("docs", []string{"a", "missing"})
	if err != nil || len(fetched) != 1 || fetched[0].Vector[0] != 2 {
		t.Fatalf("fetched = %+v, err=%v", fetched, err)
	}
	fetched[0].Metadata["version"] = 999
	again, _ := s.Fetch("docs", []string{"a"})
	if again[0].Metadata["version"] != 2 {
		t.Fatal("caller mutated stored metadata")
	}
	deleted, err := s.Delete("docs", []string{"a", "missing"})
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
}

func TestValidationErrors(t *testing.T) {
	s := &Store{}
	if err := s.CreateCollection("bad", 0, Cosine); err == nil {
		t.Fatal("expected invalid dimension")
	}
	if err := s.CreateCollection("bad", 2, "mystery"); !errors.Is(err, ErrInvalidMetric) {
		t.Fatalf("error = %v", err)
	}
	if _, err := s.Query("missing", Query{Vector: []float64{1}, TopK: 1}); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("error = %v", err)
	}
	s = seededStore(t, Cosine)
	if _, err := s.Query("docs", Query{Vector: []float64{1}, TopK: 1}); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("error = %v", err)
	}
	if _, err := s.Query("docs", Query{Vector: []float64{1, 0}, TopK: MaxTopK + 1}); !errors.Is(err, ErrInvalidTopK) {
		t.Fatalf("error = %v", err)
	}
}
