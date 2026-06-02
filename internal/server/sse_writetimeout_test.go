package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// deadlineWriter is an http.ResponseWriter that records every
// SetWriteDeadline call. It embeds a ResponseRecorder for the
// Header/Write/WriteHeader/Flush surface.
type deadlineWriter struct {
	http.ResponseWriter
	mu        sync.Mutex
	deadlines []time.Time
}

func (d *deadlineWriter) SetWriteDeadline(tm time.Time) error {
	d.mu.Lock()
	d.deadlines = append(d.deadlines, tm)
	d.mu.Unlock()
	return nil
}

func (d *deadlineWriter) last() (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.deadlines) == 0 {
		return time.Time{}, false
	}
	return d.deadlines[len(d.deadlines)-1], true
}

// TestStreamLogs_ResetsWriteDeadline is the F-SV-004 guard: the SSE handler
// must push the per-connection write deadline far into the future (via
// http.ResponseController, which descends through statusWriter.Unwrap) so the
// server's global WriteTimeout can't sever the long-lived feed. A deadline of
// "heartbeat + 30s" out is the contract.
func TestStreamLogs_ResetsWriteDeadline(t *testing.T) {
	bc := &LogBroadcaster{}
	h := &LogHandlers{Store: newTestStore(t), Broadcaster: bc, StreamHeartbeat: 20 * time.Millisecond}

	base := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	// Wrap in statusWriter to mirror (one layer of) the production middleware
	// chain and exercise the Unwrap path ResponseController relies on.
	sw := &statusWriter{ResponseWriter: base, status: http.StatusOK}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/v1/logs/stream", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() { h.StreamLogs(sw, req); close(done) }()

	require.Eventually(t, func() bool { _, ok := base.last(); return ok },
		time.Second, 5*time.Millisecond, "handler never set a write deadline")

	last, _ := base.last()
	cancel()
	<-done

	if d := time.Until(last); d < 25*time.Second {
		t.Errorf("write deadline is only %v out; want > 25s (per-stream reset not applied)", d)
	}
}
