package adapter

import (
	"context"
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
