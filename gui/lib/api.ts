// Typed fetch wrappers around the MockAgents management API.
// Every helper is a plain async function so it can be called from
// server components without any caching or client-side state.
//
// Authentication: when running against a multi-tenant deployment the
// helpers read an "mockagents_api_key" cookie set by the /login server
// action and forward it as an Authorization: Bearer header. In single-
// tenant mode the cookie is absent and calls go through anonymously.

import { cookies } from "next/headers";

export const AUTH_COOKIE = "mockagents_api_key";

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
  request_method?: string;
  request_path?: string;
  request_body?: string;
  response_body?: string;
  request: unknown;
  response: unknown;
  latency_ms: number;
  scenario_name?: string;
  status_code?: number;
  // Cost annotation fields populated by /api/v1/logs when a pricing
  // table is configured (see internal/server/log_handlers.go LogWithCost).
  prompt_tokens?: number;
  completion_tokens?: number;
  model?: string;
  cost_usd?: number;
}

export interface CostGroup {
  key: string;
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd: number;
}

export interface CostsResponse {
  window: { since?: string; until?: string };
  total_requests: number;
  total_prompt_tokens: number;
  total_completion_tokens: number;
  total_cost_usd: number;
  by_model: CostGroup[];
  by_agent: CostGroup[];
}

export interface AuditActor {
  name: string;
  tenant_id?: string;
  key_id?: string;
  role?: string;
  remote_ip?: string;
}

export interface AuditEvent {
  id: number;
  timestamp: string;
  kind: string;
  actor: AuditActor;
  target: string;
  details?: string;
}

function baseUrl(): string {
  return (process.env.MOCKAGENTS_API_URL ?? "http://localhost:8080").replace(/\/+$/, "");
}

/** Reads the auth cookie when running inside a server component. Returns
 * the empty string when the cookie is absent or when called outside a
 * request context (next/headers throws in that case). */
