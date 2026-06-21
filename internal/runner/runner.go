// Package runner executes declarative TestSuite definitions against agents
// or multi-agent pipelines loaded into an Engine.
package runner

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/types"
)

// PipelineRegistry resolves pipeline definitions by name.
type PipelineRegistry interface {
	GetPipeline(name string) *types.PipelineDefinition
}

// Runner executes test suites.
type Runner struct {
	Engine    *engine.Engine
	Pipelines PipelineRegistry
	Executor  *engine.PipelineExecutor
}

// New creates a Runner. The pipelines registry may be nil if the caller
// only runs agent-targeted suites.
func New(eng *engine.Engine, pipelines PipelineRegistry) *Runner {
	return &Runner{
		Engine:    eng,
		Pipelines: pipelines,
		Executor:  engine.NewPipelineExecutor(eng),
	}
}

// CaseResult holds the outcome of one test case.
type CaseResult struct {
	Name       string        `json:"name"`
	Passed     bool          `json:"passed"`
	Failures   []string      `json:"failures,omitempty"`
	Latency    time.Duration `json:"latency"`
	FinalNode  string        `json:"final_node,omitempty"`
	ErrMessage string        `json:"error,omitempty"`
}

// SuiteResult aggregates the results of every case in a suite.
type SuiteResult struct {
	SuiteName string        `json:"suite_name"`
	Target    string        `json:"target"`
	Cases     []*CaseResult `json:"cases"`
	Passed    int           `json:"passed"`
	Failed    int           `json:"failed"`
	Latency   time.Duration `json:"latency"`
}

// RunSuite executes every case in the suite and returns an aggregated result.
func (r *Runner) RunSuite(suite *types.TestSuiteDefinition) (*SuiteResult, error) {
	if suite == nil {
		return nil, errors.New("nil suite")
	}
	target := suite.Spec.Target
	if target.Agent == "" && target.Pipeline == "" {
		return nil, fmt.Errorf("suite %q: target.agent or target.pipeline is required", suite.Metadata.Name)
	}
	if target.Agent != "" && target.Pipeline != "" {
		return nil, fmt.Errorf("suite %q: only one of target.agent or target.pipeline may be set", suite.Metadata.Name)
	}

	result := &SuiteResult{
		SuiteName: suite.Metadata.Name,
		Target:    targetLabel(target),
	}
	start := time.Now()
	for i := range suite.Spec.Cases {
		cr := r.runCase(suite, &suite.Spec.Cases[i])
		result.Cases = append(result.Cases, cr)
		if cr.Passed {
			result.Passed++
		} else {
			result.Failed++
		}
	}
	result.Latency = time.Since(start)
	return result, nil
}

// evalContext is the accumulated outcome of running a case's steps that the
// assertions are evaluated against. For a single-step case it is identical to
// the old single-response view; for a multi-turn case it carries the whole
// trajectory: `final` is the last turn's response (outcome assertions like
// response_contains / refusal look here), while `toolCalls` and `nodeSeq` are
// the ordered aggregate across every turn (trajectory assertions like
// tool_call_sequence / node_sequence look here). `pr` is the final turn's
// pipeline result, used when an assertion retargets a specific node by id.
type evalContext struct {
	final     *engine.Response
	toolCalls []types.ToolCallSpec
	pr        *engine.PipelineResult
	nodeSeq   []string
	latency   time.Duration
}

