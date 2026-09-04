import Link from "next/link";

import {
  APIError,
  getCosts,
  getIdentity,
  getServerStatus,
  listLogWindow,
  type CostsResponse,
  type Identity,
} from "@/lib/api";
import { buildReport, type BuildReportInput } from "@/lib/report";

import { InstrumentStrip, OfflineBar } from "../InstrumentStrip";
import { ReportExport } from "./ReportExport";

export const dynamic = "force-dynamic";

// UX-06: reports you can trust, v1.
//
// This page exports what is already loaded and already authorized — a bounded
// local snapshot. It is deliberately unable to claim more than that: there is
// no server-generated report behind it, no complete-history window, and no
// redaction policy. Everything it does NOT know renders as "unknown".
//
// The scope shown on screen and the file that downloads are built from the same
// pure module (lib/report.ts), so the caveats printed here are the caveats in
// the file.

const WINDOWS: { id: string; label: string; hours: number }[] = [
  { id: "1h", label: "1h", hours: 1 },
  { id: "24h", label: "24h", hours: 24 },
  { id: "7d", label: "7d", hours: 24 * 7 },
];

// The row cap for the evidence read. Bounded on purpose, and stated on screen —
// a report that quietly pulls "everything" is the one that lies about its window.
const ROW_LIMIT = 500;
// The scan cap sent to /api/v1/costs. The API caps its own scan and returns no
// truncation flag, so the GUI must remember what it asked for in order to say
// whether the estimate may be partial.
const COST_SCAN_LIMIT = 1000;

