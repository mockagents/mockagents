// UX-06: reports you can trust, v1.
//
// Release A exports what the browser has ALREADY loaded and was already
// authorized to see. That is a bounded local snapshot — not a server-attested
// report, not a complete history, not proof of retention. Every claim this
// module makes is therefore hedged by construction:
//
//   - the window is stated, including the row limit that bounded it;
//   - anything the source did not supply (engine version, pricing version,
//     redaction policy) is `null` and rendered "unknown" — never synthesized;
//   - cost is labelled an ESTIMATE of upstream spend, never verified savings,
//     and never presented as a full-window total when the scan hit its cap;
//   - raw bodies are opt-in behind an explicit review of what would leave the
//     browser, and they are exported verbatim because this console applies no
//     redaction at all. Claiming a redaction policy we do not run would be the
//     most dangerous sentence in the product.
//
// The schema version belongs to this exporter, not to the server (epic §8).

import type { CostsResponse, InteractionLog } from "./api";

/** Schema version owned by the GUI exporter. Bump on any breaking change to
 * the JSON shape. It deliberately does not borrow the server's version. */
export const REPORT_SCHEMA = "mockagents.gui.report/1";

/** The window the evidence came from. Every field is what was ASKED FOR or
 * what came back — never an inferred total. */
export interface ReportWindow {
  /** RFC3339 lower bound, or null when the read was not time-bounded. */
  since: string | null;
  until: string | null;
  /** Row limit requested from the server. */
  requestedLimit: number;
  offset: number;
  /** Rows actually loaded into the browser. */
  rowsLoaded: number;
}

/** What is knowably missing. Never a quality score — just facts. */
export interface ReportCompleteness {
  /** Always true. This is a browser snapshot of already-loaded, already
   * authorized data. There is no server attestation behind it. */
  boundedSnapshot: true;
  /** The page came back full, so older matching rows probably exist beyond
   * what was exported. */
  truncatedByLimit: boolean;
  /** Offset paging is not a cursor: an insert between page reads shifts every
   * later row, so a paged read can double-count or skip. False means the
   * sequence in this export cannot be trusted as contiguous. */
  pagingStable: boolean;
  /** Rows whose stored body was clipped at the capture cap. What is exported
   * for these is NOT the complete payload. */
  truncatedCaptures: number;
  /** Rows carrying an engine-side error. */
  erroredRows: number;
  /** Rows with no usage/token annotation, so they contribute nothing to the
   * cost estimate — unknown, not zero. */
  rowsWithoutUsage: number;
}

export interface ReportOmission {
  /** Stable machine code so a reader can diff two exports. */
  code: string;
  /** One sentence a human can act on. */
  detail: string;
}

/** Cost, labelled honestly. Every field name says what it is. */
export interface ReportCost {
  /** Estimated cost the captured requests WOULD have incurred upstream. Not
   * money spent, not money saved, not verified. */
  estimatedUpstreamUSD: number;
  /** Rows the server scanned to compute the estimate. */
  requestsScanned: number;
  /** The scan cap that bounded it. */
  scanLimit: number;
  /** True when the scan returned exactly its cap, so the estimate is a partial
   * sum over the most recent rows — not the window total. The costs API
   * carries no truncation flag, so this is the only signal available and it is
   * reported as "may have", never as certainty. */
  scanCapMayHaveBeenReached: boolean;
  /** Requests attributed to an unidentifiable model. Their cost is unknown and
   * is NOT counted as zero in any claim made about coverage. */
  unpricedRequests: number;
  /** The API does not expose which pricing table produced these numbers, so
   * this is null and renders as "unknown". Do not fill it in from a guess. */
  pricingVersion: string | null;
}

export interface ReportProvenance {
  schema: string;
  /** When this export was produced, by the browser's clock. */
  generatedAt: string;
  /** Which server the evidence came from. */
  source: string;
  /** Server-reported version, or null when it could not be read. */
  serverVersion: string | null;
  tenantId: string | null;
  /** "local" | "multi_tenant" | null. */
  mode: string | null;
  /** Role the exporter held. null in local mode — not a viewer. */
  role: string | null;
  /** This console applies NO redaction. Null means exactly that: there is no
   * policy, so nothing may claim the content was scrubbed. */
  redactionPolicy: null;
}

