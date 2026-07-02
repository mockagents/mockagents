package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// realtimeServer mounts the Realtime adapter routes on an httptest server and
// returns the ws:// base URL.
func realtimeServer(t *testing.T) (string, func()) {
	t.Helper()
	h := &RealtimeHandler{Engine: testEngine(testOpenAIAgent())}
	mux := http.NewServeMux()
	for _, rt := range h.Routes() {
		mux.HandleFunc(rt.Pattern, rt.Handler)
	}
	srv := httptest.NewServer(mux)
	return srv.URL, srv.Close
}

func wsWrite(t *testing.T, ctx context.Context, c *websocket.Conn, ev map[string]any) {
	t.Helper()
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	require.NoError(t, c.Write(ctx, websocket.MessageText, data))
}

func wsRead(t *testing.T, ctx context.Context, c *websocket.Conn) map[string]any {
	t.Helper()
	typ, data, err := c.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, typ)
	var ev map[string]any
	require.NoError(t, json.Unmarshal(data, &ev))
	return ev
}

// pcmB64 builds ms milliseconds of constant-amplitude PCM16LE @ 24 kHz base64
// (0 = silence; large amplitudes read as speech to the server VAD).
func pcmB64(ms int, amplitude int16) string {
	samples := ms * 24
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		buf[i*2] = byte(uint16(amplitude))
		buf[i*2+1] = byte(uint16(amplitude) >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// A server-VAD voice turn over the wire: enable turn detection, stream speech
// then silence, and receive speech_started → speech_stopped → auto-commit →
// auto-response without ever sending commit or response.create.
func TestRealtime_WebSocketServerVADTurn(t *testing.T) {
	base, closeFn := realtimeServer(t)
	defer closeFn()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/v1/realtime?model=gpt-4o"
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"realtime"}})
	require.NoError(t, err)
	defer c.CloseNow()

	require.Equal(t, "session.created", wsRead(t, ctx, c)["type"])
	wsWrite(t, ctx, c, map[string]any{"type": "session.update", "session": map[string]any{
		"audio": map[string]any{"input": map[string]any{"turn_detection": map[string]any{"type": "server_vad"}}}}})
	require.Equal(t, "session.updated", wsRead(t, ctx, c)["type"])

	// Speech, then enough silence to end the turn. No commit, no response.create.
	wsWrite(t, ctx, c, map[string]any{"type": "input_audio_buffer.append", "audio": pcmB64(400, 20000)})
	require.Equal(t, "input_audio_buffer.speech_started", wsRead(t, ctx, c)["type"])
	wsWrite(t, ctx, c, map[string]any{"type": "input_audio_buffer.append", "audio": pcmB64(600, 0)})

	var seen []string
	for {
		ev := wsRead(t, ctx, c)
		seen = append(seen, ev["type"].(string))
		if ev["type"] == "response.done" {
			break
		}
	}
	for _, want := range []string{"input_audio_buffer.speech_stopped", "input_audio_buffer.committed",
		"conversation.item.added", "response.created"} {
		require.Contains(t, seen, want)
	}
	require.NoError(t, c.Close(websocket.StatusNormalClosure, ""))
}

