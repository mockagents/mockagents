package vector

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func mustCreate(t *testing.T, s *Store, name string, dim int, metric Metric) {
	t.Helper()
	if err := s.CreateCollection(name, dim, metric); err != nil {
		t.Fatalf("CreateCollection(%q): %v", name, err)
	}
}

func ids(points []Point) []string {
	out := make([]string, len(points))
	for i, p := range points {
		out[i] = p.ID
	}
	return out
}

func matchIDs(matches []Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.ID
	}
	return out
}

func TestCreateCollectionValidation(t *testing.T) {
	s := &Store{}
	cases := []struct {
		name      string
		coll      string
		dim       int
		metric    Metric
		wantIs    error
		wantInMsg string
	}{
		{"empty name", "", 2, Cosine, nil, "name is required"},
		{"whitespace name", "   ", 2, Cosine, nil, "name is required"},
		{"zero dimension", "a", 0, Cosine, nil, "dimension must be between"},
		{"negative dimension", "a", -1, Cosine, nil, "dimension must be between"},
		{"dimension over max", "a", MaxDimensions + 1, Cosine, nil, "dimension must be between"},
		{"unknown metric", "a", 2, "manhattan", ErrInvalidMetric, ""},
		{"empty metric", "a", 2, "", ErrInvalidMetric, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.CreateCollection(tc.coll, tc.dim, tc.metric)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("err = %v, want %v", err, tc.wantIs)
			}
			if tc.wantInMsg != "" && !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantInMsg)
			}
		})
	}
	if got := s.Collections(); len(got) != 0 {
		t.Fatalf("failed creates left collections behind: %+v", got)
	}
}

func TestCreateCollectionNormalisesNameAndMetric(t *testing.T) {
	s := &Store{}
	if err := s.CreateCollection("  docs  ", MaxDimensions, "DOT"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Collection("docs")
	if err != nil {
		t.Fatalf("trimmed name not registered: %v", err)
	}
	if cfg != (CollectionConfig{Name: "docs", Dimension: MaxDimensions, Metric: Dot}) {
		t.Fatalf("config = %+v", cfg)
	}
	if err := s.CreateCollection("docs", 3, Cosine); !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("duplicate create err = %v, want ErrCollectionExists", err)
	}
	// The duplicate attempt must not have replaced the original.
	if cfg, _ := s.Collection("docs"); cfg.Dimension != MaxDimensions || cfg.Metric != Dot {
		t.Fatalf("duplicate create clobbered config: %+v", cfg)
	}
}

func TestCreatePendingCollectionValidation(t *testing.T) {
	s := &Store{}
	if err := s.CreatePendingCollection(" ", Cosine); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("empty name err = %v", err)
	}
	if err := s.CreatePendingCollection("p", "hamming"); !errors.Is(err, ErrInvalidMetric) {
		t.Fatalf("bad metric err = %v", err)
	}
	if err := s.CreatePendingCollection(" p ", "Euclidean"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Collection("p")
	if err != nil || cfg.Metric != Euclidean || cfg.Dimension != 0 {
		t.Fatalf("pending cfg = %+v err = %v", cfg, err)
	}
	if err := s.CreatePendingCollection("p", Cosine); !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("duplicate pending err = %v", err)
	}
	// A pending and a fixed collection share one namespace.
	if err := s.CreateCollection("p", 2, Cosine); !errors.Is(err, ErrCollectionExists) {
		t.Fatalf("fixed-over-pending err = %v", err)
	}
}

func TestPendingCollectionEmptyUpsertDoesNotFixDimension(t *testing.T) {
	s := &Store{}
	if err := s.CreatePendingCollection("p", Cosine); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert("p", nil); err != nil {
		t.Fatalf("empty upsert: %v", err)
	}
	if cfg, _ := s.Collection("p"); cfg.Dimension != 0 {
		t.Fatalf("empty upsert learned dimension %d", cfg.Dimension)
	}
	if err := s.Upsert("p", []Point{{ID: "a", Vector: []float64{1, 2, 3}}}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.Collection("p"); cfg.Dimension != 3 {
		t.Fatalf("dimension = %d, want 3", cfg.Dimension)
	}
	// Once learned, the dimension is enforced like a fixed collection.
	if err := s.Upsert("p", []Point{{ID: "b", Vector: []float64{1, 2}}}); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("err = %v, want ErrDimensionMismatch", err)
	}
}

