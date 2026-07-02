package engine

import "github.com/mockagents/mockagents/internal/toolschema"

// ToolValidator moved to the leaf package internal/toolschema (round-11) so
// non-engine surfaces (MCP tools/call argument validation, future strict-mode
// checks) can share the same JSON-schema validator without importing the
// engine. These aliases keep the engine API and behavior unchanged.
type ToolValidator = toolschema.Validator

// NewToolValidator creates a new ToolValidator.
func NewToolValidator() *ToolValidator {
	return toolschema.NewValidator()
}

// equalScalar reports whether two decoded JSON/YAML values are equal —
// numeric kinds compare by value (1 == 1.0), kinds are never conflated.
func equalScalar(a, b any) bool {
	return toolschema.EqualScalar(a, b)
}
