package types

const (
	PipelineKind = "Pipeline"

	TopologySequential = "sequential"
	TopologyParallel   = "parallel"
	TopologyGraph      = "graph"
)

// PipelineDefinition is a multi-agent topology document.
type PipelineDefinition struct {
	APIVersion string       `yaml:"apiVersion" json:"apiVersion"`
	Kind       string       `yaml:"kind" json:"kind"`
	Metadata   Metadata     `yaml:"metadata" json:"metadata"`
	Spec       PipelineSpec `yaml:"spec" json:"spec"`
}

// PipelineSpec wires agents together into a topology.
type PipelineSpec struct {
	Topology string          `yaml:"topology" json:"topology"`
	Agents   []PipelineAgent `yaml:"agents" json:"agents"`
	Edges    []PipelineEdge  `yaml:"edges,omitempty" json:"edges,omitempty"`
}

// PipelineAgent is a node in the pipeline that references a loaded agent.
type PipelineAgent struct {
	ID  string `yaml:"id" json:"id"`
	Ref string `yaml:"ref" json:"ref"`
}

// PipelineEdge describes a directed connection in a graph topology.
// When empty, the edge is unconditional; when set, the edge fires only if
// the upstream node's output content contains the substring.
type PipelineEdge struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	When string `yaml:"when_contains,omitempty" json:"when_contains,omitempty"`
}
