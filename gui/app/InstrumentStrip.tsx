// UX-02: the instrument strip — persistent server context on every screen.
//
// From the approved design (hybrid direction: Worksheet body + instrument
// strip). Its job is that nobody has to guess which server they are looking at,
// whether it can actually serve, or how old the data is.
//
// Every cell is real. The design prototype hardcoded localhost:8080, engine
// v0.4.2 and acme-prod; those are fixtures. A value this component cannot
// establish renders as "unknown", never as a plausible-looking placeholder —
// a wrong engine version is worse than an absent one.
//
// This is a server component: it renders from data the page already fetched,
// so it adds no client JavaScript.

import {
  describeRefresh,
  livenessLabel,
  readinessLabel,
  readinessTone,
  type ServerStatus,
} from "@/lib/serverState";

export interface InstrumentStripProps {
  status: ServerStatus;
  /** Base URL of the server being inspected. */
  apiUrl: string;
  /** Server-reported role, or null when unknown/local. */
  role: string | null;
  tenantId: string | null;
  /** "local" | "multi_tenant" | null when identity could not be read. */
  mode: string | null;
}

function Cell({
  label,
  children,
  grow,
}: {
  label: string;
  children: React.ReactNode;
  grow?: boolean;
}) {
  return (
    <div className={"cell" + (grow ? " grow" : "")}>
      <span className="l">{label}</span>
      <span className="v">{children}</span>
    </div>
  );
}

export function InstrumentStrip({ status, apiUrl, role, tenantId, mode }: InstrumentStripProps) {
  const live = status.liveness === "process-up";
  const tone = readinessTone(status.readiness);

  return (
    // The strip scrolls horizontally at narrow widths (the design is explicit
    // that cells scroll rather than being dropped). A scrollable region with no
    // focusable content inside it is unreachable by keyboard — there is nothing
    // to Tab to and therefore no way to scroll it without a pointer. Making the
    // region itself focusable is the fix: arrow keys then scroll it, and the
    // status role plus label still describe what was reached.
    <div className="strip" role="status" aria-label="server context" tabIndex={0}>
      <Cell label="server">
        <span className="mono">{apiUrl}</span>
      </Cell>

      <Cell label="liveness">
        {/* Dot AND text: status is never conveyed by colour alone. */}
        <span className={live ? "ok" : "bad"}>
          <span className={"sdot " + (live ? "ok" : "bad")} aria-hidden="true" />
          {livenessLabel(status.liveness)}
        </span>
      </Cell>

      <Cell label="readiness">
        <span className={tone === "none" ? undefined : tone}>
          {tone !== "none" && <span className={"sdot " + tone} aria-hidden="true" />}
          {readinessLabel(status.readiness)}
        </span>
      </Cell>

      <Cell label="tenant / role">
        {/* Local mode is named explicitly. It is not a signed-in viewer, and a
            UI that blurs the two shows access control where there is none. */}
        {mode === "local"
          ? "local mode · unauthenticated"
          : role
            ? `${tenantId ?? "—"} · ${role}`
            : "unknown"}
      </Cell>

      {/* Release A executes against the live runtime; the strip says so
          everywhere, not only on the run button. */}
      <Cell label="exec mode">
        <span className="warn">ACTIVE CONFIG</span>
      </Cell>

      <Cell label="revision">per-resource</Cell>

      <Cell label="engine">{status.version ?? "unknown"}</Cell>

      <Cell label="last refresh" grow>
        <span className={status.stale ? "warn" : undefined}>
          {describeRefresh(status.checkedAt, new Date(), status.stale)}
        </span>
      </Cell>
    </div>
  );
}

/** Shown under the strip when the server cannot be reached. Says what is frozen
 * and what is disabled, and explicitly does not retry stateful actions. */
export function OfflineBar({ status }: { status: ServerStatus }) {
  if (status.liveness !== "unreachable") return null;
  return (
    <div className="offline-bar" role="alert">
      <b>Server unreachable.</b>
      <span>
        Showing whatever was already loaded. Reads are frozen; writes and runs are
        disabled. Nothing stateful is retried automatically.
      </span>
      <span className="when">last attempt {status.checkedAt.slice(11, 19)}Z</span>
    </div>
  );
}