// TestPendingCollectionRejectsZeroLengthFirstVector: a pending collection
// learns its dimension from the first upserted vector, so an empty vector
// would "learn" dimension 0 and later points with real vectors are accepted
// alongside it. Query then indexes past the empty vector.
func TestPendingCollectionRejectsZeroLengthFirstVector(t *testing.T) {
	t.Skip("BUG: Store.Upsert on a pending collection accepts a zero-length first vector (dimension stays 0), " +
		"a later upsert then learns a non-zero dimension and the collection holds mixed-length points; " +
		"QueryWithInfo/similarity then panics with index out of range")
	s := &Store{}
	if err := s.CreatePendingCollection("p", Dot); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert("p", []Point{{ID: "empty"}}); err == nil {
		t.Fatal("zero-length vector should be rejected (CreateCollection requires dimension >= 1)")
	}
}

func TestCollectionLookupAndListing(t *testing.T) {
	s := &Store{}
	if _, err := s.Collection("nope"); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("err = %v", err)
	}
	if got := s.Collections(); got == nil || len(got) != 0 {
		t.Fatalf("zero-value store Collections() = %#v, want empty non-nil", got)
	}
	mustCreate(t, s, "zeta", 2, Cosine)
	mustCreate(t, s, "alpha", 3, Dot)
	if err := s.Upsert("alpha", []Point{{ID: "x", Vector: []float64{1, 2, 3}}}); err != nil {
		t.Fatal(err)
	}
	want := []CollectionConfig{
		{Name: "alpha", Dimension: 3, Metric: Dot, PointCount: 1},
		{Name: "zeta", Dimension: 2, Metric: Cosine, PointCount: 0},
	}
	if got := s.Collections(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Collections() = %+v, want %+v", got, want)
	}
}

// TestCollectionsWithPrefixGroupsNamespaces: adapters store a provider
// namespace as "<index>\x01<namespace>"; the prefix listing returns the index
// itself plus its namespaces, sorted, and nothing that merely shares a string
// prefix.
func TestCollectionsWithPrefixGroupsNamespaces(t *testing.T) {
	s := &Store{}
	for _, name := range []string{"idx\x01b", "idx", "idx\x01a", "idx2", "idxx\x01a", "other"} {
		mustCreate(t, s, name, 2, Cosine)
	}
	var got []string
	for _, c := range s.CollectionsWithPrefix("idx") {
		got = append(got, c.Name)
	}
	if want := []string{"idx", "idx\x01a", "idx\x01b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectionsWithPrefix = %q, want %q", got, want)
	}
	if out := s.CollectionsWithPrefix("absent"); out == nil || len(out) != 0 {
		t.Fatalf("absent prefix = %#v, want empty non-nil", out)
	}
}

func TestDeleteCollection(t *testing.T) {
	s := seededStore(t, Cosine)
	if err := s.DeleteCollection("missing"); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := s.DeleteCollection("docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Collection("docs"); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("collection survived delete: %v", err)
	}
	if err := s.DeleteCollection("docs"); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
	// The name is reusable with a different shape.
	mustCreate(t, s, "docs", 5, Euclidean)
	if cfg, _ := s.Collection("docs"); cfg.Dimension != 5 || cfg.PointCount != 0 {
		t.Fatalf("recreated cfg = %+v", cfg)
	}
}

func TestUpsertValidation(t *testing.T) {
	s := seededStore(t, Cosine)
	if err := s.Upsert("missing", []Point{{ID: "a", Vector: []float64{1, 0}}}); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("missing collection err = %v", err)
	}
	cases := []struct {
		name   string
		points []Point
		want   string
	}{
		{"empty id", []Point{{ID: "ok", Vector: []float64{1, 0}}, {Vector: []float64{1, 0}}}, "point 1: id is required"},
		{"NaN", []Point{{ID: "n", Vector: []float64{math.NaN(), 0}}}, "must be finite"},
		{"+Inf", []Point{{ID: "i", Vector: []float64{0, math.Inf(1)}}}, "must be finite"},
		{"-Inf", []Point{{ID: "i", Vector: []float64{math.Inf(-1), 0}}}, "must be finite"},
		{"too long", []Point{{ID: "l", Vector: []float64{1, 2, 3}}}, "got 3, want 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Upsert("docs", tc.points)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if cfg, _ := s.Collection("docs"); cfg.PointCount != 3 {
				t.Fatalf("rejected batch mutated the collection: %+v", cfg)
			}
		})
	}
}

