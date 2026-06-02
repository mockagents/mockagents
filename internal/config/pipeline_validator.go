package config

import (
	"fmt"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// validPipelineTopologies lists the three topology strings the engine's
// pipeline executor understands. Keep in sync with
// internal/types/pipeline.go and internal/engine/pipeline.go.
var validPipelineTopologies = []string{
	types.TopologySequential,
	types.TopologyParallel,
	types.TopologyGraph,
}

// ValidatePipeline runs rule-based validation against a
// PipelineDefinition. Mirrors the shape of Validator.Validate for
// agents so callers (ValidateBytes, the GUI editor, mockagents
// validate) can treat both document kinds uniformly. Returns nil on
// success so the caller can branch on the nil check rather than on
// HasErrors.
//
// The rules are intentionally minimal: everything the engine's
// pipeline executor would fail on at runtime (missing nodes, dangling
// edges, unknown topology) is caught here at load time with a
// file+line+column reference so operators can fix it in their editor.
// Graph-topology-only checks (cycles + unreachable nodes) run as a
// second pass so a well-formed pipeline can still be rejected for a
// structural graph error the executor would silently tolerate.
//
// A `filePath` argument of "" is OK; the line-number surfacing still
// works as long as the caller passes the yaml.Node from the decode
// step. Empty path simply renders as `:N:M` in text output.
func ValidatePipeline(def *types.PipelineDefinition, filePath string, node *yaml.Node) *ValidationErrorList {
	ctx := &validationContext{file: filePath, node: node}

	validatePipelineAPIVersion(ctx, def)
	validatePipelineKind(ctx, def)
	validatePipelineMetadata(ctx, def)
	validatePipelineSpec(ctx, def)
	validatePipelineGraph(ctx, def)

	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}

func validatePipelineAPIVersion(ctx *validationContext, def *types.PipelineDefinition) {
	if def.APIVersion == "" {
		ctx.addError("apiVersion", "required field missing",
			fmt.Sprintf("Add apiVersion: %s", types.AgentAPIVersion))
		return
	}
	if def.APIVersion != types.AgentAPIVersion {
		ctx.addError("apiVersion",
			fmt.Sprintf("unsupported version %q", def.APIVersion),
			fmt.Sprintf("Use apiVersion: %s", types.AgentAPIVersion))
	}
}

func validatePipelineKind(ctx *validationContext, def *types.PipelineDefinition) {
	if def.Kind == "" {
		ctx.addError("kind", "required field missing",
			fmt.Sprintf("Add kind: %s", types.PipelineKind))
		return
	}
	if def.Kind != types.PipelineKind {
		ctx.addError("kind",
			fmt.Sprintf("unsupported kind %q", def.Kind),
			fmt.Sprintf("Use kind: %s", types.PipelineKind))
	}
}

func validatePipelineMetadata(ctx *validationContext, def *types.PipelineDefinition) {
	if def.Metadata.Name == "" {
		ctx.addError("metadata.name", "required field missing",
			"Add a kebab-case name, e.g. metadata.name: research-pipeline")
		return
	}
	if len(def.Metadata.Name) > 63 {
		ctx.addError("metadata.name",
			fmt.Sprintf("name exceeds 63 characters (got %d)", len(def.Metadata.Name)),
			"Shorten the pipeline name to 63 characters or fewer.")
	}
	if !metadataNameRe.MatchString(def.Metadata.Name) {
		ctx.addError("metadata.name",
			fmt.Sprintf("invalid name %q: must be lowercase kebab-case", def.Metadata.Name),
			"Use lowercase letters, numbers, and hyphens only.")
	}
}

func validatePipelineSpec(ctx *validationContext, def *types.PipelineDefinition) {
	// Topology
	if def.Spec.Topology == "" {
		ctx.addError("spec.topology", "required field missing",
			fmt.Sprintf("Set spec.topology to one of: %s", strings.Join(validPipelineTopologies, ", ")))
	} else {
		var ok bool
		for _, t := range validPipelineTopologies {
			if def.Spec.Topology == t {
				ok = true
				break
			}
		}
		if !ok {
			ctx.addError("spec.topology",
				fmt.Sprintf("invalid topology %q", def.Spec.Topology),
				fmt.Sprintf("Must be one of: %s", strings.Join(validPipelineTopologies, ", ")))
		}
	}

	// Agents: non-empty and unique ids.
	if len(def.Spec.Agents) == 0 {
		ctx.addError("spec.agents", "pipeline declares no agents",
			"Add at least one node under spec.agents with an id and a ref.")
	}

	seen := make(map[string]int, len(def.Spec.Agents))
	for i, a := range def.Spec.Agents {
		field := fmt.Sprintf("spec.agents[%d]", i)
		if a.ID == "" {
			ctx.addError(field+".id", "required field missing",
				"Every pipeline node needs an id (unique within the pipeline).")
			continue
		}
		if a.Ref == "" {
			ctx.addError(field+".ref", "required field missing",
				"Every pipeline node needs a ref pointing at a loaded agent's metadata.name.")
		}
		if prev, dup := seen[a.ID]; dup {
			ctx.addError(field+".id",
				fmt.Sprintf("duplicate node id %q (first seen at spec.agents[%d])", a.ID, prev),
				"Each node id must be unique within a pipeline.")
			continue
		}
		seen[a.ID] = i
	}

	// Edges: only meaningful under graph topology; every reference
	// must point at a declared node; no self-loops; no duplicate
	// (from, to, when_contains) triples; no empty or whitespace-
	// only when_contains guards.
	if len(def.Spec.Edges) > 0 {
		if def.Spec.Topology != "" && def.Spec.Topology != types.TopologyGraph {
			ctx.addError("spec.edges",
				fmt.Sprintf("edges are only honored under topology %q (got %q)", types.TopologyGraph, def.Spec.Topology),
				"Either change spec.topology to graph or remove spec.edges.")
		}
		// Track (from, to, when_contains) triples so duplicates are
		// flagged exactly once at the second occurrence with a
		// back-reference to the first.
		type edgeKey struct {
			from, to, when string
		}
		seenEdges := make(map[edgeKey]int, len(def.Spec.Edges))
		for i, edge := range def.Spec.Edges {
			field := fmt.Sprintf("spec.edges[%d]", i)
			if edge.From == "" {
				ctx.addError(field+".from", "required field missing",
					"Every edge needs a `from` node id.")
			} else if _, ok := seen[edge.From]; !ok {
				ctx.addError(field+".from",
					fmt.Sprintf("references unknown node %q", edge.From),
					"Point `from` at a node id declared under spec.agents.")
			}
			if edge.To == "" {
				ctx.addError(field+".to", "required field missing",
					"Every edge needs a `to` node id.")
			} else if _, ok := seen[edge.To]; !ok {
				ctx.addError(field+".to",
					fmt.Sprintf("references unknown node %q", edge.To),
					"Point `to` at a node id declared under spec.agents.")
			}
			if edge.From != "" && edge.From == edge.To {
				ctx.addError(field,
					fmt.Sprintf("self-loop on node %q", edge.From),
					"Pipeline edges must connect two distinct nodes.")
			}
			// when_contains: the substring guard on graph edges.
			// An absent field is valid (unconditional edge).
			// A present-but-empty or whitespace-only field is
			// almost always a typo — it looks like a filter but
			// matches everything, equivalent to not setting it
			// at all.
			if edge.WhenContains != "" && strings.TrimSpace(edge.WhenContains) == "" {
				ctx.addError(field+".when_contains",
					"when_contains is whitespace-only",
					"Either remove when_contains entirely or set it to a meaningful substring.")
			}
			// Duplicate edge detection. Skip when endpoints are
			// invalid — the earlier errors already cover that
			// case and duplicate-tracking here would produce
			// noise.
			if edge.From == "" || edge.To == "" {
				continue
			}
			key := edgeKey{from: edge.From, to: edge.To, when: edge.WhenContains}
			if prev, dup := seenEdges[key]; dup {
				msg := fmt.Sprintf("duplicate edge %s → %s (first seen at spec.edges[%d])", edge.From, edge.To, prev)
				if edge.WhenContains != "" {
					msg = fmt.Sprintf("duplicate edge %s → %s guarded by %q (first seen at spec.edges[%d])", edge.From, edge.To, edge.WhenContains, prev)
				}
				ctx.addError(field, msg,
					"Remove the duplicate edge. Two identical guards are redundant; different guards should use distinct when_contains substrings.")
				continue
			}
			seenEdges[key] = i
		}
	}
}

// validatePipelineGraph runs the two graph-topology-only checks:
// cycle detection (DFS with 3-color marking) and
// reachable-from-source (BFS from every in-degree-zero node). Both
// catch structural problems the executor would silently tolerate —
// a cycle loops one extra time before the visited-set guards it, an
// unreachable node never fires at all. Surfacing them at load time
// is the whole point of the validator.
//
// The pass is a no-op unless topology == graph, and it short-
// circuits quickly if spec-level errors already left the agent list
// or edges malformed so we don't pile on noise.
func validatePipelineGraph(ctx *validationContext, def *types.PipelineDefinition) {
	if def.Spec.Topology != types.TopologyGraph {
		return
	}
	if len(def.Spec.Agents) == 0 {
		return
	}

	// Build forward adjacency + in-degree. We deliberately skip
	// edges that reference unknown nodes — the earlier spec pass
	// already flagged them, and including them here would pollute
	// both the cycle and reachability results with phantom arcs.
	forward := make(map[string][]string, len(def.Spec.Agents))
	inDegree := make(map[string]int, len(def.Spec.Agents))
	indexByID := make(map[string]int, len(def.Spec.Agents))
	for i, a := range def.Spec.Agents {
		if a.ID == "" {
			continue
		}
		forward[a.ID] = nil
		inDegree[a.ID] = 0
		indexByID[a.ID] = i
	}
	for _, e := range def.Spec.Edges {
		if _, ok := forward[e.From]; !ok {
			continue
		}
		if _, ok := forward[e.To]; !ok {
			continue
		}
		if e.From == e.To {
			continue
		}
		forward[e.From] = append(forward[e.From], e.To)
		inDegree[e.To]++
	}

	cycleFound := detectPipelineCycle(ctx, def, forward)

	// If the graph has a cycle, the set of "sources" becomes
	// unreliable (the cycle has no in-degree-zero member) and the
	// reachability report would just add noise on top of the cycle
	// error. Users should fix the cycle first, then rerun.
	if cycleFound {
		return
	}

	// Reachability: BFS from every in-degree-zero node.
	var sources []string
	for id, deg := range inDegree {
		if deg == 0 {
			sources = append(sources, id)
		}
	}
	// A cycle-free graph with edges always has at least one source
	// (topological sort proves this). A graph with no edges has
	// every node as a source, and every node is trivially
	// reachable — no work.
	if len(sources) == 0 {
		return
	}

	reachable := make(map[string]bool, len(def.Spec.Agents))
	queue := append([]string(nil), sources...)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if reachable[n] {
			continue
		}
		reachable[n] = true
		queue = append(queue, forward[n]...)
	}
	for _, a := range def.Spec.Agents {
		if a.ID == "" {
			continue
		}
		if reachable[a.ID] {
			continue
		}
		field := fmt.Sprintf("spec.agents[%d].id", indexByID[a.ID])
		ctx.addError(field,
			fmt.Sprintf("node %q is unreachable from any source", a.ID),
			"Add an inbound edge from another node, or remove the unreachable node.")
	}
}

