package types

import "encoding/json"

// ToolDefinition describes a tool an agent can call, with input schema and response mappings.
type ToolDefinition struct {
	Name        string             `yaml:"name" json:"name"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Parameters  JSONSchemaObject   `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Responses   []ToolResponseRule `yaml:"responses,omitempty" json:"responses,omitempty"`
	Validate    bool               `yaml:"validate,omitempty" json:"validate,omitempty"`
	ErrorRate   float64            `yaml:"error_rate,omitempty" json:"error_rate,omitempty"`
}

// JSONSchemaObject holds a JSON Schema parsed from YAML or JSON.
// It decodes from YAML maps and can marshal to json.RawMessage for validation.
type JSONSchemaObject map[string]any

// ToJSON converts the schema object to JSON bytes for JSON Schema validation.
func (s JSONSchemaObject) ToJSON() (json.RawMessage, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal(s)
}

// ToolResponseRule maps a match condition to a response or error for a tool call.
type ToolResponseRule struct {
	Match     map[string]any `yaml:"match,omitempty" json:"match,omitempty"`
	Response  any            `yaml:"response,omitempty" json:"response,omitempty"`
	Error     *ToolError     `yaml:"error,omitempty" json:"error,omitempty"`
	IsDefault bool           `yaml:"default,omitempty" json:"default,omitempty"`
}

// ToolError represents a simulated tool error.
type ToolError struct {
	Code    string `yaml:"code" json:"code"`
	Message string `yaml:"message" json:"message"`
}