export async function getAuthKey(): Promise<string> {
  try {
    const store = await cookies();
    return store.get(AUTH_COOKIE)?.value ?? "";
  } catch {
    return "";
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  /** Pass an explicit API key to bypass the cookie lookup — used by the
   * /login server action which needs to validate a key before setting
   * the cookie. */
  authKey?: string;
}

async function fetchJSON<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const url = `${baseUrl()}${path}`;
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  const key = opts.authKey ?? (await getAuthKey());
  if (key) headers.Authorization = `Bearer ${key}`;

  const res = await fetch(url, {
    // Always skip the Next.js data cache — operators want the GUI to
    // reflect the running server state in real time, not a stale snapshot.
    cache: "no-store",
    method: opts.method ?? "GET",
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new APIError(res.status, `${path}: ${res.status} ${body.slice(0, 200)}`);
  }
  // 204 No Content responses (DELETE) have no body to parse.
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
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

/** Re-read an agent's YAML from disk and replace it in the registry. Requires
 * the editor role in multi-tenant mode; open in single-tenant mode. */
export async function reloadAgent(name: string): Promise<void> {
  await fetchJSON<void>(`/api/v1/agents/${encodeURIComponent(name)}/reload`, { method: "POST" });
}

export interface ListLogsOptions {
  limit?: number;
  agent?: string;
  since?: string;
  until?: string;
}

/** Fetch recent interaction logs. */
export async function listLogs(options: ListLogsOptions = {}): Promise<InteractionLog[]> {
  const params = new URLSearchParams();
  if (options.limit !== undefined) params.set("limit", String(options.limit));
  if (options.agent) params.set("agent", options.agent);
  if (options.since) params.set("since", options.since);
  if (options.until) params.set("until", options.until);
  const qs = params.toString();
  const path = qs ? `/api/v1/logs?${qs}` : "/api/v1/logs";
  return fetchJSON<InteractionLog[]>(path);
}

/** Fetch a single interaction log by id. Returns null on 404 so the
 * detail page can render a friendly empty state instead of throwing. */
export async function getLog(id: number): Promise<InteractionLog | null> {
  try {
    return await fetchJSON<InteractionLog>(`/api/v1/logs/${id}`);
  } catch (err) {
    if (err instanceof APIError && err.status === 404) return null;
    throw err;
  }
}

export interface ListCostsOptions {
  since?: string;
  until?: string;
  agent?: string;
  limit?: number;
}

/** Fetch the cost aggregate response. Returns null on 503 (logging
 * disabled) so the page can show an explanatory empty state. */
export async function getCosts(options: ListCostsOptions = {}): Promise<CostsResponse | null> {
  const params = new URLSearchParams();
  if (options.since) params.set("since", options.since);
  if (options.until) params.set("until", options.until);
  if (options.agent) params.set("agent", options.agent);
  if (options.limit !== undefined) params.set("limit", String(options.limit));
  const qs = params.toString();
  const path = qs ? `/api/v1/costs?${qs}` : "/api/v1/costs";
  try {
    return await fetchJSON<CostsResponse>(path);
  } catch (err) {
    if (err instanceof APIError && err.status === 503) return null;
    throw err;
  }
}

export interface ListAuditOptions {
  kind?: string;
  actor?: string;
  since?: string;
  limit?: number;
}

/** Fetch recent audit events. Returns null on 401/403 so multi-tenant
 * deployments can show a "needs admin token" notice instead of crashing. */
export async function listAudit(options: ListAuditOptions = {}): Promise<AuditEvent[] | null> {
  const params = new URLSearchParams();
  if (options.kind) params.set("kind", options.kind);
  if (options.actor) params.set("actor", options.actor);
  if (options.since) params.set("since", options.since);
  if (options.limit !== undefined) params.set("limit", String(options.limit));
  const qs = params.toString();
  const path = qs ? `/api/v1/audit?${qs}` : "/api/v1/audit";
  try {
    return await fetchJSON<AuditEvent[]>(path);
  } catch (err) {
    if (err instanceof APIError && (err.status === 401 || err.status === 403)) return null;
    throw err;
  }
}

/** Returns the base URL the GUI is configured to talk to. */
export function getBaseUrl(): string {
  return baseUrl();
}

// --- Tenancy (multi-tenant mode only) -------------------------------

export type Role = "viewer" | "editor" | "admin";

export interface Tenant {
  id: string;
  name: string;
  created_at: string;
}

export interface APIKey {
  id: string;
  tenant_id: string;
  name: string;
  prefix: string;
  role: Role;
  created_at: string;
  last_used?: string;
}

export interface NewAPIKeyResult {
  key: APIKey;
  plaintext: string;
}

/** Probe whether the configured API key is accepted. Used by /login to
 * validate a pasted token before we persist it in the cookie. Returns
 * null when the endpoint is unreachable entirely so callers can
 * distinguish network errors from auth rejections. */
export async function probeTenants(authKey: string): Promise<Tenant[] | null> {
  try {
    return await fetchJSON<Tenant[]>("/api/v1/tenants", { authKey });
  } catch (err) {
    if (err instanceof APIError) throw err;
    return null;
  }
}

/** List every tenant. Returns null on 401/403 so the page can render a
 * "needs admin token" notice instead of crashing. */
export async function listTenants(): Promise<Tenant[] | null> {
  try {
    return await fetchJSON<Tenant[]>("/api/v1/tenants");
  } catch (err) {
    if (err instanceof APIError && (err.status === 401 || err.status === 403)) return null;
    throw err;
  }
}

export async function createTenant(name: string): Promise<Tenant> {
  return fetchJSON<Tenant>("/api/v1/tenants", { method: "POST", body: { name } });
}

export async function deleteTenant(id: string): Promise<void> {
  await fetchJSON<void>(`/api/v1/tenants/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listAPIKeys(tenantId: string): Promise<APIKey[] | null> {
  try {
    return await fetchJSON<APIKey[]>(`/api/v1/tenants/${encodeURIComponent(tenantId)}/keys`);
  } catch (err) {
    if (err instanceof APIError && (err.status === 401 || err.status === 403)) return null;
    throw err;
  }
}

export async function createAPIKey(tenantId: string, name: string, role: Role): Promise<NewAPIKeyResult> {
  return fetchJSON<NewAPIKeyResult>(`/api/v1/tenants/${encodeURIComponent(tenantId)}/keys`, {
    method: "POST",
    body: { name, role },
  });
}

export async function updateAPIKeyRole(id: string, role: Role): Promise<{ id: string; role: Role }> {
  return fetchJSON<{ id: string; role: Role }>(`/api/v1/keys/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: { role },
  });
}

export async function deleteAPIKey(id: string): Promise<void> {
  await fetchJSON<void>(`/api/v1/keys/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export interface BulkRotateResult {
  count: number;
  results: NewAPIKeyResult[];
}

/** Rotate every key in a tenant inside a single server-side
 * transaction. Returns one NewAPIKeyResult per key plus a count
 * aggregate. Admin-only. Use this as the emergency response to a
 * suspected tenant-wide credential compromise — any other flow
 * leaves a window where some keys are still the old secrets.
 *
 * Pass `exceptSelf: true` to exclude the caller's own key from
 * the rotation so the admin doesn't lock themselves out of the
 * very console they're using. */
export async function bulkRotateTenantKeys(
  tenantId: string,
  opts: { exceptSelf?: boolean } = {},
): Promise<BulkRotateResult> {
  const qs = opts.exceptSelf ? "?except=self" : "";
  return fetchJSON<BulkRotateResult>(
    `/api/v1/tenants/${encodeURIComponent(tenantId)}/keys/rotate${qs}`,
    { method: "POST" },
  );
}

/** Atomically regenerate an existing key's secret. The key id, name,
 * role, and tenant stay the same; only the plaintext changes. The
 * returned plaintext is only shown once — the caller must surface it
 * to the operator immediately and then discard it. */
export async function rotateAPIKey(id: string): Promise<NewAPIKeyResult> {
  return fetchJSON<NewAPIKeyResult>(`/api/v1/keys/${encodeURIComponent(id)}/rotate`, {
    method: "POST",
  });
}

/** Rotate the caller's OWN key — the one the current session cookie
 * is authenticated as. Any authenticated role (viewer/editor/admin)
 * can self-rotate because the handler reads the key id from the
 * request context, not a path parameter. Returns the new plaintext
 * exactly once; after the cookie is updated with the fresh secret
 * the old one is invalid immediately. */
export async function rotateMyAPIKey(): Promise<NewAPIKeyResult> {
  return fetchJSON<NewAPIKeyResult>(`/api/v1/keys/me/rotate`, {
    method: "POST",
  });
}

/** Burn the caller's OWN key: rotates in place but discards the new
 * plaintext rather than returning it. The response is 204 No Content
 * — the caller's current cookie is already dead by the time the
 * POST returns, so the caller should also clear its session cookie
 * and redirect to /login. Use this when the current browser
 * session is suspected to be compromised: the new plaintext never
 * touches the compromised machine, and recovery goes through an
 * out-of-band channel. */
export async function burnMyAPIKey(): Promise<void> {
  await fetchJSON<void>(`/api/v1/keys/me/burn`, { method: "POST" });
}

// --- Config editor ---

export interface ValidationError {
  file?: string;
  line?: number;
  column?: number;
  field: string;
  message: string;
  suggestion?: string;
}

export interface ValidateResult {
  ok: boolean;
  kind: string;
  errors: ValidationError[];
}

// --- Pipelines ---

export interface PipelineSummary {
  name: string;
  description?: string;
  topology: string;
  agent_count: number;
  edge_count: number;
}

export interface PipelineAgent {
  id: string;
  ref: string;
}

export interface PipelineEdge {
  from: string;
  to: string;
  when_contains?: string;
}

export interface PipelineDefinition {
  apiVersion: string;
  kind: string;
  metadata: { name: string; description?: string; tags?: string[] };
  spec: {
    topology: string;
    agents: PipelineAgent[];
    edges?: PipelineEdge[];
  };
}

/** List every registered pipeline. Returns an empty array when the
 * server was started without any Pipeline YAML documents (the
 * endpoint is unmounted in that case, so a 404 maps to `[]`). */
export async function listPipelines(): Promise<PipelineSummary[]> {
  try {
    return await fetchJSON<PipelineSummary[]>("/api/v1/pipelines");
  } catch (err) {
    if (err instanceof APIError && err.status === 404) return [];
    throw err;
  }
}

/** Fetch a single pipeline definition. Returns null on 404 so the
 * detail page can render a friendly empty state. */
export async function getPipeline(name: string): Promise<PipelineDefinition | null> {
  try {
    return await fetchJSON<PipelineDefinition>(`/api/v1/pipelines/${encodeURIComponent(name)}`);
  } catch (err) {
    if (err instanceof APIError && err.status === 404) return null;
    throw err;
  }
}

/** Send a YAML document to the server validator. Returns the full
 * report so the editor can render errors inline. Throws APIError on
 * transport failures — the server always returns 200 for validation
 * outcomes (ok=true) OR validation problems (ok=false). */
export async function validateYAML(yaml: string): Promise<ValidateResult> {
  const key = await getAuthKey();
  const headers: Record<string, string> = { "Content-Type": "application/x-yaml" };
  if (key) headers.Authorization = `Bearer ${key}`;
  const res = await fetch(`${baseUrl()}/api/v1/config/validate`, {
    method: "POST",
    cache: "no-store",
    headers,
    body: yaml,
  });
  if (!res.ok) {
    const body = await res.text();
    throw new APIError(res.status, `/api/v1/config/validate: ${res.status} ${body.slice(0, 200)}`);
  }
  return (await res.json()) as ValidateResult;
}
