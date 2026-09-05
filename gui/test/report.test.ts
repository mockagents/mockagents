// UX-06: the export contract.
//
// These tests are about what the report is NOT allowed to claim. A report that
// renders a missing value as zero, a sampled scan as a total, or an estimate as
// verified savings is worse than no report — someone will make a decision on it.
import { describe, expect, it } from "vitest";

import type { CostsResponse, InteractionLog } from "@/lib/api";
import {
  REPORT_SCHEMA,
  buildReport,
  describeOmissions,
  escapeHTML,
  reportToJSON,
  reportToPrintableHTML,
  reviewIncludedData,
  type BuildReportInput,
} from "@/lib/report";

function log(over: Partial<InteractionLog> = {}): InteractionLog {
  return {
    id: 1,
    timestamp: "2026-09-04T14:31:52.000Z",
    agent_name: "support-starter",
    protocol: "openai",
    request: null,
    response: null,
    latency_ms: 96.4,
    ...over,
  };
}

function input(over: Partial<BuildReportInput> = {}): BuildReportInput {
  return {
    rows: [log()],
    window: { since: "2026-09-04T08:00:00.000Z", until: null, limit: 500, offset: 0 },
    mayHaveMore: false,
    pagingStable: true,
    costs: null,
    costScanLimit: 1000,
    source: "http://localhost:8080",
    serverVersion: "0.4.2",
    tenantId: "acme-prod",
    mode: "multi_tenant",
    role: "editor",
    includeBodies: false,
    now: new Date("2026-09-04T14:32:04.000Z"),
    ...over,
  };
}

function costs(over: Partial<CostsResponse> = {}): CostsResponse {
  return {
    window: {},
    total_requests: 42,
    total_prompt_tokens: 100,
    total_completion_tokens: 50,
    total_cost_usd: 3.41,
    by_model: [],
    by_agent: [],
    ...over,
  };
}

describe("provenance", () => {
  it("owns its own schema version rather than borrowing the server's", () => {
    const r = buildReport(input());
    expect(r.provenance.schema).toBe(REPORT_SCHEMA);
    expect(r.provenance.schema).not.toContain(r.provenance.serverVersion ?? "");
  });

  // The epic is explicit: never synthesize a version the source did not give.
  it("reports an unreadable server version as null, not a guess", () => {
    const r = buildReport(input({ serverVersion: null }));
    expect(r.provenance.serverVersion).toBeNull();
    expect(reportToPrintableHTML(r)).toContain("unknown");
  });

  it("never claims a redaction policy, because none is applied", () => {
    const r = buildReport(input());
    expect(r.provenance.redactionPolicy).toBeNull();
    expect(reportToPrintableHTML(r)).toContain("none applied");
  });
});

describe("window and completeness", () => {
  it("states the limit that bounded the read alongside the rows returned", () => {
    const r = buildReport(input({ rows: [log({ id: 1 }), log({ id: 2 })] }));
    expect(r.window.requestedLimit).toBe(500);
    expect(r.window.rowsLoaded).toBe(2);
  });

  it("says older rows probably exist when the page came back full", () => {
    const r = buildReport(input({ mayHaveMore: true }));
    expect(r.completeness.truncatedByLimit).toBe(true);
    expect(r.omissions.map((o) => o.code)).toContain("row-limit-reached");
  });

  it("discloses that offset paging can duplicate or skip rows", () => {
    const r = buildReport(input({ pagingStable: false }));
    expect(r.omissions.map((o) => o.code)).toContain("offset-paging-unstable");
  });

  it("counts clipped captures and says they are fragments", () => {
    const r = buildReport(input({ rows: [log({ truncated: true }), log({ id: 2 })] }));
    expect(r.completeness.truncatedCaptures).toBe(1);
    const omission = r.omissions.find((o) => o.code === "truncated-capture");
    expect(omission?.detail).toMatch(/fragment/);
  });

  // A row with no usage annotation told us nothing. Counting it as a $0 request
  // would quietly understate the estimate and look like data.
  it("treats a row with no usage annotation as unknown, not zero", () => {
    const r = buildReport(input({ rows: [log(), log({ id: 2, model: "gpt-4o" })] }));
    expect(r.completeness.rowsWithoutUsage).toBe(1);
    expect(r.omissions.find((o) => o.code === "usage-unknown")?.detail).toMatch(
      /not counted as zero/,
    );
  });

  it("always declares itself a bounded snapshot", () => {
    const r = buildReport(input());
    expect(r.completeness.boundedSnapshot).toBe(true);
    expect(r.omissions[0].code).toBe("bounded-snapshot");
  });
});

