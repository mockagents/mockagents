package types

// A2AServerKind is the document kind for a mock A2A (Agent2Agent) server.
const A2AServerKind = "A2AServer"

// DefaultA2AProtocolVersion is reported in the Agent Card when the document
// leaves protocolVersion unset.
const DefaultA2AProtocolVersion = "0.3.0"

// A2AServerDefinition is a declarative mock A2A server (NF-04). A2A is Google's
// agent-to-agent protocol (now Linux-Foundation-governed): a JSON-RPC 2.0
// surface plus a public "Agent Card" served at /.well-known/agent-card.json. The
// mock serves the declared card and answers message/send with canned,
// match-based responses, mirroring how kind:MCPServer mocks the Model Context
// Protocol.
type A2AServerDefinition struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   Metadata      `yaml:"metadata" json:"metadata"`
	Spec       A2AServerSpec `yaml:"spec" json:"spec"`
}

// A2AServerSpec is the Agent Card plus the canned message responses.
type A2AServerSpec struct {
	Card      A2AAgentCard         `yaml:"card" json:"card"`
	Responses []A2AMessageResponse `yaml:"responses,omitempty" json:"responses,omitempty"`
}

// A2AAgentCard is the public Agent Card (the A2A discovery document). The server
// fills url/protocolVersion/capabilities defaults at serve time.
type A2AAgentCard struct {
	Name               string          `yaml:"name" json:"name"`
	Description        string          `yaml:"description,omitempty" json:"description,omitempty"`
	URL                string          `yaml:"url,omitempty" json:"url,omitempty"`
	Version            string          `yaml:"version,omitempty" json:"version,omitempty"`
	ProtocolVersion    string          `yaml:"protocolVersion,omitempty" json:"protocolVersion"`
	DefaultInputModes  []string        `yaml:"defaultInputModes,omitempty" json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string        `yaml:"defaultOutputModes,omitempty" json:"defaultOutputModes,omitempty"`
	Capabilities       A2ACapabilities `yaml:"capabilities,omitempty" json:"capabilities"`
	Skills             []A2ASkill      `yaml:"skills,omitempty" json:"skills,omitempty"`
}

// A2ACapabilities advertises optional protocol features in the Agent Card.
type A2ACapabilities struct {
	Streaming         bool `yaml:"streaming,omitempty" json:"streaming"`
	PushNotifications bool `yaml:"pushNotifications,omitempty" json:"pushNotifications"`
}

// A2ASkill is one capability descriptor in the Agent Card.
type A2ASkill struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Examples    []string `yaml:"examples,omitempty" json:"examples,omitempty"`
}

// A2AMessageResponse is one match-based canned reply for message/send. The first
// response whose Match is a substring of the incoming message text wins, with
// the Default entry as the fallback.
type A2AMessageResponse struct {
	Match   string `yaml:"match,omitempty" json:"match,omitempty"`
	Default bool   `yaml:"default,omitempty" json:"default,omitempty"`
	// Text is the agent's reply; it becomes the task's artifact and status
	// message.
	Text string `yaml:"text,omitempty" json:"text,omitempty"`
	// State is the terminal task state to report (default "completed"); set e.g.
	// "failed" or "input-required" to exercise non-happy paths.
	State string `yaml:"state,omitempty" json:"state,omitempty"`
}
