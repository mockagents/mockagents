package engine

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mockagents/mockagents/internal/types"
)

// PipelineExecutor runs multi-agent pipelines against a shared Engine.
type PipelineExecutor struct {
	Engine *Engine
}

// NewPipelineExecutor wires a pipeline executor to an engine.
func NewPipelineExecutor(eng *Engine) *PipelineExecutor {
	return &PipelineExecutor{Engine: eng}
}

// NodeResult captures one agent invocation inside a pipeline run.
type NodeResult struct {
	NodeID    string        `json:"node_id"`
	AgentName string        `json:"agent_name"`
	Response  *Response     `json:"response"`
	Latency   time.Duration `json:"latency"`
}

// PipelineResult is the aggregated output of a pipeline run.
type PipelineResult struct {
	PipelineName string        `json:"pipeline_name"`
	Topology     string        `json:"topology"`
	Nodes        []*NodeResult `json:"nodes"`
	Latency      time.Duration `json:"latency"`
}

// FinalResponse returns the last non-nil response produced by the run, or nil.
func (r *PipelineResult) FinalResponse() *Response {
	for i := len(r.Nodes) - 1; i >= 0; i-- {
		if r.Nodes[i].Response != nil {
			return r.Nodes[i].Response
		}
	}
	return nil
}

// ResponseByNodeID returns the response associated with a pipeline node id.
func (r *PipelineResult) ResponseByNodeID(id string) *Response {
	for _, n := range r.Nodes {
		if n.NodeID == id {
			return n.Response
		}
	}
	return nil
}

// ErrUnknownTopology is returned when a pipeline spec uses an unsupported topology.
var ErrUnknownTopology = errors.New("unknown pipeline topology")

// Run executes a pipeline definition with the given initial user message and
// session id. Each agent invocation creates its own session scoped by the
// pipeline node so state does not collide across nodes.
func (p *PipelineExecutor) Run(def *types.PipelineDefinition, userMsg, sessionID string) (*PipelineResult, error) {
	if def == nil {
		return nil, errors.New("nil pipeline definition")
	}
	if len(def.Spec.Agents) == 0 {
		return nil, errors.New("pipeline has no agents")
	}

	start := time.Now()
	res := &PipelineResult{
		PipelineName: def.Metadata.Name,
		Topology:     def.Spec.Topology,
	}

	var err error
	switch def.Spec.Topology {
	case types.TopologySequential, "":
		err = p.runSequential(def, userMsg, sessionID, res)
	case types.TopologyParallel:
		err = p.runParallel(def, userMsg, sessionID, res)
	case types.TopologyGraph:
		err = p.runGraph(def, userMsg, sessionID, res)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownTopology, def.Spec.Topology)
	}

	res.Latency = time.Since(start)
	return res, err
}

func (p *PipelineExecutor) runSequential(def *types.PipelineDefinition, userMsg, sessionID string, res *PipelineResult) error {
	input := userMsg
	for _, node := range def.Spec.Agents {
		nr, err := p.invokeNode(def.Metadata.Name, node, input, sessionID)
		res.Nodes = append(res.Nodes, nr)
		if err != nil {
			return err
		}
		// Feed the upstream agent's content as the downstream user message.
		input = nr.Response.Content
	}
	return nil
}

func (p *PipelineExecutor) runParallel(def *types.PipelineDefinition, userMsg, sessionID string, res *PipelineResult) error {
	results := make([]*NodeResult, len(def.Spec.Agents))
	errs := make([]error, len(def.Spec.Agents))

	var wg sync.WaitGroup
	for i, node := range def.Spec.Agents {
		wg.Add(1)
		go func(i int, node types.PipelineAgent) {
			defer wg.Done()
			nr, err := p.invokeNode(def.Metadata.Name, node, userMsg, sessionID)
			results[i] = nr
			errs[i] = err
		}(i, node)
	}
	wg.Wait()

	for i := range results {
		if results[i] != nil {
			res.Nodes = append(res.Nodes, results[i])
		}
	}
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (p *PipelineExecutor) runGraph(def *types.PipelineDefinition, userMsg, sessionID string, res *PipelineResult) error {
	nodesByID := make(map[string]types.PipelineAgent, len(def.Spec.Agents))
	for _, n := range def.Spec.Agents {
		nodesByID[n.ID] = n
	}
	outgoing := make(map[string][]types.PipelineEdge)
	incoming := make(map[string]int)
	for _, e := range def.Spec.Edges {
		outgoing[e.From] = append(outgoing[e.From], e)
		incoming[e.To]++
	}

	// Start from nodes with zero incoming edges; fall back to the first agent
	// when every node has an inbound edge (cyclic pipelines are unsupported).
	var start string
	for _, n := range def.Spec.Agents {
		if incoming[n.ID] == 0 {
			start = n.ID
			break
		}
	}
	if start == "" {
		start = def.Spec.Agents[0].ID
	}

	visited := make(map[string]bool)
	results := make(map[string]*NodeResult)

	var walk func(id, input string) error
	walk = func(id, input string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		node, ok := nodesByID[id]
		if !ok {
			return fmt.Errorf("pipeline %q references unknown node id %q", def.Metadata.Name, id)
		}
		nr, err := p.invokeNode(def.Metadata.Name, node, input, sessionID)
		results[id] = nr
		res.Nodes = append(res.Nodes, nr)
		if err != nil {
			return err
		}
		for _, edge := range outgoing[id] {
			if edge.When != "" && !strings.Contains(nr.Response.Content, edge.When) {
				continue
			}
			if err := walk(edge.To, nr.Response.Content); err != nil {
				return err
			}
		}
		return nil
	}

	return walk(start, userMsg)
}

func (p *PipelineExecutor) invokeNode(pipelineName string, node types.PipelineAgent, input, sessionID string) (*NodeResult, error) {
	if node.Ref == "" {
		return &NodeResult{NodeID: node.ID}, fmt.Errorf("pipeline %q node %q missing agent ref", pipelineName, node.ID)
	}
	// Scope the session per pipeline node so conversation state on one agent
	// does not leak into another when the same engine is reused.
	scopedSession := fmt.Sprintf("%s::%s::%s", sessionID, pipelineName, node.ID)
	req := &InboundRequest{
		AgentName: node.Ref,
		SessionID: scopedSession,
		Messages:  []RequestMessage{{Role: "user", Content: input}},
	}
	start := time.Now()
	resp, err := p.Engine.ProcessRequest(req)
	latency := time.Since(start)
	return &NodeResult{
		NodeID:    node.ID,
		AgentName: node.Ref,
		Response:  resp,
		Latency:   latency,
	}, err
}
