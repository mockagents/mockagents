import Link from "next/link";

import { AgentSummary, APIError, getBaseUrl, getHealth, listAgents } from "@/lib/api";
import { Icon } from "@/lib/icons";
import { AgentCatalog } from "./AgentCatalog";
import { Stat } from "./Stat";

export default async function HomePage() {
  let agents: AgentSummary[] = [];
  let error: string | null = null;
  try {
    agents = await listAgents();
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }
  const health = await getHealth();
  const host = getBaseUrl().replace(/^https?:\/\//, "");

  const totalScenarios = agents.reduce((s, a) => s + a.scenario_count, 0);
  const totalTools = agents.reduce((s, a) => s + a.tool_count, 0);

  return (
    <div>
      <div className="page-head">
        <div className="head-row">
          <div className="grow">
            <h1 className="page-title">Agent catalog</h1>
            <p className="page-lede">
              Every agent loaded by the running MockAgents server at <code>{host}</code>. Click an
              agent to inspect its scenarios, tools, and chaos config.
            </p>
          </div>
          <div className="row gap-2">
            <Link href="/editor" className="btn btn-default btn-sm">
              <Icon name="plus" size={15} /> New agent
            </Link>
          </div>
        </div>
      </div>

      {error && (
        <div className="banner banner-error">
          <strong>MockAgents server unreachable.</strong> {error}
          <span className="hint">
            Start it with <code>mockagents start --agents-dir ./agents</code>, or set{" "}
            <code>MOCKAGENTS_API_URL</code> when launching the GUI.
          </span>
        </div>
      )}

      {!error && (
        <>
          <div className="grid grid-4 mb-6">
            <Stat icon="bot" label="Agents loaded" value={String(agents.length)} sub="from the agents directory" />
            <Stat icon="list-tree" label="Scenarios" value={String(totalScenarios)} sub="total match rules" />
            <Stat icon="wrench" label="Tools" value={String(totalTools)} sub="simulated tool calls" />
            <Stat
              icon="circle-dot"
              label="Server"
              value={health ? "online" : "offline"}
              sub={health?.version ? `v${health.version}` : host}
            />
          </div>

          {agents.length === 0 ? (
            <div className="empty">
              No agents loaded. Add a YAML file to the agents directory and reload.
            </div>
          ) : (
            <AgentCatalog agents={agents} />
          )}
        </>
      )}
    </div>
  );
}
