"use client";

import Link from "next/link";
import { useMemo, useState } from "react";

import type { AgentSummary } from "@/lib/api";
import { Icon } from "@/lib/icons";

import { DangerConfirm } from "./DangerConfirm";

/** Server action, submitted as a form by the confirmation dialog. It reads the
 * agent name from the form data and redirects with the outcome — see page.tsx.
 *
 * Deliberately not a `(name) => Promise<result>` call from a transition. The
 * dialog owns a form so the destructive control is a submit button gated by an
 * exact-name field, which is what makes the action deliberate. */
export type DeleteAction = (formData: FormData) => void | Promise<void>;

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
export function AgentCatalog({
  agents,
  deleteAction,
  canWrite = true,
  singleAgentFallback = false,
}: {
  agents: AgentSummary[];
  deleteAction: DeleteAction;
  /** UX-01/U1-2: false only when the SERVER has said this credential lacks
   * `agents.write`. Unknown stays true — the server authorizes the request
   * anyway, and disabling a control on a hunch is its own kind of lie. */
  canWrite?: boolean;
  /** True when deleting would leave exactly one agent on a server that serves
   * anonymous callers. The engine then routes a request naming ANY agent to
   * the single survivor (engine.go resolveAgent), so the deletion turns a
   * would-be failure into a silent misroute. Worth saying out loud before the
   * fact, not after. */
  singleAgentFallback?: boolean;
}) {
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
            <AgentCard
              key={a.name}
              a={a}
              deleteAction={deleteAction}
              canWrite={canWrite}
              singleAgentFallback={singleAgentFallback}
            />
          ))}
        </div>
      )}
    </>
  );
}

/** Durability and revision at a glance (U3-6).
 *
 * "Which of my agents survive a restart?" used to be answerable only one agent
 * at a time. The three states are kept distinct because they lead somewhere
 * different: a file-backed agent is fine, a runtime-only one was created that
 * way, and a tracked file that has gone missing is a surprise waiting for the
 * next restart.
 *
 * An older server sends no `persistence` field. That is UNKNOWN, and renders as
 * nothing at all rather than as "runtime" — guessing here would tell someone
 * their durable agents are about to disappear. */
function PersistenceLine({ a }: { a: AgentSummary }) {
  if (!a.persistence && !a.effective_revision) return null;
  return (
    <div className="row gap-2" style={{ flexWrap: "wrap", marginTop: 2 }}>
      {a.persistence === "file" && (
        <span className="badge badge-ok" title={a.file ? `backed by ${a.file}` : undefined}>
          persisted
        </span>
      )}
      {a.persistence === "runtime" && (
        <span className="badge badge-warn" title="created at runtime; lost when the server restarts">
          runtime-only
        </span>
      )}
      {a.persistence === "missing" && (
        <span
          className="badge badge-destructive"
          title={`${a.file ?? "its backing file"} is tracked but not present; this definition is serving from memory`}
        >
          file missing
        </span>
      )}
      {a.effective_revision && (
        <span
          className="tag mono"
          title="revision of the running definition (not the ETag — fetch the agent for a precondition)"
        >
          {a.effective_revision.slice(0, 8)}
        </span>
      )}
    </div>
  );
}

function AgentCard({
  a,
  deleteAction,
  canWrite,
  singleAgentFallback,
}: {
  a: AgentSummary;
  deleteAction: DeleteAction;
  canWrite: boolean;
  singleAgentFallback: boolean;
}) {
  // Consequences are stated as facts about THIS agent, not as a generic
  // warning. Each line below is something the server actually does.
  const consequences = [
    "It stops serving immediately. A client calling it mid-conversation fails on its next request.",
    "If it was loaded from a file, that file is deleted from the agents directory too. The catalog cannot tell from here which agents are file-backed, so treat this one as file-backed.",
    "Any pipeline with a node referencing it fails at that node on the next run.",
    "Interaction logs are kept and keep naming an agent that no longer exists.",
    "There is no undo and no soft-delete. Recreating it means re-authoring the definition or restoring the file from version control.",
  ];
  if (singleAgentFallback) {
    // Not a generic caution: with one agent left and no tenant scoping, the
    // engine treats the survivor as the default for ANY request, so requests
    // aimed at the deleted agent are answered rather than refused.
    consequences.push(
      "This would leave one agent on a server that accepts unauthenticated calls. Requests naming this agent will then be answered by the remaining agent instead of failing — a silent misroute, not an error.",
    );
  }

  return (
    <div className="agent-card">
      <div className="ac-top">
        <div className="agent-icon">
          <Icon name="bot" size={18} />
        </div>
        <div className="grow">
          {/* Stretched link: covers the whole card but is not an ancestor of the button. */}
          <h3>
            <Link href={`/agents/${encodeURIComponent(a.name)}`} className="ac-link">
              {a.name}
            </Link>
          </h3>
          <div className="ac-proto">
            {a.model || "—"} · {protoShort(a.protocol)}
          </div>
        </div>
        {canWrite ? (
          <DangerConfirm
            action={deleteAction}
            fields={{ name: a.name }}
            triggerLabel={<Icon name="trash" size={14} />}
            triggerTitle={`Delete ${a.name}`}
            triggerClassName="btn btn-outline btn-sm ac-del"
            title={`Delete agent ${a.name}?`}
            impact={
              <>
                This removes <code className="mono">{a.name}</code> from the running
                server.
              </>
            }
            consequences={consequences}
            confirmPhrase={a.name}
            confirmPhraseLabel="Type the exact agent name to confirm"
            confirmLabel="Delete this agent"
          />
        ) : (
          // Disabled with the floor named — not hidden, and not left live to
          // 403. Hiding it would leave an operator unable to tell "this console
          // cannot delete agents" from "my key cannot".
          <button
            type="button"
            className="btn btn-outline btn-sm ac-del"
            disabled
            aria-label={`Delete ${a.name}`}
            title={`Deleting agents needs the editor role. Your credential does not have it.`}
          >
            <Icon name="trash" size={14} />
          </button>
        )}
      </div>
      {a.description && <p className="ac-desc">{a.description}</p>}
      <PersistenceLine a={a} />
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
    </div>
  );
}
