package audit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore opens a fresh SQLite-backed store under t.TempDir() so
// every test case has isolated storage.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// --- Event kind validity ---

func TestEventKindValid(t *testing.T) {
	valid := []EventKind{
		EventTenantCreated, EventTenantDeleted,
		EventAPIKeyCreated, EventAPIKeyDeleted, EventAPIKeyRoleChanged,
		EventAgentReloaded, EventAuthDenied,
	}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("kind %q should be valid", k)
		}
	}
	if EventKind("bogus").Valid() {
		t.Error("bogus kind should not be valid")
	}
}

// --- Concurrent append (regression guard for multi-conn pool) ---

// TestConcurrentAppend fires append calls from many goroutines at
// once to verify the multi-conn pool + WAL + no-mutex design still
// persists every event exactly once with a unique ID. Before #7 this
// would have been serialized behind a single connection and a
// sync.Mutex; after, it runs in parallel at the Go level and the
// database layer's own WAL write lock provides ordering.
func TestConcurrentAppend(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const (
		goroutines  = 8
		perGoroutine = 25
		total        = goroutines * perGoroutine
	)

	errs := make(chan error, total)
	done := make(chan struct{})
	running := 0
	for g := 0; g < goroutines; g++ {
		running++
		go func(gid int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < perGoroutine; i++ {
				evt := &Event{
					Kind:   EventAuthDenied,
					Target: "GET /api/v1/tenants",
					Actor:  Actor{Name: "anonymous"},
				}
				if err := s.Append(ctx, evt); err != nil {
					errs <- err
					return
				}
				if evt.ID == 0 {
					errs <- nil // sentinel; we check ID below
				}
			}
		}(g)
	}
	for i := 0; i < running; i++ {
		<-done
	}
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Append: %v", err)
		}
	}

	events, err := s.List(ctx, Query{Limit: 1000})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != total {
		t.Errorf("persisted %d events, want %d", len(events), total)
	}
	// IDs must be unique.
	seen := make(map[int64]bool, total)
	for _, e := range events {
		if seen[e.ID] {
			t.Errorf("duplicate event ID %d", e.ID)
		}
		seen[e.ID] = true
	}
}

// --- Append + Get ---

