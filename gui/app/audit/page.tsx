import Link from "next/link";

import { APIError, AuditEvent, listAudit } from "@/lib/api";

export const dynamic = "force-dynamic";

// The audit log emits exactly these eight event kinds (see internal/audit/).
const KINDS = [
  "tenant.created",
  "tenant.deleted",
  "api_key.created",
  "api_key.deleted",
  "api_key.rotated",
  "api_key.role_changed",
  "agent.reloaded",
  "auth.denied",
];

const VARIANT: Record<string, string> = {
  "tenant.created": "success",
  "tenant.deleted": "destructive",
  "api_key.created": "success",
  "api_key.deleted": "destructive",
  "api_key.rotated": "info",
  "api_key.role_changed": "warning",
  "agent.reloaded": "secondary",
  "auth.denied": "destructive",
};

export default async function AuditPage({
  searchParams,
}: {
  searchParams: Promise<{ kind?: string }>;
}) {
  const { kind: kindParam } = await searchParams;
  const kind = kindParam && KINDS.includes(kindParam) ? kindParam : "all";

  let events: AuditEvent[] | null = null;
  let error: string | null = null;
  try {
    events = await listAudit({ limit: 200, kind: kind === "all" ? undefined : kind });
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <div className="page-head">
        <h1 className="page-title">Audit log</h1>
        <p className="page-lede">
          Append-only record of every control-plane mutation. Always on — written to{" "}
          <code>.mockagents-audit.db</code>. Plaintext keys are never recorded.
        </p>
      </div>

      {error && (
        <div className="banner banner-error">
          <strong>Could not load audit events.</strong> {error}
        </div>
      )}

      {!error && events === null && (
        <div className="banner banner-warn">
          <strong>Admin access required.</strong> The audit endpoint is gated behind the admin role
          in multi-tenant mode. <Link href="/login">Switch keys</Link>.
        </div>
      )}

      {events && (
        <>
          <div className="row wrap gap-2 mb-4">
            <Link href="/audit" className={"pill" + (kind === "all" ? " active" : "")}>
              all
            </Link>
            {KINDS.map((k) => (
              <Link key={k} href={`/audit?kind=${k}`} className={"pill" + (kind === k ? " active" : "")}>
                {k}
              </Link>
            ))}
          </div>

          {events.length === 0 ? (
            <div className="empty">No audit events match this filter.</div>
          ) : (
            <div className="card" style={{ overflow: "hidden" }}>
              <table className="tbl">
                <thead>
                  <tr>
                    <th>event</th>
                    <th>actor</th>
                    <th>target</th>
                    <th>details</th>
                    <th className="right">when</th>
                  </tr>
                </thead>
                <tbody>
                  {events.map((e) => (
                    <tr key={e.id}>
                      <td>
                        <span className={`badge badge-${VARIANT[e.kind] ?? "secondary"}`}>
                          <span className="badge-dot" />
                          {e.kind}
                        </span>
                      </td>
                      <td>
                        <ActorCell actor={e.actor} />
                      </td>
                      <td className="mono" style={{ fontSize: 12 }}>
                        {e.target || "—"}
                      </td>
                      <td className="muted mono" style={{ fontSize: 11.5 }}>
                        <Details details={e.details} />
                      </td>
                      <td className="num muted nowrap">{fmtWhen(e.timestamp)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function ActorCell({ actor }: { actor: AuditEvent["actor"] }) {
  if (!actor || actor.name === "anonymous") {
    return <span className="muted">anonymous</span>;
  }
  const sub = [actor.role ?? "—", actor.remote_ip].filter(Boolean).join(" · ");
  return (
    <div className="col" style={{ gap: 0 }}>
      <span style={{ fontWeight: 500 }}>{actor.name}</span>
      <span className="muted mono" style={{ fontSize: 10.5 }}>
        {sub}
      </span>
    </div>
  );
}

function Details({ details }: { details: string | undefined }) {
  if (!details) return <span className="muted">—</span>;
  // details is a JSON blob (audit.MarshalDetails). Flatten to k=v pairs when it
  // parses; otherwise show the raw string so nothing is silently dropped.
  try {
    const parsed = JSON.parse(details) as Record<string, unknown>;
    const entries = Object.entries(parsed);
    if (entries.length === 0) return <span className="muted">—</span>;
    return <>{entries.map(([k, v]) => `${k}=${String(v)}`).join("  ")}</>;
  } catch {
    return <>{details}</>;
  }
}

function fmtWhen(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
