import Link from "next/link";

import { AgentSummary, APIError, getBaseUrl, listAgents } from "@/lib/api";

export default async function HomePage() {
  let agents: AgentSummary[] = [];
  let error: string | null = null;
  try {
    agents = await listAgents();
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  return (
    <div>
      <h1 className="page-title">Agent catalog</h1>
      <p className="page-lede">
        Every agent loaded by the running MockAgents server at{" "}
        <code>{getBaseUrl()}</code>.
      </p>

      {error && (
        <div className="banner banner-error">
          <strong>MockAgents server unreachable.</strong> {error}
          <div className="hint">
            Start it with <code>mockagents start --agents-dir ./agents</code>
            {" or set "}
            <code>MOCKAGENTS_API_URL</code> when launching the GUI.
          </div>
        </div>
      )}

      {!error && agents.length === 0 && (
        <div className="empty">
          No agents loaded. Add a YAML file to the agents directory and reload.
        </div>
      )}

      <div className="card-grid">
        {agents.map((agent) => (
          <Link key={agent.name} href={`/agents/${agent.name}`} className="card">
            <div className="card-head">
              <h2>{agent.name}</h2>
              <span className="badge">{agent.protocol}</span>
            </div>
            {agent.description && <p className="card-desc">{agent.description}</p>}
            <dl className="stats">
              <div>
                <dt>Model</dt>
                <dd>{agent.model || "—"}</dd>
              </div>
              <div>
                <dt>Scenarios</dt>
                <dd>{agent.scenario_count}</dd>
              </div>
              <div>
                <dt>Tools</dt>
                <dd>{agent.tool_count}</dd>
              </div>
            </dl>
            {agent.tags && agent.tags.length > 0 && (
              <div className="tags">
                {agent.tags.map((tag) => (
                  <span key={tag} className="tag">
                    {tag}
                  </span>
                ))}
              </div>
            )}
          </Link>
        ))}
      </div>
    </div>
  );
}
