package recording

import "fmt"

// RecordMode controls how `mockagents replay` treats a request that does not
// match any recorded interaction (VCR-style record modes, R-01).
type RecordMode string

const (
	// RecordModeNone replays only; a miss returns the 404 diagnostics. Default.
	RecordModeNone RecordMode = "none"
	// RecordModeNewEpisodes replays recorded interactions and, on a miss,
	// forwards to the upstream, serves the client, and records the new
	// interaction so it replays next time.
	RecordModeNewEpisodes RecordMode = "new_episodes"
	// RecordModeOnce behaves like new_episodes when the cassette did not exist
	// at startup, and like none (replay-only) when the cassette pre-existed.
	RecordModeOnce RecordMode = "once"
	// RecordModeAll never replays: every request is forwarded to the upstream
	// and recorded (re-record / passthrough).
	RecordModeAll RecordMode = "all"
)

// ParseRecordMode validates a --record-mode flag value. An empty string maps to
// the default (none); any unrecognized value is an error.
func ParseRecordMode(s string) (RecordMode, error) {
	switch RecordMode(s) {
	case "", RecordModeNone:
		return RecordModeNone, nil
	case RecordModeNewEpisodes:
		return RecordModeNewEpisodes, nil
	case RecordModeOnce:
		return RecordModeOnce, nil
	case RecordModeAll:
		return RecordModeAll, nil
	default:
		return "", fmt.Errorf("unknown record mode %q (want none|new_episodes|once|all)", s)
	}
}

// RequiresUpstream reports whether a mode can record and therefore needs an
// --upstream URL. once requires it up front because whether it will actually
// record is only known after the cassette's pre-existence is checked.
func (m RecordMode) RequiresUpstream() bool {
	switch m {
	case RecordModeNewEpisodes, RecordModeOnce, RecordModeAll:
		return true
	default:
		return false
	}
}

// Records reports whether the mode records new interactions on a miss (used
// after once has been resolved against the cassette's pre-existence).
func (m RecordMode) Records() bool {
	return m == RecordModeNewEpisodes || m == RecordModeAll
}

// Resolve maps `once` to a concrete mode based on how many interactions the
// loaded cassette holds: an empty/absent cassette ("nothing recorded yet")
// records like new_episodes, a populated one replays only (none). All other
// modes pass through unchanged. Keying on the recorded count — not file
// presence — means a leftover 0-byte cassette from a prior empty run still
// records instead of failing.
func (m RecordMode) Resolve(recordedCount int) RecordMode {
	if m == RecordModeOnce {
		if recordedCount > 0 {
			return RecordModeNone
		}
		return RecordModeNewEpisodes
	}
	return m
}
