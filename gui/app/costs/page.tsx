import Link from "next/link";

import { APIError, CostGroup, getCosts } from "@/lib/api";
import { Stat } from "../Stat";

export const dynamic = "force-dynamic";

const WINDOWS: { id: string; label: string; days: number }[] = [
  { id: "24h", label: "24h", days: 1 },
  { id: "7d", label: "7d", days: 7 },
  { id: "30d", label: "30d", days: 30 },
];

export default async function CostsPage({
  searchParams,
}: {
  searchParams: Promise<{ window?: string }>;
}) {
  const { window: windowParam } = await searchParams;
  const win = WINDOWS.find((w) => w.id === windowParam) ?? WINDOWS[1]; // default 7d
  const since = new Date(Date.now() - win.days * 86_400_000).toISOString();

  let costs: Awaited<ReturnType<typeof getCosts>> = null;
  let error: string | null = null;
  try {
    costs = await getCosts({ since, limit: 1000 });
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  const maxModel = costs ? Math.max(0, ...costs.by_model.map((m) => m.cost_usd)) : 0;
  const maxAgent = costs ? Math.max(0, ...costs.by_agent.map((m) => m.cost_usd)) : 0;

  return (
    <div>
      <div className="head-row page-head">
        <div className="grow">
          <h1 className="page-title">Cost &amp; usage</h1>
          <p className="page-lede">
            Estimated spend from <code>/api/v1/costs</code>, priced from the configured table.
            MockAgents never bills — this mirrors what the real upstream would have cost.
          </p>
        </div>
        <div className="row gap-2">
          {WINDOWS.map((w) => (
            <Link key={w.id} href={`/costs?window=${w.id}`} className={"pill" + (w.id === win.id ? " active" : "")}>
              {w.label}
            </Link>
          ))}
        </div>
      </div>

      {error && (
        <div className="banner banner-error">
          <strong>Could not load costs.</strong> {error}
        </div>
      )}

      {!error && costs === null && (
        <div className="empty">
          Cost aggregation requires the interaction log store to be enabled.
        </div>
      )}

      {costs && (
        <>
          <div className="grid grid-4 mb-6">
            <Stat icon="activity" label="Requests" value={costs.total_requests.toLocaleString()} sub={`last ${win.label}`} />
            <Stat icon="hash" label="Prompt tokens" value={costs.total_prompt_tokens.toLocaleString()} />
            <Stat icon="hash" label="Completion tokens" value={costs.total_completion_tokens.toLocaleString()} />
            <Stat
              icon="dollar-sign"
              label="Est. spend avoided"
              value={fmtUSD(costs.total_cost_usd)}
              sub="tokens never sent upstream"
            />
          </div>

          <div className="grid grid-2" style={{ alignItems: "start" }}>
            <CostTable title="By model" rows={costs.by_model} max={maxModel} kcol="model" />
            <CostTable title="By agent" rows={costs.by_agent} max={maxAgent} kcol="agent" />
          </div>
        </>
      )}
    </div>
  );
}

function CostTable({
  title,
  rows,
  max,
  kcol,
}: {
  title: string;
  rows: CostGroup[];
  max: number;
  kcol: string;
}) {
  return (
    <div className="card">
      <div className="card-head">
        <h3>{title}</h3>
      </div>
      {rows.length === 0 ? (
        <div className="card-pad muted txt-sm">No data in the current window.</div>
      ) : (
        <table className="tbl">
          <thead>
            <tr>
              <th>{kcol}</th>
              <th className="right">req</th>
              <th style={{ width: 140 }}>cost</th>
              <th className="right">usd</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.key}>
                <td className="mono" style={{ fontSize: 12 }}>
                  {r.key}
                </td>
                <td className="num muted">{r.requests.toLocaleString()}</td>
                <td>
                  <div className="meter">
                    <span style={{ width: `${max > 0 ? (r.cost_usd / max) * 100 : 0}%` }} />
                  </div>
                </td>
                <td className="num" style={{ fontWeight: 600 }}>
                  {fmtUSD(r.cost_usd)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function fmtUSD(amount: number): string {
  // Estimates are typically < $1 in dev, so 4 decimals keep a $0.0023 row
  // from rendering as "$0.00".
  return `$${amount.toFixed(4)}`;
}