// Phase 2 over the wire: after a VAD turn's response completes, the server's
// idle timeout fires on its own — the client sends nothing and still receives
// timeout_triggered plus the follow-up response.
func TestRealtime_WebSocketIdleTimeout(t *testing.T) {
	base, closeFn := realtimeServer(t)
	defer closeFn()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/v1/realtime?model=gpt-4o"
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"realtime"}})
	require.NoError(t, err)
	defer c.CloseNow()

	require.Equal(t, "session.created", wsRead(t, ctx, c)["type"])
	wsWrite(t, ctx, c, map[string]any{"type": "session.update", "session": map[string]any{
		"audio": map[string]any{"input": map[string]any{"turn_detection": map[string]any{
			"type": "server_vad", "idle_timeout_ms": 150}}}}})
	require.Equal(t, "session.updated", wsRead(t, ctx, c)["type"])

	// One VAD turn, then go silent (no frames at all).
	wsWrite(t, ctx, c, map[string]any{"type": "input_audio_buffer.append", "audio": pcmB64(400, 20000)})
	wsWrite(t, ctx, c, map[string]any{"type": "input_audio_buffer.append", "audio": pcmB64(600, 0)})
	for wsRead(t, ctx, c)["type"] != "response.done" {
	}

	// The idle timeout fires server-side: timeout_triggered, the empty-segment
	// commit, and a follow-up response — all without another client frame.
	var seen []string
	for {
		ev := wsRead(t, ctx, c)
		seen = append(seen, ev["type"].(string))
		if ev["type"] == "response.done" {
			break
		}
	}
	require.Equal(t, "input_audio_buffer.timeout_triggered", seen[0])
	for _, want := range []string{"input_audio_buffer.committed", "conversation.item.added", "response.created"} {
		require.Contains(t, seen, want)
	}
	require.NoError(t, c.Close(websocket.StatusNormalClosure, ""))
}

func TestRealtime_WebSocketEndToEnd(t *testing.T) {
	base, closeFn := realtimeServer(t)
	defer closeFn()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/v1/realtime?model=gpt-4o"
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"realtime"}})
	require.NoError(t, err, "Realtime WebSocket dial")
	defer c.CloseNow()

	// 1. session.created on connect.
	require.Equal(t, "session.created", wsRead(t, ctx, c)["type"])

	// 2. session.update is acked.
	wsWrite(t, ctx, c, map[string]any{"type": "session.update", "session": map[string]any{"voice": "verse"}})
	updated := wsRead(t, ctx, c)
	require.Equal(t, "session.updated", updated["type"])

	// 3. a text item, then a response.
	wsWrite(t, ctx, c, map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "hello"}}},
	})
	// GA announces the new item with the added → done pair.
	require.Equal(t, "conversation.item.added", wsRead(t, ctx, c)["type"])
	require.Equal(t, "conversation.item.done", wsRead(t, ctx, c)["type"])

	wsWrite(t, ctx, c, map[string]any{"type": "response.create"})
	var seen []string
	var transcript string
	sawAudio := false
	for {
		ev := wsRead(t, ctx, c)
		seen = append(seen, ev["type"].(string))
		switch ev["type"] {
		case "response.output_audio.delta":
			if ev["delta"].(string) != "" {
				sawAudio = true
			}
		case "response.output_audio_transcript.done":
			transcript = ev["transcript"].(string)
		case "response.done":
			goto done
		}
	}
done:
	require.Equal(t, "response.created", seen[0])
	require.Contains(t, seen, "response.output_audio_transcript.delta")
	require.True(t, sawAudio, "expected non-empty audio deltas")
	// testOpenAIAgent answers "hello" with the greeting scenario "Hi there!".
	require.Equal(t, "Hi there!", transcript)

	require.NoError(t, c.Close(websocket.StatusNormalClosure, ""))
}

func TestRealtime_ClientSecret(t *testing.T) {
	h := &RealtimeHandler{Engine: testEngine(testOpenAIAgent())}
	req := httptest.NewRequest("POST", "/v1/realtime/client_secrets",
		strings.NewReader(`{"session":{"model":"gpt-4o-realtime","voice":"verse"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleClientSecret(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Regexp(t, `^ek_`, body["value"])
	require.NotNil(t, body["expires_at"])
	sess := body["session"].(map[string]any)
	require.Equal(t, "gpt-4o-realtime", sess["model"])
	require.Equal(t, "realtime", sess["type"])
	// GA shape: voice is nested under audio.output, not top-level.
	require.Nil(t, sess["voice"])
	audioOut := sess["audio"].(map[string]any)["output"].(map[string]any)
	require.Equal(t, "verse", audioOut["voice"])
}
