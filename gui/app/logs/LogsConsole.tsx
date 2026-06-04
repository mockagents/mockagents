"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";

import type { InteractionLog } from "@/lib/api";

const MAX_ROWS = 200;

export function LogsConsole({
  initialLogs,
  agents,
}: {
  initialLogs: InteractionLog[];
  agents: string[];
}) {
  const [rows, setRows] = useState<InteractionLog[]>(initialLogs);
  const [live, setLive] = useState(false);
  const [agent, setAgent] = useState("all");
  const [sel, setSel] = useState<number | null>(initialLogs[0]?.id ?? null);
  const [flashId, setFlashId] = useState<number | null>(null);
  const [connected, setConnected] = useState(false);
  const [lastEventAt, setLastEventAt] = useState<Date | null>(null);
  const [dropped, setDropped] = useState(0);
  const retryRef = useRef(0);

  // Live SSE subscription, ported from AutoRefreshLogs: open an EventSource on
  // the same-origin proxy, prepend each "log" frame, track "dropped" backpressure
  // frames, and reconnect with capped backoff on error.
  useEffect(() => {
    if (!live) return;
    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;

    function connect() {
      if (disposed) return;
      es = new EventSource("/api/logs/stream");
      es.addEventListener("open", () => {
        if (disposed) return;
        setConnected(true);
        retryRef.current = 0;
      });
      es.addEventListener("log", (evt: MessageEvent<string>) => {
        if (disposed) return;
        let row: InteractionLog;
        try {
          row = JSON.parse(evt.data) as InteractionLog;
        } catch {
          return;
        }
        setFlashId(row.id);
        setRows((prev) => [row, ...prev].slice(0, MAX_ROWS));
        setLastEventAt(new Date());
      });
      es.addEventListener("dropped", (evt: MessageEvent<string>) => {
        if (disposed) return;
        try {
          const p = JSON.parse(evt.data) as { count?: number };
          if (typeof p.count === "number") setDropped(p.count);
        } catch {
          /* ignore */
        }
      });
      es.addEventListener("error", () => {
        if (disposed) return;
        setConnected(false);
        es?.close();
        es = null;
        const delay = Math.min(30_000, 1000 * 2 ** retryRef.current);
        retryRef.current += 1;
        retryTimer = setTimeout(connect, delay);
      });
    }

    connect();
    return () => {
      disposed = true;
      setConnected(false);
      if (retryTimer) clearTimeout(retryTimer);
      if (es) es.close();
    };
  }, [live]);

  const filtered = useMemo(
    () => (agent === "all" ? rows : rows.filter((r) => r.agent_name === agent)),
    [rows, agent],
  );
  const selRow = filtered.find((r) => r.id === sel) ?? filtered[0];

  return (
    <div>
      <div className="head-row page-head">
        <div className="grow">
          <h1 className="page-title">Interaction logs</h1>
          <p className="page-lede">
            Every request the engine has served, newest first. Live mode subscribes to{" "}
            <code>GET /api/v1/logs/stream</code> over SSE.
          </p>
        </div>
        <div className="row gap-3">
          <div className="row gap-2" style={{ padding: "0 4px" }}>
            <span className="txt-sm muted">Live</span>
            <button
              type="button"
              role="switch"
              aria-checked={live}
              className={"switch" + (live ? " on" : "")}
              onClick={() => setLive((v) => !v)}
            >
              <span className="knob" />
            </button>
            {live && (
              <span className={"badge " + (connected ? "badge-info" : "badge-secondary")}>
                <span className={"badge-dot" + (connected ? " pulse" : "")} />
                {connected ? "sse" : "…"}
              </span>
            )}
          </div>
        </div>
      </div>

      <div className="row gap-3 mb-4" style={{ flexWrap: "wrap" }}>
        <select
          className="select"
          style={{ width: 220 }}
          value={agent}
          onChange={(e) => setAgent(e.target.value)}
        >
          <option value="all">All agents</option>
          {agents.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
        <span className="muted txt-sm">
          {filtered.length} rows{live ? " · streaming" : ""}
        </span>
        {dropped > 0 && (
          <span className="drop-badge" title="Backend dropped log events because this tab's buffer was full.">
            ⚠ {dropped} dropped
          </span>
        )}
        {live && lastEventAt && (
          <span className="muted txt-xs">last event {lastEventAt.toLocaleTimeString()}</span>
        )}
      </div>

      {filtered.length === 0 ? (
        <div className="empty">
          {live
            ? "Waiting for traffic. Send a request to the MockAgents server."
            : "No interactions match the current filter."}
        </div>
      ) : (
        <div className="grid" style={{ gridTemplateColumns: "1fr 380px", alignItems: "start", gap: 16 }}>
          <div className="card" style={{ overflow: "hidden" }}>
            <div style={{ maxHeight: 560, overflow: "auto" }}>
              <table className="tbl">
                <thead>
                  <tr>
                    <th>id</th>
                    <th>time</th>
                    <th>agent</th>
                    <th>status</th>
                    <th>scenario</th>
                    <th className="right">latency</th>
                    <th className="right">cost</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((r) => (
                    <tr
                      key={r.id}
                      className={
                        "click" + (r.id === selRow?.id ? " sel" : "") + (r.id === flashId ? " flash" : "")
                      }
                      onClick={() => setSel(r.id)}
                    >
                      <td className="mono muted">{r.id}</td>
                      <td className="mono" style={{ fontSize: 11.5 }}>
                        {fmtTime(r.timestamp)}
                      </td>
                      <td>
                        <div className="col" style={{ gap: 0 }}>
                          <span style={{ fontWeight: 500 }}>{r.agent_name || "—"}</span>
                          <span className="muted mono" style={{ fontSize: 10.5 }}>
                            {r.request_path ?? ""}
                          </span>
                        </div>
                      </td>
                      <td>
                        <StatusBadge code={r.response_status} />
                      </td>
                      <td className="mono muted" style={{ fontSize: 11.5 }}>
                        {r.scenario_name || "—"}
                      </td>
                      <td className="num">{r.latency_ms != null ? `${Math.round(r.latency_ms)}ms` : "—"}</td>
                      <td className="num muted">
                        {r.cost_usd != null && r.cost_usd > 0 ? `$${r.cost_usd.toFixed(5)}` : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <Inspector row={selRow} />
        </div>
      )}
    </div>
  );
}

function Inspector({ row }: { row?: InteractionLog }) {
  if (!row) {
    return (
      <div className="card card-pad">
        <div className="empty">Select a row to inspect.</div>
      </div>
    );
  }
  return (
    <div className="card" style={{ position: "sticky", top: 0 }}>
      <div className="card-head">
        <div className="grow">
          <h3 className="mono">log #{row.id}</h3>
          <div className="sub">{fmtDateTime(row.timestamp)}</div>
        </div>
        <StatusBadge code={row.response_status} />
      </div>
      <div className="card-pad col gap-4" style={{ maxHeight: 560, overflow: "auto" }}>
        <dl className="kv" style={{ gridTemplateColumns: "92px 1fr" }}>
          <dt>agent</dt>
          <dd className="mono">{row.agent_name || "—"}</dd>
          <dt>model</dt>
          <dd className="mono">{row.model || "—"}</dd>
          <dt>endpoint</dt>
          <dd className="mono">
            {row.request_method ?? "POST"} {row.request_path ?? "—"}
          </dd>
          <dt>scenario</dt>
          <dd className="mono">{row.scenario_name || "—"}</dd>
          <dt>latency</dt>
          <dd className="mono">{row.latency_ms != null ? `${row.latency_ms.toFixed(1)} ms` : "—"}</dd>
          <dt>tokens</dt>
          <dd className="mono">
            {row.prompt_tokens ?? 0} → {row.completion_tokens ?? 0}
          </dd>
          <dt>cost</dt>
          <dd className="mono">{row.cost_usd != null ? `$${row.cost_usd.toFixed(6)}` : "—"}</dd>
          <dt>streamed</dt>
          <dd>
            {row.streaming ? (
              <span className="badge badge-info">sse</span>
            ) : (
              <span className="badge badge-secondary">no</span>
            )}
          </dd>
        </dl>
        {row.request_body ? (
          <div>
            <div className="eyebrow mb-2">request</div>
            <pre className="code">{pretty(row.request_body)}</pre>
          </div>
        ) : null}
        <div>
          <div className="eyebrow mb-2">response</div>
          {row.response_body ? (
            <pre className="code">{pretty(row.response_body)}</pre>
          ) : (
            <div className="muted txt-sm">(no body — streamed or not captured)</div>
          )}
        </div>
        <Link href={`/logs/${row.id}`} className="btn btn-ghost btn-sm" style={{ alignSelf: "flex-start" }}>
          Open full record ↗
        </Link>
      </div>
    </div>
  );
}

function StatusBadge({ code }: { code?: number }) {
  if (code == null) return <span className="badge badge-secondary">—</span>;
  const v =
    code >= 500 ? "destructive" : code >= 400 ? "warning" : code >= 300 ? "info" : "success";
  return <span className={`badge badge-${v} mono`}>{code}</span>;
}

function pretty(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2);
  } catch {
    return body;
  }
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleTimeString();
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
