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
		if s.ChunkDelayMs == 0 {
			s.ChunkDelayMs = defaultChunkDelayMs
		}
	}
}
