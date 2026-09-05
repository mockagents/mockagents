"use client";

// UX-04: the request / session explorer.
//
// Rules this screen is built around:
//
//   - Filtering is SERVER-backed and lives in the URL, so a filtered view is
//     shareable and is not limited to whatever happened to be fetched. Only
//     metadata goes in the URL — never a body, never a credential.
//   - The live feed deduplicates. A reconnect re-delivers rows; showing them
//     twice would make an operator miscount requests.
//   - A drop in the feed is reported, and if recovery cannot PROVE it caught
//     everything, the gap is shown as unresolved rather than papered over.
//   - The selected row survives live traffic, the row cap, and reconnects.
//   - Bodies are behind an explicit reveal, and a clipped body says so.

import Link from "next/link";
import { useRouter } from "next/navigation";
import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { InteractionLog, LogWindow } from "@/lib/api";
import {
  DEFAULT_FILTERS,
  describeGapRange,
  filtersToQuery,
  interpretRecovery,
  isFiltered,
  mergeRows,
  newestSeen,
  type GapState,
  type LastSeen,
  type LogFilters,
} from "@/lib/logfeed";
import { Icon } from "@/lib/icons";

const MAX_ROWS = 500;
/** How far back a post-reconnect recovery fetch will reach. Bounded on purpose:
 * the point is a best effort with an honest verdict, not an unbounded scan. */
const RECOVERY_LIMIT = 200;

export interface LogsConsoleProps {
  window: LogWindow;
  agents: string[];
  filters: LogFilters;
  /** Fetch rows newer than `afterId`, bounded by `limit`. Server action. */
  recoverAction: (afterId: number | null, limit: number) => Promise<InteractionLog[]>;
  /** Fetch ONE row's bodies (§9.1). The listing is metadata-only, so a body has
   * genuinely not left the server until this is called — which is what lets the
   * screen say bodies are not fetched rather than merely not shown. */
  revealAction: (id: number) => Promise<{ request?: string; response?: string } | null>;
}

