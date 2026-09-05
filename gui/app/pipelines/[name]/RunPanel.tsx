"use client";

// UX-05: explicit pipeline execution against the ACTIVE runtime.
//
// The constraints this panel exists to respect (epic §8.1):
//
//   - This is not a preview. It runs the same engine the provider adapters
//     use, against the definitions currently loaded, and it advances per-node
//     session state. The UI says so before the button is pressed, not after.
//   - A fresh session id per run stops turns being reused by accident. It does
//     NOT pin definitions or isolate fixtures, and must not be described as if
//     it does.
//   - A lost response is an UNKNOWN outcome, never a failure. The run is
//     stateful, so nothing is retried automatically.
//   - Duplicate submission is disabled while a run is in flight.
//   - Latencies arrive as Go nanoseconds and must be converted.
//   - For a parallel topology the node array is DEFINITION order. It is not a
//     completion chronology, and presenting it as a timeline would invent
//     causality the server never reported.

import { useState } from "react";

import type { PipelineAgent, PipelineNodeResult, PipelineRunOutcome } from "@/lib/api";
import { formatDuration } from "@/lib/duration";
import { Icon } from "@/lib/icons";

export interface RunPanelProps {
  pipelineName: string;
  topology: string;
  /** The nodes the DEFINITION declares, in definition order. A node missing
   * from a run's results did not execute, and the difference between the two
   * lists is the only way to see that — the server reports what ran, never what
   * did not. Absent is unknown, not zero (epic §10). */
  nodes: PipelineAgent[];
  /** False when the caller may not run pipelines; the control is disabled with
   * a reason rather than failing on click. */
  canRun: boolean;
  runAction: (input: string, sessionId: string) => Promise<PipelineRunOutcome>;
  /** Injected in tests; defaults to a real random id. */
  newSessionId?: () => string;
}

function defaultSessionId(): string {
  const rand =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : Math.random().toString(36).slice(2);
  return `gui-run-${rand}`;
}

export function RunPanel({
  pipelineName,
  topology,
  nodes,
  canRun,
  runAction,
  newSessionId = defaultSessionId,
}: RunPanelProps) {
  const [input, setInput] = useState("");
  const [running, setRunning] = useState(false);
  const [outcome, setOutcome] = useState<PipelineRunOutcome | null>(null);
  /** What was actually submitted, recorded so the evidence on screen is tied to
   * a known input and session rather than to whatever is in the box now. */
  const [submitted, setSubmitted] = useState<{ input: string; sessionId: string } | null>(null);

  async function onRun() {
    if (running || !input.trim()) return;
    const sessionId = newSessionId();
    setRunning(true);
    setOutcome(null);
    setSubmitted({ input, sessionId });
    try {
      setOutcome(await runAction(input, sessionId));
    } finally {
      setRunning(false);
    }
  }

  return (
    <div className="card card-pad col gap-3">
      <div className="row gap-2">
        <Icon name="workflow" size={15} />
        <div className="grow">
          <h2 className="section-title" style={{ margin: 0 }}>
            Run
          </h2>
        </div>
      </div>

      {/* Stated before the run, not after. */}
      <div className="banner banner-warn" role="note">
        <strong>Runs against active configuration.</strong> This executes the same engine
        that serves live traffic, using the definitions currently loaded, and it advances
        session state. It is not an isolated preview and it is not a dry run. Each run
        gets a fresh session id so turns are not reused — that does not pin the
        definitions or isolate fixtures.
      </div>

      <div className="field">
        <label htmlFor="run-input">Input</label>
        <textarea
          id="run-input"
          className="textarea"
          rows={3}
          value={input}
          disabled={!canRun || running}
          onChange={(e) => setInput(e.target.value)}
          placeholder="The user message to send through the pipeline"
        />
      </div>

      <div className="row gap-2">
        <button
          type="button"
          className="btn btn-default btn-sm"
          disabled={!canRun || running || !input.trim()}
          aria-busy={running}
          onClick={onRun}
          title={
            !canRun
              ? "Running pipelines needs an authenticated role on this server."
              : !input.trim()
                ? "Enter an input first"
                : undefined
          }
        >
          <Icon name="zap" size={15} />
          {/* The accessible name stays "Run pipeline" while a run is in flight.
              Swapping it for "Running…" renames the control mid-interaction,
              which is disorienting for anyone navigating by name; the busy
              state is carried by aria-busy and the status message instead. */}
          Run pipeline
        </button>
        {running && (
          <span className="muted txt-sm" role="status">
            Submitted — waiting for the engine. Duplicate submission is disabled.
          </span>
        )}
      </div>

      {!canRun && (
        <p className="hint">
          The server is refusing the action, not failing. Nothing here is broken.
        </p>
      )}

      {submitted && !running && outcome && (
        <RunEvidence
          outcome={outcome}
          submitted={submitted}
          topology={topology}
          pipelineName={pipelineName}
          declared={nodes}
        />
      )}
    </div>
  );
}

