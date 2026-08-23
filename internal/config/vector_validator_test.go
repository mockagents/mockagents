package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/require"
)

func TestValidateVectorCollection(t *testing.T) {
	def := &types.VectorCollectionDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.VectorCollectionKind,
		Metadata: types.Metadata{Name: "support-docs"},
		Spec: types.VectorCollectionSpec{Dimension: 2, Metric: "cosine", Points: []types.VectorPoint{
			{ID: "refund", Vector: []float64{1, 0}},
			{ID: 2, Vector: []float64{0, 1}},
		}},
	}
	require.Nil(t, ValidateVectorCollection(def, "vectors.yaml", nil))
}

func TestValidateVectorCollectionRejectsDuplicateAndWrongDimension(t *testing.T) {
	def := &types.VectorCollectionDefinition{
		APIVersion: types.AgentAPIVersion, Kind: types.VectorCollectionKind,
		Metadata: types.Metadata{Name: "support-docs"},
		Spec: types.VectorCollectionSpec{Dimension: 2, Metric: "cosine", Points: []types.VectorPoint{
			{ID: "same", Vector: []float64{1}}, {ID: "same", Vector: []float64{0, 1}},
		}},
	}
	errs := ValidateVectorCollection(def, "vectors.yaml", nil)
	require.NotNil(t, errs)
	require.Len(t, errs.Errors, 2)
}

func TestValidateBytesVectorCollection(t *testing.T) {
	report := ValidateBytes([]byte("apiVersion: mockagents/v1\nkind: VectorCollection\nmetadata:\n  name: docs\nspec:\n  dimension: 1\n  metric: dot\n  points:\n    - id: one\n      vector: [1]\n"))
	require.Empty(t, report.Errors)
}

func TestValidateVectorCollectionRejectsInvalidPartialLimit(t *testing.T) {
	def := &types.VectorCollectionDefinition{APIVersion: types.AgentAPIVersion, Kind: types.VectorCollectionKind,
		Metadata: types.Metadata{Name: "docs"}, Spec: types.VectorCollectionSpec{Dimension: 1, Metric: "dot",
			Faults: types.VectorFaults{PartialResults: &types.VectorPartialResultsFault{MaxResults: 1001}}}}
	errs := ValidateVectorCollection(def, "vectors.yaml", nil)
	require.NotNil(t, errs)
	require.Contains(t, errs.Errors[0].Field, "partial_results.max_results")
}

func TestValidateVectorCollectionChaosRate(t *testing.T) {
	valid := ValidateBytes([]byte("apiVersion: mockagents/v1\nkind: VectorCollection\nmetadata:\n  name: docs\nspec:\n  dimension: 1\n  metric: dot\n  faults:\n    seed: 42\n    rate: 0.5\n    partial_results:\n      max_results: 1\n"))
	require.Empty(t, valid.Errors)

	invalid := ValidateBytes([]byte("apiVersion: mockagents/v1\nkind: VectorCollection\nmetadata:\n  name: docs\nspec:\n  dimension: 1\n  metric: dot\n  faults:\n    rate: -0.1\n"))
	require.Len(t, invalid.Errors, 1)
}
