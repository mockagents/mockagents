package engine

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveStrictTools(t *testing.T) {
	cases := []struct {
		name string
		cfg  *types.StrictToolsConfig
		env  StrictMode
		want StrictToolModes
	}{
		{"nil cfg, no env", nil, StrictOff,
			StrictToolModes{StrictOff, StrictOff, StrictOff}},
		{"nil cfg, env warn", nil, StrictWarn,
			StrictToolModes{StrictWarn, StrictWarn, StrictWarn}},
		{"block without level implies strict", &types.StrictToolsConfig{}, StrictOff,
			StrictToolModes{StrictEnforce, StrictEnforce, StrictEnforce}},
		{"level warn fills dimensions", &types.StrictToolsConfig{Level: "warn"}, StrictOff,
			StrictToolModes{StrictWarn, StrictWarn, StrictWarn}},
		{"boolean false excludes a dimension",
			&types.StrictToolsConfig{Level: "strict", IDs: boolPtr(false)}, StrictOff,
			StrictToolModes{StrictOff, StrictEnforce, StrictEnforce}},
		{"agent level overrides env",
			&types.StrictToolsConfig{Level: "off"}, StrictEnforce,
			StrictToolModes{StrictOff, StrictOff, StrictOff}},
	}
	for _, tc := range cases {
		if got := resolveStrictTools(tc.cfg, tc.env); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestParseStrictLevel(t *testing.T) {
	for in, want := range map[string]StrictMode{
		"off": StrictOff, "": StrictOff, "banana": StrictOff,
		"warn": StrictWarn, "WARN": StrictWarn,
		"strict": StrictEnforce, "1": StrictEnforce, "true": StrictEnforce,
	} {
		if got := ParseStrictLevel(in); got != want {
			t.Errorf("ParseStrictLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