function RunEvidence({
  outcome,
  submitted,
  topology,
  pipelineName,
  declared,
}: {
  outcome: PipelineRunOutcome;
  submitted: { input: string; sessionId: string };
  topology: string;
  pipelineName: string;
  declared: PipelineAgent[];
}) {
  // An unknown outcome is the one case where nothing may be claimed about what
  // happened on the server.
  if (outcome.status === "unknown") {
    return (
      <div className="banner banner-error" role="alert">
        <strong>Outcome unknown.</strong> {outcome.message}
        <p className="txt-sm" style={{ marginTop: 6 }}>
          Session <code className="mono">{submitted.sessionId}</code> was submitted. It is
          not retried automatically, because a pipeline run is stateful.
        </p>
      </div>
    );
  }
  if (outcome.status === "denied" || outcome.status === "unavailable" || outcome.status === "invalid") {
    return (
      <div
        className={"banner " + (outcome.status === "denied" ? "banner-warn" : "banner-error")}
        role="alert"
      >
        <strong>Not run.</strong> {outcome.message}
      </div>
    );
  }

  const result = outcome.status === "ok" ? outcome.result : outcome.result;
  const nodes = result?.nodes ?? [];

  // Nodes the definition declares that the run did not report. The server tells
  // us what ran; it never tells us what did not, so this difference is the only
  // evidence they exist. Rendering only the returned rows would show a partial
  // run as if the pipeline were shorter than it is.
  const ran = new Set(nodes.map((n) => n.node_id));
  const absent = declared.filter((d) => !ran.has(d.id));

  // Which nodes actually failed, taken from the data rather than parsed out of
  // the error message.
  const failed = nodes.filter((n) => n.response === null);

  return (
    <div className="col gap-3">
      {outcome.status === "blocked" ? (
        <div className="banner banner-error" role="alert">
          {/* NOT "the run was not started" — that is what the design prototype
              says, and it is false here. Refs resolve at node-execution time,
              so everything before the missing one has already run. */}
          <strong>
            Blocked — {failed.length === 1 ? "a node references" : "nodes reference"} an agent
            that is not loaded.
          </strong>{" "}
          <span className="mono">{outcome.error}</span>
          <p className="txt-sm" style={{ marginTop: 6 }}>
            {nodes.length > failed.length
              ? "The run did start. Nodes before this one executed and their session state has advanced — re-running is not a no-op."
              : "Nothing executed before it."}{" "}
            Load the missing definition and reload the server, then run again.
          </p>
        </div>
      ) : outcome.status === "partial" ? (
        <div className="banner banner-error" role="alert">
          <strong>Partial run — a node could not execute.</strong>{" "}
          <span className="mono">{outcome.error}</span>
          <p className="txt-sm" style={{ marginTop: 6 }}>
            {nodes.length > 0
              ? "The nodes that did complete are shown below; they really ran, and their effects stand."
              : "No node results were returned, so nothing is known about what ran."}
          </p>
        </div>
      ) : (
        <div className="banner banner-ok" role="status">
          <strong>Run completed.</strong> {nodes.length} node result(s) in{" "}
          {result ? formatDuration(result.latency) : "unknown"}.
        </div>
      )}

      <dl className="kv">
        <dt>pipeline</dt>
        <dd className="mono">{result?.pipeline_name || pipelineName}</dd>
        <dt>session</dt>
        <dd className="mono">{submitted.sessionId}</dd>
        <dt>input</dt>
        <dd className="mono">{submitted.input}</dd>
        <dt>topology</dt>
        <dd className="mono">{result?.topology || topology}</dd>
      </dl>

      {(nodes.length > 0 || absent.length > 0) && (
        <>
          {isParallel(result?.topology ?? topology) && (
            <div className="banner banner-warn" role="note">
              <strong>Definition order, not a timeline.</strong> This pipeline is{" "}
              <code className="mono">{result?.topology ?? topology}</code>, so the order below
              is the order the nodes are declared in. The server does not report when each
              finished, so this must not be read as a sequence of events.
            </div>
          )}
          <NodeResults nodes={nodes} absent={absent} />
        </>
      )}

      <p className="hint">
        These are the results the run returned. They are not linked to interaction-log
        entries — the server does not report a log id for a run — so finding the
        corresponding requests means searching the logs by time or agent.
      </p>
    </div>
  );
}

function isParallel(topology: string): boolean {
  return topology === "parallel" || topology === "graph";
}

/** Cap on the JSON rendered in the inspector. Node responses are engine output
 * rather than arbitrary payloads, so this is generous — but the design is
 * explicit that body viewers are bounded, and a pathological scenario response
 * should degrade to a notice rather than freeze the tab. */
const MAX_INSPECT_CHARS = 64 * 1024;

