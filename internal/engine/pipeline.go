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

// ErrPipelineCycle is returned by the graph executor when the pipeline's
// edges form a cycle. The config validator already rejects cyclic
// pipelines at load, but programmatic pipelines reach the executor
// directly, so this keeps the "cyclic pipelines unsupported" contract
// honest instead of silently executing a truncated traversal.
var ErrPipelineCycle = errors.New("pipeline graph has a cycle")

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

	// outgoing keeps the real edges (with their When conditions) for the
	// execution walk. indegree counts only edges between known nodes and
	// ignores self-loops, mirroring config.validatePipelineGraph so the
	// executor and the load-time validator agree on what is cyclic and on
	// which nodes are roots — otherwise a pipeline could pass `validate`
	// yet error at runtime (or vice versa).
	outgoing := make(map[string][]types.PipelineEdge)
	indegree := make(map[string]int, len(def.Spec.Agents))
	for _, n := range def.Spec.Agents {
		indegree[n.ID] = 0
	}
	for _, e := range def.Spec.Edges {
		outgoing[e.From] = append(outgoing[e.From], e)
		if e.From == e.To {
			continue
		}
		_, fromKnown := nodesByID[e.From]
		_, toKnown := nodesByID[e.To]
		// Count an inbound edge only when both endpoints are real nodes.
		// An edge from an unknown source is never peeled by Kahn's, so
		// counting it would leave the target permanently stuck and falsely
		// reported as cyclic; the walk surfaces the dangling ref instead.
		if fromKnown && toKnown {
			indegree[e.To]++
		}
	}

	// Reject cycles before executing anything (F-PL-003 / F-PL-005). The old
	// code fell back to Agents[0] when no zero-indegree node existed and let
	// the `visited` set silently truncate the traversal, so a cyclic pipeline
	// ran a partial, order-dependent subset of its nodes and reported success.
	if cyclic := cyclicNodes(def.Spec.Agents, outgoing, indegree); len(cyclic) > 0 {
		return fmt.Errorf("%w: nodes %v", ErrPipelineCycle, cyclic)
	}

	visited := make(map[string]bool)

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

	// Walk from every root (zero incoming edges), in definition order. The old
	// code started from a single root, so a multi-root or disconnected DAG had
	// every node outside the first root's reachable subgraph silently dropped
	// from res.Nodes (F-PL-004). In an acyclic graph every node is reachable
	// from some root, so the only nodes left unvisited are those deliberately
	// pruned by an unmet edge `When` condition.
	for _, n := range def.Spec.Agents {
		if indegree[n.ID] == 0 {
			if err := walk(n.ID, userMsg); err != nil {
				return err
			}
		}
	}
	return nil
}

// cyclicNodes runs Kahn's algorithm over the node set and returns the ids
// that cannot be peeled by repeatedly removing zero-indegree nodes — i.e.
// the nodes in, or reachable only through, a cycle. An empty result means
// the graph is acyclic. indegree is treated as read-only (a working copy
// is used) so the caller can reuse it for root selection. The returned
// list is in definition order for a stable error message.
func cyclicNodes(agents []types.PipelineAgent, outgoing map[string][]types.PipelineEdge, indegree map[string]int) []string {
	remaining := make(map[string]int, len(indegree))
	for id, d := range indegree {
		remaining[id] = d
	}
	queue := make([]string, 0, len(remaining))
	for id, d := range remaining {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		delete(remaining, id)
		for _, e := range outgoing[id] {
			if e.From == e.To {
				continue
			}
			if _, ok := remaining[e.To]; !ok {
				continue
			}
			remaining[e.To]--
			if remaining[e.To] == 0 {
				queue = append(queue, e.To)
			}
		}
	}
	if len(remaining) == 0 {
		return nil
	}
	var cyclic []string
	for _, n := range agents {
		if _, ok := remaining[n.ID]; ok {
			cyclic = append(cyclic, n.ID)
			delete(remaining, n.ID) // de-dup if agents repeats an id
		}
	}
	return cyclic
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
	// Defensive (F-PL-001/002): callers read nr.Response.Content directly,
	// so a nil Response with no error would nil-deref. ProcessRequest is
	// expected to always return a non-nil resp on success (ApplyTurn
	// guarantees it), but if that invariant ever breaks, surface it as a
	// node error here — the error paths return before any deref.
	if err == nil && resp == nil {
		err = fmt.Errorf("pipeline %q node %q: engine returned no response", pipelineName, node.ID)
	}
	return &NodeResult{
		NodeID:    node.ID,
		AgentName: node.Ref,
		Response:  resp,
		Latency:   latency,
	}, err
}