export function LogsConsole({
  window: initial,
  agents,
  filters,
  recoverAction,
  revealAction,
}: LogsConsoleProps) {
  const router = useRouter();

  const [rows, setRows] = useState<InteractionLog[]>(initial.rows);
  const [sel, setSel] = useState<number | null>(initial.rows[0]?.id ?? null);
  const [live, setLive] = useState(false);
  const [connected, setConnected] = useState(false);
  const [dropped, setDropped] = useState(0);
  const [gap, setGap] = useState<GapState>({ kind: "none" });
  const [authExpired, setAuthExpired] = useState(false);
  const [flashId, setFlashId] = useState<number | null>(null);
  const [revealBodies, setRevealBodies] = useState(false);
  /** Bodies fetched so far, by row id. Cleared when the window changes, so a
   * body can never be shown against a row from a different result set. */
  const [bodies, setBodies] = useState<Map<number, { request?: string; response?: string }>>(
    new Map(),
  );
  const [revealState, setRevealState] = useState<"idle" | "loading" | "failed">("idle");

  const retryRef = useRef(0);
  // The newest row seen before a disconnect, so recovery knows where to resume
  // — and, if it cannot catch up, where the resulting gap begins.
  const lastSeenRef = useRef<LastSeen | null>(newestSeen(initial.rows));
  const selRef = useRef<number | null>(sel);
  selRef.current = sel;

  // A new server-rendered page (filters changed) replaces the feed wholesale.
  useEffect(() => {
    setRows(initial.rows);
    setSel(initial.rows[0]?.id ?? null);
    lastSeenRef.current = newestSeen(initial.rows);
    setGap({ kind: "none" });
    setBodies(new Map());
    setRevealBodies(false);
  }, [initial]);

  const addRows = useCallback((incoming: InteractionLog[]) => {
    setRows((prev) => {
      const merged = mergeRows(prev, incoming, { max: MAX_ROWS, pinnedId: selRef.current });
      lastSeenRef.current = newestSeen(merged);
      return merged;
    });
  }, []);

  /** After a reconnect, try to fetch what was missed. The verdict may be
   * "unknown", and that is reported as such. */
  const recover = useCallback(async () => {
    const resumeFrom = lastSeenRef.current;
    setGap({ kind: "recovering" });
    try {
      const fetched = await recoverAction(resumeFrom?.id ?? null, RECOVERY_LIMIT);
      const outcome = interpretRecovery(fetched, RECOVERY_LIMIT, resumeFrom);
      if (outcome.rows.length > 0) addRows(outcome.rows);
      setGap(outcome.gap);
    } catch {
      setGap({
        kind: "unresolved",
        reason:
          "The feed reconnected, but the requests missed during the outage could not be " +
          "retrieved. This view may be incomplete.",
      });
    }
  }, [addRows, recoverAction]);

  useEffect(() => {
    if (!live) return;
    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;
    // Only a reconnect needs recovery; the first connect has the server-rendered
    // page as its baseline.
    let hadConnected = false;

    function connect() {
      if (disposed) return;
      es = new EventSource("/api/logs/stream");

      es.addEventListener("open", () => {
        if (disposed) return;
        setConnected(true);
        setAuthExpired(false);
        retryRef.current = 0;
        if (hadConnected) void recover();
        hadConnected = true;
      });

      es.addEventListener("log", (evt: MessageEvent<string>) => {
        if (disposed) return;
        try {
          const row = JSON.parse(evt.data) as InteractionLog;
          setFlashId(row.id);
          addRows([row]);
        } catch {
          /* a malformed frame is dropped rather than corrupting the list */
        }
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

        // EventSource hides the status code, so probe the proxy to find out
        // whether this is a network problem or a rejected credential. Retrying
        // forever against a 401 would never recover and would never say why.
        void fetch("/api/logs/stream", { method: "HEAD" })
          .then((res) => {
            if (disposed) return;
            if (res.status === 401 || res.status === 403) {
              setAuthExpired(true);
              setLive(false);
              return;
            }
            scheduleRetry();
          })
          .catch(() => {
            if (!disposed) scheduleRetry();
          });
      });
    }

    function scheduleRetry() {
      const delay = Math.min(30_000, 1000 * 2 ** retryRef.current);
      retryRef.current += 1;
      retryTimer = setTimeout(connect, delay);
    }

    connect();
    return () => {
      disposed = true;
      setConnected(false);
      if (retryTimer) clearTimeout(retryTimer);
      if (es) es.close();
    };
  }, [live, addRows, recover]);

  const selRow = useMemo(() => rows.find((r) => r.id === sel) ?? null, [rows, sel]);

  /** Reveal is a FETCH, not a style change: the listing withheld the bodies, so
   * this is the moment they leave the server. A failure says so rather than
   * rendering an empty pane that reads like an empty body. */
  async function onToggleBodies() {
    if (revealBodies) {
      setRevealBodies(false);
      return;
    }
    const id = selRef.current;
    if (id == null) return;
    setRevealBodies(true);
    if (bodies.has(id)) return;
    setRevealState("loading");
    try {
      const fetched = await revealAction(id);
      if (fetched) {
        setBodies((prev) => new Map(prev).set(id, fetched));
        setRevealState("idle");
      } else {
        setRevealState("failed");
      }
    } catch {
      setRevealState("failed");
    }
  }

  function applyFilters(next: Partial<LogFilters>) {
    // Any filter change resets paging: keeping an offset from a different
    // result set would point at unrelated rows.
    const merged: LogFilters = { ...filters, ...next, offset: next.offset ?? 0 };
    const qs = filtersToQuery(merged);
    router.push(qs ? `/logs?${qs}` : "/logs");
  }

  return (
    <div>
      <div className="head-row page-head">
        <div className="grow">
          <h1 className="page-title">Interaction logs</h1>
          <p className="page-lede">
            Every request the engine has served, newest first. Filters run on the server
            and live in the URL, so a filtered view can be shared. Live mode subscribes to{" "}
            <code>GET /api/v1/logs/stream</code> over SSE.
          </p>
        </div>
        <div className="row gap-3">
          <div className="row gap-2" style={{ padding: "0 4px" }}>
            <span className="txt-sm muted" id="live-feed-label">
              Live
            </span>
            <button
              type="button"
              role="switch"
              aria-labelledby="live-feed-label"
              aria-checked={live}
              className={"switch" + (live ? " on" : "")}
              onClick={() => setLive((v) => !v)}
            >
              <span className="knob" />
            </button>
            {live && (
              <span className={"badge " + (connected ? "badge-info" : "badge-secondary")}>
                <span className={"badge-dot" + (connected ? " pulse" : "")} />
                {connected ? "sse" : "reconnecting"}
              </span>
            )}
          </div>
        </div>
      </div>

      <FilterBar agents={agents} filters={filters} onApply={applyFilters} />

      <WindowNotices
        window={initial}
        filters={filters}
        rowCount={rows.length}
        live={live}
        gap={gap}
        dropped={dropped}
        authExpired={authExpired}
      />

      {rows.length === 0 ? (
        <EmptyWindow filters={filters} live={live} />
      ) : (
        <div className="grid" style={{ gridTemplateColumns: "1fr 420px", alignItems: "start", gap: 16 }}>
          <LogTable
            rows={rows}
            gap={gap}
            selectedId={selRow?.id ?? null}
            flashId={flashId}
            onSelect={setSel}
            onSession={(id) => applyFilters({ session_id: id })}
          />
          <LogDetail
            row={selRow}
            revealBodies={revealBodies}
            bodies={selRow ? bodies.get(selRow.id) : undefined}
            revealState={revealState}
            onToggleBodies={onToggleBodies}
            onSession={(id) => applyFilters({ session_id: id })}
          />
        </div>
      )}

      <Pager window={initial} filters={filters} onApply={applyFilters} />
    </div>
  );
}

