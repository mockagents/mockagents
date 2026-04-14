// Typed fetch wrappers around the MockAgents management API.
// Every helper is a plain async function so it can be called from
// server components without any caching or client-side state.

export interface Health {
  status: string;
  version?: string;
  uptime?: string;
}

export interface AgentSummary {
  name: string;
  description?: string;
  model: string;
  protocol: string;
  scenario_count: number;
  tool_count: number;
  tags?: string[];
}

export interface InteractionLog {
  id: number;
  timestamp: string;
  agent_name: string;
  protocol: string;
  request: unknown;
  response: unknown;
  latency_ms: number;
  scenario_name?: string;
  status_code?: number;
}

function baseUrl(): string {
  return (process.env.MOCKAGENTS_API_URL ?? "http://localhost:8080").replace(/\/+$/, "");
}

async function fetchJSON<T>(path: string): Promise<T> {
  const url = `${baseUrl()}${path}`;
  const res = await fetch(url, {
    // Always skip the Next.js data cache — operators want the GUI to
    // reflect the running server state in real time, not a stale snapshot.
    cache: "no-store",
    headers: { Accept: "application/json" },
  });
  if (!res.ok) {
    const body = await res.text();
    throw new APIError(res.status, `${path}: ${res.status} ${body.slice(0, 200)}`);
  }
  return (await res.json()) as T;
}

export class APIError extends Error {
  public readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "APIError";
    this.status = status;
  }
}

/** Probe /api/v1/health. Returns null when the server is unreachable
 * instead of throwing — server components render a banner on null. */
export async function getHealth(): Promise<Health | null> {
  try {
    return await fetchJSON<Health>("/api/v1/health");
  } catch {
    return null;
  }
}

/** List every loaded agent. Throws APIError when the server is down. */
export async function listAgents(): Promise<AgentSummary[]> {
  return fetchJSON<AgentSummary[]>("/api/v1/agents");
}

/** Fetch the full agent definition by metadata.name. */
export async function getAgent(name: string): Promise<Record<string, unknown>> {
  return fetchJSON<Record<string, unknown>>(`/api/v1/agents/${encodeURIComponent(name)}`);
}

export interface ListLogsOptions {
  limit?: number;
  agent?: string;
}

/** Fetch recent interaction logs. */
export async function listLogs(options: ListLogsOptions = {}): Promise<InteractionLog[]> {
  const params = new URLSearchParams();
  if (options.limit !== undefined) params.set("limit", String(options.limit));
  if (options.agent) params.set("agent", options.agent);
  const qs = params.toString();
  const path = qs ? `/api/v1/logs?${qs}` : "/api/v1/logs";
  return fetchJSON<InteractionLog[]>(path);
}

/** Returns the base URL the GUI is configured to talk to. */
export function getBaseUrl(): string {
  return baseUrl();
}