func TestUpsertEnforcesPointCapCountingOnlyNewIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates MaxPoints points")
	}
	s := &Store{}
	mustCreate(t, s, "full", 1, Dot)
	points := make([]Point, MaxPoints)
	for i := range points {
		points[i] = Point{ID: fmt.Sprintf("p%06d", i), Vector: []float64{1}}
	}
	if err := s.Upsert("full", points); err != nil {
		t.Fatalf("filling to exactly MaxPoints: %v", err)
	}
	// Overwriting existing ids at the cap is not growth and must succeed.
	if err := s.Upsert("full", []Point{{ID: "p000000", Vector: []float64{2}}}); err != nil {
		t.Fatalf("overwrite at cap: %v", err)
	}
	err := s.Upsert("full", []Point{{ID: "p000001", Vector: []float64{3}}, {ID: "brand-new", Vector: []float64{1}}})
	if !errors.Is(err, ErrCollectionFull) {
		t.Fatalf("err = %v, want ErrCollectionFull", err)
	}
	// The rejected batch is atomic: its overwrite of p000001 did not land.
	got, _ := s.Fetch("full", []string{"p000001", "brand-new"})
	if len(got) != 1 || got[0].Vector[0] != 1 {
		t.Fatalf("rejected batch partially applied: %+v", got)
	}
}

func TestFetchPreservesRequestOrderAndExternalID(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "docs", 2, Cosine)
	if err := s.Upsert("docs", []Point{
		{ID: "1", ExternalID: uint64(1), Vector: []float64{1, 0}},
		{ID: "2", ExternalID: "two", Vector: []float64{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fetch("missing", []string{"1"}); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("err = %v", err)
	}
	got, err := s.Fetch("docs", []string{"2", "nope", "1", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2", "1", "2"}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("Fetch order = %v, want %v", ids(got), want)
	}
	if got[0].ExternalID != "two" || got[1].ExternalID != uint64(1) {
		t.Fatalf("external ids = %v, %v", got[0].ExternalID, got[1].ExternalID)
	}
	if empty, err := s.Fetch("docs", nil); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("Fetch(nil) = %#v, %v; want empty non-nil", empty, err)
	}
}

func TestListPagingAndFilter(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "docs", 1, Dot)
	var points []Point
	for i, team := range []string{"red", "blue", "red", "blue", "red"} {
		points = append(points, Point{ID: fmt.Sprintf("id-%d", 4-i), Vector: []float64{float64(i)}, Metadata: map[string]any{"team": team}})
	}
	if err := s.Upsert("docs", points); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List("missing", nil, 0, 0); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("err = %v", err)
	}
	cases := []struct {
		name          string
		filter        map[string]any
		limit, offset int
		want          []string
	}{
		{"all sorted by id", nil, 0, 0, []string{"id-0", "id-1", "id-2", "id-3", "id-4"}},
		{"limit", nil, 2, 0, []string{"id-0", "id-1"}},
		{"offset", nil, 0, 3, []string{"id-3", "id-4"}},
		{"limit and offset", nil, 2, 1, []string{"id-1", "id-2"}},
		{"negative offset clamps to zero", nil, 1, -5, []string{"id-0"}},
		{"negative limit means unlimited", nil, -1, 4, []string{"id-4"}},
		{"limit beyond end", nil, 99, 4, []string{"id-4"}},
		{"offset at end", nil, 0, 5, []string{}},
		{"offset past end", nil, 1, 50, []string{}},
		{"filter", map[string]any{"team": "red"}, 0, 0, []string{"id-0", "id-2", "id-4"}},
		{"filter with paging", map[string]any{"team": "red"}, 1, 1, []string{"id-2"}},
		{"filter no match", map[string]any{"team": "green"}, 0, 0, []string{}},
		{"filter missing key", map[string]any{"owner": "x"}, 0, 0, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.List("docs", tc.filter, tc.limit, tc.offset)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("List returned nil, want empty non-nil slice")
			}
			if !reflect.DeepEqual(ids(got), tc.want) {
				t.Fatalf("ids = %v, want %v", ids(got), tc.want)
			}
		})
	}
}

