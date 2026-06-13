package types

const (
	MCPServerKind = "MCPServer"

	// DefaultMCPProtocolVersion is the version reported in `initialize`
	// responses when the agent definition does not override it. Bumped to the
	// current Streamable HTTP revision; the older revisions remain accepted in
	// the MCP-Protocol-Version request header (see mcp.SupportedProtocolVersions).
	DefaultMCPProtocolVersion = "2025-11-25"
)

// MCPServerDefinition is a declarative mock MCP server.
type MCPServerDefinition struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   Metadata      `yaml:"metadata" json:"metadata"`
	Spec       MCPServerSpec `yaml:"spec" json:"spec"`
}

// MCPServerSpec describes the capabilities, tools, resources, and prompts
// exposed by the mock MCP server.
type MCPServerSpec struct {
	ProtocolVersion string          `yaml:"protocolVersion,omitempty" json:"protocolVersion,omitempty"`
	Capabilities    MCPCapabilities `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Tools           []MCPTool       `yaml:"tools,omitempty" json:"tools,omitempty"`
	Resources       []MCPResource   `yaml:"resources,omitempty" json:"resources,omitempty"`
	Prompts         []MCPPrompt     `yaml:"prompts,omitempty" json:"prompts,omitempty"`
	Completions     []MCPCompletion `yaml:"completions,omitempty" json:"completions,omitempty"`
}

// MCPCapabilities controls which sections the server advertises during
// initialize. Unset sections still work but are omitted from capabilities.
type MCPCapabilities struct {
	Tools     bool `yaml:"tools,omitempty" json:"tools,omitempty"`
	Resources bool `yaml:"resources,omitempty" json:"resources,omitempty"`
	Prompts   bool `yaml:"prompts,omitempty" json:"prompts,omitempty"`
	Logging   bool `yaml:"logging,omitempty" json:"logging,omitempty"`
}

// MCPTool is a tool exposed by the server.
type MCPTool struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	InputSchema JSONSchemaObject  `yaml:"inputSchema,omitempty" json:"inputSchema,omitempty"`
	Responses   []MCPToolResponse `yaml:"responses,omitempty" json:"responses,omitempty"`
}

// MCPToolResponse is one match-based stub for a tool call. The first entry
// whose Match is a subset of the incoming arguments wins, with Default
// acting as the fallback.
type MCPToolResponse struct {
	Match   map[string]any    `yaml:"match,omitempty" json:"match,omitempty"`
	Default bool              `yaml:"default,omitempty" json:"default,omitempty"`
	Content []MCPContentBlock `yaml:"content,omitempty" json:"content,omitempty"`
	IsError bool              `yaml:"isError,omitempty" json:"isError,omitempty"`
}

// MCPContentBlock is one entry in a tool or prompt response.
// Supported types: "text", "image", "resource".
type MCPContentBlock struct {
	Type     string `yaml:"type" json:"type"`
	Text     string `yaml:"text,omitempty" json:"text,omitempty"`
	Data     string `yaml:"data,omitempty" json:"data,omitempty"`
	MimeType string `yaml:"mimeType,omitempty" json:"mimeType,omitempty"`
	URI      string `yaml:"uri,omitempty" json:"uri,omitempty"`
}

// MCPResource is a static resource served by the mock.
type MCPResource struct {
	URI         string `yaml:"uri" json:"uri"`
	Name        string `yaml:"name,omitempty" json:"name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	MimeType    string `yaml:"mimeType,omitempty" json:"mimeType,omitempty"`
	Text        string `yaml:"text,omitempty" json:"text,omitempty"`
	Blob        string `yaml:"blob,omitempty" json:"blob,omitempty"`
}

// MCPPrompt is a named prompt template served by the mock.
type MCPPrompt struct {
	Name        string             `yaml:"name" json:"name"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Arguments   []MCPPromptArg     `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	Messages    []MCPPromptMessage `yaml:"messages,omitempty" json:"messages,omitempty"`
}

// MCPPromptArg declares a prompt argument for discovery.
type MCPPromptArg struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

// MCPPromptMessage is one rendered message returned by prompts/get.
type MCPPromptMessage struct {
	Role    string          `yaml:"role" json:"role"`
	Content MCPContentBlock `yaml:"content" json:"content"`
}

// MCPCompletion describes the autocomplete suggestions the mock
// returns for `completion/complete` requests. Each entry binds a
// (refType, refName, argName) triple — matching the MCP spec's
// `ref` / `argument` shape — to a static list of values. When the
// incoming argument value is non-empty the mock filters the list
// with a case-insensitive prefix match, mirroring how a real server
// would scope suggestions to what the user has typed so far.
type MCPCompletion struct {
	// RefType is "ref/prompt" or "ref/resource". Empty matches both.
	RefType string `yaml:"refType,omitempty" json:"refType,omitempty"`
	// RefName is the prompt or resource template name. Empty matches any.
	RefName string `yaml:"refName,omitempty" json:"refName,omitempty"`
	// ArgName is the argument the suggestions belong to.
	ArgName string `yaml:"argName" json:"argName"`
	// Values is the static list of candidate completions.
	Values []string `yaml:"values" json:"values"`
}
