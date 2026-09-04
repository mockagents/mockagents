package streaming

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// Regression tests for the chunk_delay_ms defect.
//
// `chunk_delay_ms: 0` means "stream with no artificial delay". The JSON schema
// documents it (`minimum: 0`) and examples/gemini-agent.yaml asks for it, but
// the field used to be a plain int, so config.ApplyDefaults could not tell an
// explicit 0 from an unset field and replaced it with 50. The agent then ran
// ~50 ms slower per chunk than it said it would, and every streaming path's
// `if ChunkDelayMs >= 0` guard was dead code because defaulting had already
// happened.
//
// The field is now a *int. These tests pin both halves: an explicit zero is
// honoured, and an omitted field still gets the documented default.

// The pacer is where the value becomes behaviour, and its chunkDelay is
// deterministic — so this is the assertion that cannot flake.
func TestPacer_ChunkDelayFromConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  *types.StreamingConfig
		want time.Duration
	}{
		{
			name: "explicit zero means no delay",
			cfg:  &types.StreamingConfig{ChunkDelayMs: types.Ptr(0)},
			want: 0,
		},
		{
			name: "unset falls back to the documented default",
			cfg:  &types.StreamingConfig{},
			want: DefaultChunkDelayMs * time.Millisecond,
		},
		{
			name: "no config at all falls back to the default",
			cfg:  nil,
			want: DefaultChunkDelayMs * time.Millisecond,
		},
		{
			name: "an explicit value is used as given",
			cfg:  &types.StreamingConfig{ChunkDelayMs: types.Ptr(120)},
			want: 120 * time.Millisecond,
		},
		{
			name: "an explicit one millisecond is not rounded away",
			cfg:  &types.StreamingConfig{ChunkDelayMs: types.Ptr(1)},
			want: time.Millisecond,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, newPacer(tc.cfg).chunkDelay)
		})
	}
}

// And the same thing observed as elapsed time through a real stream, which is
// what an operator actually experiences.
//
// The bounds are deliberately wide. With this many chunks the default pacing
// would cost seconds, so a generous ceiling still separates the two cases by an
// order of magnitude and leaves plenty of room for a loaded CI box.
func TestStreamOpenAI_ExplicitZeroDelayDoesNotSleep(t *testing.T) {
	resp := &engine.Response{
		AgentName: "test-agent",
		Model:     "gpt-4o",
		// Chunk size 1 over this many words means many inter-chunk gaps.
		Content: "one two three four five six seven eight nine ten",
	}

	start := time.Now()
	rec := httptest.NewRecorder()
	err := StreamOpenAI(context.Background(), rec, resp,
		&types.StreamingConfig{ChunkSize: 1, ChunkDelayMs: types.Ptr(0)})
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotEmpty(t, parseSSEDataLines(rec.Body.String()))

	// At the default 50 ms this content would take well over a second. If the
	// explicit zero were being clobbered again, this would blow straight past
	// the ceiling.
	require.Less(t, elapsed, 500*time.Millisecond,
		"an explicit chunk_delay_ms: 0 must not sleep between chunks (took %s)", elapsed)
}

// The complement: omitting the field must still pace the stream, or the fix
// would have silently turned the default off for everyone.
func TestStreamOpenAI_UnsetDelayStillPaces(t *testing.T) {
	resp := &engine.Response{
		AgentName: "test-agent",
		Model:     "gpt-4o",
		Content:   "one two three four",
	}

	start := time.Now()
	rec := httptest.NewRecorder()
	// ChunkDelayMs omitted → nil → DefaultChunkDelayMs.
	err := StreamOpenAI(context.Background(), rec, resp,
		&types.StreamingConfig{ChunkSize: 1})
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Four chunks at 50 ms is ~150 ms of gaps; assert only half of that so a
	// coarse timer cannot make this flaky.
	require.Greater(t, elapsed, 75*time.Millisecond,
		"an unset chunk_delay_ms must still apply the default pacing (took %s)", elapsed)
}
