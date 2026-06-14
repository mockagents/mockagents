package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

func decodeMCPServerYAML(t *testing.T, src string) (*types.MCPServerDefinition, *yaml.Node) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(src), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var def types.MCPServerDefinition
	if err := node.Decode(&def); err != nil {
		t.Fatalf("decode mcpserver: %v", err)
	}
	return &def, &node
}

func TestValidateMCPServer_Valid(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: weather-mcp
spec:
  tools:
    - name: get_forecast
      responses:
        - match:
            city: "Austin"
          content:
            - type: text
              text: "sunny"
        - default: true
          content:
            - type: text
              text: "cloudy"
  resources:
    - uri: file:///docs/readme.md
      name: readme
  prompts:
    - name: greet
      messages:
        - role: assistant
          content:
            type: text
            text: "hi"
`)
	if errs := ValidateMCPServer(def, "", node); errs != nil {
		t.Errorf("unexpected errors: %v", errs.Error())
	}
}

func TestValidateMCPServer_EmptyServer(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: empty
spec: {}
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "exposes no tools") {
		t.Errorf("expected empty-server error: %v", errs)
	}
}

func TestValidateMCPServer_DuplicateToolName(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: dup
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: text
              text: ok
    - name: get
      responses:
        - default: true
          content:
            - type: text
              text: ok
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "duplicate tool name") {
		t.Errorf("expected dup tool error: %v", errs)
	}
}

func TestValidateMCPServer_ToolWithoutResponses(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsField(errs, "spec.tools[0].responses") {
		t.Errorf("expected no-responses error: %v", errs)
	}
}

func TestValidateMCPServer_ResponseWithNeitherMatchNorDefault(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - content:
            - type: text
              text: ok
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "neither") {
		t.Errorf("expected neither-match-nor-default error: %v", errs)
	}
}

func TestValidateMCPServer_ResponseWithBothMatchAndDefault(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - match: { city: X }
          default: true
          content:
            - type: text
              text: ok
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "both `match` and `default: true`") {
		t.Errorf("expected both error: %v", errs)
	}
}

func TestValidateMCPServer_MultipleDefaults(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: text
              text: one
        - default: true
          content:
            - type: text
              text: two
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "multiple default") {
		t.Errorf("expected multi-default error: %v", errs)
	}
}

func TestValidateMCPServer_ContentBlockMissingText(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: text
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "empty text") {
		t.Errorf("expected empty-text error: %v", errs)
	}
}

func TestValidateMCPServer_UnknownContentType(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: video
              data: xyz
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "unknown content type") {
		t.Errorf("expected unknown-type error: %v", errs)
	}
}

func TestValidateMCPServer_ResourceMissingURI(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  resources:
    - name: readme
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsField(errs, "spec.resources[0].uri") {
		t.Errorf("expected resource.uri error: %v", errs)
	}
}

func TestValidateMCPServer_DuplicateResourceURI(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  resources:
    - uri: file:///a
    - uri: file:///a
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "duplicate resource URI") {
		t.Errorf("expected dup resource error: %v", errs)
	}
}

func TestValidateMCPServer_DuplicatePromptName(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  prompts:
    - name: greet
    - name: greet
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "duplicate prompt name") {
		t.Errorf("expected dup prompt error: %v", errs)
	}
}

func TestValidateMCPServer_ImageBlockMissingFields(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: image
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil {
		t.Fatal("expected image-block errors")
	}
	if !containsMessage(errs, "empty data") {
		t.Errorf("no empty-data error: %v", errs)
	}
	if !containsMessage(errs, "no mimeType") {
		t.Errorf("no mimeType error: %v", errs)
	}
}

func TestValidateMCPServer_AudioBlockMissingFields(t *testing.T) {
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: audio
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil {
		t.Fatal("expected audio-block errors")
	}
	if !containsMessage(errs, "empty data") {
		t.Errorf("no empty-data error: %v", errs)
	}
	if !containsMessage(errs, "no mimeType") {
		t.Errorf("no mimeType error: %v", errs)
	}
}

func TestValidateMCPServer_EmbeddedResourceBlock(t *testing.T) {
	// Valid: uri + exactly one of text/blob.
	def, node := decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: resource
              uri: "test://r"
              text: "hello"
`)
	if errs := ValidateMCPServer(def, "", node); errs != nil {
		t.Errorf("unexpected errors for valid embedded resource: %v", errs.Error())
	}

	// Missing both text and blob.
	def, node = decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: resource
              uri: "test://r"
`)
	errs := ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "no inline text or blob") {
		t.Errorf("expected missing text/blob error: %v", errs)
	}

	// Both text and blob set (must be XOR).
	def, node = decodeMCPServerYAML(t, `apiVersion: mockagents/v1
kind: MCPServer
metadata:
  name: x
spec:
  tools:
    - name: get
      responses:
        - default: true
          content:
            - type: resource
              uri: "test://r"
              text: "hello"
              blob: "QUJD"
`)
	errs = ValidateMCPServer(def, "", node)
	if errs == nil || !containsMessage(errs, "both text and blob") {
		t.Errorf("expected text-XOR-blob error: %v", errs)
	}
}
