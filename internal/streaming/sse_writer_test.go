package streaming

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEWriter_Creation(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	require.NoError(t, err)
	require.NotNil(t, sse)

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))
}

func TestSSEWriter_WriteData(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	require.NoError(t, err)

	err = sse.WriteData(map[string]string{"msg": "hello"})
	require.NoError(t, err)

	body := rec.Body.String()
	assert.Contains(t, body, `data: {"msg":"hello"}`)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(body), `}`))
}

func TestSSEWriter_WriteEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	require.NoError(t, err)

	err = sse.WriteEvent("message_start", map[string]string{"type": "message_start"})
	require.NoError(t, err)

	body := rec.Body.String()
	assert.Contains(t, body, "event: message_start\n")
	assert.Contains(t, body, `data: {"type":"message_start"}`)
}

func TestSSEWriter_WriteRaw(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	require.NoError(t, err)

	err = sse.WriteRaw("[DONE]")
	require.NoError(t, err)

	body := rec.Body.String()
	assert.Contains(t, body, "data: [DONE]\n")
}

func TestSSEWriter_MultipleEvents(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	require.NoError(t, err)

	sse.WriteData(map[string]int{"a": 1})
	sse.WriteData(map[string]int{"b": 2})
	sse.WriteRaw("[DONE]")

	body := rec.Body.String()
	assert.Equal(t, 3, strings.Count(body, "data: "))
}
