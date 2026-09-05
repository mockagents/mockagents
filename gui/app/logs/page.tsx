import { APIError, getLog, listAgents, listLogWindow, listLogs, type InteractionLog } from "@/lib/api";
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
        // §9.1: metadata only. Bodies are fetched one row at a time when an
        // operator reveals one, so a window of captured payloads never crosses
        // the network on the strength of someone opening this page.
        metadataOnly: true,
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
        metadataOnly: true,
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

    // Fetching ONE row's bodies, on explicit reveal. Deliberately a per-row
    // read rather than a re-fetch of the window: revealing one body must not
    // pull back every other body with it.
    async function revealAction(id: number): Promise<{ request?: string; response?: string } | null> {
      "use server";
      const row = await getLog(id);
      if (!row) return null;
      return { request: row.request_body, response: row.response_body };
    }

    return (
      <LogsConsole
        window={window}
        agents={allAgents.map((a) => a.name)}
        filters={filters}
        recoverAction={recoverAction}
        revealAction={revealAction}
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
