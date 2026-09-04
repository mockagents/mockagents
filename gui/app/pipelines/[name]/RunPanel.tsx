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

import type { PipelineNodeResult, PipelineRunOutcome } from "@/lib/api";
import { formatDuration } from "@/lib/duration";
import { Icon } from "@/lib/icons";

export interface RunPanelProps {
  pipelineName: string;
  topology: string;
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
}: {
  outcome: PipelineRunOutcome;
  submitted: { input: string; sessionId: string };
  topology: string;
  pipelineName: string;
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

  return (
    <div className="col gap-3">
      {outcome.status === "partial" ? (
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

      {nodes.length > 0 && (
        <>
          {isParallel(result?.topology ?? topology) && (
            <div className="banner banner-warn" role="note">
              <strong>Definition order, not a timeline.</strong> This pipeline is{" "}
              <code className="mono">{result?.topology ?? topology}</code>, so the order below
              is the order the nodes are declared in. The server does not report when each
              finished, so this must not be read as a sequence of events.
            </div>
          )}
          <NodeResults nodes={nodes} />
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

function NodeResults({ nodes }: { nodes: PipelineNodeResult[] }) {
  return (
    <div style={{ overflowX: "auto" }}>
      <table className="tbl">
        <caption className="sr-only">Node results returned by the run</caption>
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
            <tr key={`${n.node_id}-${i}`}>
              <td className="mono">{n.node_id || "—"}</td>
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
        </tbody>
      </table>
    </div>
  );
}

function truncate(s: string, max = 120): string {
  return s.length > max ? s.slice(0, max) + "…" : s;
}