func (r *Runner) runCase(suite *types.TestSuiteDefinition, tc *types.TestCase) *CaseResult {
	cr := &CaseResult{Name: tc.Name}
	if len(tc.Steps) == 0 {
		cr.Passed = false
		cr.Failures = append(cr.Failures, "case has no steps")
		return cr
	}

	userMsgs := userSteps(tc.Steps)
	if len(userMsgs) == 0 {
		cr.Failures = append(cr.Failures, "case has no user step")
		return cr
	}

	sessionID := fmt.Sprintf("test::%s::%s", suite.Metadata.Name, tc.Name)
	target := suite.Spec.Target

	ec := &evalContext{}
	start := time.Now()

	if target.Agent != "" {
		// Replay every user step as a turn in one session, so the engine
		// accumulates conversation history and per-session turn count — a real
		// multi-turn trajectory rather than only the final message.
		for i, msg := range userMsgs {
			resp, err := r.Engine.ProcessRequest(&engine.InboundRequest{
				AgentName: target.Agent,
				SessionID: sessionID,
				Messages:  []engine.RequestMessage{{Role: "user", Content: msg}},
			})
			if err != nil {
				cr.Latency = time.Since(start)
				cr.ErrMessage = err.Error()
				cr.Failures = append(cr.Failures, fmt.Sprintf("engine error on turn %d: %s", i+1, err.Error()))
				return cr
			}
			ec.final = resp
			ec.toolCalls = append(ec.toolCalls, resp.ToolCalls...)
		}
	} else {
		if r.Pipelines == nil {
			cr.Failures = append(cr.Failures, "no pipeline registry wired into runner")
			return cr
		}
		pdef := r.Pipelines.GetPipeline(target.Pipeline)
		if pdef == nil {
			cr.Failures = append(cr.Failures, fmt.Sprintf("pipeline %q not found", target.Pipeline))
			return cr
		}
		for i, msg := range userMsgs {
			pr, err := r.Executor.Run(pdef, msg, sessionID)
			if err != nil {
				cr.Latency = time.Since(start)
				cr.ErrMessage = err.Error()
				cr.Failures = append(cr.Failures, fmt.Sprintf("pipeline error on turn %d: %s", i+1, err.Error()))
				return cr
			}
			ec.pr = pr
			ec.final = pr.FinalResponse()
			for _, n := range pr.Nodes {
				ec.nodeSeq = append(ec.nodeSeq, n.NodeID)
				if n.Response != nil {
					ec.toolCalls = append(ec.toolCalls, n.Response.ToolCalls...)
				}
			}
			if len(pr.Nodes) > 0 {
				cr.FinalNode = pr.Nodes[len(pr.Nodes)-1].NodeID
			}
		}
	}
	cr.Latency = time.Since(start)
	ec.latency = cr.Latency

	for _, assertion := range tc.Assertions {
		if msg := evaluateAssertion(assertion, ec); msg != "" {
			cr.Failures = append(cr.Failures, msg)
		}
	}
	cr.Passed = len(cr.Failures) == 0
	return cr
}

func evaluateAssertion(a types.TestAssertion, ec *evalContext) string {
	// Resolve which response + tool calls this assertion looks at. By default an
	// outcome assertion reads the final turn's response and a trajectory
	// assertion reads the whole-run aggregate. When node_id is set the assertion
	// is scoped to a single pipeline node (in the final turn), so both views
	// collapse to that node's response — preserving the original node_id
	// semantics.
	outcome := ec.final
	toolCalls := ec.toolCalls
	if a.NodeID != "" {
		if ec.pr == nil {
			return fmt.Sprintf("assertion %q: node_id set but case is not a pipeline target", a.Type)
		}
		nodeResp := ec.pr.ResponseByNodeID(a.NodeID)
		if nodeResp == nil {
			return fmt.Sprintf("assertion %q: node %q produced no response", a.Type, a.NodeID)
		}
		outcome = nodeResp
		toolCalls = nodeResp.ToolCalls
	}

	switch a.Type {
	case types.AssertResponseContains:
		if outcome == nil {
			return "response_contains: no response produced"
		}
		if !strings.Contains(outcome.Content, a.Value) {
			return fmt.Sprintf("response_contains: %q not found in %q", a.Value, truncate(outcome.Content, 120))
		}
	case types.AssertScenarioMatched:
		if outcome == nil {
			return "scenario_matched: no response produced"
		}
		if outcome.ScenarioName != a.Value {
			return fmt.Sprintf("scenario_matched: expected %q, got %q", a.Value, outcome.ScenarioName)
		}
	case types.AssertResponseMatches:
		if outcome == nil {
			return "response_matches: no response produced"
		}
		re, err := regexp.Compile(a.Value)
		if err != nil {
			return fmt.Sprintf("response_matches: invalid regular expression %q: %v", a.Value, err)
		}
		if !re.MatchString(outcome.Content) {
			return fmt.Sprintf("response_matches: %q did not match %q", a.Value, truncate(outcome.Content, 120))
		}
	case types.AssertToolCall:
		if !hasToolCall(toolCalls, a.Tool, a.Args) {
			return fmt.Sprintf("tool_call: expected call to %q with args %v, got %v",
				a.Tool, a.Args, toolCallSummary(toolCalls))
		}
	case types.AssertToolCallArgs:
		if a.Tool == "" {
			return "tool_call_args: tool is required"
		}
		if len(a.Args) == 0 {
			return "tool_call_args: arguments is required"
		}
		if !matchToolArgs(toolCalls, a.Tool, a.Args) {
			return fmt.Sprintf("tool_call_args: no call to %q matched args %v, got %v",
				a.Tool, a.Args, toolCallSummary(toolCalls))
		}
	case types.AssertNoToolCall:
		if len(toolCalls) != 0 {
			return fmt.Sprintf("no_tool_call: expected no tool calls, got %v", toolCallNames(toolCalls))
		}
	case types.AssertRefusal:
		if outcome == nil {
			return "refusal: no response produced"
		}
		if outcome.Refusal == "" {
			return "refusal: expected a refusal, but the response did not refuse"
		}
		if a.Value != "" && !strings.Contains(outcome.Refusal, a.Value) {
			return fmt.Sprintf("refusal: %q not found in refusal %q", a.Value, truncate(outcome.Refusal, 120))
		}
	case types.AssertLatencyMsLT:
		ms := ec.latency.Milliseconds()
		if ms >= a.MaxMs {
			return fmt.Sprintf("latency_ms_lt: latency %dms >= max %dms", ms, a.MaxMs)
		}
	case types.AssertToolCallCount:
		if a.Count == nil {
			return "tool_call_count: count is required"
		}
		if got := len(toolCalls); got != *a.Count {
			return fmt.Sprintf("tool_call_count: expected %d tool call(s), got %d %v",
				*a.Count, got, toolCallNames(toolCalls))
		}
	case types.AssertToolCallSequence:
		got := toolCallNames(toolCalls)
		if !equalStrings(got, a.Sequence) {
			return fmt.Sprintf("tool_call_sequence: expected %v, got %v", a.Sequence, got)
		}
	case types.AssertNodeSequence:
		// A pipeline-trajectory check: the ordered node ids that ran across the
		// whole (possibly multi-turn) run. It reads the aggregate, so it ignores
		// node_id retargeting.
		if ec.pr == nil {
			return "node_sequence: assertion requires a pipeline target"
		}
		if !equalStrings(ec.nodeSeq, a.Sequence) {
			return fmt.Sprintf("node_sequence: expected %v, got %v", a.Sequence, ec.nodeSeq)
		}
	default:
		return fmt.Sprintf("unknown assertion type %q", a.Type)
	}
	return ""
}