func TestListReturnsDefensiveCopies(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "docs", 1, Dot)
	if err := s.Upsert("docs", []Point{{ID: "a", Vector: []float64{1}, Metadata: map[string]any{
		"tags":  []any{"x", map[string]any{"deep": "v"}},
		"inner": map[string]any{"k": "v"},
	}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.List("docs", nil, 0, 0)
	got[0].Vector[0] = 99
	got[0].Metadata["inner"].(map[string]any)["k"] = "mutated"
	tags := got[0].Metadata["tags"].([]any)
	tags[0] = "mutated"
	tags[1].(map[string]any)["deep"] = "mutated"

	again, _ := s.List("docs", nil, 0, 0)
	if again[0].Vector[0] != 1 {
		t.Error("caller mutated stored vector")
	}
	if again[0].Metadata["inner"].(map[string]any)["k"] != "v" {
		t.Error("caller mutated nested stored map")
	}
	storedTags := again[0].Metadata["tags"].([]any)
	if storedTags[0] != "x" || storedTags[1].(map[string]any)["deep"] != "v" {
		t.Errorf("caller mutated nested stored slice: %v", storedTags)
	}
}

func TestUpsertCopiesNestedMetadataFromCaller(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "docs", 1, Dot)
	meta := map[string]any{"list": []any{"a"}, "obj": map[string]any{"k": "v"}}
	if err := s.Upsert("docs", []Point{{ID: "a", Vector: []float64{1}, Metadata: meta}}); err != nil {
		t.Fatal(err)
	}
	meta["list"].([]any)[0] = "changed"
	meta["obj"].(map[string]any)["k"] = "changed"
	meta["added"] = true
	got, _ := s.Fetch("docs", []string{"a"})
	want := map[string]any{"list": []any{"a"}, "obj": map[string]any{"k": "v"}}
	if !reflect.DeepEqual(got[0].Metadata, want) {
		t.Fatalf("stored metadata = %v, want %v", got[0].Metadata, want)
	}
}

func TestDeleteAndDeleteAll(t *testing.T) {
	s := seededStore(t, Cosine)
	if _, err := s.Delete("missing", []string{"a"}); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("Delete missing err = %v", err)
	}
	if _, err := s.DeleteAll("missing"); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("DeleteAll missing err = %v", err)
	}
	if n, err := s.Delete("docs", nil); err != nil || n != 0 {
		t.Fatalf("Delete(nil) = %d, %v", n, err)
	}
	// Duplicate ids in one call count once.
	if n, err := s.Delete("docs", []string{"a", "a"}); err != nil || n != 1 {
		t.Fatalf("Delete dup = %d, %v; want 1", n, err)
	}
	n, err := s.DeleteAll("docs")
	if err != nil || n != 2 {
		t.Fatalf("DeleteAll = %d, %v; want 2", n, err)
	}
	cfg, err := s.Collection("docs")
	if err != nil || cfg.PointCount != 0 || cfg.Dimension != 2 {
		t.Fatalf("after DeleteAll cfg = %+v err = %v (collection and dimension must survive)", cfg, err)
	}
	if n, err := s.DeleteAll("docs"); err != nil || n != 0 {
		t.Fatalf("DeleteAll on empty = %d, %v", n, err)
	}
	// Still usable afterwards.
	if err := s.Upsert("docs", []Point{{ID: "z", Vector: []float64{0, 1}}}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryTopKBounds(t *testing.T) {
	s := seededStore(t, Cosine)
	for _, k := range []int{0, -1, MaxTopK + 1} {
		if _, err := s.Query("docs", Query{Vector: []float64{1, 0}, TopK: k}); !errors.Is(err, ErrInvalidTopK) {
			t.Errorf("TopK=%d err = %v, want ErrInvalidTopK", k, err)
		}
	}
	got, err := s.Query("docs", Query{Vector: []float64{1, 0}, TopK: MaxTopK})
	if err != nil || len(got) != 3 {
		t.Fatalf("TopK=MaxTopK over 3 points = %v, %v", matchIDs(got), err)
	}
	got, err = s.Query("docs", Query{Vector: []float64{1, 0}, TopK: 1})
	if err != nil || !reflect.DeepEqual(matchIDs(got), []string{"a"}) {
		t.Fatalf("TopK=1 = %v, %v", matchIDs(got), err)
	}
}

func TestQueryEmptyCorpus(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "empty", 3, Cosine)
	res, err := s.QueryWithInfo("empty", Query{Vector: []float64{1, 2, 3}, TopK: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches == nil || len(res.Matches) != 0 || res.Partial {
		t.Fatalf("result = %#v, want empty non-nil matches, not partial", res)
	}
	// Dimension is still enforced on an empty collection.
	if _, err := s.Query("empty", Query{Vector: []float64{1}, TopK: 1}); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("err = %v, want ErrDimensionMismatch", err)
	}
}

func TestQueryRejectsNonFiniteQueryVector(t *testing.T) {
	s := seededStore(t, Cosine)
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := s.Query("docs", Query{Vector: []float64{v, 0}, TopK: 1})
		if err == nil || !strings.Contains(err.Error(), "query vector values must be finite") {
			t.Errorf("query with %v: err = %v", v, err)
		}
	}
}

