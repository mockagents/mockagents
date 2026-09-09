package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// pacedAgent streams `words` one word at a time with delayMs between frames.
func pacedAgent(words int, delayMs int) *types.AgentDefinition {
	content := strings.TrimSpace(strings.Repeat("word ", words))
	return &types.AgentDefinition{
		Metadata: types.Metadata{Name: "paced"},
		Spec: types.AgentSpec{
			Model:    "gpt-4o",
			Protocol: "openai-chat-completions",
			Behavior: types.BehaviorConfig{
				Streaming: &types.StreamingConfig{ChunkSize: 1, ChunkDelayMs: types.Ptr(delayMs)},
				Scenarios: []types.Scenario{{Name: "default", Response: types.ScenarioResponse{Content: content}}},
			},
		},
	}
}

func startTestServer(t *testing.T, cfg Config, agent *types.AgentDefinition) (*Server, string) {
	t.Helper()
	eng := newTestEngineFromReg()
	eng.Registry.Register(agent)
	cfg.Host, cfg.Port = "127.0.0.1", 0
	srv := New(eng, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	return srv, "http://" + srv.ListenAddr()
}

// openStream starts a streaming chat completion and returns a reader over
// the SSE body; the caller consumes it.
func openStream(t *testing.T, base string) *http.Response {
	t.Helper()
	body := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(base+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	return resp
}

// countFrames reads SSE data lines until EOF and reports how many arrived
// and whether the [DONE] terminator was among them.
func countFrames(r io.Reader) (frames int, done bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if strings.TrimPrefix(line, "data: ") == "[DONE]" {
			done = true
			continue
		}
		frames++
	}
	return frames, done
}

// TestStream_SurvivesShortWriteTimeout is the audit M-16 guard at the server
// level: with WriteTimeout far shorter than the stream, every frame still
// arrives and the stream ends with [DONE], because each frame extends the
// connection's write deadline.
func TestStream_SurvivesShortWriteTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WriteTimeout = 300 * time.Millisecond
	srv, base := startTestServer(t, cfg, pacedAgent(8, 150)) // ~1.2s of frames
	defer func() { _ = srv.Shutdown() }()

	resp := openStream(t, base)
	defer resp.Body.Close()
	frames, done := countFrames(resp.Body)
	if !done {
		t.Fatalf("stream was cut before [DONE] (got %d frames); WriteTimeout severed it", frames)
	}
	if frames < 8 {
		t.Fatalf("got %d frames, want at least 8", frames)
	}
}

// TestShutdown_DrainsReadinessThenCancelsStreams is the audit M-34 guard:
// readiness flips to 503 "draining" while the drain delay keeps serving, a
// stream longer than the timeout is cancelled (the client sees EOF without
// [DONE]) instead of pinning the process, and Shutdown reports the deadline
// as ErrShutdownDeadline within a bounded time.
func TestShutdown_DrainsReadinessThenCancelsStreams(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = 400 * time.Millisecond
	cfg.ShutdownDrainDelay = 300 * time.Millisecond
	srv, base := startTestServer(t, cfg, pacedAgent(200, 100)) // ~20s stream

	// Before shutdown: ready.
	if r, err := http.Get(base + "/api/v1/ready"); err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("ready before shutdown: %v %v", r, err)
	}

	resp := openStream(t, base)
	defer resp.Body.Close()
	// Make sure the stream is actually flowing before we pull the plug.
	first := make([]byte, 1)
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatal(err)
	}

	shutdownDone := make(chan error, 1)
	start := time.Now()
	go func() { shutdownDone <- srv.Shutdown() }()

	// During the drain delay the listener still serves and readiness says so.
	time.Sleep(60 * time.Millisecond)
	r, err := http.Get(base + "/api/v1/ready")
	if err != nil {
		t.Fatalf("ready during drain: %v", err)
	}
	var rr ReadinessResponse
	_ = json.NewDecoder(r.Body).Decode(&rr)
	r.Body.Close()
	if r.StatusCode != http.StatusServiceUnavailable || len(rr.Checks) == 0 || rr.Checks[0].Name != "draining" || rr.Checks[0].Status != "failed" {
		t.Fatalf("ready during drain = %d %+v, want 503 with draining failed first", r.StatusCode, rr)
	}

	// The long stream is cancelled at the deadline: EOF without [DONE].
	frames, done := countFrames(resp.Body)
	if done {
		t.Fatal("a 20s stream completed during a 0.4s shutdown window")
	}
	if frames == 0 {
		t.Fatal("no frames at all — the stream never started")
	}

	select {
	case err := <-shutdownDone:
		if !errors.Is(err, ErrShutdownDeadline) {
			t.Fatalf("Shutdown returned %v, want ErrShutdownDeadline", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown did not return")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("Shutdown took %v; the stream pinned it", took)
	}
	// Request contexts are cancelled for good.
	if srv.baseCtx.Err() == nil {
		t.Fatal("base context still live after shutdown")
	}
}

// TestShutdown_CleanWhenIdle: with nothing in flight, Shutdown returns nil
// well inside the timeout and readiness stays draining afterwards.
func TestShutdown_CleanWhenIdle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = 2 * time.Second
	srv, _ := startTestServer(t, cfg, pacedAgent(1, 0))
	start := time.Now()
	if err := srv.Shutdown(); err != nil {
		t.Fatalf("idle shutdown: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("idle shutdown took %v", time.Since(start))
	}
	if !srv.draining.Load() {
		t.Fatal("draining flag not set")
	}
}
