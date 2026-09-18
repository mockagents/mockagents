package types

const (
	AgentAPIVersion = "mockagents/v1"
	AgentKind       = "Agent"
	DefaultModel    = "mock-agent"
)

// AgentDefinition is the top-level structure for a mock agent YAML/JSON file.
type AgentDefinition struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Spec       AgentSpec `yaml:"spec" json:"spec"`
}

// Metadata contains identifying information for an agent.
//
// TenantID optionally owns an agent within one tenant. Visibility and writes
// use the authenticated principal tenant from request context, never a
// client-selected tenant header. Empty ownership makes the agent global;
// tenant-owned agents shadow global agents with the same name or model.
// Other document registries define their own use of shared Metadata.
type Metadata struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	TenantID    string   `yaml:"tenant_id,omitempty" json:"tenant_id,omitempty"`
}

// AgentSpec defines the agent's protocol, model, tools, and behavior.
type AgentSpec struct {
	Protocol     string           `yaml:"protocol" json:"protocol"`
	Model        string           `yaml:"model,omitempty" json:"model,omitempty"`
	SystemPrompt string           `yaml:"systemPrompt,omitempty" json:"systemPrompt,omitempty"`
	Tools        []ToolDefinition `yaml:"tools,omitempty" json:"tools,omitempty"`
	Behavior     BehaviorConfig   `yaml:"behavior" json:"behavior"`
}
