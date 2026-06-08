package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadAndValidate(t *testing.T, yaml string) *ValidationErrorList {
	t.Helper()
	path := writeTestFile(t, "test.yaml", yaml)
	result, err := LoadFile(path)
	require.NoError(t, err)
	ApplyDefaults(result.Definition)
	v := &Validator{}
	return v.Validate(result.Definition, result.FilePath, result.Node)
}

func assertHasError(t *testing.T, errs *ValidationErrorList, field, msgSubstr string) {
	t.Helper()
	require.NotNil(t, errs, "expected validation errors but got none")
	for _, e := range errs.Errors {
		if e.Field == field && contains(e.Message, msgSubstr) {
			return
		}
	}
	t.Errorf("expected error on field %q containing %q; got: %s", field, msgSubstr, errs)
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestValidate_ValidMinimal(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assert.Nil(t, errs)
}

func TestValidate_ValidFull(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: full-agent
  description: A fully configured agent
  tags: [test, demo]
spec:
  protocol: anthropic-messages
  model: claude-3-opus
  systemPrompt: You are helpful.
  tools:
    - name: lookup_order
      description: Look up an order
      parameters:
        type: object
        properties:
          order_id: { type: string }
      responses:
        - match:
            order_id: "123"
          response:
            status: shipped
        - default: true
          response:
            status: processing
  behavior:
    scenarios:
      - name: order-query
        match:
          content_contains: "order"
        response:
          content: "Let me look that up."
          tool_calls:
            - name: lookup_order
              arguments:
                order_id: "123"
      - name: default
        response:
          content: "How can I help?"
    streaming:
      enabled: true
      chunk_size: 8
      chunk_delay_ms: 25
`)
	assert.Nil(t, errs)
}

// --- apiVersion tests ---

func TestValidate_MissingAPIVersion(t *testing.T) {
	errs := loadAndValidate(t, `
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "apiVersion", "required")
}

func TestValidate_WrongAPIVersion(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v2
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "apiVersion", "unsupported version")
}

// --- kind tests ---

func TestValidate_MissingKind(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "kind", "required")
}

func TestValidate_WrongKind(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Pipeline
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "kind", "unsupported kind")
}

// --- metadata tests ---

func TestValidate_MissingMetadataName(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  description: No name here
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "metadata.name", "required")
}

func TestValidate_InvalidMetadataName_Uppercase(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: MyAgent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "metadata.name", "kebab-case")
}

func TestValidate_InvalidMetadataName_TooLong(t *testing.T) {
	longName := "a"
	for i := 0; i < 63; i++ {
		longName += "a"
	}
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: `+longName+`
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "metadata.name", "exceeds 63")
}

// --- protocol tests ---

func TestValidate_MissingProtocol(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "spec.protocol", "required")
}

func TestValidate_InvalidProtocol(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: gemini-pro
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "spec.protocol", "invalid protocol")
}

func TestValidate_ValidProtocols(t *testing.T) {
	for _, proto := range []string{"openai-chat-completions", "anthropic-messages"} {
		t.Run(proto, func(t *testing.T) {
			errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: `+proto+`
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
			assert.Nil(t, errs)
		})
	}
}

// --- scenario tests ---

func TestValidate_NoScenarios(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios: []
`)
	assertHasError(t, errs, "spec.behavior.scenarios", "at least one")
}

func TestValidate_DuplicateScenarioNames(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
      - name: default
        response:
          content: "World"
`)
	assertHasError(t, errs, "spec.behavior.scenarios.1.name", "duplicate")
}

func TestValidate_ScenarioMissingName(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - response:
          content: "Hello"
`)
	assertHasError(t, errs, "spec.behavior.scenarios.0.name", "required")
}

func TestValidate_ScenarioMissingContent(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: default
        response:
          content: ""
`)
	assertHasError(t, errs, "spec.behavior.scenarios.0.response.content", "required")
}

// --- match rule tests ---

func TestValidate_MutuallyExclusiveMatchRules(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: conflict
        match:
          content_contains: "hello"
          content_regex: "hel.*"
        response:
          content: "Hi"
`)
	assertHasError(t, errs, "spec.behavior.scenarios.0.match", "mutually exclusive")
}

func TestValidate_InvalidRegex(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: bad-regex
        match:
          content_regex: "[invalid"
        response:
          content: "Hi"
`)
	assertHasError(t, errs, "spec.behavior.scenarios.0.match.content_regex", "invalid regex")
}

// --- tool tests ---

func TestValidate_DuplicateToolNames(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  tools:
    - name: search
    - name: search
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "spec.tools.1.name", "duplicate")
}

func TestValidate_InvalidToolName(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  tools:
    - name: Invalid-Tool
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "spec.tools.0.name", "snake_case")
}

func TestValidate_ToolMissingName(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  tools:
    - description: A nameless tool
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "spec.tools.0.name", "required")
}

func TestValidate_ToolInvalidJSONSchemaParameters(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  tools:
    - name: bad_params
      parameters:
        properties:
          id: { type: string }
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	assertHasError(t, errs, "spec.tools.0.parameters", "missing 'type'")
}

// --- cross-reference tests ---

func TestValidate_UndefinedToolCallReference(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  tools:
    - name: search
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
          tool_calls:
            - name: nonexistent_tool
`)
	assertHasError(t, errs, "spec.behavior.scenarios.0.response.tool_calls.0.name", "undefined tool")
}

func TestValidate_ValidToolCallReference(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: openai-chat-completions
  tools:
    - name: search
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
          tool_calls:
            - name: search
`)
	assert.Nil(t, errs)
}

// --- multi-error collection test ---

func TestValidate_CollectsMultipleErrors(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v2
kind: Pipeline
metadata:
  name: BAD NAME
spec:
  protocol: invalid
  behavior:
    scenarios: []
`)
	require.NotNil(t, errs)
	// Should have at least 4 errors: apiVersion, kind, metadata.name, protocol, scenarios
	assert.GreaterOrEqual(t, len(errs.Errors), 4, "expected multiple errors collected, got: %d", len(errs.Errors))
}

// --- line number tests ---

func TestValidate_ErrorsHaveLineNumbers(t *testing.T) {
	errs := loadAndValidate(t, `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: test-agent
spec:
  protocol: bad-protocol
  behavior:
    scenarios:
      - name: default
        response:
          content: "Hello"
`)
	require.NotNil(t, errs)
	protocolErr := findError(errs, "spec.protocol")
	require.NotNil(t, protocolErr, "expected protocol error")
	assert.Greater(t, protocolErr.Line, 0, "expected line number > 0")
}

func findError(errs *ValidationErrorList, field string) *ValidationError {
	for _, e := range errs.Errors {
		if e.Field == field {
			return e
		}
	}
	return nil
}

func TestValidate_InvalidHallucinationType(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: h
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: bad
        response:
          content: "wrong"
          hallucination:
            type: typoed
`)
	assertHasError(t, errs, "spec.behavior.scenarios.0.response.hallucination.type", "invalid hallucination type")
}

func TestValidate_ValidHallucinationType(t *testing.T) {
	errs := loadAndValidate(t, `
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: h
spec:
  protocol: openai-chat-completions
  behavior:
    scenarios:
      - name: bad
        response:
          content: "wrong"
          hallucination:
            type: ungrounded
`)
	assert.Nil(t, errs)
}
