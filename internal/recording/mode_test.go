package recording

import "testing"

func TestParseRecordMode(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    RecordMode
		wantErr bool
	}{
		{"", RecordModeNone, false},
		{"none", RecordModeNone, false},
		{"new_episodes", RecordModeNewEpisodes, false},
		{"once", RecordModeOnce, false},
		{"all", RecordModeAll, false},
		{"ALL", "", true},
		{"record", "", true},
		{"new-episodes", "", true},
	} {
		got, err := ParseRecordMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseRecordMode(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRecordMode(%q): unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseRecordMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRecordMode_RequiresUpstream(t *testing.T) {
	for m, want := range map[RecordMode]bool{
		RecordModeNone:        false,
		RecordModeNewEpisodes: true,
		RecordModeOnce:        true,
		RecordModeAll:         true,
	} {
		if got := m.RequiresUpstream(); got != want {
			t.Errorf("%s.RequiresUpstream() = %v, want %v", m, got, want)
		}
	}
}

func TestRecordMode_Resolve(t *testing.T) {
	for _, tc := range []struct {
		mode     RecordMode
		recorded int
		want     RecordMode
	}{
		// once on an empty cassette (incl. a leftover 0-byte file) must record.
		{RecordModeOnce, 0, RecordModeNewEpisodes},
		// once on a populated cassette replays only.
		{RecordModeOnce, 3, RecordModeNone},
		// other modes pass through unchanged.
		{RecordModeNone, 0, RecordModeNone},
		{RecordModeNewEpisodes, 0, RecordModeNewEpisodes},
		{RecordModeAll, 5, RecordModeAll},
	} {
		if got := tc.mode.Resolve(tc.recorded); got != tc.want {
			t.Errorf("%s.Resolve(%d) = %s, want %s", tc.mode, tc.recorded, got, tc.want)
		}
	}
}

func TestRecordMode_Records(t *testing.T) {
	for m, want := range map[RecordMode]bool{
		RecordModeNone:        false,
		RecordModeOnce:        false, // resolved to none/new_episodes before use
		RecordModeNewEpisodes: true,
		RecordModeAll:         true,
	} {
		if got := m.Records(); got != want {
			t.Errorf("%s.Records() = %v, want %v", m, got, want)
		}
	}
}