// detectPipelineCycle returns true when the graph has a directed
// cycle. Uses a 3-color DFS: white = unvisited, gray = on the
// current recursion stack, black = finished. Any gray→gray edge is
// a back edge, which is a cycle. One error is added per detected
// cycle — we could report every member of the cycle, but a single
// well-located error is enough for the operator to diagnose and
// fix it without drowning the report.
func detectPipelineCycle(ctx *validationContext, def *types.PipelineDefinition, forward map[string][]string) bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(forward))
	var cycleFound bool
	var dfs func(n string)
	dfs = func(n string) {
		if cycleFound {
			return
		}
		color[n] = gray
		for _, m := range forward[n] {
			if cycleFound {
				return
			}
			switch color[m] {
			case gray:
				ctx.addError("spec.edges",
					fmt.Sprintf("graph contains a cycle involving node %q", m),
					"Pipeline execution would revisit a node — remove the back edge or split the cycle with a guard.")
				cycleFound = true
				return
			case white:
				dfs(m)
			}
		}
		color[n] = black
	}
	// Iterate in YAML declaration order so cycle reports are
	// deterministic for a given document — helps snapshot tests
	// and diff-based review workflows.
	for _, a := range def.Spec.Agents {
		if cycleFound {
			break
		}
		if a.ID == "" {
			continue
		}
		if color[a.ID] == white {
			dfs(a.ID)
		}
	}
	return cycleFound
}
