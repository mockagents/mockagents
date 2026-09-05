// Live-feed bookkeeping for the request explorer (UX-04).
//
// Kept out of the component because these are the parts that are easy to get
// subtly wrong and hard to see: a reconnect replaying rows already on screen, a
// selected row silently evicted by the row cap, a gap in the feed that nobody
// tells the operator about. Each is a correctness rule, so each is a pure
// function with tests rather than an effect buried in a hook.

import type { InteractionLog } from "./api";

/** Rows are newest-first everywhere in the console. */
function byIdDesc(a: InteractionLog, b: InteractionLog): number {
  return b.id - a.id;
}

export interface MergeOptions {
  /** Hard cap on retained rows, so a long-running tab cannot grow forever. */
  max: number;
  /** Row the operator has selected. It is never evicted by the cap: losing the
   * thing being read because unrelated traffic arrived is the single most
   * annoying way for a live feed to behave. */
  pinnedId?: number | null;
}

/** Merge incoming rows into the current list: newest first, no duplicates,
 * capped — with the selected row protected from eviction.
 *
 * Deduplication is by id and is not optional. A reconnect re-delivers whatever
 * the server still holds, so without it every drop would visibly duplicate
 * rows, and an operator counting requests would count them twice. */
export function mergeRows(
  current: InteractionLog[],
  incoming: InteractionLog[],
  { max, pinnedId }: MergeOptions,
): InteractionLog[] {
  const byId = new Map<number, InteractionLog>();
  // Later writes win, so a re-delivered row refreshes rather than duplicates.
  for (const row of current) byId.set(row.id, row);
  for (const row of incoming) byId.set(row.id, row);

  const sorted = [...byId.values()].sort(byIdDesc);
  if (sorted.length <= max) return sorted;

  const kept = sorted.slice(0, max);
  if (pinnedId != null && !kept.some((r) => r.id === pinnedId)) {
    const pinned = sorted.find((r) => r.id === pinnedId);
    // Drop the oldest kept row to make room, so the cap still holds.
    if (pinned) return [...kept.slice(0, max - 1), pinned];
  }
  return kept;
}

/** Highest id currently known, or null when there is nothing. Used as the
 * "resume from here" marker after a disconnect. */
export function highestId(rows: InteractionLog[]): number | null {
  let best: number | null = null;
  for (const row of rows) if (best == null || row.id > best) best = row.id;
  return best;
}

/** The newest row known, as the resume marker. Carries the timestamp as well as
 * the id so a gap discovered later can be described in wall-clock terms — the
 * id alone means nothing to the person reading it. */
export function newestSeen(rows: InteractionLog[]): LastSeen | null {
  let best: InteractionLog | null = null;
  for (const row of rows) if (best === null || row.id > best.id) best = row;
  return best === null ? null : { id: best.id, timestamp: best.timestamp };
}

export type GapState =
  | { kind: "none" }
  | { kind: "recovering" }
  /** The feed dropped and recovery proved it caught everything. */
  | { kind: "closed"; recovered: number }
  /** The feed dropped and we CANNOT prove nothing was missed.
   *
   * The bounds matter as much as the fact: an operator investigating an
   * incident needs to know whether the hole overlaps the window they care
   * about, and "some requests are missing" cannot answer that. Both ends are
   * known locally — the last row seen before the drop, and the oldest row
   * recovery managed to reach — so neither is guessed. */
  | {
      kind: "unresolved";
      reason: string;
      /** Timestamp of the last row seen BEFORE the drop. The gap starts after
       * this row; undefined when nothing had been seen. */
      from?: string;
      /** Timestamp of the oldest row recovery reached. The gap ends before this
       * row; undefined when recovery returned nothing. */
      to?: string;
      /** Id of the row the gap sits immediately above in a newest-first list.
       * The table uses it to place the gap between the rows it separates,
       * rather than in a banner that scrolls away from them. */
      afterId?: number;
    };

export interface RecoveryOutcome {
  rows: InteractionLog[];
  gap: GapState;
}

/** The last row seen before a disconnect. The timestamp is carried alongside
 * the id because the id alone cannot describe a gap to a human. */
export interface LastSeen {
  id: number;
  timestamp: string;
}

/** Newest and oldest of a newest-first page, tolerating an unsorted input. */
function oldestOf(rows: InteractionLog[]): InteractionLog | null {
  let oldest: InteractionLog | null = null;
  for (const row of rows) if (oldest === null || row.id < oldest.id) oldest = row;
  return oldest;
}

/** Human range for a gap, e.g. "13:58:41Z – 14:20:06Z". Ends that are not known
 * are named as unknown rather than filled in with a plausible time. */
