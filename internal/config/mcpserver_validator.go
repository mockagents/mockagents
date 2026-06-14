package config

import (
	"fmt"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// validMCPContentTypes lists the content block types the mock MCP
// server understands. Keep in sync with internal/mcp/server.go.
var validMCPContentTypes = []string{"text", "image", "audio", "resource"}

// ValidateMCPServer runs rule-based validation against an
// MCPServerDefinition. Mirrors the shape of ValidatePipeline and
// ValidateTestSuite so every document kind flows through the same
// plumbing. Returns nil on success.
//
// Rules:
//   - apiVersion required and equal to mockagents/v1
//   - kind required and equal to MCPServer
//   - metadata.name required, kebab-case, ≤63 chars
//   - at least one of tools/resources/prompts declared (an MCP
//     server that exposes nothing is almost always a typo)
//   - unique tool names, unique resource URIs, unique prompt names
//   - every tool has a non-empty name
//   - every tool response has either a match map OR default: true
//   - every content block has a known type + the right field for it
//   - every resource has a URI
//   - every prompt has a non-empty name; prompt arguments have
//     non-empty names
func ValidateMCPServer(def *types.MCPServerDefinition, filePath string, node *yaml.Node) *ValidationErrorList {
	ctx := &validationContext{file: filePath, node: node}

	if def.APIVersion == "" {
		ctx.addError("apiVersion", "required field missing",
			fmt.Sprintf("Add apiVersion: %s", types.AgentAPIVersion))
	} else if def.APIVersion != types.AgentAPIVersion {
		ctx.addError("apiVersion",
			fmt.Sprintf("unsupported version %q", def.APIVersion),
			fmt.Sprintf("Use apiVersion: %s", types.AgentAPIVersion))
	}
	if def.Kind == "" {
		ctx.addError("kind", "required field missing",
			fmt.Sprintf("Add kind: %s", types.MCPServerKind))
	} else if def.Kind != types.MCPServerKind {
		ctx.addError("kind",
			fmt.Sprintf("unsupported kind %q", def.Kind),
			fmt.Sprintf("Use kind: %s", types.MCPServerKind))
	}
	validateMetadataName(ctx, def.Metadata.Name, "metadata.name", "mcp-server")

	// An MCP server that exposes none of tools/resources/prompts
	// is legal per the protocol (initialize still works) but is
	// almost always a typo in the mock — warn at load time so the
	// operator notices before the client side hits an empty
	// response.
	if len(def.Spec.Tools) == 0 && len(def.Spec.Resources) == 0 && len(def.Spec.Prompts) == 0 {
		ctx.addError("spec",
			"MCP server exposes no tools, resources, or prompts",
			"Add at least one entry under spec.tools, spec.resources, or spec.prompts.")
	}

	validateMCPTools(ctx, def)
	validateMCPResources(ctx, def)
	validateMCPPrompts(ctx, def)

	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}

func validateMCPTools(ctx *validationContext, def *types.MCPServerDefinition) {
	seenNames := make(map[string]int, len(def.Spec.Tools))
	for i, tool := range def.Spec.Tools {
		field := fmt.Sprintf("spec.tools[%d]", i)
		if tool.Name == "" {
			ctx.addError(field+".name", "required field missing",
				"Every tool needs a unique name.")
		} else if prev, dup := seenNames[tool.Name]; dup {
			ctx.addError(field+".name",
				fmt.Sprintf("duplicate tool name %q (first seen at spec.tools[%d])", tool.Name, prev),
				"Each tool name must be unique within an MCP server.")
		} else {
			seenNames[tool.Name] = i
		}
		// A tool without any responses is legal — the mock will
		// return an empty content block. Operators who hit that
		// almost always wanted to define a response.
		if len(tool.Responses) == 0 {
			ctx.addError(field+".responses",
				"tool has no responses",
				"Add at least one entry under tool.responses with either `match:` or `default: true`.")
			continue
		}
		var sawDefault bool
		for j, resp := range tool.Responses {
			rField := fmt.Sprintf("%s.responses[%d]", field, j)
			// Each response must either match specific args OR be
			// the default fallback. Neither → the response can
			// never fire; both → operator intent is ambiguous.
			hasMatch := len(resp.Match) > 0
			if !hasMatch && !resp.Default {
				ctx.addError(rField,
					"tool response has neither `match` nor `default: true`",
					"Either add a `match:` map of args to values or set `default: true`.")
			}
			if hasMatch && resp.Default {
				ctx.addError(rField,
					"tool response sets both `match` and `default: true`",
					"Pick one: `match:` (selective) or `default: true` (fallback).")
			}
			if resp.Default && sawDefault {
				ctx.addError(rField,
					"multiple default tool responses",
					"Only one response per tool may set `default: true`.")
			}
			if resp.Default {
				sawDefault = true
			}
			for k, block := range resp.Content {
				validateMCPContentBlock(ctx, fmt.Sprintf("%s.content[%d]", rField, k), &block)
			}
		}
	}
}

func validateMCPResources(ctx *validationContext, def *types.MCPServerDefinition) {
	seenURIs := make(map[string]int, len(def.Spec.Resources))
	for i, res := range def.Spec.Resources {
		field := fmt.Sprintf("spec.resources[%d]", i)
		if res.URI == "" {
			ctx.addError(field+".uri", "required field missing",
				"Every resource needs a URI (e.g. file:///docs/readme.md).")
			continue
		}
		if prev, dup := seenURIs[res.URI]; dup {
			ctx.addError(field+".uri",
				fmt.Sprintf("duplicate resource URI %q (first seen at spec.resources[%d])", res.URI, prev),
				"Each resource URI must be unique within an MCP server.")
			continue
		}
		seenURIs[res.URI] = i
	}
}

func validateMCPPrompts(ctx *validationContext, def *types.MCPServerDefinition) {
	seenNames := make(map[string]int, len(def.Spec.Prompts))
	for i, p := range def.Spec.Prompts {
		field := fmt.Sprintf("spec.prompts[%d]", i)
		if p.Name == "" {
			ctx.addError(field+".name", "required field missing",
				"Every prompt needs a unique name.")
			continue
		}
		if prev, dup := seenNames[p.Name]; dup {
			ctx.addError(field+".name",
				fmt.Sprintf("duplicate prompt name %q (first seen at spec.prompts[%d])", p.Name, prev),
				"Each prompt name must be unique within an MCP server.")
			continue
		}
		seenNames[p.Name] = i
		for j, arg := range p.Arguments {
			if arg.Name == "" {
				ctx.addError(fmt.Sprintf("%s.arguments[%d].name", field, j),
					"required field missing",
					"Every prompt argument needs a name.")
			}
		}
		for j, msg := range p.Messages {
			if msg.Role == "" {
				ctx.addError(fmt.Sprintf("%s.messages[%d].role", field, j),
					"required field missing",
					"Set role to user, assistant, or system.")
			}
			validateMCPContentBlock(ctx, fmt.Sprintf("%s.messages[%d].content", field, j), &msg.Content)
		}
	}
}

// validateMCPContentBlock checks that a content block has a
// recognized type and the field that type requires. Shared between
// tool responses and prompt messages.
func validateMCPContentBlock(ctx *validationContext, field string, block *types.MCPContentBlock) {
	if block.Type == "" {
		ctx.addError(field+".type", "required field missing",
			"Set type to one of: text, image, audio, resource.")
		return
	}
	var known bool
	for _, t := range validMCPContentTypes {
		if block.Type == t {
			known = true
			break
		}
	}
	if !known {
		ctx.addError(field+".type",
			fmt.Sprintf("unknown content type %q", block.Type),
			"Supported types: text, image, audio, resource.")
		return
	}
	switch block.Type {
	case "text":
		if block.Text == "" {
			ctx.addError(field+".text", "text content block has empty text",
				"Set block.text to the text payload.")
		}
	case "image":
		if block.Data == "" {
			ctx.addError(field+".data", "image content block has empty data",
				"Set block.data to the base64-encoded image bytes.")
		}
		if block.MimeType == "" {
			ctx.addError(field+".mimeType", "image content block has no mimeType",
				"Set block.mimeType to e.g. image/png.")
		}
	case "audio":
		if block.Data == "" {
			ctx.addError(field+".data", "audio content block has empty data",
				"Set block.data to the base64-encoded audio bytes.")
		}
		if block.MimeType == "" {
			ctx.addError(field+".mimeType", "audio content block has no mimeType",
				"Set block.mimeType to e.g. audio/wav.")
		}
	case "resource":
		// An embedded resource (`{type:resource, resource:{...}}`) needs
		// a URI plus its inline payload. Per the MCP spec the inner
		// contents are TextResourceContents XOR BlobResourceContents,
		// so exactly one of text/blob must be set.
		if block.URI == "" {
			ctx.addError(field+".uri", "resource content block has no URI",
				"Set block.uri to the referenced resource's URI.")
		}
		switch {
		case block.Text == "" && block.Blob == "":
			ctx.addError(field+".text", "resource content block has no inline text or blob",
				"Set block.text (or block.blob) to the embedded resource's contents.")
		case block.Text != "" && block.Blob != "":
			ctx.addError(field+".text", "resource content block sets both text and blob",
				"Set exactly one of block.text or block.blob (text XOR blob).")
		}
	}
}