function NodeResults({
  nodes,
  absent,
}: {
  nodes: PipelineNodeResult[];
  absent: PipelineAgent[];
}) {
  // U5-3: the table is a summary; the evidence lives in the inspector. Without
  // it a failed node could be FOUND but not read — the acceptance walkthrough
  // asks for both.
  const [selected, setSelected] = useState<string | null>(nodes[0]?.node_id ?? null);
  const selectedNode = nodes.find((n) => n.node_id === selected) ?? null;
  const selectedAbsent = absent.find((d) => d.id === selected) ?? null;

  return (
    <div>
      <div style={{ overflowX: "auto" }}>
      <table className="tbl">
        <caption className="sr-only">
          Node results returned by the run, followed by declared nodes the run did not
          report
        </caption>
        <thead>
          <tr>
            <th>node</th>
            <th>agent</th>
            <th>scenario</th>
            <th>output</th>
            <th className="right">latency</th>
          </tr>
        </thead>
        <tbody>
          {nodes.map((n, i) => (
            <tr key={`${n.node_id}-${i}`} className={n.node_id === selected ? "sel" : undefined}>
              <td className="mono">
                {/* A button, not a row handler: a selectable row has to be
                    reachable and operable from the keyboard. */}
                <button
                  type="button"
                  className="btn btn-ghost btn-sm mono"
                  aria-pressed={n.node_id === selected}
                  onClick={() => setSelected(n.node_id)}
                >
                  {n.node_id || "—"}
                </button>
              </td>
              <td className="mono">{n.agent_name || "—"}</td>
              <td className="mono">{n.response?.scenario_name || <span className="muted">—</span>}</td>
              <td>
                {/* A null response is a real, distinct outcome: the node did not
                    produce one. Rendering it as an empty string would read as
                    "it replied with nothing". */}
                {n.response === null ? (
                  <span className="tag">no response</span>
                ) : (
                  <span className="mono txt-xs">{truncate(n.response.content ?? "")}</span>
                )}
              </td>
              <td className="right mono">{formatDuration(n.latency)}</td>
            </tr>
          ))}

          {/* Declared nodes the run never reported. They are not zero and they
              are not failures — the server does not say why a node did not run,
              so neither does this. Omitting them would make a truncated run
              look like a complete short one. */}
          {absent.map((d) => (
            <tr key={`absent-${d.id}`} className="absent-row">
              <td className="mono">
                <button
                  type="button"
                  className="btn btn-ghost btn-sm mono"
                  aria-pressed={d.id === selected}
                  onClick={() => setSelected(d.id)}
                >
                  {d.id}
                </button>
              </td>
              <td className="mono">{d.ref}</td>
              <td className="muted">—</td>
              <td>
                <span className="badge badge-outline">not executed · unknown</span>
              </td>
              <td className="right muted">—</td>
            </tr>
          ))}
        </tbody>
      </table>
      {absent.length > 0 && (
        <p className="hint" style={{ marginTop: 8 }}>
          {absent.length} declared node{absent.length === 1 ? "" : "s"} produced no result.
          The server reports what ran, never what did not, so whether{" "}
          {absent.length === 1 ? "it was" : "they were"} skipped by an edge condition or
          never reached is unknown from here.
        </p>
      )}
      </div>

      <NodeInspector nodeId={selected} node={selectedNode} absent={selectedAbsent !== null} />
    </div>
  );
}

/** The selected node's evidence, in full (U5-3).
 *
 * Three distinct states, because they mean different things: a response the
 * engine produced, a node that ran and produced none, and a node that never
 * ran at all. The middle one is the interesting failure and the one the design
 * walks through. */
function NodeInspector({
  nodeId,
  node,
  absent,
}: {
  nodeId: string | null;
  node: PipelineNodeResult | null;
  absent: boolean;
}) {
  if (!nodeId) return null;

  const body = node?.response ? JSON.stringify(node.response, null, 2) : "";
  const clipped = body.length > MAX_INSPECT_CHARS;

  return (
    <div className="card card-pad col gap-2" style={{ marginTop: 12 }}>
      <div className="eyebrow">
        node inspector · <span className="mono">{nodeId}</span>
      </div>

      {absent ? (
        <p className="txt-sm">
          This node is declared by the pipeline but the run reported no result for it, so
          there is nothing to inspect. Whether it was skipped or never reached is not
          something the server says.
        </p>
      ) : node?.response ? (
        <>
          <pre
            className="mono"
            style={{ fontSize: 11.5, overflowX: "auto", maxHeight: 260, margin: 0 }}
          >
            {clipped ? body.slice(0, MAX_INSPECT_CHARS) : body}
          </pre>
          {clipped && (
            <p className="hint">
              Response is larger than {MAX_INSPECT_CHARS / 1024} KB and is shown clipped —
              what you see is not the complete value.
            </p>
          )}
        </>
      ) : (
        <div className="banner banner-error" role="note">
          <strong>The response is null.</strong> This node executed and produced nothing —
          which is not the same as producing an empty answer. The most common cause is that
          no scenario matched the input after normalisation; the server does not report
          which predicate failed, and Match Explain (Release B) is the proposed API for
          that.
        </div>
      )}
    </div>
  );
}

function truncate(s: string, max = 120): string {
  return s.length > max ? s.slice(0, max) + "…" : s;
}
