import Link from "next/link";
import { notFound } from "next/navigation";

import { APIError, getLog } from "@/lib/api";

export const dynamic = "force-dynamic";

export default async function LogDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const numericId = Number(id);
  if (!Number.isFinite(numericId)) {
    notFound();
  }

  let log: Awaited<ReturnType<typeof getLog>> = null;
  let error: string | null = null;
  try {
    log = await getLog(numericId);
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  if (!log && !error) {
    notFound();
  }

  return (
    <div>
      <p className="page-lede">
        <Link href="/logs">← back to logs</Link>
      </p>
      <h1 className="page-title">Interaction #{numericId}</h1>

      {error && (
        <div className="banner banner-error">
          <strong>Could not load this log entry.</strong> {error}
        </div>
      )}

      {log && (
        <>
          <dl className="meta-grid">
            <div>
              <dt>Time</dt>
              <dd>{formatTimestamp(log.timestamp)}</dd>
            </div>
            <div>
              <dt>Agent</dt>
              <dd>
                {log.agent_name ? (
                  <Link href={`/agents/${log.agent_name}`}>{log.agent_name}</Link>
                ) : (
                  <span className="muted">—</span>
                )}
              </dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{log.response_status ?? "—"}</dd>
            </div>
            <div>
              <dt>Latency</dt>
              <dd>{log.latency_ms !== undefined ? `${log.latency_ms.toFixed(1)} ms` : "—"}</dd>
            </div>
            <div>
              <dt>Method · Path</dt>
              <dd>
                <code>
                  {log.request_method ?? "POST"} {log.request_path ?? "—"}
                </code>
              </dd>
            </div>
            <div>
              <dt>Scenario</dt>
              <dd>{log.scenario_name || <span className="muted">—</span>}</dd>
            </div>
            {log.cost_usd !== undefined && (
              <div>
                <dt>Estimated cost</dt>
                <dd>${log.cost_usd.toFixed(6)}</dd>
              </div>
            )}
            {(log.prompt_tokens || log.completion_tokens) && (
              <div>
                <dt>Tokens</dt>
                <dd>
                  {log.prompt_tokens ?? 0} prompt · {log.completion_tokens ?? 0} completion
                </dd>
              </div>
            )}
          </dl>

          <h2 className="section-title">Response body</h2>
          <pre className="json-block">{prettyOrRaw(log.response_body)}</pre>

          {log.request_body && (
            <>
              <h2 className="section-title">Request body</h2>
              <pre className="json-block">{prettyOrRaw(log.request_body)}</pre>
            </>
          )}
        </>
      )}
    </div>
  );
}

function prettyOrRaw(body: string | undefined): string {
  if (!body) return "(empty)";
  try {
    return JSON.stringify(JSON.parse(body), null, 2);
  } catch {
    return body;
  }
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