/** One exported interaction. Metadata always; bodies only on explicit opt-in. */
export interface ReportRecord {
  id: number;
  timestamp: string;
  agent: string;
  protocol: string;
  path: string | null;
  status: number | null;
  latencyMs: number;
  sessionId: string | null;
  scenario: string | null;
  error: string | null;
  /** True when the stored body was clipped at the capture cap. */
  truncatedCapture: boolean;
  chaos: { action: string; source: string | null; seed: number | null; rate: number | null } | null;
  model: string | null;
  promptTokens: number | null;
  completionTokens: number | null;
  estimatedCostUSD: number | null;
  /** Present only when bodies were explicitly included. Verbatim, unredacted. */
  requestBody?: string;
  responseBody?: string;
}

export interface Report {
  provenance: ReportProvenance;
  window: ReportWindow;
  completeness: ReportCompleteness;
  omissions: ReportOmission[];
  /** null when cost data was not loaded — distinct from a zero estimate. */
  cost: ReportCost | null;
  bodiesIncluded: boolean;
  records: ReportRecord[];
}

export interface BuildReportInput {
  rows: InteractionLog[];
  window: { since: string | null; until: string | null; limit: number; offset: number };
  /** From LogWindow: the page came back full. */
  mayHaveMore: boolean;
  /** From LogWindow: offset paging is trustworthy for this read. */
  pagingStable: boolean;
  costs: CostsResponse | null;
  /** The scan cap that was sent to /api/v1/costs. */
  costScanLimit: number;
  source: string;
  serverVersion: string | null;
  tenantId: string | null;
  mode: string | null;
  role: string | null;
  includeBodies: boolean;
  /** Injected so tests are deterministic. */
  now?: Date;
}

function nullable<T>(value: T | undefined | null): T | null {
  return value === undefined || value === null ? null : value;
}

/** Whether a row carries enough usage annotation to contribute to cost. A row
 * with no model AND no token counts told us nothing — that is unknown, and
 * counting it as a $0 request would understate the estimate silently. */
function hasUsage(row: InteractionLog): boolean {
  return (
    (row.model !== undefined && row.model !== "") ||
    row.prompt_tokens !== undefined ||
    row.completion_tokens !== undefined
  );
}

function toRecord(row: InteractionLog, includeBodies: boolean): ReportRecord {
  const record: ReportRecord = {
    id: row.id,
    timestamp: row.timestamp,
    agent: row.agent_name,
    protocol: row.protocol,
    path: nullable(row.request_path),
    status: nullable(row.response_status),
    latencyMs: row.latency_ms,
    sessionId: nullable(row.session_id),
    scenario: nullable(row.scenario_name),
    error: nullable(row.error),
    truncatedCapture: row.truncated === true,
    chaos: row.chaos_action
      ? {
          action: row.chaos_action,
          source: nullable(row.chaos_source),
          seed: nullable(row.chaos_seed),
          rate: nullable(row.chaos_rate),
        }
      : null,
    model: nullable(row.model),
    promptTokens: nullable(row.prompt_tokens),
    completionTokens: nullable(row.completion_tokens),
    estimatedCostUSD: nullable(row.cost_usd),
  };
  if (includeBodies) {
    // Verbatim. Absent is absent — an empty string here would claim the server
    // captured an empty body when it may simply not have captured one.
    if (row.request_body !== undefined) record.requestBody = row.request_body;
    if (row.response_body !== undefined) record.responseBody = row.response_body;
  }
  return record;
}

/** Everything a reviewer must see BEFORE agreeing to embed raw bodies. */
export interface IncludedDataReview {
  requestBodies: number;
  responseBodies: number;
  /** UTF-16 length of the payloads that would be embedded. Approximate for
   * multi-byte content, and labelled as characters rather than bytes so it is
   * not mistaken for an exact transfer size. */
  characters: number;
  /** Rows whose body is already clipped: including them exports a fragment. */
  truncatedCaptures: number;
}

export function reviewIncludedData(rows: InteractionLog[]): IncludedDataReview {
  let requestBodies = 0;
  let responseBodies = 0;
  let characters = 0;
  let truncatedCaptures = 0;
  for (const row of rows) {
    if (row.request_body !== undefined) {
      requestBodies++;
      characters += row.request_body.length;
    }
    if (row.response_body !== undefined) {
      responseBodies++;
      characters += row.response_body.length;
    }
    if (row.truncated === true) truncatedCaptures++;
  }
  return { requestBodies, responseBodies, characters, truncatedCaptures };
}

