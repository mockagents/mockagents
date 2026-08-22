// Package vector implements the provider-neutral deterministic core for
// VectorMock. It performs no network IO and keeps all state in bounded memory.
package vector

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
)

const (
	MaxDimensions = 65536
	MaxTopK       = 1000
	MaxPoints     = 100000
)

type Metric string

const (
	Cosine    Metric = "cosine"
	Dot       Metric = "dot"
	Euclidean Metric = "euclidean"
)

var (
	ErrCollectionNotFound = errors.New("vector collection not found")
	ErrCollectionExists   = errors.New("vector collection already exists")
	ErrDimensionMismatch  = errors.New("vector dimension mismatch")
	ErrInvalidMetric      = errors.New("invalid vector metric")
	ErrInvalidTopK        = errors.New("invalid top-k")
	ErrPointNotFound      = errors.New("vector point not found")
	ErrCollectionFull     = errors.New("vector collection point limit exceeded")
)

type CollectionConfig struct {
	Name       string `json:"name"`
	Dimension  int    `json:"dimension"`
	Metric     Metric `json:"metric"`
	PointCount int    `json:"point_count"`
}

type Point struct {
	ID         string         `json:"id"`
	ExternalID any            `json:"-"`
	Vector     []float64      `json:"vector"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Match struct {
	ID         string         `json:"id"`
	ExternalID any            `json:"-"`
	Score      float64        `json:"score"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Query struct {
	Vector   []float64
	TopK     int
	Filter   map[string]any
	MinScore *float64
}

type QueryResult struct {
	Matches []Match
	Partial bool
}

type collection struct {
	dimension          int
	metric             Metric
	points             map[string]Point
	partialResultLimit *int
}

// SetPartialResultLimit configures deterministic post-ranking truncation for a
// collection. A nil limit disables the fault; zero returns an empty partial page.
func (s *Store) SetPartialResultLimit(name string, limit *int) error {
	if limit != nil && (*limit < 0 || *limit > MaxTopK) {
		return fmt.Errorf("partial result limit must be between 0 and %d", MaxTopK)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.collections[name]
	if c == nil {
		return fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	if limit == nil {
		c.partialResultLimit = nil
	} else {
		value := *limit
		c.partialResultLimit = &value
	}
	return nil
}

// Store is a concurrency-safe in-memory vector store. The zero value is usable.
type Store struct {
	mu          sync.RWMutex
	collections map[string]*collection
}

// ScopedCollectionName returns the internal collection key used for tenant
// isolation. Anonymous single-tenant collections retain their public name.
func ScopedCollectionName(tenantID, name string) string {
	if tenantID == "" {
		return name
	}
	return tenantID + "\x00" + name
}

func (s *Store) CreateCollection(name string, dimension int, metric Metric) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("collection name is required")
	}
	if dimension <= 0 || dimension > MaxDimensions {
		return fmt.Errorf("dimension must be between 1 and %d", MaxDimensions)
	}
	metric = Metric(strings.ToLower(string(metric)))
	if metric != Cosine && metric != Dot && metric != Euclidean {
		return fmt.Errorf("%w: %q", ErrInvalidMetric, metric)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collections == nil {
		s.collections = make(map[string]*collection)
	}
	if _, exists := s.collections[name]; exists {
		return fmt.Errorf("%w: %q", ErrCollectionExists, name)
	}
	s.collections[name] = &collection{dimension: dimension, metric: metric, points: make(map[string]Point)}
	return nil
}

func (s *Store) Collection(name string) (CollectionConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.collections[name]
	if c == nil {
		return CollectionConfig{}, fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	return CollectionConfig{Name: name, Dimension: c.dimension, Metric: c.metric, PointCount: len(c.points)}, nil
}

// CollectionsWithPrefix snapshots collection metadata for adapter-level
// namespace summaries without exposing mutable internal state.
func (s *Store) CollectionsWithPrefix(prefix string) []CollectionConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CollectionConfig, 0)
	for name, c := range s.collections {
		if name == prefix || strings.HasPrefix(name, prefix+"\x01") {
			out = append(out, CollectionConfig{Name: name, Dimension: c.dimension, Metric: c.metric, PointCount: len(c.points)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) DeleteCollection(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.collections[name]; !ok {
		return fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	delete(s.collections, name)
	return nil
}

func (s *Store) Upsert(name string, points []Point) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.collections[name]
	if c == nil {
		return fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	// Validate the entire batch before mutating so a bad final point cannot
	// leave a partially-applied upsert.
	for i, point := range points {
		if point.ID == "" {
			return fmt.Errorf("point %d: id is required", i)
		}
		if len(point.Vector) != c.dimension {
			return fmt.Errorf("point %q: %w: got %d, want %d", point.ID, ErrDimensionMismatch, len(point.Vector), c.dimension)
		}
		for _, value := range point.Vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("point %q: vector values must be finite", point.ID)
			}
		}
	}
	newIDs := make(map[string]struct{})
	for _, point := range points {
		if _, exists := c.points[point.ID]; !exists {
			newIDs[point.ID] = struct{}{}
		}
	}
	if len(c.points)+len(newIDs) > MaxPoints {
		return fmt.Errorf("%w: maximum %d", ErrCollectionFull, MaxPoints)
	}
	for _, point := range points {
		c.points[point.ID] = clonePoint(point)
	}
	return nil
}

func (s *Store) Fetch(name string, ids []string) ([]Point, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.collections[name]
	if c == nil {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	out := make([]Point, 0, len(ids))
	for _, id := range ids {
		if point, ok := c.points[id]; ok {
			out = append(out, clonePoint(point))
		}
	}
	return out, nil
}

func (s *Store) Delete(name string, ids []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.collections[name]
	if c == nil {
		return 0, fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	deleted := 0
	for _, id := range ids {
		if _, ok := c.points[id]; ok {
			delete(c.points, id)
			deleted++
		}
	}
	return deleted, nil
}

func (s *Store) DeleteAll(name string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.collections[name]
	if c == nil {
		return 0, fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	deleted := len(c.points)
	c.points = make(map[string]Point)
	return deleted, nil
}

func (s *Store) Query(name string, query Query) ([]Match, error) {
	result, err := s.QueryWithInfo(name, query)
	return result.Matches, err
}

// QueryWithInfo returns ranked matches plus whether a configured fault
// intentionally truncated the result set.
func (s *Store) QueryWithInfo(name string, query Query) (QueryResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.collections[name]
	if c == nil {
		return QueryResult{}, fmt.Errorf("%w: %q", ErrCollectionNotFound, name)
	}
	if len(query.Vector) != c.dimension {
		return QueryResult{}, fmt.Errorf("%w: got %d, want %d", ErrDimensionMismatch, len(query.Vector), c.dimension)
	}
	if query.TopK <= 0 || query.TopK > MaxTopK {
		return QueryResult{}, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidTopK, MaxTopK)
	}
	for _, value := range query.Vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return QueryResult{}, errors.New("query vector values must be finite")
		}
	}
	matches := make([]Match, 0, min(query.TopK, len(c.points)))
	for _, point := range c.points {
		if !metadataMatches(point.Metadata, query.Filter) {
			continue
		}
		score := similarity(c.metric, query.Vector, point.Vector)
		if query.MinScore != nil && score < *query.MinScore {
			continue
		}
		matches = append(matches, Match{ID: point.ID, ExternalID: point.ExternalID, Score: score, Metadata: cloneMap(point.Metadata)})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].ID < matches[j].ID
	})
	if len(matches) > query.TopK {
		matches = matches[:query.TopK]
	}
	partial := false
	if c.partialResultLimit != nil && len(matches) > *c.partialResultLimit {
		matches = matches[:*c.partialResultLimit]
		partial = true
	}
	return QueryResult{Matches: matches, Partial: partial}, nil
}

func similarity(metric Metric, a, b []float64) float64 {
	switch metric {
	case Dot:
		var dot float64
		for i := range a {
			dot += a[i] * b[i]
		}
		return dot
	case Euclidean:
		var squared float64
		for i := range a {
			d := a[i] - b[i]
			squared += d * d
		}
		return 1 / (1 + math.Sqrt(squared))
	default:
		var dot, aa, bb float64
		for i := range a {
			dot += a[i] * b[i]
			aa += a[i] * a[i]
			bb += b[i] * b[i]
		}
		if aa == 0 || bb == 0 {
			return 0
		}
		return dot / (math.Sqrt(aa) * math.Sqrt(bb))
	}
}

func metadataMatches(metadata, filter map[string]any) bool {
	for key, want := range filter {
		got, ok := metadata[key]
		if !ok || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return true
}

func clonePoint(point Point) Point {
	return Point{ID: point.ID, ExternalID: point.ExternalID, Vector: append([]float64(nil), point.Vector...), Metadata: cloneMap(point.Metadata)}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneValue(typed[i])
		}
		return out
	default:
		return value
	}
}