describe("cost labelling", () => {
  it("makes no cost claim when no aggregate was loaded", () => {
    const r = buildReport(input({ costs: null }));
    expect(r.cost).toBeNull();
    expect(r.omissions.find((o) => o.code === "cost-not-loaded")?.detail).toMatch(
      /unknown, not zero/,
    );
  });

  it("flags a scan that reached its cap as a possible partial sum", () => {
    const r = buildReport(input({ costs: costs({ total_requests: 1000 }), costScanLimit: 1000 }));
    expect(r.cost?.scanCapMayHaveBeenReached).toBe(true);
    expect(r.omissions.map((o) => o.code)).toContain("cost-scan-cap");
  });

  it("does not flag a scan that stayed under its cap", () => {
    const r = buildReport(input({ costs: costs({ total_requests: 42 }), costScanLimit: 1000 }));
    expect(r.cost?.scanCapMayHaveBeenReached).toBe(false);
    expect(r.omissions.map((o) => o.code)).not.toContain("cost-scan-cap");
  });

  it("reports unpriced requests rather than folding them in as free", () => {
    const r = buildReport(
      input({
        costs: costs({
          by_model: [
            { key: "gpt-4o", requests: 40, prompt_tokens: 0, completion_tokens: 0, cost_usd: 3.41 },
            { key: "(unknown)", requests: 2, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0 },
          ],
        }),
      }),
    );
    expect(r.cost?.unpricedRequests).toBe(2);
    expect(r.omissions.find((o) => o.code === "cost-unpriced-model")?.detail).toMatch(
      /unknown rather than \$0/,
    );
  });

  it("never presents the estimate as spend or savings", () => {
    const r = buildReport(input({ costs: costs() }));
    const html = reportToPrintableHTML(r);
    expect(html).toContain("estimated upstream cost");
    expect(html).toMatch(/not verified spend and not verified savings/);
    expect(html).not.toMatch(/spend avoided|saved/i);
    expect(r.cost?.pricingVersion).toBeNull();
  });
});

describe("raw bodies", () => {
  const withBodies = [
    log({ id: 1, request_body: '{"model":"gpt-4o"}', response_body: '{"ok":true}' }),
    log({ id: 2, request_body: "second", truncated: true }),
  ];

  it("excludes bodies by default and says so", () => {
    const r = buildReport(input({ rows: withBodies, includeBodies: false }));
    expect(r.bodiesIncluded).toBe(false);
    expect(r.records[0].requestBody).toBeUndefined();
    expect(r.records[0].responseBody).toBeUndefined();
    expect(r.omissions.map((o) => o.code)).toContain("bodies-excluded");
    expect(reportToJSON(r)).not.toContain("gpt-4o");
  });

  it("embeds bodies verbatim when opted in, and warns they are unredacted", () => {
    const r = buildReport(input({ rows: withBodies, includeBodies: true }));
    expect(r.records[0].requestBody).toBe('{"model":"gpt-4o"}');
    const omission = r.omissions.find((o) => o.code === "bodies-verbatim");
    expect(omission?.detail).toMatch(/no redaction/);
  });

  it("summarizes exactly what a reviewer would be agreeing to", () => {
    const review = reviewIncludedData(withBodies);
    expect(review.requestBodies).toBe(2);
    expect(review.responseBodies).toBe(1);
    expect(review.truncatedCaptures).toBe(1);
    expect(review.characters).toBe(
      '{"model":"gpt-4o"}'.length + '{"ok":true}'.length + "second".length,
    );
  });

  // An absent body is not an empty one: claiming the server captured "" would
  // be inventing evidence.
  it("omits an absent body rather than exporting an empty string", () => {
    const r = buildReport(input({ rows: [log({ request_body: undefined })], includeBodies: true }));
    expect("requestBody" in r.records[0]).toBe(false);
  });
});

describe("printable output", () => {
  it("renders captured bodies as text, never as markup", () => {
    const r = buildReport(
      input({
        rows: [log({ request_body: '<img src=x onerror="alert(1)">' })],
        includeBodies: true,
      }),
    );
    const html = reportToPrintableHTML(r);
    expect(html).not.toContain("<img src=x");
    expect(html).toContain("&lt;img src=x onerror=&quot;alert(1)&quot;&gt;");
  });

  it("escapes every character that could break out of text or an attribute", () => {
    expect(escapeHTML(`<&>"'`)).toBe("&lt;&amp;&gt;&quot;&#39;");
  });

  // The file has to survive being emailed and printed offline with its caveats
  // intact, so it fetches nothing. The source URL appears as TEXT (it is
  // provenance) but never as something the document loads.
  it("carries no script and loads no external asset", () => {
    const html = reportToPrintableHTML(buildReport(input({ costs: costs() })));
    expect(html).not.toMatch(/<script/i);
    expect(html).not.toMatch(/\bsrc=/i);
    expect(html).not.toMatch(/<link\b/i);
    expect(html).not.toMatch(/@import/i);
    expect(html).not.toMatch(/url\(/i);
  });

  it("says so plainly when the window held nothing", () => {
    const html = reportToPrintableHTML(buildReport(input({ rows: [] })));
    expect(html).toContain("No interactions in this window.");
  });
});

describe("omission ordering", () => {
  // Two exports of the same situation must diff cleanly, so the order is fixed.
  it("is deterministic for identical inputs", () => {
    const completeness = buildReport(input({ mayHaveMore: true })).completeness;
    const a = describeOmissions(completeness, null, false).map((o) => o.code);
    const b = describeOmissions(completeness, null, false).map((o) => o.code);
    expect(a).toEqual(b);
    expect(a[0]).toBe("bounded-snapshot");
    expect(a[a.length - 1]).toBe("bodies-excluded");
  });
});
