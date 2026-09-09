package streaming

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// deadlineRecorder records SetWriteDeadline calls; ResponseController finds
// the method through the embedded writer chain.
type deadlineRecorder struct {
	http.ResponseWriter
	mu        sync.Mutex
	deadlines []time.Time
}

func (d *deadlineRecorder) Flush() { d.ResponseWriter.(http.Flusher).Flush() }

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadlines = append(d.deadlines, t)
	return nil
}

// TestSSEWriter_ExtendsWriteDeadlinePerFrame is the audit M-16 guard: every
// frame pushes the connection write deadline StreamWriteWindow into the
// future, so the server's one-shot WriteTimeout cannot sever a long paced
// stream mid-frame.
func TestSSEWriter_ExtendsWriteDeadlinePerFrame(t *testing.T) {
	rec := &deadlineRecorder{ResponseWriter: httptest.NewRecorder()}
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := sse.WriteData(map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := sse.WriteEvent("ping", map[string]int{"n": 2}); err != nil {
		t.Fatal(err)
	}
	if err := sse.WriteRaw("[DONE]"); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.deadlines) != 4 { // headers + three frames
		t.Fatalf("SetWriteDeadline called %d times, want 4", len(rec.deadlines))
	}
	for i, d := range rec.deadlines {
		if until := time.Until(d); until < StreamWriteWindow-5*time.Second || until > StreamWriteWindow {
			t.Errorf("deadline %d is %v out, want ~%v", i, until, StreamWriteWindow)
		}
	}
}

// A plain recorder cannot set deadlines; the writer must not fail on it.
func TestSSEWriter_ToleratesUnsupportedDeadline(t *testing.T) {
	sse, err := NewSSEWriter(httptest.NewRecorder())
	if err != nil {
		t.Fatal(err)
	}
	if err := sse.WriteRaw("ok"); err != nil {
		t.Fatal(err)
	}
}
