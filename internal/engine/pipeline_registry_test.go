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