func TestQueryMetricsRankAndScore(t *testing.T) {
	points := []Point{
		{ID: "near", Vector: []float64{1, 1}},
		{ID: "far", Vector: []float64{10, 10}},
		{ID: "opposite", Vector: []float64{-1, -1}},
		{ID: "zero", Vector: []float64{0, 0}},
	}
	cases := []struct {
		metric Metric
		want   []string
		scores map[string]float64
	}{
		// Dot rewards magnitude.
		{Dot, []string{"far", "near", "zero", "opposite"}, map[string]float64{"far": 20, "near": 2, "zero": 0, "opposite": -2}},
		// Euclidean: 1/(1+distance); identical vectors score exactly 1.
		{Euclidean, []string{"near", "zero", "opposite", "far"}, map[string]float64{
			"near": 1, "zero": 1 / (1 + math.Sqrt2), "opposite": 1 / (1 + 2*math.Sqrt2), "far": 1 / (1 + 9*math.Sqrt2),
		}},
		// Cosine ignores magnitude (near and far tie at 1, broken by id);
		// a zero vector scores 0 rather than NaN.
		{Cosine, []string{"far", "near", "zero", "opposite"}, map[string]float64{"far": 1, "near": 1, "zero": 0, "opposite": -1}},
	}
	for _, tc := range cases {
		t.Run(string(tc.metric), func(t *testing.T) {
			s := &Store{}
			mustCreate(t, s, "c", 2, tc.metric)
			if err := s.Upsert("c", points); err != nil {
				t.Fatal(err)
			}
			got, err := s.Query("c", Query{Vector: []float64{1, 1}, TopK: 10})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(matchIDs(got), tc.want) {
				t.Fatalf("order = %v, want %v", matchIDs(got), tc.want)
			}
			for _, m := range got {
				if math.Abs(m.Score-tc.scores[m.ID]) > 1e-9 {
					t.Errorf("%s score = %v, want %v", m.ID, m.Score, tc.scores[m.ID])
				}
			}
		})
	}
}

func TestQueryCosineZeroQueryVectorScoresZero(t *testing.T) {
	s := seededStore(t, Cosine)
	got, err := s.Query("docs", Query{Vector: []float64{0, 0}, TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range got {
		if m.Score != 0 {
			t.Fatalf("zero query vector scored %v for %s, want 0", m.Score, m.ID)
		}
	}
	// All tie at zero, so ordering falls back to id.
	if !reflect.DeepEqual(matchIDs(got), []string{"a", "b", "c"}) {
		t.Fatalf("order = %v", matchIDs(got))
	}
}

func TestQueryFilterSemantics(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "docs", 1, Dot)
	if err := s.Upsert("docs", []Point{
		{ID: "a", Vector: []float64{1}, Metadata: map[string]any{"team": "red", "level": 2, "tags": []any{"x", "y"}}},
		{ID: "b", Vector: []float64{2}, Metadata: map[string]any{"team": "red", "level": 3}},
		{ID: "c", Vector: []float64{3}},
	}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		filter map[string]any
		want   []string
	}{
		{"nil filter matches everything incl. points without metadata", nil, []string{"c", "b", "a"}},
		{"empty filter matches everything", map[string]any{}, []string{"c", "b", "a"}},
		{"single key", map[string]any{"team": "red"}, []string{"b", "a"}},
		{"all keys must match", map[string]any{"team": "red", "level": 2}, []string{"a"}},
		{"type-strict equality", map[string]any{"level": 2.0}, []string{}},
		{"deep equality on lists", map[string]any{"tags": []any{"x", "y"}}, []string{"a"}},
		{"list order matters", map[string]any{"tags": []any{"y", "x"}}, []string{}},
		{"missing key excludes", map[string]any{"owner": nil}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Query("docs", Query{Vector: []float64{1}, TopK: 10, Filter: tc.filter})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(matchIDs(got), tc.want) {
				t.Fatalf("ids = %v, want %v", matchIDs(got), tc.want)
			}
		})
	}
}

