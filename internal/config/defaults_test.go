package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestApplyDefaults_ModelDefaultsToMockAgent(t *testing.T) {
	def := &types.AgentDefinition{}
	ApplyDefaults(def)
	assert.Equal(t, "mock-agent", def.Spec.Model)
}

func TestApplyDefaults_PreservesExplicitModel(t *testing.T) {
	def := &types.AgentDefinition{
		Spec: types.AgentSpec{Model: "gpt-4o"},
	}
	ApplyDefaults(def)
	assert.Equal(t, "gpt-4o", def.Spec.Model)
}

func TestApplyDefaults_StreamingChunkDefaults(t *testing.T) {
	def := &types.AgentDefinition{
		Spec: types.AgentSpec{
			Behavior: types.BehaviorConfig{
				Streaming: &types.StreamingConfig{Enabled: true},
			},
		},
	}
	ApplyDefaults(def)
	assert.Equal(t, 4, def.Spec.Behavior.Streaming.ChunkSize)
	assert.Equal(t, 50, def.Spec.Behavior.Streaming.ChunkDelayMs)
}

func TestApplyDefaults_PreservesExplicitStreaming(t *testing.T) {
	def := &types.AgentDefinition{
		Spec: types.AgentSpec{
			Behavior: types.BehaviorConfig{
				Streaming: &types.StreamingConfig{
					Enabled:      true,
					ChunkSize:    10,
					ChunkDelayMs: 100,
				},
			},
		},
	}
	ApplyDefaults(def)
	assert.Equal(t, 10, def.Spec.Behavior.Streaming.ChunkSize)
	assert.Equal(t, 100, def.Spec.Behavior.Streaming.ChunkDelayMs)
}

func TestApplyDefaults_NilStreamingNoOp(t *testing.T) {
	def := &types.AgentDefinition{}
	ApplyDefaults(def)
	assert.Nil(t, def.Spec.Behavior.Streaming)
}
