"use client";

import Link from "next/link";
import { useMemo, useState } from "react";

import type { AgentSummary } from "@/lib/api";
import { Icon } from "@/lib/icons";

/** Short, human protocol label (the wire values are long). */
export function protoShort(p: string): string {
  if (p.startsWith("anthropic")) return "anthropic";
  if (p.startsWith("openai")) return "openai";
  return p;
}

// Client island: search + protocol filter over the server-fetched agents,
// rendered as the design's catalog cards. No fabricated telemetry — only the
// fields the management API actually returns (name, model, protocol,
// scenario_count, tool_count, tags).
export function AgentCatalog({ agents }: { agents: AgentSummary[] }) {
  const [q, setQ] = useState("");
  const [proto, setProto] = useState("all");

  const protocols = useMemo(
    () => Array.from(new Set(agents.map((a) => a.protocol))).sort(),
    [agents],
  );

  const needle = q.trim().toLowerCase();
  const filtered = agents.filter((a) => {
    if (proto !== "all" && a.protocol !== proto) return false;
    if (needle) {
      const hay = `${a.name} ${a.description ?? ""} ${a.model} ${(a.tags ?? []).join(" ")}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });

  return (
    <>
      <div className="row gap-3 mb-4" style={{ flexWrap: "wrap" }}>
        <div className="search" style={{ width: 280 }}>
          <Icon name="search" size={15} />
          <input
            className="input"
            placeholder="Search agents, tags, models…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
        <div className="row gap-2">
          <button type="button" className="pill" aria-pressed={proto === "all"} onClick={() => setProto("all")}>
            all protocols
          </button>
          {protocols.map((p) => (
            <button key={p} type="button" className="pill" aria-pressed={proto === p} onClick={() => setProto(p)}>
              {protoShort(p)}
            </button>
          ))}
        </div>
        <span className="muted txt-sm" style={{ marginLeft: "auto" }}>
          {filtered.length} of {agents.length}
        </span>
      </div>

      {filtered.length === 0 ? (
        <div className="empty">No agents match this filter.</div>
      ) : (
        <div className="catalog">
          {filtered.map((a) => (
            <AgentCard key={a.name} a={a} />
          ))}
        </div>
      )}
    </>
  );
}

function AgentCard({ a }: { a: AgentSummary }) {
  return (
    <Link href={`/agents/${encodeURIComponent(a.name)}`} className="agent-card">
      <div className="ac-top">
        <div className="agent-icon">
          <Icon name="bot" size={18} />
        </div>
        <div className="grow">
          <h3>{a.name}</h3>
          <div className="ac-proto">
            {a.model || "—"} · {protoShort(a.protocol)}
          </div>
        </div>
      </div>
      {a.description && <p className="ac-desc">{a.description}</p>}
      <div className="ac-stats">
        <div className="ac-stat">
          <span className="n">{a.scenario_count}</span>
          <span className="l">scenarios</span>
        </div>
        <div className="ac-stat">
          <span className="n">{a.tool_count}</span>
          <span className="l">tools</span>
        </div>
      </div>
      {a.tags && a.tags.length > 0 && (
        <div className="ac-tags">
          {a.tags.map((t) => (
            <span key={t} className="tag">
              {t}
            </span>
          ))}
        </div>
      )}
    </Link>
  );
}
