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

export type GapState =
  | { kind: "none" }
  | { kind: "recovering" }
  /** The feed dropped and recovery proved it caught everything. */
  | { kind: "closed"; recovered: number }
  /** The feed dropped and we CANNOT prove nothing was missed. */
  | { kind: "unresolved"; reason: string };

export interface RecoveryOutcome {
  rows: InteractionLog[];
  gap: GapState;
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
  lastSeenId: number | null,
): RecoveryOutcome {
  if (fetched.length >= limit) {
    return {
      rows: fetched,
      gap: {
        kind: "unresolved",
        reason:
          `More than ${limit} requests arrived while the feed was disconnected, which is as far ` +
          `back as recovery can reach. Some are missing from this view. Reload with a time filter ` +
          `to see the full period.`,
      },
    };
  }
  if (lastSeenId == null) {
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
  since: string;
  until: string;
  limit: number;
  offset: number;
}

export const DEFAULT_FILTERS: LogFilters = {
  agent: "",
  session_id: "",
  since: "",
  until: "",
  limit: 100,
  offset: 0,
};

const ALLOWED_KEYS: ReadonlyArray<keyof LogFilters> = [
  "agent",
  "session_id",
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
  return Boolean(filters.agent || filters.session_id || filters.since || filters.until || filters.offset);
}