// toolCallNames returns the tool-call names in invocation order.
func toolCallNames(calls []types.ToolCallSpec) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Name
	}
	return out
}

// equalStrings reports whether two string slices are equal element-wise.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasToolCall(calls []types.ToolCallSpec, name string, wantArgs map[string]any) bool {
	for _, tc := range calls {
		if tc.Name != name {
			continue
		}
		if len(wantArgs) == 0 {
			return true
		}
		match := true
		for k, v := range wantArgs {
			got, ok := tc.Arguments[k]
			if !ok || !reflect.DeepEqual(got, v) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// matchToolArgs reports whether some call to `name` has arguments matching every
// (path, want) pair. Keys may be dotted paths into nested objects, and values
// compare type-tolerantly (see looseEqual).
func matchToolArgs(calls []types.ToolCallSpec, name string, want map[string]any) bool {
	for _, tc := range calls {
		if tc.Name != name {
			continue
		}
		all := true
		for path, wantVal := range want {
			got, ok := resolvePath(tc.Arguments, path)
			if !ok || !looseEqual(got, wantVal) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// resolvePath walks a dotted path into nested map[string]any arguments. A path
// without dots is a plain top-level key. Returns false if any segment is missing
// or a non-leaf segment is not an object.
func resolvePath(args map[string]any, path string) (any, bool) {
	cur := any(args)
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// looseEqual compares two argument values tolerantly: numbers compare by float
// value (so a YAML int 2 matches a JSON float 2, the shape args take over the
// wire), everything else falls back to reflect.DeepEqual.
func looseEqual(got, want any) bool {
	if gf, ok := toFloat(got); ok {
		if wf, ok := toFloat(want); ok {
			return gf == wf
		}
	}
	return reflect.DeepEqual(got, want)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	default:
		return 0, false
	}
}

func toolCallSummary(calls []types.ToolCallSpec) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, fmt.Sprintf("%s(%v)", c.Name, c.Arguments))
	}
	return out
}

// userSteps returns the content of every user step in order. A step with an
// empty role is treated as a user step (the historical default). Non-user steps
// (assistant/system) are not replayed: the mock generates the assistant side
// itself, so a case drives the conversation through its user turns.
func userSteps(steps []types.TestStep) []string {
	var out []string
	for _, s := range steps {
		if s.Role == "user" || s.Role == "" {
			out = append(out, s.Content)
		}
	}
	return out
}

func targetLabel(t types.TestTarget) string {
	if t.Agent != "" {
		return "agent:" + t.Agent
	}
	return "pipeline:" + t.Pipeline
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
