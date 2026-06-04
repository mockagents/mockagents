"use client";

import { useState, type ReactNode } from "react";

import { Icon, type IconName } from "@/lib/icons";

type Dict = Record<string, unknown>;
const asDict = (v: unknown): Dict =>
  v && typeof v === "object" && !Array.isArray(v) ? (v as Dict) : {};
const asArr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const asStr = (v: unknown): string => (typeof v === "string" ? v : v == null ? "" : String(v));

const EXAMPLE_SSE = `data: {"id":"chatcmpl-…","choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":"! Welcome"}}]}

data: [DONE]`;

export function AgentTabs({
  definition,
  name,
}: {
  definition: Record<string, unknown>;
  name: string;
}) {
  const spec = asDict(definition.spec);
  const behavior = asDict(spec.behavior);
  const scenarios = asArr(behavior.scenarios);
  const tools = asArr(spec.tools);
  const streaming = asDict(behavior.streaming);
  const chaos = asDict(behavior.chaos);
  const systemPrompt = asStr(spec.systemPrompt).trim();
  const streamingOn = streaming.enabled === true;
  const chaosOn = chaos.enabled === true;

  const tabs: { id: string; label: string; icon: IconName; count?: number }[] = [
    { id: "overview", label: "Overview", icon: "layout-dashboard" },
    { id: "scenarios", label: "Scenarios", icon: "list-tree", count: scenarios.length },
    { id: "tools", label: "Tools", icon: "wrench", count: tools.length },
    { id: "chaos", label: "Chaos", icon: "zap" },
    { id: "streaming", label: "Streaming", icon: "wifi" },
    { id: "definition", label: "Definition", icon: "file-code" },
  ];
  const [tab, setTab] = useState("overview");

  return (
    <div>
      <div className="tabs mb-4">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            className={"tab" + (tab === t.id ? " active" : "")}
            onClick={() => setTab(t.id)}
          >
            <Icon name={t.icon} size={14} />
            {t.label}
            {t.count != null && <span className="count">{t.count}</span>}
          </button>
        ))}
      </div>

      <div className="mt-4">
        {tab === "overview" && (
          <Overview
            spec={spec}
            scenarios={scenarios}
            tools={tools}
            streamingOn={streamingOn}
            chaosOn={chaosOn}
            systemPrompt={systemPrompt}
            name={name}
          />
        )}
        {tab === "scenarios" && <Scenarios scenarios={scenarios} />}
        {tab === "tools" && <Tools tools={tools} />}
        {tab === "chaos" && <Chaos chaosOn={chaosOn} chaos={chaos} />}
        {tab === "streaming" && <Streaming streamingOn={streamingOn} streaming={streaming} />}
        {tab === "definition" && <Definition definition={definition} name={name} />}
      </div>
    </div>
  );
}

function Overview({
  spec,
  scenarios,
  tools,
  streamingOn,
  chaosOn,
  systemPrompt,
  name,
}: {
  spec: Dict;
  scenarios: unknown[];
  tools: unknown[];
  streamingOn: boolean;
  chaosOn: boolean;
  systemPrompt: string;
  name: string;
}) {
  const kv = (
    <div className="card card-pad">
      <div className="eyebrow mb-4">Definition</div>
      <dl className="kv">
        <dt>protocol</dt>
        <dd className="mono">{asStr(spec.protocol) || "—"}</dd>
        <dt>model</dt>
        <dd className="mono">{asStr(spec.model) || "—"}</dd>
        <dt>scenarios</dt>
        <dd>{scenarios.length}</dd>
        <dt>tools</dt>
        <dd>{tools.length}</dd>
        <dt>streaming</dt>
        <dd>{streamingOn ? "enabled" : "disabled"}</dd>
        <dt>chaos</dt>
        <dd>{chaosOn ? <span className="badge badge-warning">active</span> : "none"}</dd>
        <dt>source</dt>
        <dd className="mono">agents/{name}.yaml</dd>
      </dl>
    </div>
  );
  if (!systemPrompt) return <div style={{ maxWidth: 480 }}>{kv}</div>;
  return (
    <div className="grid grid-2" style={{ gridTemplateColumns: "1.4fr 1fr" }}>
      <div className="card card-pad">
        <div className="eyebrow mb-2">System prompt</div>
        <div className="code-light">{systemPrompt}</div>
      </div>
      {kv}
    </div>
  );
}