/** The reachable-and-empty state (U4-3).
 *
 * This branch is only reached when the store ANSWERED. Unreachable and
 * unauthorized are caught on the server and render their own screens, so this
 * one can say plainly that the store is fine — and it should, because "no rows"
 * is otherwise the most ambiguous thing a log view can show.
 *
 * It also states the window. This API has no default time window: an unfiltered
 * read is the newest N of everything recorded. Saying so is what stops "empty"
 * being read as "empty for the period I care about". */
function EmptyWindow({ filters, live }: { filters: LogFilters; live: boolean }) {
  const window = describeWindow(filters);
  return (
    <div className="empty">
      <strong>No records in this window.</strong>
      <div className="mt-2">
        Window: <span className="mono">{window}</span>
      </div>
      <p className="txt-sm" style={{ marginTop: 8 }}>
        The log store is reachable and empty — which is not the same as unreachable (a
        connection error) or unauthorized (403); those render differently.
      </p>
      <p className="txt-sm" style={{ marginTop: 8 }}>
        {isFiltered(filters)
          ? "Widen the filter, or clear it to see recent traffic."
          : live
            ? "Send a request to any mocked endpoint and it will appear here as it arrives."
            : "Send a request to any mocked endpoint and reload, or turn on Live to watch them arrive."}
      </p>
    </div>
  );
}

/** Plain-language description of the window a result covers. Never invents a
 * default period: without a time filter this API returns the newest N of
 * everything it holds, and that is what gets said. */
function describeWindow(filters: LogFilters): string {
  const parts: string[] = [];
  if (filters.since || filters.until) {
    parts.push(`${filters.since || "the beginning"} → ${filters.until || "now"}`);
  } else {
    parts.push(`the newest ${filters.limit}, no time filter`);
  }
  if (filters.agent) parts.push(`agent ${filters.agent}`);
  if (filters.session_id) parts.push(`session ${filters.session_id}`);
  if (filters.offset) parts.push(`from offset ${filters.offset}`);
  return parts.join(" · ");
}

