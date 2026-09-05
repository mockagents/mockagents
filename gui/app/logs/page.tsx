import { APIError, listAgents, listLogWindow, listLogs, type InteractionLog } from "@/lib/api";
import { parseFilters } from "@/lib/logfeed";

import { LogsConsole } from "./LogsConsole";

export const dynamic = "force-dynamic";

type PageProps = {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export default async function LogsPage({ searchParams }: PageProps) {
  // UX-04: filters come from the URL and are applied by the SERVER, so a
  // filtered view is shareable and is not limited to whatever a single fetch
  // happened to return. parseFilters allowlists the keys, so nothing else in
  // the query string reaches the API.
  const raw = await searchParams;
  const flat: Record<string, string | undefined> = {};
  for (const [key, value] of Object.entries(raw)) {
    flat[key] = Array.isArray(value) ? value[0] : value;
  }
  const filters = parseFilters(flat);

  try {
    const [window, allAgents] = await Promise.all([
      listLogWindow({
        limit: filters.limit,
        offset: filters.offset,
        agent: filters.agent || undefined,
        session_id: filters.session_id || undefined,
        session_prefix: filters.session_prefix || undefined,
        since: filters.since || undefined,
        until: filters.until || undefined,
      }),
      listAgents(),
    ]);

    async function recoverAction(afterId: number | null, limit: number): Promise<InteractionLog[]> {
      "use server";
      // Bounded best-effort recovery after a dropped feed. There is no cursor
      // API, so this refetches the newest `limit` rows under the same filters
      // and lets the client work out what it had not seen. The client treats a
      // FULL page as "cannot prove the gap is closed".
      const rows = await listLogs({
        limit,
        agent: filters.agent || undefined,
        session_id: filters.session_id || undefined,
        session_prefix: filters.session_prefix || undefined,
        since: filters.since || undefined,
        until: filters.until || undefined,
      });
      if (afterId == null) return rows;
      return rows.filter((r) => r.id > afterId);
    }

    return (
      <LogsConsole
        window={window}
        agents={allAgents.map((a) => a.name)}
        filters={filters}
        recoverAction={recoverAction}
      />
    );
  } catch (err) {
    // A server that cannot be reached, or a rejected credential, must not
    // render as "no traffic" (epic §5: offline and empty are different states).
    const status = err instanceof APIError ? err.status : 0;
    return (
      <div>
        <div className="page-head">
          <h1 className="page-title">Interaction logs</h1>
        </div>
        <div className="banner banner-error" role="alert">
          <strong>Could not load logs.</strong>{" "}
          {status === 401
            ? "Your session is no longer valid — sign in again."
            : status === 503
              ? "This server has interaction logging disabled, so there is nothing to show."
              : status
                ? `The server returned ${status}.`
                : "The server could not be reached. It may be stopped."}
        </div>
      </div>
    );
  }
}