function matchLabel(sc: Dict): string {
  const m = asDict(sc.match);
  if (typeof m.content_contains === "string") return `match · content_contains "${m.content_contains}"`;
  if (typeof m.content_regex === "string") return `match · content_regex /${m.content_regex}/`;
  return "fallback · matches everything";
}

function Scenarios({ scenarios }: { scenarios: unknown[] }) {
  if (scenarios.length === 0) return <div className="empty">No scenarios declared.</div>;
  return (
    <div className="col gap-3">
      {scenarios.map((sc0, i) => {
        const sc = asDict(sc0);
        const nm = asStr(sc.name) || `#${i}`;
        const isDefault =
          nm === "default" || sc.default === true || Object.keys(asDict(sc.match)).length === 0;
        const content = asStr(asDict(sc.response).content);
        return (
          <div key={i} className="card">
            <div className="card-head">
              <span className={"badge " + (isDefault ? "badge-secondary" : "badge-default")}>{nm}</span>
              <div className="grow" />
              <span className="muted txt-xs mono">{matchLabel(sc)}</span>
            </div>
            <div style={{ padding: 16 }}>
              <div className="eyebrow mb-2">response.content</div>
              <div className="code-light">{content || "(no content)"}</div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function Tools({ tools }: { tools: unknown[] }) {
  if (tools.length === 0) return <div className="empty">This agent declares no tools.</div>;
  return (
    <div className="col gap-4">
      {tools.map((t0, i) => {
        const t = asDict(t0);
        const params = Object.keys(asDict(asDict(t.parameters).properties));
        const responses = asArr(t.responses);
        return (
          <div key={i} className="card">
            <div className="card-head">
              <Icon name="wrench" size={15} className="muted" />
              <div className="grow">
                <h3 className="mono">{asStr(t.name)}</h3>
                <div className="sub">{asStr(t.description)}</div>
              </div>
              <span className="badge badge-outline mono">
                {responses.length} response{responses.length === 1 ? "" : "s"}
              </span>
            </div>
            <div style={{ padding: "12px 16px" }}>
              <div className="eyebrow mb-2">parameters</div>
              <div className="row wrap gap-2 mb-4">
                {params.length ? (
                  params.map((p) => (
                    <span key={p} className="tag">
                      {p}
                    </span>
                  ))
                ) : (
                  <span className="muted txt-xs">none</span>
                )}
              </div>
              <div className="eyebrow mb-2">match → response</div>
              <table className="tbl" style={{ border: "1px solid var(--sr-border)", borderRadius: 8 }}>
                <thead>
                  <tr>
                    <th>match</th>
                    <th>kind</th>
                    <th>body</th>
                  </tr>
                </thead>
                <tbody>
                  {responses.map((r0, j) => {
                    const r = asDict(r0);
                    const isErr = "error" in r;
                    const matchStr = r.default === true ? "default" : JSON.stringify(r.match ?? {});
                    const body = JSON.stringify(isErr ? r.error : r.response);
                    return (
                      <tr key={j}>
                        <td className="mono">{matchStr}</td>
                        <td>
                          {isErr ? (
                            <span className="badge badge-destructive">error</span>
                          ) : (
                            <span className="badge badge-success">response</span>
                          )}
                        </td>
                        <td className="mono muted">{body}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function ChaosCard({ icon, title, rows }: { icon: IconName; title: string; rows: [string, ReactNode][] }) {
  if (rows.length === 0) return null;
  return (
    <div className="card card-pad">
      <div className="row gap-2 mb-3">
        <Icon name={icon} size={16} className="muted" />
        <span className="sec-title">{title}</span>
      </div>
      <dl className="kv" style={{ gridTemplateColumns: "110px 1fr" }}>
        {rows.map(([k, v]) => (
          <div key={k} style={{ display: "contents" }}>
            <dt>{k}</dt>
            <dd className="mono">{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function Chaos({ chaosOn, chaos }: { chaosOn: boolean; chaos: Dict }) {
  if (!chaosOn) return <div className="empty">Chaos injection is disabled for this agent.</div>;
  const latency = asDict(chaos.latency);
  const errors = asDict(chaos.errors);
  const rl = asDict(chaos.rate_limit);
  return (
    <div className="col gap-4">
      <div className="banner banner-warn">
        <span>
          <strong>Chaos is active.</strong> Faults are evaluated in the engine before tool
          resolution — identical for the OpenAI and Anthropic endpoints.
        </span>
      </div>
      <div className="grid grid-3">
        <ChaosCard
          icon="timer"
          title="Latency"
          rows={
            Object.keys(latency).length
              ? [
                  ["distribution", asStr(latency.distribution) || "—"],
                  ["min_ms", asStr(latency.min_ms)],
                  ["max_ms", asStr(latency.max_ms)],
                ]
              : []
          }
        />
        <ChaosCard
          icon="alert-triangle"
          title="Errors"
          rows={
            Object.keys(errors).length
              ? [
                  ["rate", errors.rate != null ? `${Number(errors.rate) * 100}%` : "—"],
                  ["status_codes", asArr(errors.status_codes).join(", ")],
                  ["message", <span className="muted txt-xs">{asStr(errors.message)}</span>],
                ]
              : []
          }
        />
        <ChaosCard
          icon="gauge"
          title="Rate limit"
          rows={
            Object.keys(rl).length
              ? [
                  ["requests", asStr(rl.requests)],
                  ["window_ms", asStr(rl.window_ms)],
                  ["returns", "429 + Retry-After"],
                ]
              : []
          }
        />
      </div>
    </div>
  );
}

function Streaming({ streamingOn, streaming }: { streamingOn: boolean; streaming: Dict }) {
  if (!streamingOn)
    return (
      <div className="empty">Streaming is disabled — responses return as a single non-streamed body.</div>
    );
  return (
    <div className="grid grid-2" style={{ gridTemplateColumns: "1fr 1.2fr" }}>
      <div className="card card-pad">
        <div className="eyebrow mb-4">SSE configuration</div>
        <dl className="kv" style={{ gridTemplateColumns: "130px 1fr" }}>
          <dt>enabled</dt>
          <dd>
            <span className="badge badge-info">true</span>
          </dd>
          <dt>chunk_size</dt>
          <dd className="mono">{asStr(streaming.chunk_size)} tokens</dd>
          <dt>chunk_delay_ms</dt>
          <dd className="mono">{asStr(streaming.chunk_delay_ms)} ms</dd>
          <dt>transport</dt>
          <dd className="mono">text/event-stream</dd>
        </dl>
      </div>
      <div className="card card-pad">
        <div className="eyebrow mb-2">Example chunk</div>
        <pre className="code">{EXAMPLE_SSE}</pre>
      </div>
    </div>
  );
}

function Definition({ definition, name }: { definition: Record<string, unknown>; name: string }) {
  return (
    <div className="card">
      <div className="card-head">
        <Icon name="file-code" size={16} className="muted" />
        <div className="grow">
          <h3>Loaded definition</h3>
          <div className="sub">
            parsed from <span className="mono">agents/{name}.yaml</span>
          </div>
        </div>
      </div>
      <div style={{ padding: 16 }}>
        <pre className="code">{JSON.stringify(definition, null, 2)}</pre>
      </div>
    </div>
  );
}
