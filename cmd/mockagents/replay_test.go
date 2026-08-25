package main

import (
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/recording"
)

func TestValidateStrictReplay(t *testing.T) {
	tests := []struct {
		name     string
		strict   bool
		mode     recording.RecordMode
		upstream string
		apiKey   string
		wantErr  string
	}{
		{name: "disabled allows recording", mode: recording.RecordModeAll},
		{name: "strict replay only", strict: true, mode: recording.RecordModeNone},
		{name: "reject record mode", strict: true, mode: recording.RecordModeNewEpisodes, wantErr: "record-mode=none"},
		{name: "reject upstream", strict: true, mode: recording.RecordModeNone, upstream: "https://example.test", wantErr: "--upstream"},
		{name: "reject api key", strict: true, mode: recording.RecordModeNone, apiKey: "secret", wantErr: "--api-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStrictReplay(tt.strict, tt.mode, tt.upstream, tt.apiKey)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