export function describeGapRange(gap: { from?: string; to?: string }): string {
  const clock = (iso?: string): string => {
    if (!iso) return "unknown";
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? "unknown" : d.toISOString().slice(11, 19) + "Z";
  };
  if (!gap.from && !gap.to) return "time range unknown";
  return `${clock(gap.from)} – ${clock(gap.to)}`;
}

/** Interpret a bounded recovery fetch made after a reconnect.
 *
 * `fetched` are rows newer than the last id seen before the drop, capped at
 * `limit`. If the fetch came back FULL, there may be more rows beyond the cap
 * that we will never see — the gap is unresolved, and saying "caught up" would
 * be a lie. A short page means we reached the last-seen row and the feed is
 * continuous again.
 *
 * This is a bounded best effort by design: the API pages by offset and has no
 * cursor, so guaranteed recovery is not available (epic UX-04 lists a stable
 * cursor as NEW work). The honest outcome is therefore sometimes "unknown". */
export function interpretRecovery(
  fetched: InteractionLog[],
  limit: number,
  lastSeen: LastSeen | null,
): RecoveryOutcome {
  if (fetched.length >= limit) {
    // The hole lies between the last row seen before the drop and the oldest
    // row recovery could reach. Both ends are known here, so the gap is
    // reported with them rather than as an unbounded "some are missing".
    const oldest = oldestOf(fetched);
    return {
      rows: fetched,
      gap: {
        kind: "unresolved",
        reason:
          `More than ${limit} requests arrived while the feed was disconnected, which is as far ` +
          `back as recovery can reach. Some are missing from this view. Reload with a time filter ` +
          `to see the full period.`,
        from: lastSeen?.timestamp,
        to: oldest?.timestamp,
        afterId: lastSeen?.id,
      },
    };
  }
  if (lastSeen == null) {
    // Nothing had been seen before the drop, so there is no gap to speak of.
    return { rows: fetched, gap: { kind: "none" } };
  }
  return { rows: fetched, gap: { kind: "closed", recovered: fetched.length } };
}

/** Filters that are safe to put in a shareable URL.
 *
 * Deliberately excludes anything derived from a request or response body, and
 * any credential: a saved or pasted filter link must not carry payloads or
 * secrets (epic §5). Everything here is metadata an operator would type. */
export interface LogFilters {
  agent: string;
  session_id: string;
  /** Narrow to every session id starting with this value (§9.3). A pipeline run
   * scopes its nodes as "<session>::<pipeline>::<node>", so the id a run
   * reports matches nothing under equality — this is how a run's evidence is
   * reachable from the run itself. Metadata, so it is URL-safe. */
  session_prefix: string;
  since: string;
  until: string;
  limit: number;
  offset: number;
}

export const DEFAULT_FILTERS: LogFilters = {
  agent: "",
  session_id: "",
  session_prefix: "",
  since: "",
  until: "",
  limit: 100,
  offset: 0,
};

const ALLOWED_KEYS: ReadonlyArray<keyof LogFilters> = [
  "agent",
  "session_id",
  "session_prefix",
  "since",
  "until",
  "limit",
  "offset",
];

function clampInt(raw: string | null, fallback: number, min: number, max: number): number {
  const n = Number.parseInt(raw ?? "", 10);
  if (!Number.isFinite(n)) return fallback;
  return Math.min(max, Math.max(min, n));
}

/** Read filters from URL search params, ignoring anything not allowlisted. */
export function parseFilters(params: URLSearchParams | Record<string, string | undefined>): LogFilters {
  const get = (key: string): string =>
    params instanceof URLSearchParams ? (params.get(key) ?? "") : (params[key] ?? "");
  return {
    agent: get("agent").trim(),
    session_id: get("session_id").trim(),
    session_prefix: get("session_prefix").trim(),
    since: get("since").trim(),
    until: get("until").trim(),
    limit: clampInt(get("limit") || null, DEFAULT_FILTERS.limit, 1, 1000),
    offset: clampInt(get("offset") || null, 0, 0, 1_000_000),
  };
}

/** Serialize filters back to a query string, omitting defaults so a plain link
 * stays short and readable. Only allowlisted keys are ever emitted. */
export function filtersToQuery(filters: LogFilters): string {
  const params = new URLSearchParams();
  for (const key of ALLOWED_KEYS) {
    const value = filters[key];
    if (key === "limit") {
      if (value !== DEFAULT_FILTERS.limit) params.set(key, String(value));
      continue;
    }
    if (key === "offset") {
      if (value !== 0) params.set(key, String(value));
      continue;
    }
    if (value) params.set(key, String(value));
  }
  return params.toString();
}

/** True when any filter narrows the view — used to explain an empty result as
 * "your filter matched nothing" rather than "there is no traffic". */
export function isFiltered(filters: LogFilters): boolean {
  return Boolean(
    filters.agent ||
      filters.session_id ||
      filters.session_prefix ||
      filters.since ||
      filters.until ||
      filters.offset,
  );
}
