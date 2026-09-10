package recording

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactorSSEEveryNetworkSplit(t *testing.T) {
	const secret = "ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	frame := "data: {\"token\":\"" + secret + "\"}\n\n"
	r, _ := NewRedactor(nil)
	for split := 1; split < len(frame); split++ {
		it := &Interaction{Streaming: true, StreamEvents: []StreamEvent{
			{DelayMs: 1, Data: frame[:split]}, {DelayMs: 2, Data: frame[split:]},
		}}
		if err := r.Apply(it); err != nil {
			t.Fatalf("split %d: %v", split, err)
		}
		if len(it.StreamEvents) != 1 {
			t.Fatalf("split %d: got %d frames", split, len(it.StreamEvents))
		}
		if strings.Contains(it.StreamEvents[0].Data, secret) {
			t.Fatalf("split %d leaked secret", split)
		}
		if it.StreamEvents[0].DelayMs != 2 {
			t.Fatalf("split %d lost completion delay", split)
		}
	}
}

func TestRedactorSSECRLFMultipleFramesFragmented(t *testing.T) {
	r, _ := NewRedactor(nil)
	input := "data: {\"token\":\"ghp_1234567890abcdefghijklmnopqrstuvwxyz\"}\r\n\r\ndata: {\"ok\":true}\n\n"
	chunks := []StreamEvent{{DelayMs: 1, Data: input[:7]}, {DelayMs: 2, Data: input[7:31]}, {DelayMs: 3, Data: input[31:]}}
	it := &Interaction{Streaming: true, StreamEvents: chunks}
	if err := r.Apply(it); err != nil {
		t.Fatal(err)
	}
	if len(it.StreamEvents) != 2 {
		t.Fatalf("frames = %d", len(it.StreamEvents))
	}
	if strings.Contains(it.StreamEvents[0].Data, "ghp_") {
		t.Fatal("secret leaked")
	}
	if !strings.HasSuffix(it.StreamEvents[0].Data, "\r\n\r\n") || !strings.HasSuffix(it.StreamEvents[1].Data, "\n\n") {
		t.Fatalf("delimiters changed: %#v", it.StreamEvents)
	}
}

func TestProxyStreamingRedactionErrorTrailerAndNoCassette(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"token\":\"sk-secret\"}") // deliberately incomplete
	}))
	defer upstream.Close()
	cass := New("")
	p, _ := NewProxy(upstream.URL, cass)
	p.Redactor, _ = NewRedactor(nil)
	front := httptest.NewServer(p)
	defer front.Close()
	resp, err := http.Get(front.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if got := resp.Trailer.Get("X-Mockagents-Record-Error"); !strings.Contains(got, "incomplete") {
		t.Fatalf("trailer = %q", got)
	}
	if len(cass.All()) != 0 {
		t.Fatal("unsafe partial frame was persisted")
	}
}

func TestReplayAcceptsLegacyChunkedStreamingCassette(t *testing.T) {
	cass := New("")
	it := &Interaction{Method: http.MethodGet, Path: "/legacy", ResponseStatus: http.StatusOK, Streaming: true,
		StreamEvents: []StreamEvent{{Data: "data: legacy"}, {Data: "\n\n"}}}
	it.Hash = HashRequest(it.Method, it.Path, nil)
	if err := cass.Append(it); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	NewReplay(cass).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/legacy", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "data: legacy\n\n" {
		t.Fatalf("legacy replay: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRedactorSSERejectsIncompleteAndOversizeFrames(t *testing.T) {
	r, _ := NewRedactor(nil)
	incomplete := &Interaction{Streaming: true, StreamEvents: []StreamEvent{{Data: "data: {\"token\":\"sk-secret\"}"}}}
	if err := r.Apply(incomplete); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete error = %v", err)
	}
	oversize := &Interaction{Streaming: true, StreamEvents: []StreamEvent{{Data: strings.Repeat("x", maxRecordedSSEFrameBytes+1)}}}
	if err := r.Apply(oversize); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize error = %v", err)
	}
}
