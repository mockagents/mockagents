package engine

import (
	"os"
	"strings"
	"sync"

	"github.com/mockagents/mockagents/internal/types"
)

// StrictMode is the effective enforcement mode of one strict-tools dimension.
type StrictMode int

const (
	// StrictOff — lenient, today's default behavior.
	StrictOff StrictMode = iota
	// StrictWarn — run the check, log a warning and surface it via the
	// X-Mockagents-Strict-Violation response header; the request succeeds.
	StrictWarn
	// StrictEnforce — fail the request with the provider's real 400 shape.
	StrictEnforce
)

// StrictToolModes is the resolved per-dimension mode set for one request.
type StrictToolModes struct {
	IDs        StrictMode // round-trip tool id validation (R9-15)
	ToolChoice StrictMode // required/named forcing + parallel cap (R9-16a)
	Schemas    StrictMode // strict:true schema-subset validation (R9-16b)
}

// Any reports whether any dimension is active at all — hot-path early-out.
func (m StrictToolModes) Any() bool {
	return m.IDs != StrictOff || m.ToolChoice != StrictOff || m.Schemas != StrictOff
}

// ParseStrictLevel maps a level string to a StrictMode. "1"/"true" are
// accepted as "strict" for MOCKAGENTS_REALTIME_STRICT-style boolean muscle
// memory; anything unrecognized (including "") is off.
func ParseStrictLevel(s string) StrictMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "warn":
		return StrictWarn
	case "strict", "1", "true":
		return StrictEnforce
	}
	return StrictOff
}

// envStrictLevel caches the MOCKAGENTS_STRICT_TOOLS fleet default; read once
// per process like the other env knobs.
var envStrictLevel = sync.OnceValue(func() StrictMode {
	return ParseStrictLevel(os.Getenv("MOCKAGENTS_STRICT_TOOLS"))
})

// StrictToolsFor resolves the effective strict-tools modes for an agent:
// the agent's spec.behavior.strict_tools block when present, else the
// MOCKAGENTS_STRICT_TOOLS env default, else off (YAML > env > off).
func StrictToolsFor(agent *types.AgentDefinition) StrictToolModes {
	var cfg *types.StrictToolsConfig
	if agent != nil {
		cfg = agent.Spec.Behavior.StrictTools
	}
	return resolveStrictTools(cfg, envStrictLevel())
}

// resolveStrictTools is the pure worker (unit-testable without env
// manipulation). A block present without a level implies "strict" — writing
// the block turns it on; the level fills every dimension the author left
// unset, and a boolean set to false excludes that dimension.
func resolveStrictTools(cfg *types.StrictToolsConfig, envLevel StrictMode) StrictToolModes {
	level := envLevel
	if cfg != nil {
		if cfg.Level != "" {
			level = ParseStrictLevel(cfg.Level)
		} else {
			level = StrictEnforce
		}
	}
	dim := func(enabled *bool) StrictMode {
		if enabled != nil && !*enabled {
			return StrictOff
		}
		return level
	}
	if cfg == nil {
		return StrictToolModes{IDs: level, ToolChoice: level, Schemas: level}
	}
	return StrictToolModes{
		IDs:        dim(cfg.IDs),
		ToolChoice: dim(cfg.ToolChoice),
		Schemas:    dim(cfg.Schemas),
	}
}
