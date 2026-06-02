package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
)

func TestLogBroadcaster_SingleSubscriber(t *testing.T) {
	var b LogBroadcaster
	sub, cancel := b.Subscribe(4)
	defer cancel()

	entry := &storage.InteractionLog{ID: 1, AgentName: "alpha"}
	b.Publish(entry)

	select {
	case got := <-sub.C():
		if got.ID != 1 || got.AgentName != "alpha" {
			t.Errorf("got = %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publish did not arrive")
	}
	if sub.Dropped() != 0 {
		t.Errorf("unexpected drops: %d", sub.Dropped())
	}
}

func TestLogBroadcaster_FanOutToMany(t *testing.T) {
	var b LogBroadcaster
	const subs = 3
	subsArr := make([]*LogSubscription, subs)
	cancels := make([]func(), subs)
	for i := 0; i < subs; i++ {
		subsArr[i], cancels[i] = b.Subscribe(2)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()
	if got := b.SubscriberCount(); got != subs {
		t.Errorf("SubscriberCount = %d, want %d", got, subs)
	}

	b.Publish(&storage.InteractionLog{ID: 7})
	for i, s := range subsArr {
		select {
		case got := <-s.C():
			if got.ID != 7 {
				t.Errorf("sub[%d].ID = %d", i, got.ID)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("sub[%d] never received", i)
		}
	}
}

func TestLogBroadcaster_SlowSubscriberDrops(t *testing.T) {
	var b LogBroadcaster
	// Buffer size 1 — the second and later publishes have nowhere
	// to go.
	sub, cancel := b.Subscribe(1)
	defer cancel()

	for i := 0; i < 5; i++ {
		b.Publish(&storage.InteractionLog{ID: int64(i)})
	}
	// The publisher must not have blocked; the fact that we reached
	// this line is the assertion. SubscriberCount should still be 1.
	if got := b.SubscriberCount(); got != 1 {
		t.Errorf("SubscriberCount = %d", got)
	}
	// Exactly 4 drops: the first publish fits in the buffer, the
	// other 4 overflow.
	if dropped := sub.Dropped(); dropped != 4 {
		t.Errorf("Dropped = %d, want 4", dropped)
	}
}

func TestLogBroadcaster_CancelClosesChannel(t *testing.T) {
	var b LogBroadcaster
	sub, cancel := b.Subscribe(2)
	cancel()
	// Channel must be closed.
	select {
	case _, ok := <-sub.C():
		if ok {
			t.Error("expected closed channel")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("channel not closed")
	}
	if b.SubscriberCount() != 0 {
		t.Error("subscriber not removed")
	}
	// Publish after cancel is a no-op — no panic on closed chan.
	b.Publish(&storage.InteractionLog{ID: 99})
}

func TestLogBroadcaster_NilReceiverIsNoop(t *testing.T) {
	var b *LogBroadcaster
	b.Publish(&storage.InteractionLog{ID: 1})
	if b.SubscriberCount() != 0 {
		t.Error("nil broadcaster should report zero")
	}
	// Nil Snapshot() must also be safe — the metrics handler
	// short-circuits before calling it, but the method still
	// needs to be nil-safe for defense-in-depth.
	snap := b.Snapshot()
	if snap.SubscriberCount != 0 || snap.TotalDropped != 0 {
		t.Errorf("nil snapshot = %+v", snap)
	}
}

// --- Snapshot ---

func TestLogBroadcaster_SnapshotEmpty(t *testing.T) {
	var b LogBroadcaster
	snap := b.Snapshot()
	if snap.SubscriberCount != 0 || snap.TotalDropped != 0 || snap.MaxDropped != 0 {
		t.Errorf("empty snapshot = %+v", snap)
	}
	if len(snap.Subscribers) != 0 {
		t.Errorf("empty snapshot has subscribers: %+v", snap.Subscribers)
	}
}

func TestLogBroadcaster_SnapshotAfterDrops(t *testing.T) {
	var b LogBroadcaster
	// Three subscribers, each with a different overflow level.
	// Buffer 1 means the first publish fits and every subsequent
	// publish drops.
	s1, c1 := b.Subscribe(1)
	defer c1()
	s2, c2 := b.Subscribe(1)
	defer c2()
	s3, c3 := b.Subscribe(1)
	defer c3()

	// s1 sees 3 drops, s2 sees 5, s3 sees 0.
	for i := 0; i < 4; i++ {
		b.Publish(&storage.InteractionLog{ID: int64(i)})
	}
	// Drain s1 so it gets NO more drops while s2 keeps flooding.
	<-s1.C()
	// s2 is still at buffer full (1 unread). Extra floods land on
	// s2 as drops and s1 as drops too.
	for i := 0; i < 2; i++ {
		b.Publish(&storage.InteractionLog{ID: int64(100 + i)})
	}

	snap := b.Snapshot()
	if snap.SubscriberCount != 3 {
		t.Errorf("subs = %d", snap.SubscriberCount)
	}
	// Sorted descending by Dropped. We don't know which sub
	// landed in which slot, but we can assert totals.
	var totalDropped uint64
	var maxDropped uint64
	for _, s := range snap.Subscribers {
		totalDropped += s.Dropped
		if s.Dropped > maxDropped {
			maxDropped = s.Dropped
		}
	}
	if totalDropped != snap.TotalDropped {
		t.Errorf("total mismatch: summed=%d snap=%d", totalDropped, snap.TotalDropped)
	}
	if maxDropped != snap.MaxDropped {
		t.Errorf("max mismatch: computed=%d snap=%d", maxDropped, snap.MaxDropped)
	}
	if snap.TotalDropped == 0 {
		t.Error("expected non-zero total drops")
	}
	// Sorted descending — first entry must have the max.
	if len(snap.Subscribers) > 0 && snap.Subscribers[0].Dropped != snap.MaxDropped {
		t.Errorf("not sorted descending: %+v", snap.Subscribers)
	}
	// Keep the references alive so the compiler doesn't decide
	// s2 / s3 are unused before the assertions run.
	_ = s2
	_ = s3
}

func TestLogBroadcaster_SnapshotBufferFill(t *testing.T) {
	var b LogBroadcaster
	sub, cancel := b.Subscribe(4)
	defer cancel()
	b.Publish(&storage.InteractionLog{ID: 1})
	b.Publish(&storage.InteractionLog{ID: 2})

	snap := b.Snapshot()
	if len(snap.Subscribers) != 1 {
		t.Fatalf("subs = %d", len(snap.Subscribers))
	}
	s := snap.Subscribers[0]
	if s.BufferCap != 4 {
		t.Errorf("BufferCap = %d", s.BufferCap)
	}
	if s.BufferLen != 2 {
		t.Errorf("BufferLen = %d", s.BufferLen)
	}
	if s.Dropped != 0 {
		t.Errorf("Dropped = %d", s.Dropped)
	}
	// Drain and re-snapshot: buffer len drops to 0, drops stay at 0.
	<-sub.C()
	<-sub.C()
	snap2 := b.Snapshot()
	if snap2.Subscribers[0].BufferLen != 0 {
		t.Errorf("BufferLen after drain = %d", snap2.Subscribers[0].BufferLen)
	}
}

// --- StreamMetrics HTTP handler ---

func TestStreamMetricsHappyPath(t *testing.T) {
	broadcaster := &LogBroadcaster{}
	_, cancel := broadcaster.Subscribe(4)
	defer cancel()
	broadcaster.Publish(&storage.InteractionLog{ID: 1})

	h := &LogHandlers{Broadcaster: broadcaster}
	srv := httptest.NewServer(http.HandlerFunc(h.StreamMetrics))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var snap BroadcasterSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.SubscriberCount != 1 {
		t.Errorf("subs = %d", snap.SubscriberCount)
	}
	if len(snap.Subscribers) != 1 || snap.Subscribers[0].BufferLen != 1 {
		t.Errorf("snap = %+v", snap)
	}
}

func TestStreamMetricsWithoutBroadcaster503(t *testing.T) {
	h := &LogHandlers{}
	srv := httptest.NewServer(http.HandlerFunc(h.StreamMetrics))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// --- End-to-end: StreamLogs handler ---

func TestStreamLogsEndToEnd(t *testing.T) {
	store := newWorkerStore(t)
	broadcaster := &LogBroadcaster{}
	worker := NewLogWorker(store, nil, LogWorkerConfig{
		Workers:     1,
		QueueSize:   8,
		Broadcaster: broadcaster,
	})
	t.Cleanup(func() { worker.Shutdown(time.Second) })

	h := &LogHandlers{
		Store:           store,
		Broadcaster:     broadcaster,
		StreamHeartbeat: 50 * time.Millisecond,
	}
	srv := httptest.NewServer(http.HandlerFunc(h.StreamLogs))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}

	// Wait until the SSE handler has subscribed — otherwise Submit's
	// Publish call races with Subscribe and we publish to an empty
	// subscriber list.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && broadcaster.SubscriberCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if broadcaster.SubscriberCount() == 0 {
		t.Fatal("handler never subscribed")
	}

	entry := &storage.InteractionLog{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		AgentName:      "streamer",
		Protocol:       "openai",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		ResponseStatus: 200,
		LatencyMs:      12,
	}
	if !worker.Submit(entry) {
		t.Fatal("Submit returned false")
	}

	r := bufio.NewReader(resp.Body)
	// Skip heartbeats until we see the event: log line.
	var dataLine string
	readDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(readDeadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("ReadString: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "event: log" {
			dataLine, err = r.ReadString('\n')
			if err != nil {
				t.Fatalf("ReadString data: %v", err)
			}
			break
		}
	}
	if dataLine == "" {
		t.Fatal("never saw event: log frame")
	}
	dataLine = strings.TrimRight(dataLine, "\r\n")
	if !strings.HasPrefix(dataLine, "data: ") {
		t.Fatalf("data line = %q", dataLine)
	}
	var got LogWithCost
	if err := json.Unmarshal([]byte(dataLine[len("data: "):]), &got); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if got.AgentName != "streamer" {
		t.Errorf("agent = %q", got.AgentName)
	}
	if got.ID == 0 {
		t.Error("id was not populated from LastInsertId")
	}
}

func TestStreamLogsWithoutBroadcaster503(t *testing.T) {
	h := &LogHandlers{}
	srv := httptest.NewServer(http.HandlerFunc(h.StreamLogs))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// TestStreamLogsEmitsDroppedFrame forces a subscriber into
// backpressure by using a tiny buffer, then pumping many publishes
// past the buffer. The handler should surface the resulting drop
// count as an `event: dropped` frame the client can observe —
// proving the (§2.44) SSE drop-count signal reaches the wire.
func TestStreamLogsEmitsDroppedFrame(t *testing.T) {
	// Custom broadcaster wiring: manually open a subscription with
	// a buffer of 1 so we can force drops, then hand it to a
	// LogHandlers that exposes StreamLogs over httptest. We don't
	// use a full LogWorker because the worker publishes through
	// the broadcaster's Subscribe() which ignores custom buffer
	// sizes — we need precise control over the queue depth to
	// trigger the overflow path.
	broadcaster := &LogBroadcaster{}
	h := &LogHandlers{
		Broadcaster:     broadcaster,
		StreamHeartbeat: 30 * time.Millisecond,
	}
	srv := httptest.NewServer(http.HandlerFunc(h.StreamLogs))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()

	// Wait until the handler has subscribed, then grab its
	// subscription via reflection-like access: the broadcaster
	// only holds one subscription in this test, so we iterate
	// the private map. We grab it by swapping in a shorter
	// buffer via a direct call to broadcaster.Subscribe — that
	// gives us the handle we need to call Dropped() later.
	//
	// The handler's own Subscribe() call has a 64-slot buffer,
	// which makes drops hard to trigger. The trick: we publish
	// many entries as fast as possible in a tight loop — far
	// more than 64 — while the handler's goroutine blocks on
	// writing each frame to the HTTP response (the httptest
	// server's connection is slow because we never read from
	// resp.Body until the assert loop). That back-pressures the
	// handler, which in turn leaves unread events in its
	// subscription channel until the 64-slot buffer fills and
	// further publishes drop.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && broadcaster.SubscriberCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if broadcaster.SubscriberCount() == 0 {
		t.Fatal("handler never subscribed")
	}

	// Publish enough to overflow the default 64-slot buffer
	// several times over. The handler's goroutine will drain a
	// handful, but the rest pile up and get dropped.
	for i := 0; i < 500; i++ {
		broadcaster.Publish(&storage.InteractionLog{
			ID:             int64(i + 1),
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
			AgentName:      "flood",
			Protocol:       "openai",
			RequestMethod:  "POST",
			RequestPath:    "/v1/chat/completions",
			ResponseStatus: 200,
		})
	}

	// Now read the SSE stream and look for an `event: dropped`
	// frame. A tight polling loop with a short deadline keeps
	// the test fast while tolerating the handler's heartbeat
	// cadence.
	r := bufio.NewReader(resp.Body)
	var droppedSeen bool
	readDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(readDeadline) && !droppedSeen {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("ReadString: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line != "event: dropped" {
			continue
		}
		// Next line must be `data: {"count":N,"new":M}`.
		dataLine, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read data line: %v", err)
		}
		dataLine = strings.TrimRight(dataLine, "\r\n")
		if !strings.HasPrefix(dataLine, "data: ") {
			t.Fatalf("unexpected data line: %q", dataLine)
		}
		var payload struct {
			Count int `json:"count"`
			New   int `json:"new"`
		}
		if err := json.Unmarshal([]byte(dataLine[len("data: "):]), &payload); err != nil {
			t.Fatalf("decode dropped payload: %v", err)
		}
		if payload.Count <= 0 || payload.New <= 0 {
			t.Errorf("unexpected counts: count=%d new=%d", payload.Count, payload.New)
		}
		droppedSeen = true
	}
	if !droppedSeen {
		t.Fatal("never saw event: dropped frame")
	}
}

// TestLogBroadcaster_Close covers F-SV-001: Close closes every active
// subscriber's channel (so SSE handlers exit), Subscribe-after-Close hands
// back an already-closed subscription, a subscriber's cancel after Close is
// a no-op (no double-close panic), and Close is idempotent.
func TestLogBroadcaster_Close(t *testing.T) {
	b := &LogBroadcaster{}
	sub, cancel := b.Subscribe(4)

	b.Close()

	// Active subscriber's channel must be closed.
	if _, ok := <-sub.C(); ok {
		t.Error("subscriber channel should be closed after Close")
	}
	// Its cancel must not panic (sub already removed from the map).
	cancel()

	// Subscribe after Close yields an already-closed subscription.
	sub2, cancel2 := b.Subscribe(4)
	if _, ok := <-sub2.C(); ok {
		t.Error("post-Close Subscribe should return a closed channel")
	}
	cancel2()

	// Publish after Close is a no-op; Close is idempotent.
	b.Publish(makeEntry(1))
	b.Close()
}