function FilterBar({
  agents,
  filters,
  onApply,
}: {
  agents: string[];
  filters: LogFilters;
  onApply: (next: Partial<LogFilters>) => void;
}) {
  return (
    <form
      className="row gap-3 mb-4"
      style={{ flexWrap: "wrap", alignItems: "flex-end" }}
      onSubmit={(e) => {
        e.preventDefault();
        const data = new FormData(e.currentTarget);
        onApply({
          agent: String(data.get("agent") ?? ""),
          session_id: String(data.get("session_id") ?? "").trim(),
          since: String(data.get("since") ?? "").trim(),
          until: String(data.get("until") ?? "").trim(),
        });
      }}
    >
      <div className="field" style={{ width: 200 }}>
        <label htmlFor="filter-agent">Agent</label>
        <select id="filter-agent" name="agent" className="select" defaultValue={filters.agent}>
          <option value="">All agents</option>
          {agents.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
      </div>
      <div className="field" style={{ width: 240 }}>
        <label htmlFor="filter-session">Session</label>
        <input
          id="filter-session"
          name="session_id"
          className="input mono"
          placeholder="session id"
          defaultValue={filters.session_id}
        />
      </div>
      <div className="field" style={{ width: 210 }}>
        <label htmlFor="filter-since">Since</label>
        <input
          id="filter-since"
          name="since"
          className="input mono"
          placeholder="2026-09-03T00:00:00Z"
          defaultValue={filters.since}
        />
      </div>
      <div className="field" style={{ width: 210 }}>
        <label htmlFor="filter-until">Until</label>
        <input
          id="filter-until"
          name="until"
          className="input mono"
          placeholder="RFC3339"
          defaultValue={filters.until}
        />
      </div>
      <button type="submit" className="btn btn-default btn-sm">
        <Icon name="search" size={15} /> Apply
      </button>
      {isFiltered(filters) && (
        <button
          type="button"
          className="btn btn-outline btn-sm"
          onClick={() => onApply({ ...DEFAULT_FILTERS })}
        >
          Clear
        </button>
      )}
    </form>
  );
}

function WindowNotices({
  window: win,
  filters,
  rowCount,
  live,
  gap,
  dropped,
  authExpired,
}: {
  window: LogWindow;
  filters: LogFilters;
  rowCount: number;
  live: boolean;
  gap: GapState;
  dropped: number;
  authExpired: boolean;
}) {
  return (
    <div className="col gap-2 mb-4">
      <div className="row gap-3" style={{ flexWrap: "wrap" }}>
        <span className="muted txt-sm">
          {rowCount} rows{live ? " · streaming" : ""}
        </span>
        {/* Never "N of M": the API returns no total, and inventing one would
            present unknown as fact. */}
        {win.mayHaveMore && (
          <span className="tag" title="This is a bounded window, not the complete history.">
            showing the {win.limit} most recent in this window — older requests exist
          </span>
        )}
      </div>

      {authExpired && (
        <div className="banner banner-error" role="alert">
          <strong>Live feed stopped: your session is no longer valid.</strong>{" "}
          <Link href="/login">Sign in again</Link> to resume streaming. The rows already
          loaded are still shown.
        </div>
      )}

      {!win.stable && (
        <div className="banner banner-warn" role="note">
          <strong>Paged view — rows can shift.</strong> This API pages by offset, not by a
          stable cursor, so a request arriving while you page can push rows onto the next
          page: something may appear twice or be skipped. Filter by time or session for a
          view that does not move.
        </div>
      )}

      {gap.kind === "unresolved" && (
        <div className="banner banner-error" role="alert">
          <strong>Feed gap — this view is incomplete.</strong> {gap.reason}{" "}
          {/* The range is the part an operator can act on: it answers whether
              the hole overlaps the window they are investigating. */}
          Missing range: <span className="mono">{describeGapRange(gap)}</span>. It is marked
          in the table at the point it occurred.
        </div>
      )}
      {gap.kind === "recovering" && (
        <div className="banner banner-warn" role="status">
          Reconnected. Fetching requests missed while disconnected…
        </div>
      )}
      {gap.kind === "closed" && (
        <div className="banner banner-ok" role="status">
          Reconnected and caught up
          {gap.recovered > 0 ? ` — recovered ${gap.recovered} request(s).` : "; nothing was missed."}
        </div>
      )}

      {dropped > 0 && (
        <div className="banner banner-warn" role="note">
          <strong>{dropped} event(s) dropped by the server.</strong> This tab could not keep
          up, so those requests were never sent to it. They are still in the log — reload to
          see them.
        </div>
      )}

      {isFiltered(filters) && (
        <p className="hint">
          Filtered view. This URL is shareable and contains only filter metadata — never
          request bodies or credentials.
        </p>
      )}
    </div>
  );
}

function LogTable({
  rows,
  gap,
  selectedId,
  flashId,
  onSelect,
  onSession,
}: {
  rows: InteractionLog[];
  gap: GapState;
  selectedId: number | null;
  flashId: number | null;
  onSelect: (id: number) => void;
  onSession: (id: string) => void;
}) {
  // Where the gap belongs in a newest-first list: immediately ABOVE the last
  // row seen before the drop, which is the boundary it describes. A banner
  // above the table scrolls out of view, leaving two rows that are adjacent on
  // screen but hours apart reading as consecutive traffic.
  const gapAfterId = gap.kind === "unresolved" ? gap.afterId : undefined;
  const gapIndex =
    gapAfterId === undefined ? -1 : rows.findIndex((r) => r.id <= gapAfterId);
  return (
    <div className="card" style={{ overflow: "hidden" }}>
      <div style={{ maxHeight: 560, overflow: "auto" }}>
        <table className="tbl">
          <caption className="sr-only">Interactions, newest first</caption>
          <thead>
            <tr>
              <th>id</th>
              <th>time</th>
              <th>agent</th>
              <th>session</th>
              <th>status</th>
              <th>signals</th>
              <th className="right">latency</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <Fragment key={r.id}>
                {/* The gap sits between the rows it separates, not above the
                    table. Without it, two rows that are adjacent on screen but
                    hours apart read as consecutive traffic. */}
                {i === gapIndex && gap.kind === "unresolved" && (
                  <tr className="gap-row">
                    <td colSpan={7}>
                      <span aria-hidden="true">⚠ </span>
                      <strong>Unresolved gap</strong> ·{" "}
                      <span className="mono">{describeGapRange(gap)}</span> · the feed dropped
                      and bounded recovery could not prove it caught up. Requests in this
                      range may exist and are not shown.
                    </td>
                  </tr>
                )}
              <tr
                className={
                  (r.id === selectedId ? "sel " : "") + (r.id === flashId ? "flash" : "")
                }
              >
                <td className="mono muted">
                  {/* A button, not a click handler on the row: a selectable row
                      must be reachable and operable from the keyboard. */}
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm mono"
                    aria-pressed={r.id === selectedId}
                    onClick={() => onSelect(r.id)}
                  >
                    {r.id}
                  </button>
                </td>
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
                  {r.session_id ? (
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm mono"
                      title={`Show only ${r.session_id}`}
                      onClick={() => onSession(r.session_id!)}
                    >
                      {shortId(r.session_id)}
                    </button>
                  ) : (
                    <span className="muted">—</span>
                  )}
                </td>
                <td>
                  <StatusBadge row={r} />
                </td>
                <td>
                  <Signals row={r} />
                </td>
                <td className="right mono">{r.latency_ms}ms</td>
              </tr>
              </Fragment>
            ))}
            {/* A gap older than every row on screen still has to be visible, so
                it lands at the bottom rather than being dropped. */}
            {gap.kind === "unresolved" && gapIndex === -1 && (
              <tr className="gap-row">
                <td colSpan={7}>
                  <span aria-hidden="true">⚠ </span>
                  <strong>Unresolved gap</strong> ·{" "}
                  <span className="mono">{describeGapRange(gap)}</span> · the feed dropped and
                  bounded recovery could not prove it caught up. Requests in this range may
                  exist and are not shown.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StatusBadge({ row }: { row: InteractionLog }) {
  // A pipeline node never had an HTTP response, so it has no status — and a
  // dash here means "not applicable", not "we lost it". Rendering it red
  // alongside real 5xx rows would invent a failure.
  if (row.source === "pipeline") {
    return (
      <span className="badge badge-outline" title="no HTTP response — this was a pipeline node">
        n/a
      </span>
    );
  }
  const status = row.response_status ?? 0;
  const ok = status >= 200 && status < 400;
  return (
    <span className={"badge " + (ok ? "badge-ok" : "badge-destructive")}>
      {/* Text, not colour alone. */}
      {status || "—"}
    </span>
  );
}

/** Non-colour signals an operator needs at a glance: was a fault injected, did
 * it error, is the captured body clipped. */
function Signals({ row }: { row: InteractionLog }) {
  const signals: string[] = [];
  // A pipeline node is a real interaction but not an HTTP one, and its empty
  // method/path/status would otherwise read as missing data rather than as
  // "there was never a request here".
  if (row.source === "pipeline") signals.push("pipeline");
  if (row.chaos_action) signals.push(`chaos:${row.chaos_action}`);
  if (row.error) signals.push("error");
  if (row.truncated) signals.push("truncated");
  if (row.streaming) signals.push("stream");
  if (row.tool_calls_count) signals.push(`tools:${row.tool_calls_count}`);
  if (signals.length === 0) return <span className="muted">—</span>;
  return (
    <div className="row gap-2" style={{ flexWrap: "wrap" }}>
      {signals.map((s) => (
        <span className="tag" key={s}>
          {s}
        </span>
      ))}
    </div>
  );
}

function LogDetail({
  row,
  revealBodies,
  bodies,
  revealState,
  onToggleBodies,
  onSession,
}: {
  row: InteractionLog | null;
  revealBodies: boolean;
  bodies?: { request?: string; response?: string };
  revealState: "idle" | "loading" | "failed";
  onToggleBodies: () => void;
  onSession: (id: string) => void;
}) {
  if (!row) {
    return (
      <div className="card card-pad">
        <p className="txt-sm muted">Select a request to inspect it.</p>
      </div>
    );
  }
  return (
    <div className="card card-pad col gap-3">
      <div className="row gap-2">
        <div className="grow">
          <div className="eyebrow">request {row.id}</div>
        </div>
        <Link href={`/logs/${row.id}`} className="btn btn-outline btn-sm">
          Open
        </Link>
      </div>

      <dl className="kv">
        <dt>agent</dt>
        <dd className="mono">{row.agent_name || "—"}</dd>
        <dt>session</dt>
        <dd className="mono">
          {row.session_id ? (
            <button type="button" className="btn btn-ghost btn-sm mono" onClick={() => onSession(row.session_id!)}>
              {row.session_id}
            </button>
          ) : (
            "—"
          )}
        </dd>
        <dt>scenario</dt>
        <dd className="mono">{row.scenario_name || "—"}</dd>
        <dt>protocol</dt>
        <dd className="mono">{row.protocol || "—"}</dd>
        <dt>source</dt>
        <dd>
          {row.source === "pipeline" ? (
            <span className="badge badge-info" title="one node of a pipeline run">
              pipeline run
            </span>
          ) : (
            <span className="mono">HTTP request</span>
          )}
        </dd>
        <dt>latency</dt>
        <dd className="mono">{row.latency_ms}ms</dd>
        {/* U4-5: stated either way. This is the one field on the pane whose
            absence would otherwise carry meaning — "no warning" is a weaker
            thing to read than "complete". */}
        <dt>capture</dt>
        <dd>
          {row.truncated ? (
            <span className="badge badge-warn">clipped at the capture cap</span>
          ) : (
            <span className="badge badge-ok">complete</span>
          )}
        </dd>
      </dl>

      {row.error && (
        <div className="banner banner-error" role="note">
          <strong>Engine error.</strong> <span className="mono">{row.error}</span>
        </div>
      )}

      {row.chaos_action && (
        <div className="banner banner-warn" role="note">
          <strong>Fault injected: {row.chaos_action}.</strong> This response was shaped by
          chaos configuration, not by the scenario alone.
          <dl className="kv" style={{ marginTop: 6 }}>
            <dt>source</dt>
            <dd className="mono">{row.chaos_source ?? "unknown"}</dd>
            {row.chaos_seed !== undefined && (
              <>
                <dt>seed</dt>
                <dd className="mono">{row.chaos_seed}</dd>
              </>
            )}
            {row.chaos_rate !== undefined && (
              <>
                <dt>rate</dt>
                <dd className="mono">{row.chaos_rate}</dd>
              </>
            )}
          </dl>
        </div>
      )}

      {row.truncated && (
        <div className="banner banner-warn" role="note">
          <strong>Captured body is clipped.</strong> It exceeded the capture cap, so what is
          stored is not the complete payload. Do not treat it as an exact record.
        </div>
      )}

      <div>
        <button type="button" className="btn btn-outline btn-sm" onClick={onToggleBodies}>
          {revealBodies ? "Hide bodies" : "Show request/response bodies"}
        </button>
        <p className="hint" style={{ marginTop: 6 }}>
          Bodies are not fetched until revealed — this listing asked the server for
          metadata only, so nothing captured here has left it yet. They can contain
          whatever a client sent.
        </p>
      </div>

      {revealBodies && revealState === "loading" && (
        <p className="txt-sm muted" role="status">
          Fetching this request&apos;s bodies…
        </p>
      )}
      {revealBodies && revealState === "failed" && (
        <div className="banner banner-error" role="alert">
          <strong>Could not fetch the bodies.</strong> They were not retrieved, which is
          not the same as this request having had none.
        </div>
      )}
      {revealBodies && revealState === "idle" && (
        <>
          <Body label="request" text={bodies?.request} />
          <Body label="response" text={bodies?.response} />
        </>
      )}
    </div>
  );
}

function Body({ label, text }: { label: string; text?: string }) {
  return (
    <div>
      <div className="eyebrow mb-3">{label}</div>
      {/* Rendered as text, never as markup: log content is untrusted. */}
      <pre className="mono" style={{ fontSize: 11.5, overflowX: "auto", maxHeight: 220 }}>
        {text && text.length > 0 ? text : "(empty)"}
      </pre>
    </div>
  );
}

function Pager({
  window: win,
  filters,
  onApply,
}: {
  window: LogWindow;
  filters: LogFilters;
  onApply: (next: Partial<LogFilters>) => void;
}) {
  const canPrev = filters.offset > 0;
  if (!canPrev && !win.mayHaveMore) return null;
  return (
    <div className="row gap-2 mt-4" style={{ alignItems: "center" }}>
      <button
        type="button"
        className="btn btn-outline btn-sm"
        disabled={!canPrev}
        onClick={() => onApply({ offset: Math.max(0, filters.offset - filters.limit) })}
      >
        Newer
      </button>
      <button
        type="button"
        className="btn btn-outline btn-sm"
        disabled={!win.mayHaveMore}
        onClick={() => onApply({ offset: filters.offset + filters.limit })}
      >
        Older
      </button>
      <span className="muted txt-sm">
        offset {filters.offset}
        {win.mayHaveMore ? "" : " · end of this window"}
      </span>
    </div>
  );
}

function shortId(id: string): string {
  return id.length > 10 ? id.slice(0, 10) + "…" : id;
}

function fmtTime(ts: string): string {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  return d.toLocaleTimeString();
}
