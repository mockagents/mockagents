"use client";

// PipelineEditor is the interactive drag-to-rewire canvas for a Pipeline
// document (REF-07, M2). It renders the pipeline's agents as React Flow nodes
// and its edges as connections, and lets the operator drag nodes, add/remove
// agents, and — in graph topology — draw/delete edges and set when_contains
// guards. The editor serializes the canvas back to a PipelineDefinition and
// shows it live in a preview panel.
//
// Persistence is deliberately NOT wired here: the Save round-trip (PUT with
// If-Match, validation + conflict handling) lands in M3. For now the preview
// is the export surface — copy the definition the editor produced.

import "@xyflow/react/dist/style.css";

import { useCallback, useMemo, useState } from "react";
import {
  addEdge,
  Background,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";

import { Icon } from "@/lib/icons";
import type { PipelineDefinition, PipelineEdge } from "@/lib/api";

type Topology = "sequential" | "parallel" | "graph";

type AgentData = Record<string, unknown> & { id: string; ref: string };
type AgentNode = Node<AgentData, "agent">;

interface PipelineEditorProps {
  pipeline: PipelineDefinition;
  agentNames: string[];
}

const NODE_X = 230;
const NODE_Y = 120;

// AgentFlowNode renders one pipeline agent: its node id and the agent it
// refers to, with a target handle on the left and a source handle on the right
// so edges flow left-to-right.
function AgentFlowNode({ data }: NodeProps<AgentNode>) {
  return (
    <div className="pe-node">
      <Handle type="target" position={Position.Left} />
      <div className="pe-node-id">{data.id}</div>
      <div className="pe-node-ref">{data.ref}</div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}

const nodeTypes = { agent: AgentFlowNode };

// deriveEdges returns the edges implied by a topology. graph uses the declared
// edges; sequential chains the agents in declaration order; parallel has none.
function deriveEdges(
  topology: Topology,
  agentIds: string[],
  declared: PipelineEdge[],
): PipelineEdge[] {
  if (topology === "parallel") return [];
  if (topology === "sequential") {
    const out: PipelineEdge[] = [];
    for (let i = 0; i + 1 < agentIds.length; i++) {
      out.push({ from: agentIds[i], to: agentIds[i + 1] });
    }
    return out;
  }
  return declared;
}

// layoutPositions seeds node coordinates with a longest-path-from-source
// layering so an opened pipeline looks like the read-only viewer before the
// user drags anything. Positions are cosmetic — they are never persisted.
function layoutPositions(
  topology: Topology,
  agentIds: string[],
  edges: PipelineEdge[],
): Record<string, { x: number; y: number }> {
  const pos: Record<string, { x: number; y: number }> = {};
  if (topology === "parallel") {
    agentIds.forEach((id, i) => {
      pos[id] = { x: 40, y: 40 + i * NODE_Y };
    });
    return pos;
  }

  const forward = new Map<string, string[]>();
  const inDegree = new Map<string, number>();
  for (const id of agentIds) {
    forward.set(id, []);
    inDegree.set(id, 0);
  }
  for (const e of edges) {
    if (forward.has(e.from) && forward.has(e.to)) {
      forward.get(e.from)!.push(e.to);
      inDegree.set(e.to, (inDegree.get(e.to) ?? 0) + 1);
    }
  }
  const layer = new Map<string, number>();
  const queue: string[] = [];
  for (const id of agentIds) {
    if ((inDegree.get(id) ?? 0) === 0) {
      layer.set(id, 0);
      queue.push(id);
    }
  }
  const remaining = new Map(inDegree);
  while (queue.length > 0) {
    const n = queue.shift()!;
    const here = layer.get(n) ?? 0;
    for (const m of forward.get(n) ?? []) {
      if (here + 1 > (layer.get(m) ?? 0)) layer.set(m, here + 1);
      const left = (remaining.get(m) ?? 0) - 1;
      remaining.set(m, left);
      if (left === 0) queue.push(m);
    }
  }
  const rowOf = new Map<number, number>();
  for (const id of agentIds) {
    const l = layer.get(id) ?? 0;
    const r = rowOf.get(l) ?? 0;
    pos[id] = { x: 40 + l * NODE_X, y: 40 + r * NODE_Y };
    rowOf.set(l, r + 1);
  }
  return pos;
}

function toFlowEdge(e: PipelineEdge): Edge {
  return {
    id: `${e.from}->${e.to}`,
    source: e.from,
    target: e.to,
    label: e.when_contains,
    markerEnd: { type: MarkerType.ArrowClosed },
  };
}

export function PipelineEditor({ pipeline, agentNames }: PipelineEditorProps) {
  const initialTopology = (["sequential", "parallel", "graph"].includes(pipeline.spec.topology)
    ? pipeline.spec.topology
    : "graph") as Topology;

  const [topology, setTopology] = useState<Topology>(initialTopology);

  const initialAgentIds = (pipeline.spec.agents ?? []).map((a) => a.id);
  const initialEdges = useMemo(
    () => deriveEdges(initialTopology, initialAgentIds, pipeline.spec.edges ?? []),
    // Seed once from the incoming pipeline; subsequent edits live in state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
  const initialPositions = useMemo(
    () => layoutPositions(initialTopology, initialAgentIds, initialEdges),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState<AgentNode>(
    (pipeline.spec.agents ?? []).map((a) => ({
      id: a.id,
      type: "agent",
      position: initialPositions[a.id] ?? { x: 40, y: 40 },
      data: { id: a.id, ref: a.ref },
    })),
  );
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(initialEdges.map(toFlowEdge));

  const editable = topology === "graph";

  // Connecting two nodes adds a graph edge. No-op outside graph topology.
  const onConnect = useCallback(
    (conn: Connection) => {
      if (!editable) return;
      setEdges((eds) =>
        addEdge({ ...conn, markerEnd: { type: MarkerType.ArrowClosed } }, eds),
      );
    },
    [editable, setEdges],
  );

  // Switching topology recomputes edges: graph keeps the current set frozen as
  // explicit editable edges; sequential/parallel derive from node order.
  const changeTopology = useCallback(
    (next: Topology) => {
      const ids = nodes.map((n) => n.id);
      const declared: PipelineEdge[] = edges.map((e) => ({
        from: e.source,
        to: e.target,
        when_contains: typeof e.label === "string" && e.label ? e.label : undefined,
      }));
      const derived = deriveEdges(next, ids, declared);
      setEdges(derived.map(toFlowEdge));
      setTopology(next);
    },
    [nodes, edges, setEdges],
  );

  // --- Node add / remove -------------------------------------------------
  const [newId, setNewId] = useState("");
  const [newRef, setNewRef] = useState(agentNames[0] ?? "");

  const addNode = useCallback(() => {
    const id = newId.trim();
    const ref = newRef.trim();
    if (!id || !ref) return;
    if (nodes.some((n) => n.id === id)) return; // ids are unique
    setNodes((nds) =>
      nds.concat({
        id,
        type: "agent",
        position: { x: 60 + nds.length * 24, y: 60 + nds.length * 24 },
        data: { id, ref },
      }),
    );
    if (topology === "sequential") {
      // Keep the implicit chain in sync with the new tail node.
      setEdges((eds) => {
        const ids = nodes.map((n) => n.id).concat(id);
        return deriveEdges("sequential", ids, []).map(toFlowEdge);
      });
    }
    setNewId("");
  }, [newId, newRef, nodes, topology, setNodes, setEdges]);

  const removeSelected = useCallback(() => {
    const keep = nodes.filter((n) => !n.selected);
    if (keep.length === nodes.length) return;
    const keepIds = new Set(keep.map((n) => n.id));
    setNodes(keep);
    setEdges((eds) => {
      const survivors = eds.filter(
        (e) => keepIds.has(e.source) && keepIds.has(e.target) && !e.selected,
      );
      if (topology === "sequential") {
        return deriveEdges("sequential", keep.map((n) => n.id), []).map(toFlowEdge);
      }
      return survivors;
    });
  }, [nodes, topology, setNodes, setEdges]);

  // --- when_contains editing (graph only) --------------------------------
  const setEdgeGuard = useCallback(
    (edgeId: string, value: string) => {
      setEdges((eds) =>
        eds.map((e) => (e.id === edgeId ? { ...e, label: value || undefined } : e)),
      );
    },
    [setEdges],
  );

  // --- Serialize ---------------------------------------------------------
  const definition: PipelineDefinition = useMemo(() => {
    const agents = nodes.map((n) => ({ id: n.id, ref: n.data.ref }));
    const def: PipelineDefinition = {
      apiVersion: pipeline.apiVersion || "mockagents/v1",
      kind: "Pipeline",
      metadata: pipeline.metadata,
      spec: { topology, agents },
    };
    if (topology === "graph") {
      const specEdges: PipelineEdge[] = edges.map((e) => {
        const edge: PipelineEdge = { from: e.source, to: e.target };
        if (typeof e.label === "string" && e.label) edge.when_contains = e.label;
        return edge;
      });
      if (specEdges.length > 0) def.spec.edges = specEdges;
    }
    return def;
  }, [nodes, edges, topology, pipeline.apiVersion, pipeline.metadata]);

  const definitionJSON = useMemo(() => JSON.stringify(definition, null, 2), [definition]);

  const [copied, setCopied] = useState(false);
  const copyDefinition = useCallback(() => {
    void navigator.clipboard?.writeText(definitionJSON).then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    });
  }, [definitionJSON]);

  return (
    <div className="pe-grid">
      <div className="pe-canvas-col">
        <div className="pe-toolbar card card-pad">
          <label className="field" style={{ margin: 0 }}>
            <span className="pe-label">Topology</span>
            <select
              className="input"
              value={topology}
              onChange={(e) => changeTopology(e.target.value as Topology)}
            >
              <option value="sequential">sequential</option>
              <option value="parallel">parallel</option>
              <option value="graph">graph</option>
            </select>
          </label>

          <div className="pe-add">
            <input
              className="input"
              placeholder="node id"
              value={newId}
              onChange={(e) => setNewId(e.target.value)}
            />
            <select className="input" value={newRef} onChange={(e) => setNewRef(e.target.value)}>
              {agentNames.length === 0 && <option value="">(no agents loaded)</option>}
              {agentNames.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
            <button type="button" className="btn btn-outline btn-sm" onClick={addNode}>
              Add node
            </button>
            <button type="button" className="btn btn-ghost btn-sm" onClick={removeSelected}>
              <Icon name="x-circle" size={15} /> Remove selected
            </button>
          </div>
        </div>

        <div className="pe-canvas">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            nodeTypes={nodeTypes}
            edgesFocusable={editable}
            elementsSelectable
            fitView
            proOptions={{ hideAttribution: true }}
          >
            <Background />
            <Controls />
            <MiniMap pannable zoomable />
          </ReactFlow>
        </div>

        {!editable && (
          <p className="pe-hint">
            Edges are derived from {topology === "sequential" ? "agent order" : "the parallel topology"}.
            Switch topology to <code>graph</code> to draw and label connections.
          </p>
        )}

        {editable && edges.length > 0 && (
          <div className="card card-pad">
            <div className="pe-label" style={{ marginBottom: 8 }}>
              Edge guards (<code>when_contains</code>)
            </div>
            <table className="data-table">
              <thead>
                <tr>
                  <th>From → To</th>
                  <th>when_contains</th>
                </tr>
              </thead>
              <tbody>
                {edges.map((e) => (
                  <tr key={e.id}>
                    <td className="mono">
                      {e.source} → {e.target}
                    </td>
                    <td>
                      <input
                        className="input"
                        placeholder="(always)"
                        value={typeof e.label === "string" ? e.label : ""}
                        onChange={(ev) => setEdgeGuard(e.id, ev.target.value)}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="pe-preview-col">
        <div className="card card-pad pe-preview">
          <div className="row gap-2" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <div className="pe-label">Definition</div>
            <button type="button" className="btn btn-ghost btn-xs" onClick={copyDefinition}>
              <Icon name={copied ? "check-circle" : "file-code"} size={14} />
              {copied ? " Copied" : " Copy"}
            </button>
          </div>
          <pre className="pe-json">{definitionJSON}</pre>
          <p className="pe-hint" style={{ marginTop: 8 }}>
            One-click Save (write back to disk) arrives in the next slice. For now, copy this
            definition.
          </p>
        </div>
      </div>
    </div>
  );
}
