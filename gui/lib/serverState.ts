// Connection and readiness state for the instrument strip (UX-02).
//
// The design handoff fixes these names exactly (epic §10, handoff §3):
//
//   connection: process-up · ready · not-ready · unreachable · stale
//
// They are distinct on purpose. "Not ready" is a server that is running but
// cannot serve a mock — a broken agents directory, a dead log store. Rendering
// that as "offline", or as an empty catalog, sends someone to restart a process
// that is fine. And readiness cannot be *known* while the server is
// unreachable, so that case is UNKNOWN rather than false.
//
// Pure and client-safe: lib/api.ts imports next/headers and cannot be pulled
// into a client component.

export type Liveness = "process-up" | "unreachable";
export type Readiness = "ready" | "not-ready" | "unknown";

export interface ReadinessCheck {
  name: string;
  status: "ok" | "failed";
  error?: string;
}

export interface ServerStatus {
  liveness: Liveness;
  readiness: Readiness;
  /** Per-check detail, when the server answered. Empty when unreachable. */
  checks: ReadinessCheck[];
  /** Engine version as the server reports it. null when unknown — never a
   * placeholder, because a wrong version is worse than an absent one. */
  version: string | null;
  /** When this snapshot was taken (ISO). */
  checkedAt: string;
  /** True when the data on screen predates the current check because the
   * server could not be reached. */
  stale: boolean;
}

/** Why the server is not ready, in one line, or null when it is. */
export function notReadyReason(status: ServerStatus): string | null {
  if (status.readiness !== "not-ready") return null;
  const failed = status.checks.filter((c) => c.status === "failed");
  if (failed.length === 0) {
    return "The server reports not-ready but did not say which check failed.";
  }
  return failed
    .map((c) => (c.error ? `${c.name}: ${c.error}` : `${c.name} failed`))
    .join("; ");
}

/** One-word label for the readiness cell. Unknown stays unknown. */
export function readinessLabel(readiness: Readiness): string {
  switch (readiness) {
    case "ready":
      return "READY";
    case "not-ready":
      return "NOT-READY";
    default:
      return "UNKNOWN";
  }
}

export function livenessLabel(liveness: Liveness): string {
  return liveness === "process-up" ? "PROCESS-UP" : "UNREACHABLE";
}

/** Severity for styling, and — because colour is never the only signal — the
 * label above always accompanies it. */
export function readinessTone(readiness: Readiness): "ok" | "warn" | "none" {
  if (readiness === "ready") return "ok";
  if (readiness === "not-ready") return "warn";
  return "none";
}

/** Human "4s ago" / "stale (61s)" for the last-refresh cell.
 *
 * Staleness is stated in seconds rather than hidden, because the whole point of
 * the cell is to let someone judge how much to trust what is on screen. */
export function describeRefresh(checkedAt: string, now: Date, stale: boolean): string {
  const then = new Date(checkedAt);
  if (Number.isNaN(then.getTime())) return "unknown";
  const secs = Math.max(0, Math.round((now.getTime() - then.getTime()) / 1000));
  const stamp = then.toISOString().slice(11, 19) + "Z";
  if (stale) return `${stamp} · stale (${secs}s)`;
  return `${stamp} · ${secs}s ago`;
}

/** What the first-run checklist should show. Derived from real state only —
 * an item is "done" because the server said so, never because a list was
 * non-empty for some other reason. */
export interface ChecklistItem {
  id: string;
  label: string;
  done: boolean;
  /** Shown when done, e.g. the time it was confirmed. */
  meta?: string;
  /** Offered when not done. */
  action?: { label: string; href: string };
  /** Why this cannot be judged right now. */
  unknown?: string;
}

export interface OnboardingInput {
  status: ServerStatus;
  agentCount: number;
  pipelineCount: number;
  /** Whether any interaction has ever been captured. */
  hasTraffic: boolean;
  /** Null when the catalog could not be read at all — which is NOT the same as
   * an empty install and must not be rendered as one. */
  catalogReadable: boolean;
}

export function buildChecklist(input: OnboardingInput): ChecklistItem[] {
  const { status, agentCount, pipelineCount, hasTraffic, catalogReadable } = input;
  const reachable = status.liveness === "process-up";

  const items: ChecklistItem[] = [
    {
      id: "connected",
      label: "Server connected and ready",
      done: reachable && status.readiness === "ready",
      meta: reachable && status.readiness === "ready" ? status.checkedAt.slice(11, 19) + "Z" : undefined,
      unknown: reachable ? undefined : "Cannot be confirmed while the server is unreachable.",
    },
  ];

  if (!catalogReadable) {
    // Everything below depends on reading the catalog. Saying "0 agents" here
    // would report a read failure as an empty install.
    items.push({
      id: "agent",
      label: "Load an agent",
      done: false,
      unknown: "The agent catalog could not be read, so this is unknown — not zero.",
    });
    return items;
  }

  items.push({
    id: "agent",
    label:
      agentCount > 0
        ? `Agent definitions loaded (${agentCount})`
        : "Add an agent YAML to your agents directory",
    done: agentCount > 0,
    action: agentCount > 0 ? { label: "Open catalog", href: "/" } : { label: "Editor", href: "/editor" },
  });

  items.push({
    id: "request",
    label: "Point an SDK at this server and send one request",
    done: hasTraffic,
    action: hasTraffic ? { label: "View logs", href: "/logs" } : undefined,
  });

  items.push({
    id: "pipeline",
    label:
      pipelineCount > 0
        ? "Run a pipeline against active configuration"
        : "Seed the documented starter pipeline (pipeline creation is not in Release A)",
    done: false,
    action:
      pipelineCount > 0
        ? { label: "View pipelines", href: "/pipelines" }
        : undefined,
  });

  return items;
}