export function buildReport(input: BuildReportInput): Report {
  const now = input.now ?? new Date();
  const rows = input.rows;

  const truncatedCaptures = rows.filter((r) => r.truncated === true).length;
  const erroredRows = rows.filter((r) => r.error !== undefined && r.error !== "").length;
  const rowsWithoutUsage = rows.filter((r) => !hasUsage(r)).length;

  const completeness: ReportCompleteness = {
    boundedSnapshot: true,
    truncatedByLimit: input.mayHaveMore,
    pagingStable: input.pagingStable,
    truncatedCaptures,
    erroredRows,
    rowsWithoutUsage,
  };

  let cost: ReportCost | null = null;
  if (input.costs) {
    const unpriced =
      input.costs.by_model.find((g) => g.key === "(unknown)")?.requests ?? 0;
    cost = {
      estimatedUpstreamUSD: input.costs.total_cost_usd,
      requestsScanned: input.costs.total_requests,
      scanLimit: input.costScanLimit,
      scanCapMayHaveBeenReached: input.costs.total_requests >= input.costScanLimit,
      unpricedRequests: unpriced,
      pricingVersion: null,
    };
  }

  return {
    provenance: {
      schema: REPORT_SCHEMA,
      generatedAt: now.toISOString(),
      source: input.source,
      serverVersion: input.serverVersion,
      tenantId: input.tenantId,
      mode: input.mode,
      role: input.role,
      redactionPolicy: null,
    },
    window: {
      since: input.window.since,
      until: input.window.until,
      requestedLimit: input.window.limit,
      offset: input.window.offset,
      rowsLoaded: rows.length,
    },
    completeness,
    omissions: describeOmissions(completeness, cost, input.includeBodies),
    cost,
    bodiesIncluded: input.includeBodies,
    records: rows.map((r) => toRecord(r, input.includeBodies)),
  };
}

/** Turn the completeness facts into sentences a reader can act on. Order is
 * stable so two exports diff cleanly. */
export function describeOmissions(
  completeness: ReportCompleteness,
  cost: ReportCost | null,
  bodiesIncluded: boolean,
): ReportOmission[] {
  const out: ReportOmission[] = [];

  out.push({
    code: "bounded-snapshot",
    detail:
      "This is a snapshot of data already loaded in the browser, not a server-attested report. It does not prove what the server retained.",
  });

  if (completeness.truncatedByLimit) {
    out.push({
      code: "row-limit-reached",
      detail:
        "The read returned a full page, so older matching interactions probably exist beyond this export. The count here is not the window total.",
    });
  }
  if (!completeness.pagingStable) {
    out.push({
      code: "offset-paging-unstable",
      detail:
        "These rows came from an offset page. Offset paging is not a cursor: rows inserted between reads shift the sequence, so a row may be duplicated or missed.",
    });
  }
  if (completeness.truncatedCaptures > 0) {
    out.push({
      code: "truncated-capture",
      detail: `${completeness.truncatedCaptures} interaction${
        completeness.truncatedCaptures === 1 ? "'s" : "s'"
      } stored body was clipped at the capture cap. What is recorded for ${
        completeness.truncatedCaptures === 1 ? "it" : "them"
      } is a fragment, not the payload.`,
    });
  }
  if (completeness.rowsWithoutUsage > 0) {
    out.push({
      code: "usage-unknown",
      detail: `${completeness.rowsWithoutUsage} interaction${
        completeness.rowsWithoutUsage === 1 ? " has" : "s have"
      } no token or model annotation. Their cost is unknown and is not counted as zero.`,
    });
  }
  if (cost === null) {
    out.push({
      code: "cost-not-loaded",
      detail:
        "No cost aggregate was loaded, so this export makes no cost claim at all. That is unknown, not zero.",
    });
  } else {
    if (cost.scanCapMayHaveBeenReached) {
      out.push({
        code: "cost-scan-cap",
        detail: `The cost estimate scanned ${cost.requestsScanned} rows against a cap of ${cost.scanLimit}. It may therefore be a partial sum over the most recent rows rather than the window total.`,
      });
    }
    if (cost.unpricedRequests > 0) {
      out.push({
        code: "cost-unpriced-model",
        detail: `${cost.unpricedRequests} request${
          cost.unpricedRequests === 1 ? "" : "s"
        } could not be attributed to a known model, so their cost is unknown rather than $0.`,
      });
    }
    out.push({
      code: "pricing-version-unknown",
      detail:
        "The costs API does not report which pricing table produced these figures, so the pricing version is unknown. The number is an estimate of upstream cost, not verified spend or savings.",
    });
  }

  out.push(
    bodiesIncluded
      ? {
          code: "bodies-verbatim",
          detail:
            "Raw request and response bodies are embedded VERBATIM. This console applies no redaction, so any secret, token or personal data present in a captured payload is present in this export.",
        }
      : {
          code: "bodies-excluded",
          detail:
            "Raw request and response bodies were excluded. Only interaction metadata is present.",
        },
  );

  return out;
}

