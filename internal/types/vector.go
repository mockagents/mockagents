package types

const VectorCollectionKind = "VectorCollection"

// VectorCollectionDefinition declares deterministic startup data for
// VectorMock. The same collection is exposed through every vector provider
// profile; adapters own wire translation, not fixture state.
type VectorCollectionDefinition struct {
	APIVersion string               `yaml:"apiVersion" json:"apiVersion"`
	Kind       string               `yaml:"kind" json:"kind"`
	Metadata   Metadata             `yaml:"metadata" json:"metadata"`
	Spec       VectorCollectionSpec `yaml:"spec" json:"spec"`
}

type VectorCollectionSpec struct {
	Dimension int           `yaml:"dimension" json:"dimension"`
	Metric    string        `yaml:"metric" json:"metric"`
	Points    []VectorPoint `yaml:"points,omitempty" json:"points,omitempty"`
	Faults    VectorFaults  `yaml:"faults,omitempty" json:"faults,omitempty"`
}

type VectorFaults struct {
	Seed           int64                      `yaml:"seed,omitempty" json:"seed,omitempty"`
	Rate           *float64                   `yaml:"rate,omitempty" json:"rate,omitempty"`
	PartialResults *VectorPartialResultsFault `yaml:"partial_results,omitempty" json:"partial_results,omitempty"`
}

// VectorPartialResultsFault deterministically truncates otherwise-valid
// ranked searches. Zero is meaningful: it simulates an empty partial page.
type VectorPartialResultsFault struct {
	MaxResults int `yaml:"max_results" json:"max_results"`
}

type VectorPoint struct {
	ID       any            `yaml:"id" json:"id"`
	Vector   []float64      `yaml:"vector" json:"vector"`
	Metadata map[string]any `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}
