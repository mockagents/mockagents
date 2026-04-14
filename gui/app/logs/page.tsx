import Link from "next/link";

import { APIError, InteractionLog, listLogs } from "@/lib/api";

export default async function LogsPage() {
  let logs: InteractionLog[] = [];
  let error: string | null = null;
  try {
    logs = await listLogs({ limit: 50 });
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <h1 className="page-title">Interaction logs</h1>
      <p className="page-lede">
        The 50 most recent request/response pairs recorded by the running server.
      </p>

      {error && (
        <div className="banner banner-error">
          <strong>Could not load logs.</strong> {error}
        </div>
      )}

      {!error && logs.length === 0 && (
        <div className="empty">
          No interactions yet. Point a client at the MockAgents server and come back.
        </div>
      )}

      <div className="table-wrap">
        {logs.length > 0 && (
          <table className="log-table">
            <thead>
              <tr>
                <th>Time</th>
                <th>Agent</th>
                <th>Protocol</th>
                <th>Scenario</th>
                <th>Status</th>
                <th>Latency</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((log) => (
                <tr key={log.id}>
                  <td>{formatTimestamp(log.timestamp)}</td>
                  <td>
                    <Link href={`/agents/${log.agent_name}`}>{log.agent_name}</Link>
                  </td>
                  <td>
                    <code>{log.protocol}</code>
                  </td>
                  <td>{log.scenario_name || <span className="muted">—</span>}</td>
                  <td>{log.status_code ?? "—"}</td>
                  <td>{log.latency_ms !== undefined ? `${log.latency_ms.toFixed(1)} ms` : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