export function reportToJSON(report: Report): string {
  return JSON.stringify(report, null, 2);
}

/** Escape for HTML text and attribute context.
 *
 * Bodies are captured from arbitrary clients and are rendered as TEXT, never
 * as markup — an exported report that executes a payload it was supposed to
 * document would be its own incident. */
export function escapeHTML(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function unknown(value: string | null): string {
  return value === null || value === "" ? "unknown" : escapeHTML(value);
}

/** A self-contained printable document. No external assets, no script — it has
 * to survive being emailed, saved and printed with the caveats intact. */
export function reportToPrintableHTML(report: Report): string {
  const w = report.window;
  const c = report.completeness;

  const rows = report.records
    .map(
      (r) => `<tr>
<td class="mono">${r.id}</td>
<td class="mono">${escapeHTML(r.timestamp)}</td>
<td class="mono">${unknown(r.sessionId)}</td>
<td>${escapeHTML(r.agent)}</td>
<td class="mono">${r.status === null ? "unknown" : r.status}</td>
<td class="num">${r.latencyMs} ms</td>
<td>${r.truncatedCapture ? "clipped" : "complete"}</td>
<td>${r.estimatedCostUSD === null ? "unknown" : "$" + r.estimatedCostUSD.toFixed(6)}</td>
</tr>`,
    )
    .join("\n");

  const bodies = report.bodiesIncluded
    ? report.records
        .filter((r) => r.requestBody !== undefined || r.responseBody !== undefined)
        .map(
          (r) => `<section class="body">
<h3>Interaction ${r.id}${r.truncatedCapture ? " — capture clipped, fragment only" : ""}</h3>
${r.requestBody !== undefined ? `<h4>request</h4><pre>${escapeHTML(r.requestBody)}</pre>` : ""}
${r.responseBody !== undefined ? `<h4>response</h4><pre>${escapeHTML(r.responseBody)}</pre>` : ""}
</section>`,
        )
        .join("\n")
    : "";

  const omissions = report.omissions
    .map((o) => `<li><code>${escapeHTML(o.code)}</code> — ${escapeHTML(o.detail)}</li>`)
    .join("\n");

  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>MockAgents evidence export — ${escapeHTML(report.provenance.generatedAt)}</title>
<style>
  :root { color-scheme: light }
  body { font: 13px/1.5 ui-sans-serif, system-ui, "Segoe UI", Helvetica, Arial, sans-serif;
         color: #16202c; background: #fff; margin: 0; padding: 32px; }
  h1 { font-size: 20px; margin: 0 0 4px }
  h2 { font-size: 14px; margin: 28px 0 8px; padding-top: 14px; border-top: 1px solid #dde3ea }
  h3 { font-size: 12.5px; margin: 16px 0 4px }
  h4 { font-size: 11px; margin: 10px 0 3px; color: #5b6b7d; text-transform: uppercase; letter-spacing: .06em }
  .lede { color: #5b6b7d; max-width: 44em; margin: 0 0 8px }
  .mono, code, pre { font-family: ui-monospace, "SF Mono", Menlo, Consolas, monospace }
  dl { display: grid; grid-template-columns: 190px 1fr; gap: 4px 16px; margin: 8px 0 }
  dt { color: #5b6b7d }
  dd { margin: 0 }
  table { width: 100%; border-collapse: collapse; font-size: 11.5px; margin-top: 8px }
  th, td { text-align: left; padding: 5px 8px; border-bottom: 1px solid #e6ebf1; vertical-align: top }
  th { font-size: 10px; text-transform: uppercase; letter-spacing: .05em; color: #5b6b7d }
  td.num { text-align: right }
  ul { margin: 8px 0; padding-left: 20px }
  li { margin-bottom: 5px }
  pre { background: #f4f6f9; border: 1px solid #e6ebf1; border-radius: 6px; padding: 8px 10px;
        white-space: pre-wrap; word-break: break-word; font-size: 11px; margin: 0 }
  .warn { background: #fdf3e3; border: 1px solid #f0d2a0; border-radius: 6px; padding: 10px 12px; margin: 12px 0 }
  @media print { body { padding: 0 } .warn { border-color: #999 } }
</style>
</head>
<body>
<h1>MockAgents evidence export</h1>
<p class="lede">A bounded snapshot of interactions already loaded in the console. It is
not a server-attested report and does not prove what the server retained.</p>

<h2>Provenance</h2>
<dl>
<dt>schema</dt><dd class="mono">${escapeHTML(report.provenance.schema)}</dd>
<dt>generated</dt><dd class="mono">${escapeHTML(report.provenance.generatedAt)}</dd>
<dt>source</dt><dd class="mono">${escapeHTML(report.provenance.source)}</dd>
<dt>server version</dt><dd class="mono">${unknown(report.provenance.serverVersion)}</dd>
<dt>tenant</dt><dd class="mono">${unknown(report.provenance.tenantId)}</dd>
<dt>mode / role</dt><dd class="mono">${unknown(report.provenance.mode)} · ${unknown(report.provenance.role)}</dd>
<dt>redaction policy</dt><dd class="mono">none applied</dd>
</dl>

<h2>Window</h2>
<dl>
<dt>since</dt><dd class="mono">${unknown(w.since)}</dd>
<dt>until</dt><dd class="mono">${unknown(w.until)}</dd>
<dt>requested limit</dt><dd class="mono">${w.requestedLimit}${w.offset ? ` · offset ${w.offset}` : ""}</dd>
<dt>rows exported</dt><dd class="mono">${w.rowsLoaded}</dd>
</dl>

<h2>Completeness</h2>
<dl>
<dt>more rows may exist</dt><dd>${c.truncatedByLimit ? "yes — page came back full" : "no full page returned"}</dd>
<dt>paging stable</dt><dd>${c.pagingStable ? "first page, no offset shift" : "no — offset paging can duplicate or skip rows"}</dd>
<dt>clipped captures</dt><dd>${c.truncatedCaptures}</dd>
<dt>rows with an error</dt><dd>${c.erroredRows}</dd>
<dt>rows without usage</dt><dd>${c.rowsWithoutUsage} (cost unknown, not zero)</dd>
</dl>

<h2>Cost</h2>
${
  report.cost === null
    ? `<p>No cost aggregate was loaded. This export makes no cost claim — unknown, not zero.</p>`
    : `<dl>
<dt>estimated upstream cost</dt><dd class="mono">$${report.cost.estimatedUpstreamUSD.toFixed(4)}</dd>
<dt>requests scanned</dt><dd class="mono">${report.cost.requestsScanned} of a ${report.cost.scanLimit}-row cap</dd>
<dt>partial sum</dt><dd>${report.cost.scanCapMayHaveBeenReached ? "possibly — the scan reached its cap" : "no cap reached"}</dd>
<dt>unpriced requests</dt><dd class="mono">${report.cost.unpricedRequests}</dd>
<dt>pricing version</dt><dd class="mono">unknown</dd>
</dl>
<p class="lede">This is an estimate of what the captured requests would have cost upstream.
It is not verified spend and not verified savings.</p>`
}

<h2>Omissions and caveats</h2>
<ul>
${omissions}
</ul>

<h2>Interactions (${report.records.length})</h2>
<table>
<thead><tr><th>id</th><th>time</th><th>session</th><th>agent</th><th>status</th><th>latency</th><th>capture</th><th>est. cost</th></tr></thead>
<tbody>
${rows || `<tr><td colspan="8">No interactions in this window.</td></tr>`}
</tbody>
</table>
${
  report.bodiesIncluded
    ? `<h2>Raw bodies</h2>
<div class="warn"><strong>Unredacted.</strong> These payloads are embedded exactly as captured.
This console applies no redaction, so review before sharing.</div>
${bodies || "<p>No bodies were captured for these interactions.</p>"}`
    : ""
}
</body>
</html>
`;
}
