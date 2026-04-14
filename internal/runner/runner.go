// Package runner executes declarative TestSuite definitions against agents
// or multi-agent pipelines loaded into an Engine.
package runner

import (
	"errors"
	"fmt"
	"reflect"
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

func (r *Runner) runCase(suite *types.TestSuiteDefinition, tc *types.TestCase) *CaseResult {
	cr := &CaseResult{Name: tc.Name}
	if len(tc.Steps) == 0 {
		cr.Passed = false
		cr.Failures = append(cr.Failures, "case has no steps")
		return cr
	}

	userMsg := lastUserStep(tc.Steps)
	if userMsg == "" {
		cr.Failures = append(cr.Failures, "case has no user step")
		return cr
	}

	sessionID := fmt.Sprintf("test::%s::%s", suite.Metadata.Name, tc.Name)
	target := suite.Spec.Target

	start := time.Now()
	var finalResp *engine.Response
	var pipelineRes *engine.PipelineResult

	if target.Agent != "" {
		resp, err := r.Engine.ProcessRequest(&engine.InboundRequest{
			AgentName: target.Agent,
			SessionID: sessionID,
			Messages:  []engine.RequestMessage{{Role: "user", Content: userMsg}},
		})
		cr.Latency = time.Since(start)
		if err != nil {
			cr.ErrMessage = err.Error()
			cr.Failures = append(cr.Failures, "engine error: "+err.Error())
			return cr
		}
		finalResp = resp
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
		pr, err := r.Executor.Run(pdef, userMsg, sessionID)
		cr.Latency = time.Since(start)
		if err != nil {
			cr.ErrMessage = err.Error()
			cr.Failures = append(cr.Failures, "pipeline error: "+err.Error())
			return cr
		}
		pipelineRes = pr
		finalResp = pr.FinalResponse()
		if len(pr.Nodes) > 0 {
			cr.FinalNode = pr.Nodes[len(pr.Nodes)-1].NodeID
		}
	}

	for _, assertion := range tc.Assertions {
		if msg := evaluateAssertion(assertion, finalResp, pipelineRes, cr.Latency); msg != "" {
			cr.Failures = append(cr.Failures, msg)
		}
	}
	cr.Passed = len(cr.Failures) == 0
	return cr
}

func evaluateAssertion(a types.TestAssertion, resp *engine.Response, pr *engine.PipelineResult, latency time.Duration) string {
	// When a NodeID is set, retarget the response to that pipeline node.
	target := resp
	if a.NodeID != "" {
		if pr == nil {
			return fmt.Sprintf("assertion %q: node_id set but case is not a pipeline target", a.Type)
		}
		target = pr.ResponseByNodeID(a.NodeID)
		if target == nil {
			return fmt.Sprintf("assertion %q: node %q produced no response", a.Type, a.NodeID)
		}
	}

	switch a.Type {
	case types.AssertResponseContains:
		if target == nil {
			return "response_contains: no response produced"
		}
		if !strings.Contains(target.Content, a.Value) {
			return fmt.Sprintf("response_contains: %q not found in %q", a.Value, truncate(target.Content, 120))
		}
	case types.AssertScenarioMatched:
		if target == nil {
			return "scenario_matched: no response produced"
		}
		if target.ScenarioName != a.Value {
			return fmt.Sprintf("scenario_matched: expected %q, got %q", a.Value, target.ScenarioName)
		}
	case types.AssertToolCall:
		if target == nil {
			return "tool_call: no response produced"
		}
		if !hasToolCall(target.ToolCalls, a.Tool, a.Args) {
			return fmt.Sprintf("tool_call: expected call to %q with args %v, got %v",
				a.Tool, a.Args, toolCallSummary(target.ToolCalls))
		}
	case types.AssertLatencyMsLT:
		ms := latency.Milliseconds()
		if ms >= a.MaxMs {
			return fmt.Sprintf("latency_ms_lt: latency %dms >= max %dms", ms, a.MaxMs)
		}
	default:
		return fmt.Sprintf("unknown assertion type %q", a.Type)
	}
	return ""
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

func toolCallSummary(calls []types.ToolCallSpec) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, fmt.Sprintf("%s(%v)", c.Name, c.Arguments))
	}
	return out
}

func lastUserStep(steps []types.TestStep) string {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Role == "user" || steps[i].Role == "" {
			return steps[i].Content
		}
	}
	return ""
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
