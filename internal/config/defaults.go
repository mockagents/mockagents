package config

import "github.com/mockagents/mockagents/internal/types"

const (
	defaultModel        = types.DefaultModel
	defaultChunkSize    = 4
	defaultChunkDelayMs = 50
)

// ApplyDefaults fills in zero-value fields with their documented defaults.
// Must be called before validation so validators see the effective config.
func ApplyDefaults(def *types.AgentDefinition) {
	if def.Spec.Model == "" {
		def.Spec.Model = defaultModel
	}

	if def.Spec.Behavior.Streaming != nil {
		s := def.Spec.Behavior.Streaming
		if s.ChunkSize == 0 {
			s.ChunkSize = defaultChunkSize
		}
		// Only fill an UNSET delay. An author who wrote `chunk_delay_ms: 0`
		// means "no artificial delay" and must get it — that is why the field
		// is a pointer. Treating zero as unset here is what made
		// examples/gemini-agent.yaml run at 50ms while claiming 0.
		if s.ChunkDelayMs == nil {
			d := defaultChunkDelayMs
			s.ChunkDelayMs = &d
		}
	}

	// Expand a named chaos preset into its concrete sub-sections so the engine
	// and validators see an ordinary chaos block (FB-03).
	expandChaosPreset(def.Spec.Behavior.Chaos)
}