export default async function ReportsPage({
  searchParams,
}: {
  searchParams: Promise<{ window?: string }>;
}) {
  const { window: windowParam } = await searchParams;
  const win = WINDOWS.find((w) => w.id === windowParam) ?? WINDOWS[1]; // default 24h
  const since = new Date(Date.now() - win.hours * 3_600_000).toISOString();

  const status = await getServerStatus();
  const apiUrl = process.env.MOCKAGENTS_API_URL ?? "http://localhost:8080";

  let identity: Identity | null = null;
  try {
    identity = await getIdentity();
  } catch {
    identity = null;
  }

  // Each read stands alone. A cost failure must not make the evidence look
  // empty, and an evidence failure must not be reported as "no interactions".
  let logs: Awaited<ReturnType<typeof listLogWindow>> | null = null;
  let logsError: string | null = null;
  try {
    logs = await listLogWindow({ since, limit: ROW_LIMIT });
  } catch (err) {
    logsError =
      err instanceof APIError ? `The server returned ${err.status}.` : "The server could not be reached.";
  }

  let costs: CostsResponse | null = null;
  let costsUnavailable = false;
  try {
    costs = await getCosts({ since, limit: COST_SCAN_LIMIT });
    costsUnavailable = costs === null;
  } catch {
    costsUnavailable = true;
  }

  const input: Omit<BuildReportInput, "includeBodies"> | null = logs
    ? {
        rows: logs.rows,
        window: { since, until: null, limit: logs.limit, offset: logs.offset },
        mayHaveMore: logs.mayHaveMore,
        pagingStable: logs.stable,
        costs,
        costScanLimit: COST_SCAN_LIMIT,
        source: apiUrl,
        serverVersion: status.version,
        tenantId: identity?.tenant_id ?? null,
        mode: identity?.mode ?? null,
        role: identity?.role ?? null,
      }
    : null;

  // Built with bodies excluded purely to render the on-screen scope. The
  // download rebuilds with whatever the reviewer armed.
  const preview = input ? buildReport({ ...input, includeBodies: false }) : null;

  return (
    <div>
      <InstrumentStrip
        status={status}
        apiUrl={apiUrl}
        role={identity?.role ?? null}
        tenantId={identity?.tenant_id ?? null}
        mode={identity?.mode ?? null}
      />
      <OfflineBar status={status} />

      <div style={{ padding: "20px 0 0" }}>
        <div className="head-row page-head">
          <div className="grow">
            <h1 className="page-title">Reports</h1>
            <p className="page-lede">
              Export the evidence this console has already loaded, as JSON or a printable
              document. This is a bounded local snapshot — not a server-attested report,
              and not proof of what the server retained.
            </p>
          </div>
          <div className="row gap-2">
            {WINDOWS.map((w) => (
              <Link
                key={w.id}
                href={`/reports?window=${w.id}`}
                className={"pill" + (w.id === win.id ? " active" : "")}
              >
                {w.label}
              </Link>
            ))}
          </div>
        </div>

        {logsError && (
          <div className="banner banner-error" role="alert">
            <div>
              <strong>Evidence could not be read.</strong> {logsError} Nothing is exported
              from a failed read — this is unknown, not an empty window.
            </div>
          </div>
        )}

        {preview && input && (
          <div
            className="grid mt-4"
            style={{ gridTemplateColumns: "1.15fr .85fr", alignItems: "start", gap: 16 }}
          >
            <div className="col gap-4">
              <div className="card">
                <div className="card-head">
                  <h3>Export scope</h3>
                  <span className="sid">UX-06 · loaded snapshot</span>
                </div>
                <div className="card-pad col gap-3">
                  <dl className="kv">
                    <dt>window</dt>
                    <dd className="mono">
                      {preview.window.since} → now (last {win.label})
                    </dd>
                    <dt>interactions loaded</dt>
                    <dd className="mono">
                      {preview.window.rowsLoaded} of a {preview.window.requestedLimit}-row read
                    </dd>
                    <dt>source</dt>
                    <dd className="mono">{preview.provenance.source}</dd>
                    <dt>server version</dt>
                    <dd className="mono">{preview.provenance.serverVersion ?? "unknown"}</dd>
                    <dt>tenant / role</dt>
                    <dd className="mono">
                      {preview.provenance.mode === "local"
                        ? "local mode · unauthenticated"
                        : `${preview.provenance.tenantId ?? "unknown"} · ${preview.provenance.role ?? "unknown"}`}
                    </dd>
                    <dt>schema</dt>
                    <dd className="mono">{preview.provenance.schema}</dd>
                    <dt>redaction policy</dt>
                    <dd className="mono">none applied</dd>
                  </dl>

                  <ReportExport input={input} />
                </div>
              </div>

              <div className="card">
                <div className="card-head">
                  <h3>Completeness and omissions</h3>
                  <span className="muted txt-sm">
                    {preview.omissions.length} stated in the export
                  </span>
                </div>
                <div className="card-pad">
                  <ul className="omissions">
                    {preview.omissions.map((o) => (
                      <li key={o.code}>
                        <code className="mono">{o.code}</code>
                        <span> {o.detail}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              </div>
            </div>

            <div className="col gap-4">
              <div className="card">
                <div className="card-head">
                  <h3>Estimated upstream cost</h3>
                  <span className="sid">UX-06 · estimate</span>
                </div>
                <div className="card-pad col gap-3">
                  {preview.cost === null ? (
                    <div className="banner banner-warn" role="note">
                      <div>
                        {costsUnavailable ? (
                          <>
                            <strong>Cost aggregation is not available. </strong>
                            It needs the interaction log store. This export makes no cost
                            claim — unknown, not zero.
                          </>
                        ) : (
                          <>
                            <strong>No cost aggregate was loaded. </strong>
                            This export makes no cost claim — unknown, not zero.
                          </>
                        )}
                      </div>
                    </div>
                  ) : (
                    <>
                      <div className="row" style={{ alignItems: "baseline", gap: 8 }}>
                        <span
                          className="mono"
                          style={{ fontSize: 24, fontWeight: 650 }}
                        >
                          ${preview.cost.estimatedUpstreamUSD.toFixed(4)}
                        </span>
                        <span className="muted txt-sm">
                          estimated upstream cost for captured requests
                        </span>
                      </div>
                      <p className="txt-sm muted" style={{ margin: 0 }}>
                        What these requests <em>would have</em> cost against a real
                        provider. Not spend, not verified savings.
                      </p>
                      <dl className="kv">
                        <dt>requests scanned</dt>
                        <dd className="mono">
                          {preview.cost.requestsScanned} of a {preview.cost.scanLimit}-row cap
                        </dd>
                        <dt>partial sum</dt>
                        <dd>
                          {preview.cost.scanCapMayHaveBeenReached
                            ? "possibly — the scan reached its cap"
                            : "no cap reached"}
                        </dd>
                        <dt>unpriced requests</dt>
                        <dd className="mono">
                          {preview.cost.unpricedRequests}
                          {preview.cost.unpricedRequests > 0 ? " (unknown, not $0)" : ""}
                        </dd>
                        <dt>pricing version</dt>
                        <dd className="mono">unknown</dd>
                      </dl>
                    </>
                  )}
                </div>
              </div>

              <div className="card">
                <div className="card-head">
                  <h3>Not in Release A</h3>
                </div>
                <div className="card-pad txt-sm muted">
                  Server-attested reports, complete-history windows, scheduled delivery and
                  PDF output are later capabilities. This page never presents the loaded
                  snapshot as a complete record.
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
