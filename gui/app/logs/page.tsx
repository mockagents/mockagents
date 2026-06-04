import { APIError, InteractionLog, listAgents, listLogs } from "@/lib/api";

import { LogsConsole } from "./LogsConsole";

export const dynamic = "force-dynamic";

export default async function LogsPage() {
  let logs: InteractionLog[] = [];
  let agents: string[] = [];
  let error: string | null = null;
  try {
    const [rows, allAgents] = await Promise.all([listLogs({ limit: 100 }), listAgents()]);
    logs = rows;
    agents = allAgents.map((a) => a.name);
  } catch (err) {
    error = err instanceof APIError ? err.message : "unknown error";
  }

  if (error) {
    return (
      <div>
        <div className="page-head">
          <h1 className="page-title">Interaction logs</h1>
        </div>
        <div className="banner banner-error">
          <strong>Could not load logs.</strong> {error}
        </div>
      </div>
    );
  }

  return <LogsConsole initialLogs={logs} agents={agents} />;
}
