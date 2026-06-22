package config

import (
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// decodeTestSuiteYAML parses a YAML string into a TestSuiteDefinition
// + its yaml.Node for line-number-aware validation.
func decodeTestSuiteYAML(t *testing.T, src string) (*types.TestSuiteDefinition, *yaml.Node) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(src), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var def types.TestSuiteDefinition
	if err := node.Decode(&def); err != nil {
		t.Fatalf("decode testsuite: %v", err)
	}
	return &def, &node
}

func TestValidateTestSuite_Valid(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: support-cases
spec:
  target:
    agent: support-agent
  cases:
    - name: greets-on-hello
      steps:
        - role: user
          content: hello
      assertions:
        - type: response_contains
          value: "hi"
        - type: latency_ms_lt
          max_ms: 500
`)
	if errs := ValidateTestSuite(def, "", node); errs != nil {
		t.Errorf("unexpected errors: %v", errs.Error())
	}
}

func TestValidateTestSuite_BothTargets(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
    pipeline: p
  cases:
    - name: c
      steps:
        - role: user
          content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "spec.target") {
		t.Errorf("expected target error: %v", errs)
	}
}

func TestValidateTestSuite_NoTarget(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target: {}
  cases:
    - name: c
      steps:
        - role: user
          content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsMessage(errs, "no target") {
		t.Errorf("expected no-target error: %v", errs)
	}
}

func TestValidateTestSuite_NoCases(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases: []
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "spec.cases") {
		t.Errorf("expected cases error: %v", errs)
	}
}

func TestValidateTestSuite_DuplicateCaseName(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - role: user
          content: hi
    - name: c
      steps:
        - role: user
          content: hi2
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsMessage(errs, "duplicate") {
		t.Errorf("expected duplicate-case error: %v", errs)
	}
}

func TestValidateTestSuite_CaseWithoutSteps(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsMessage(errs, "no steps") {
		t.Errorf("expected no-steps error: %v", errs)
	}
}

func TestValidateTestSuite_StepMissingRole(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "spec.cases[0].steps[0].role") {
		t.Errorf("expected step.role error: %v", errs)
	}
}

func TestValidateTestSuite_AssertionRuleDispatch(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - role: user
          content: hi
      assertions:
        - type: tool_call
        - type: response_contains
        - type: scenario_matched
        - type: latency_ms_lt
          max_ms: 0
        - type: unknown_type
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil {
		t.Fatal("expected assertion errors")
	}
	// Each of the 5 assertions should contribute an error.
	if !containsMessage(errs, "tool_call assertion missing tool name") {
		t.Errorf("no tool_call error: %v", errs)
	}
	if !containsMessage(errs, "response_contains assertion missing value") {
		t.Errorf("no response_contains error: %v", errs)
	}
	if !containsMessage(errs, "scenario_matched assertion missing scenario name") {
		t.Errorf("no scenario_matched error: %v", errs)
	}
	if !containsMessage(errs, "latency_ms_lt assertion needs a positive max_ms") {
		t.Errorf("no latency_ms_lt error: %v", errs)
	}
	if !containsMessage(errs, "unknown assertion type") {
		t.Errorf("no unknown-type error: %v", errs)
	}
}

// TestValidateTestSuite_AllAssertionTypesAccepted guards against the validator
// rejecting assertion types the runner supports — the NF-03 trajectory types
// (tool_call_count/tool_call_sequence/node_sequence) and the cheap behavioral
// types (no_tool_call/refusal/response_matches) must all pass with valid fields.
func TestValidateTestSuite_AllAssertionTypesAccepted(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    pipeline: p
  cases:
    - name: c
      steps:
        - role: user
          content: hi
      assertions:
        - type: tool_call_count
          count: 0
        - type: tool_call_sequence
          sequence: [a, b]
        - type: node_sequence
          sequence: [n1, n2]
        - type: no_tool_call
        - type: refusal
        - type: refusal
          value: policy
        - type: response_matches
          value: "ord-\\d+"
        - type: tool_call_args
          tool: search
          arguments:
            destination: NYC
            filters.class: economy
        - type: tool_error
        - type: tool_error
          tool: lookup
          value: UPSTREAM
        - type: handles_tool_error
        - type: handles_tool_error
          value: manually
`)
	if errs := ValidateTestSuite(def, "", node); errs != nil {
		t.Fatalf("expected all assertion types to validate, got: %v", errs)
	}
}

// TestValidateTestSuite_BadAssertionFields covers the new per-type field rules.
func TestValidateTestSuite_BadAssertionFields(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - role: user
          content: hi
      assertions:
        - type: tool_call_count
        - type: tool_call_sequence
        - type: response_matches
          value: "["
        - type: tool_call_args
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil {
		t.Fatal("expected field errors")
	}
	if !containsMessage(errs, "tool_call_count assertion missing count") {
		t.Errorf("no tool_call_count error: %v", errs)
	}
	if !containsMessage(errs, "tool_call_sequence assertion missing sequence") {
		t.Errorf("no tool_call_sequence error: %v", errs)
	}
	if !containsMessage(errs, "not a valid regular expression") {
		t.Errorf("no response_matches regex error: %v", errs)
	}
	if !containsMessage(errs, "tool_call_args assertion missing tool name") {
		t.Errorf("no tool_call_args tool error: %v", errs)
	}
	if !containsMessage(errs, "tool_call_args assertion missing arguments") {
		t.Errorf("no tool_call_args arguments error: %v", errs)
	}
}

func TestValidateTestSuite_InvalidKind(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: Agent
metadata:
  name: x
spec:
  target:
    agent: a
  cases:
    - name: c
      steps:
        - role: user
          content: hi
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil || !containsField(errs, "kind") {
		t.Errorf("expected kind error: %v", errs)
	}
}

func TestValidateTestSuite_NodeIDRejectedOnAggregates(t *testing.T) {
	def, node := decodeTestSuiteYAML(t, `apiVersion: mockagents/v1
kind: TestSuite
metadata:
  name: x
spec:
  target:
    pipeline: p
  cases:
    - name: c
      steps:
        - role: user
          content: hi
      assertions:
        - type: node_sequence
          sequence: [a]
          node_id: n
        - type: tool_error
          node_id: n
        - type: handles_tool_error
          node_id: n
        - type: latency_ms_lt
          max_ms: 100
          node_id: n
        - type: response_contains
          value: ok
          node_id: n
`)
	errs := ValidateTestSuite(def, "", node)
	if errs == nil {
		t.Fatal("expected node_id errors")
	}
	// The four aggregate types must be rejected.
	for _, typ := range []string{"node_sequence", "tool_error", "handles_tool_error", "latency_ms_lt"} {
		if !containsMessage(errs, "node_id is not supported on \""+typ+"\"") {
			t.Errorf("expected node_id rejection for %s: %v", typ, errs)
		}
	}
	// A per-node assertion (response_contains) must NOT be rejected for node_id.
	if containsMessage(errs, "node_id is not supported on \"response_contains\"") {
		t.Errorf("response_contains should accept node_id: %v", errs)
	}
}
