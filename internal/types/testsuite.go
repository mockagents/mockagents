package types

const (
	TestSuiteKind = "TestSuite"

	AssertToolCall         = "tool_call"
	AssertResponseContains = "response_contains"
	AssertScenarioMatched  = "scenario_matched"
	AssertLatencyMsLT      = "latency_ms_lt"
	// Agent-trajectory assertions (NF-03): deeper tool-call / planning checks
	// that catch the most common 2026 agent bugs (wrong tool, wrong count, wrong
	// order). tool_call already does subset/partial argument matching.
	AssertToolCallCount    = "tool_call_count"    // exact number of tool calls in the response
	AssertToolCallSequence = "tool_call_sequence" // ordered tool-call names in the response
	AssertNodeSequence     = "node_sequence"      // ordered pipeline node ids that ran (pipeline targets)
	// Cheap behavioral assertions: catch a wrongly-eager tool call, a missing
	// refusal, or a response that should match a pattern rather than a fixed
	// substring.
	AssertNoToolCall      = "no_tool_call"     // the response made no tool calls
	AssertRefusal         = "refusal"          // the response is a refusal (optionally containing `value`)
	AssertResponseMatches = "response_matches" // the response content matches the `value` regular expression
)

// TestSuiteDefinition is a declarative collection of test cases that run
// against an agent or pipeline defined elsewhere in the project.
type TestSuiteDefinition struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   Metadata      `yaml:"metadata" json:"metadata"`
	Spec       TestSuiteSpec `yaml:"spec" json:"spec"`
}

// TestSuiteSpec lists the target and the cases to run.
type TestSuiteSpec struct {
	Target TestTarget `yaml:"target" json:"target"`
	Cases  []TestCase `yaml:"cases" json:"cases"`
}

// TestTarget identifies which agent or pipeline the suite exercises.
// Exactly one of Agent or Pipeline must be set.
type TestTarget struct {
	Agent    string `yaml:"agent,omitempty" json:"agent,omitempty"`
	Pipeline string `yaml:"pipeline,omitempty" json:"pipeline,omitempty"`
}

// TestCase is a single scenario with steps and assertions.
type TestCase struct {
	Name       string          `yaml:"name" json:"name"`
	Steps      []TestStep      `yaml:"steps" json:"steps"`
	Assertions []TestAssertion `yaml:"assertions,omitempty" json:"assertions,omitempty"`
}

// TestStep is a single message exchange in a test case.
type TestStep struct {
	Role    string `yaml:"role" json:"role"`
	Content string `yaml:"content" json:"content"`
}

// TestAssertion is a declarative check against the test case result.
// The Type field selects which other fields are meaningful.
type TestAssertion struct {
	Type  string         `yaml:"type" json:"type"`
	Tool  string         `yaml:"tool,omitempty" json:"tool,omitempty"`
	Args  map[string]any `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	Value string         `yaml:"value,omitempty" json:"value,omitempty"`
	MaxMs int64          `yaml:"max_ms,omitempty" json:"max_ms,omitempty"`
	// Count is the expected number of tool calls for tool_call_count (a pointer
	// so an omitted count is distinguishable from an explicit 0 = "no tool calls").
	Count *int `yaml:"count,omitempty" json:"count,omitempty"`
	// Sequence is the expected ordered list for tool_call_sequence (tool-call
	// names in the response) or node_sequence (pipeline node ids that ran).
	Sequence []string `yaml:"sequence,omitempty" json:"sequence,omitempty"`
	NodeID   string   `yaml:"node_id,omitempty" json:"node_id,omitempty"`
}