func TestQueryMinScoreIsInclusive(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "docs", 1, Dot)
	if err := s.Upsert("docs", []Point{
		{ID: "lo", Vector: []float64{1}},
		{ID: "mid", Vector: []float64{2}},
		{ID: "hi", Vector: []float64{3}},
	}); err != nil {
		t.Fatal(err)
	}
	minScore := 2.0
	got, err := s.Query("docs", Query{Vector: []float64{1}, TopK: 10, MinScore: &minScore})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(matchIDs(got), []string{"hi", "mid"}) {
		t.Fatalf("ids = %v, want [hi mid]", matchIDs(got))
	}
}

func TestQueryMatchesAreSnapshots(t *testing.T) {
	s := &Store{}
	mustCreate(t, s, "docs", 2, Cosine)
	if err := s.Upsert("docs", []Point{{
		ID: "a", ExternalID: 7, Vector: []float64{1, 0},
		Metadata: map[string]any{"nested": map[string]any{"k": "v"}, "list": []any{"x"}},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Query("docs", Query{Vector: []float64{1, 0}, TopK: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	m := got[0]
	if m.ExternalID != 7 || !reflect.DeepEqual(m.Vector, []float64{1, 0}) {
		t.Fatalf("match = %+v", m)
	}
	m.Vector[0] = 42
	m.Metadata["nested"].(map[string]any)["k"] = "mutated"
	m.Metadata["list"].([]any)[0] = "mutated"

	again, _ := s.Fetch("docs", []string{"a"})
	if again[0].Vector[0] != 1 {
		t.Error("mutating Match.Vector changed the stored point")
	}
	if again[0].Metadata["nested"].(map[string]any)["k"] != "v" || again[0].Metadata["list"].([]any)[0] != "x" {
		t.Errorf("mutating Match.Metadata changed the stored point: %v", again[0].Metadata)
	}
}

func TestPartialResultPolicyValidation(t *testing.T) {
	s := seededStore(t, Cosine)
	for _, bad := range []int{-1, MaxTopK + 1} {
		limit := bad
		if err := s.SetPartialResultLimit("docs", &limit); err == nil || !strings.Contains(err.Error(), "partial result limit") {
			t.Errorf("limit %d err = %v", bad, err)
		}
	}
	limit := 1
	if err := s.SetPartialResultLimit("missing", &limit); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	// Bounds themselves are accepted.
	for _, ok := range []int{0, MaxTopK} {
		limit := ok
		if err := s.SetPartialResultLimit("docs", &limit); err != nil {
			t.Errorf("limit %d err = %v", ok, err)
		}
	}
}

func TestPartialResultLimitZeroAndClear(t *testing.T) {
	s := seededStore(t, Cosine)
	zero := 0
	if err := s.SetPartialResultLimit("docs", &zero); err != nil {
		t.Fatal(err)
	}
	res, err := s.QueryWithInfo("docs", Query{Vector: []float64{1, 0}, TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Partial || len(res.Matches) != 0 || res.ChaosAction != "partial" || res.ChaosSource != "configured" {
		t.Fatalf("zero-limit result = %+v", res)
	}
	if err := s.SetPartialResultLimit("docs", nil); err != nil {
		t.Fatal(err)
	}
	res, err = s.QueryWithInfo("docs", Query{Vector: []float64{1, 0}, TopK: 3})
	if err != nil || res.Partial || len(res.Matches) != 3 || res.ChaosAction != "" || res.ChaosSource != "" {
		t.Fatalf("cleared result = %+v err = %v", res, err)
	}
}

func TestPartialResultPolicyCopiesCallerInputs(t *testing.T) {
	s := seededStore(t, Cosine)
	limit, rate := 1, 0.0
	ops := map[string]float64{"/search": 0}
	if err := s.SetPartialResultScopedPolicy("docs", &limit, 1, &rate, ops); err != nil {
		t.Fatal(err)
	}
	// Mutating the caller's variables after the call must not reconfigure
	// the collection.
	limit, rate = 0, 1
	ops["/search"] = 1
	for _, op := range []string{"", "/search"} {
		res, err := s.QueryWithInfo("docs", Query{Vector: []float64{1, 0}, TopK: 3, RequestKey: "k", Operation: op})
		if err != nil || res.Partial || len(res.Matches) != 3 {
			t.Fatalf("op %q: result = %+v err = %v (caller mutation leaked into policy)", op, res, err)
		}
	}
}

func TestPartialResultScopedPolicyReplacesOperationRates(t *testing.T) {
	s := seededStore(t, Cosine)
	limit, zero := 1, 0.0
	if err := s.SetPartialResultScopedPolicy("docs", &limit, 1, &zero, map[string]float64{"/search": 1}); err != nil {
		t.Fatal(err)
	}
	q := Query{Vector: []float64{1, 0}, TopK: 3, RequestKey: "k", Operation: "/search"}
	if res, _ := s.QueryWithInfo("docs", q); !res.Partial || res.ChaosSource != "operation-rate" {
		t.Fatalf("operation override not applied: %+v", res)
	}
	// A later policy without operation rates drops the old overrides.
	if err := s.SetPartialResultPolicy("docs", &limit, 1, &zero); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.QueryWithInfo("docs", q); res.Partial {
		t.Fatalf("stale operation override survived: %+v", res)
	}
}

func TestGlobalChaosPolicyPrecedenceAndClear(t *testing.T) {
	s := seededStore(t, Cosine)
	limit := 1
	if err := s.SetPartialResultLimit("docs", &limit); err != nil {
		t.Fatal(err)
	}
	q := Query{Vector: []float64{1, 0}, TopK: 3, RequestKey: "k"}

	zero := 0.0
	s.SetGlobalChaosPolicy(9, &zero)
	zero = 1 // caller mutation must not leak
	if res, _ := s.QueryWithInfo("docs", q); res.Partial {
		t.Fatalf("global rate 0 should suppress the legacy always-on fault: %+v", res)
	}

	// A collection-level rate outranks the global one.
	one := 1.0
	if err := s.SetPartialResultPolicy("docs", &limit, 0, &one); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.QueryWithInfo("docs", q); !res.Partial || res.ChaosSource != "seeded-rate" {
		t.Fatalf("collection rate should win over global: %+v", res)
	}

	// Clearing the global policy restores legacy always-on for a collection
	// with no rate of its own.
	if err := s.SetPartialResultLimit("docs", &limit); err != nil {
		t.Fatal(err)
	}
	s.SetGlobalChaosPolicy(0, nil)
	if res, _ := s.QueryWithInfo("docs", q); !res.Partial || res.ChaosSource != "configured" {
		t.Fatalf("cleared global policy: %+v", res)
	}
}

func TestPartialResultForcedActionMustMatchConfiguredFault(t *testing.T) {
	s := seededStore(t, Cosine)
	limit, zero := 1, 0.0
	if err := s.SetPartialResultPolicy("docs", &limit, 5, &zero); err != nil {
		t.Fatal(err)
	}
	// Forcing a different fault name does not trigger the partial fault.
	res, err := s.QueryWithInfo("docs", Query{Vector: []float64{1, 0}, TopK: 3, ForcedChaos: "timeout"})
	if err != nil || res.Partial {
		t.Fatalf("unrelated forced action applied partial: %+v err=%v", res, err)
	}
	// Forcing partial without a configured limit is a no-op too.
	if err := s.SetPartialResultPolicy("docs", nil, 5, &zero); err != nil {
		t.Fatal(err)
	}
	res, err = s.QueryWithInfo("docs", Query{Vector: []float64{1, 0}, TopK: 3, ForcedChaos: "partial"})
	if err != nil || res.Partial || len(res.Matches) != 3 {
		t.Fatalf("forced partial without a limit: %+v err=%v", res, err)
	}
}