func TestAppendAssignsIDAndDefaults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	evt := &Event{
		Kind:   EventTenantCreated,
		Target: "ten_abc",
		Actor:  Actor{}, // name should default to "anonymous"
	}
	if err := s.Append(ctx, evt); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if evt.ID == 0 {
		t.Error("expected ID to be populated")
	}
	if evt.Actor.Name != "anonymous" {
		t.Errorf("actor name default = %q, want anonymous", evt.Actor.Name)
	}
	if evt.Timestamp.IsZero() {
		t.Error("timestamp default not set")
	}

	got, err := s.Get(ctx, evt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != EventTenantCreated || got.Target != "ten_abc" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestAppendRejectsInvalidKind(t *testing.T) {
	s := newTestStore(t)
	err := s.Append(context.Background(), &Event{Kind: "bogus.kind"})
	if err == nil {
		t.Error("expected error for invalid kind")
	}
}

// --- List filters ---

func seedEvents(t *testing.T, s *SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	// Deliberately interleave timestamps so the ORDER BY id DESC
	// result is not accidentally in chronological order.
	for i, e := range []*Event{
		{Kind: EventTenantCreated, Target: "ten_a", Actor: Actor{Name: "admin"}, Timestamp: time.Now().Add(-5 * time.Minute)},
		{Kind: EventAPIKeyCreated, Target: "key_a", Actor: Actor{Name: "admin", Role: "admin"}, Timestamp: time.Now().Add(-3 * time.Minute)},
		{Kind: EventAPIKeyCreated, Target: "key_b", Actor: Actor{Name: "editor"}, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Kind: EventAPIKeyDeleted, Target: "key_a", Actor: Actor{Name: "admin"}, Timestamp: time.Now().Add(-1 * time.Minute)},
		{Kind: EventTenantDeleted, Target: "ten_a", Actor: Actor{Name: "admin"}, Timestamp: time.Now()},
	} {
		if err := s.Append(ctx, e); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
}

func TestListAll(t *testing.T) {
	s := newTestStore(t)
	seedEvents(t, s)

	out, err := s.List(context.Background(), Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 5 {
		t.Fatalf("expected 5, got %d", len(out))
	}
	// Newest first.
	if out[0].Kind != EventTenantDeleted {
		t.Errorf("first row kind = %q, want tenant.deleted", out[0].Kind)
	}
}

func TestListFilterByKind(t *testing.T) {
	s := newTestStore(t)
	seedEvents(t, s)

	out, err := s.List(context.Background(), Query{Kind: EventAPIKeyCreated})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 api_key.created events, got %d", len(out))
	}
	for _, e := range out {
		if e.Kind != EventAPIKeyCreated {
			t.Errorf("kind filter leaked: %s", e.Kind)
		}
	}
}

func TestListFilterByActor(t *testing.T) {
	s := newTestStore(t)
	seedEvents(t, s)

	out, err := s.List(context.Background(), Query{Actor: "editor"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 1 || out[0].Target != "key_b" {
		t.Errorf("actor filter wrong: %+v", out)
	}
}

func TestListFilterByActorTenant(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i, e := range []*Event{
		{Kind: EventAPIKeyCreated, Target: "key_a1", Actor: Actor{Name: "a-admin", TenantID: "ten_a"}, Timestamp: time.Now().Add(-3 * time.Minute)},
		{Kind: EventAPIKeyCreated, Target: "key_b1", Actor: Actor{Name: "b-admin", TenantID: "ten_b"}, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Kind: EventAPIKeyDeleted, Target: "key_a1", Actor: Actor{Name: "a-admin", TenantID: "ten_a"}, Timestamp: time.Now().Add(-1 * time.Minute)},
		{Kind: EventAuthDenied, Target: "", Actor: Actor{Name: "anonymous"}, Timestamp: time.Now()}, // empty tenant (system)
	} {
		if err := s.Append(ctx, e); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	out, err := s.List(ctx, Query{ActorTenant: "ten_a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 ten_a events, got %d", len(out))
	}
	for _, e := range out {
		if e.Actor.TenantID != "ten_a" {
			t.Errorf("tenant filter leaked event from %q", e.Actor.TenantID)
		}
	}
}

func TestListFilterBySince(t *testing.T) {
	s := newTestStore(t)
	seedEvents(t, s)

	out, err := s.List(context.Background(), Query{
		Since: time.Now().Add(-90 * time.Second),
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Only the last two (key_a deletion and ten_a deletion) are
	// inside the 90-second window.
	if len(out) != 2 {
		t.Errorf("expected 2 recent events, got %d", len(out))
	}
}

func TestListLimit(t *testing.T) {
	s := newTestStore(t)
	seedEvents(t, s)

	out, err := s.List(context.Background(), Query{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("limit not applied: %d", len(out))
	}
}

// --- Recorder ---

func TestRecorderNilIsNoOp(t *testing.T) {
	var r *Recorder
	r.Record(context.Background(), EventTenantCreated, Actor{}, "", "")
	r.RecordHTTP(httptest.NewRequest(http.MethodGet, "/", http.NoBody), EventTenantCreated, "", "")
	// No panic = pass.
}

func TestRecorderUsesPrincipalFn(t *testing.T) {
	s := newTestStore(t)
	called := false
	principalFn := func(r *http.Request) Actor {
		called = true
		return Actor{Name: "jane", TenantID: "ten_x", Role: "admin"}
	}
	r := NewRecorder(s, principalFn)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", http.NoBody)
	req.RemoteAddr = "10.0.0.7:443"
	r.RecordHTTP(req, EventTenantCreated, "ten_new", `{"name":"acme"}`)

	if !called {
		t.Error("principalFn was not invoked")
	}

	events, err := s.List(context.Background(), Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Actor.Name != "jane" || e.Actor.TenantID != "ten_x" || e.Actor.Role != "admin" {
		t.Errorf("actor mismatch: %+v", e.Actor)
	}
	if e.Actor.RemoteIP != "10.0.0.7:443" {
		t.Errorf("remote ip not stamped: %q", e.Actor.RemoteIP)
	}
	if e.Target != "ten_new" || e.Details != `{"name":"acme"}` {
		t.Errorf("target/details mismatch: %+v", e)
	}
}

func TestRecorderAnonymousFallback(t *testing.T) {
	s := newTestStore(t)
	r := NewRecorder(s, nil) // no principalFn

	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	r.RecordHTTP(req, EventAgentReloaded, "echo-agent", "")

	events, _ := s.List(context.Background(), Query{})
	if len(events) != 1 || events[0].Actor.Name != "anonymous" {
		t.Errorf("expected anonymous actor, got %+v", events)
	}
}

func TestRecorderSurvivesStoreAppendFailure(t *testing.T) {
	// A nil store inside a non-nil recorder must be a silent no-op.
	r := &Recorder{Store: nil}
	r.Record(context.Background(), EventTenantCreated, Actor{Name: "x"}, "", "")
	// If we got here without a nil-deref, the test passes.
	_ = io.Discard
}

// --- MarshalDetails ---

func TestMarshalDetails(t *testing.T) {
	if MarshalDetails(nil) != "" {
		t.Error("nil should marshal to empty string")
	}
	s := MarshalDetails(map[string]any{"name": "acme", "role": "admin"})
	if s == "" || len(s) < 10 {
		t.Errorf("unexpected marshal: %q", s)
	}
}
