package engine

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestPipelineRegistry_RegisterNilOrEmptyNameIgnored(t *testing.T) {
	// F-PR-002: a nil def would panic on the Name deref under the lock,
	// and an empty name would key under "" (shadowing, never lookupable).
	// Both must be ignored without panicking.
	r := NewPipelineRegistry()

	assert.NotPanics(t, func() { r.Register(nil) })
	assert.Equal(t, 0, r.Count())

	assert.NotPanics(t, func() { r.Register(&types.PipelineDefinition{}) }) // empty Metadata.Name
	assert.Equal(t, 0, r.Count())

	// A properly named pipeline still registers.
	r.Register(&types.PipelineDefinition{Metadata: types.Metadata{Name: "p1"}})
	assert.Equal(t, 1, r.Count())
	assert.NotNil(t, r.GetPipeline("p1"))
}

func TestPipelineRegistry_SourceTracking(t *testing.T) {
	r := NewPipelineRegistry()
	def := &types.PipelineDefinition{Metadata: types.Metadata{Name: "p1"}}

	// Plain Register records no source.
	r.Register(def)
	assert.Equal(t, "", r.Source("p1"))

	// RegisterWithSource records the path and replaces the def.
	r.RegisterWithSource(def, "/agents/p1.yaml")
	assert.Equal(t, "/agents/p1.yaml", r.Source("p1"))
	assert.Equal(t, 1, r.Count())

	// A later plain Register leaves the recorded source intact.
	r.Register(def)
	assert.Equal(t, "/agents/p1.yaml", r.Source("p1"))

	// An empty sourcePath clears the recorded source.
	r.RegisterWithSource(def, "")
	assert.Equal(t, "", r.Source("p1"))

	// Nil / empty-name defs are ignored without panicking.
	assert.NotPanics(t, func() { r.RegisterWithSource(nil, "/x.yaml") })
	assert.Equal(t, "", r.Source("unknown"))
}
