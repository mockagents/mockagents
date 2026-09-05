// Typed fetch wrappers around the MockAgents management API.
// Every helper is a plain async function so it can be called from
// server components without any caching or client-side state.
//
// Authentication: when running against a multi-tenant deployment the
// helpers read an "mockagents_api_key" cookie set by the /login server
// action and forward it as an Authorization: Bearer header. In single-
// tenant mode the cookie is absent and calls go through anonymously.

import { cache } from "react";

import { cookies } from "next/headers";

import type { ReadinessCheck, ServerStatus } from "./serverState";

export const AUTH_COOKIE = "mockagents_api_key";
// SESSION_COOKIE is the SSO session cookie the backend's OIDC callback sets
// (REF-08 slice D). In a same-origin deployment the GUI reads it here and
// forwards it as a Bearer credential, which the backend accepts like an API key.
export const SESSION_COOKIE = "mockagents_session";

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
  /** Revision of the RUNNING definition — what GET /agents/{name} publishes as
   * X-Mockagents-Revision-Effective. Absent from an older server.
   *
   * NOT the ETag, and not usable as an If-Match value: the ETag also covers the
   * backing file, so computing it for a listing would mean reading every
   * agent's file. Fetch the agent when you need a precondition. */
  effective_revision?: string;
  /** Whether this definition survives a restart (U3-6):
   *  `file` backed by a file that is present · `runtime` never had one ·
   *  `missing` a backing file is tracked but is gone, so it is serving from
   *  memory only. Absent from an older server, which is UNKNOWN — not runtime. */
  persistence?: "file" | "runtime" | "missing";
  /** Base name of the backing file, when there is one. */
  file?: string;
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
  response_status?: number;
  streaming?: boolean;
  // UX-04: fields the storage layer has always recorded but the console never
  // surfaced. Without them an operator cannot group a conversation, tell an
  // injected fault from a real one, or know that a captured body is clipped.
  /** Groups the requests of one conversation. */
  session_id?: string;
  /** Engine-side error for this interaction, when there was one. */
  error?: string;
  /** True when the stored body was clipped at the capture cap — what is shown
   * is NOT the complete payload. */
  truncated?: boolean;
  tool_calls_count?: number;
  /** Which chaos action fired, e.g. "error", "latency". Absent = none. */
  chaos_action?: string;
  /** Where the effective chaos setting came from (request, scenario, agent, …). */
  chaos_source?: string;
  chaos_seed?: number;
  chaos_rate?: number;
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
    // Prefer an explicit API key; fall back to an SSO session token. Both are
    // forwarded to the backend as a Bearer credential — the backend's auth
    // middleware accepts either (a session token is prefixed `mas_`).
    return store.get(AUTH_COOKIE)?.value ?? store.get(SESSION_COOKIE)?.value ?? "";
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
  /** Narrow to one conversation (UX-04). Supported by the server's filter. */
  session_id?: string;
  /** Narrow to every session id starting with this value. A pipeline run scopes
   * its nodes as "<session>::<pipeline>::<node>", so this is what makes a run's
   * requests findable from the run. */
  session_prefix?: string;
  /** Offset paging. NOT a cursor — see LogWindow.stable. */
  offset?: number;
  /** Ask the server to withhold request/response bodies (§9.1). This is a
   * privacy boundary, not a display one: without it every body in the window
   * crosses the network before anyone asks to see one, and a UI that hides them
   * can only claim they are not SHOWN. Reveal then fetches a single row. */
  metadataOnly?: boolean;
}

function logQuery(options: ListLogsOptions): string {
  const params = new URLSearchParams();
  if (options.limit !== undefined) params.set("limit", String(options.limit));
  if (options.offset) params.set("offset", String(options.offset));
  if (options.agent) params.set("agent", options.agent);
  if (options.since) params.set("since", options.since);
  if (options.until) params.set("until", options.until);
  if (options.session_id) params.set("session_id", options.session_id);
  if (options.session_prefix) params.set("session_prefix", options.session_prefix);
  if (options.metadataOnly) params.set("fields", "meta");
  return params.toString();
}

/** Fetch recent interaction logs. */
export async function listLogs(options: ListLogsOptions = {}): Promise<InteractionLog[]> {
  const qs = logQuery(options);
  const path = qs ? `/api/v1/logs?${qs}` : "/api/v1/logs";
  return fetchJSON<InteractionLog[]>(path);
}

/** A page of logs, plus what is honestly known about the window it came from.
 *
 * The server returns a plain array with no total and no cursor, so this is what
 * can be said truthfully — and nothing more. In particular there is no "N of M":
 * the row count of the whole store is not available, and inventing one would be
 * the "missing information rendered as a number" the epic forbids. */
export interface LogWindow {
  rows: InteractionLog[];
  /** The limit that was asked for. */
  limit: number;
  offset: number;
  /** True when the page came back full, so older rows probably exist. A full
   * page does NOT prove complete retention — the store prunes independently. */
  mayHaveMore: boolean;
  /** False when paging cannot be trusted to be stable.
   *
   * The API pages by OFFSET, not by a cursor. New rows are inserted at the
   * head, so any insert between two page fetches shifts every later row down —
   * a row can be seen twice or missed entirely. That is a property of the
   * contract, not a bug in the client, and the UI must say so rather than
   * implying a stable sequence. A stable cursor is proposed but does not
   * exist (epic UX-04). */
  stable: boolean;
}

/** Fetch one page of logs together with its window metadata. */
export async function listLogWindow(options: ListLogsOptions = {}): Promise<LogWindow> {
  const limit = options.limit ?? 100;
  const offset = options.offset ?? 0;
  const rows = await listLogs({ ...options, limit, offset });
  return {
    rows,
    limit,
    offset,
    mayHaveMore: rows.length >= limit,
    // Only the first page of a quiet store can be treated as a stable view;
    // any paged read is subject to insert shift.
    stable: offset === 0,
  };
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

// --- Identity (UX-01) -----------------------------------------------

/** Roles a key may be ASSIGNED through the management API. Deliberately
 * excludes "platform": the backend refuses to assign it over the API
 * (Role.IsAssignableViaAPI) so a tenant admin cannot self-escalate, and the
 * key-role dropdown must not offer it. */
export type Role = "viewer" | "editor" | "admin";

/** Roles a principal may HAVE. A platform operator exists and must be
 * displayable even though no API call can mint that role. Keeping this
 * separate from Role is what stops the admin UI from offering it. */
export type PrincipalRole = Role | "platform";

/** How the server is running. "local" is single-tenant mode with no
 * authentication at all — deliberately NOT reported as a signed-in viewer,
 * because a UI that conflates them would show an access-controlled state for
 * a server that has none. */
export type IdentityMode = "local" | "multi_tenant";

/** GET /api/v1/identity. The server is authoritative for all of it; the
 * capability list is advisory, for deciding what to render. */
export interface Identity {
  mode: IdentityMode;
  authenticated: boolean;
  tenant_id?: string;
  key_id?: string;
  /** null in local mode — never coerce this to a role. */
  role: PrincipalRole | null;
  /** Sorted capability names, e.g. "agents.write". Reflects what this server
   * actually mounts, so an action named here will not 404. */
  capabilities: string[];
  server: { version: string };
}

/** Fetch the caller's identity. Pass authKey to validate a specific token
 * (used by /login before the cookie is set).
 *
 * Throws APIError for an HTTP failure — 401 means the credential was
 * rejected, which callers must distinguish from the null returned when the
 * server could not be reached at all. Treating "offline" as "unauthorized"
 * would sign the operator out every time the mock server restarts.
 *
 * Memoized per request render: the instrument strip needs identity in the root
 * layout while individual pages still need it to decide whether to offer a
 * write. React's cache() collapses those into one call per render pass, so
 * putting the strip on every screen did not multiply the probes. */
export const getIdentity = cache(async function getIdentity(
  authKey?: string,
): Promise<Identity | null> {
  try {
    return await fetchJSON<Identity>("/api/v1/identity", authKey ? { authKey } : {});
  } catch (err) {
    if (err instanceof APIError) throw err;
    return null;
  }
});

/** Whether an identity grants a capability. Rendering only — every request is
 * still authorized by the server. */
export function can(identity: Identity | null, capability: string): boolean {
  return identity?.capabilities.includes(capability) ?? false;
}

// --- Tenancy (multi-tenant mode only) -------------------------------

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

// probeTenants was removed in UX-01. It validated a login by calling
// GET /api/v1/tenants, whose role floor is *platform* — so viewer, editor and
// admin keys were all rejected at the door, and the failure was reported as
// "needs admin role". Use getIdentity() instead: it is reachable by every
// authenticated role and reports the real one.

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

// --- Quotas (UX-07) -------------------------------------------------
//
// Two different floors, and the UI must not blur them: reading your own quota
// is open to any authenticated role, while SETTING a tenant's quota is
// platform-gated. That asymmetry is deliberate — a tenant admin raising its own
// cap would make the cap meaningless — so the console reads for everyone and
// offers the write only where the server would accept it.

/** A tenant's effective limits. 0 means unlimited in every field. */
export interface QuotaLimits {
  rate_per_sec: number;
  rate_burst: number;
  monthly_spend_usd: number;
}

/** Consumption so far. Scoped to the current UTC month. */
export interface QuotaUsage {
  /** "2006-01" in UTC. */
  month: string;
  spend_usd: number;
}

export interface QuotaResponse {
  tenant_id: string;
  limits: QuotaLimits;
  usage: QuotaUsage;
}

/** Read the caller tenant's effective quota. Returns null when quotas are not
 * enabled on this server (503) — which is NOT the same as a quota of zero. */
export async function getQuota(): Promise<QuotaResponse | null> {
  try {
    return await fetchJSON<QuotaResponse>("/api/v1/quota");
  } catch (err) {
    if (err instanceof APIError && err.status === 503) return null;
    throw err;
  }
}

/** Set a tenant's quota override. Platform-gated; the server rejects anything
 * less, and a negative value, with a 4xx that the caller should surface rather
 * than swallow. */
export async function setTenantQuota(
  tenantId: string,
  limits: QuotaLimits,
): Promise<{ tenant_id: string; limits: QuotaLimits }> {
  return fetchJSON<{ tenant_id: string; limits: QuotaLimits }>(
    `/api/v1/tenants/${encodeURIComponent(tenantId)}/quota`,
    { method: "PUT", body: limits },
  );
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

/** Outcome of listing pipelines (X-2).
 *
 * `unsupported` is NOT the same as empty. The pipeline routes are only mounted
 * when the server was started with Pipeline documents, so a 404 means the
 * feature is absent from this server — not that a server which has it is
 * holding zero pipelines. Collapsing the two, which this helper used to do,
 * shows "no pipelines registered" on a server that could never register one,
 * and sends an operator looking for a Create button that does not exist. */
export type PipelineListing =
  | { supported: true; pipelines: PipelineSummary[] }
  | { supported: false };

/** List every registered pipeline, distinguishing "none" from "not enabled on
 * this server". Throws APIError for any other failure — unreachable and
 * unauthorized are their own states and must not read as either of these. */
export async function listPipelineInventory(): Promise<PipelineListing> {
  try {
    return { supported: true, pipelines: await fetchJSON<PipelineSummary[]>("/api/v1/pipelines") };
  } catch (err) {
    if (err instanceof APIError && err.status === 404) return { supported: false };
    throw err;
  }
}

/** List every registered pipeline. Returns an empty array when the
 * server was started without any Pipeline YAML documents (the
 * endpoint is unmounted in that case, so a 404 maps to `[]`).
 *
 * Prefer listPipelineInventory where the difference between "none" and "not
 * enabled" is visible to the operator; this stays for callers that only need a
 * list to iterate. */
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

function stripQuotes(s: string): string {
  return s.replace(/^"|"$/g, "");
}

/** Fetch a pipeline together with its version (the ETag), which the editor
 * echoes back as the `If-Match` precondition when saving. Returns null on 404.
 * Separate from getPipeline because fetchJSON discards response headers. */
export async function getPipelineWithVersion(
  name: string,
): Promise<{ definition: PipelineDefinition; version: string } | null> {
  const key = await getAuthKey();
  const headers: Record<string, string> = { Accept: "application/json" };
  if (key) headers.Authorization = `Bearer ${key}`;
  const res = await fetch(`${baseUrl()}/api/v1/pipelines/${encodeURIComponent(name)}`, {
    cache: "no-store",
    headers,
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    const body = await res.text();
    throw new APIError(res.status, `/api/v1/pipelines/${name}: ${res.status} ${body.slice(0, 200)}`);
  }
  const definition = (await res.json()) as PipelineDefinition;
  return { definition, version: stripQuotes(res.headers.get("ETag") ?? "") };
}

export type SavePipelineResult =
  | { status: "ok"; version: string }
  | { status: "invalid"; errors: ValidationError[] }
  | { status: "conflict"; message: string }
  | { status: "error"; message: string };

/** Persist an edited pipeline via PUT with optimistic concurrency. `version`
 * is the ETag from getPipelineWithVersion, sent as If-Match. Returns a
 * discriminated result instead of throwing, so the editor can render each
 * outcome inline:
 *   - ok:       written; carries the new version
 *   - invalid:  422 — validation failed, the file was left untouched
 *   - conflict: 412/428 — the pipeline changed on disk (or If-Match missing)
 *   - error:    transport or other failure */
export async function savePipeline(
  name: string,
  definition: PipelineDefinition,
  version: string,
): Promise<SavePipelineResult> {
  const key = await getAuthKey();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    "If-Match": version ? `"${version}"` : "",
  };
  if (key) headers.Authorization = `Bearer ${key}`;
  let res: Response;
  try {
    res = await fetch(`${baseUrl()}/api/v1/pipelines/${encodeURIComponent(name)}`, {
      method: "PUT",
      cache: "no-store",
      headers,
      body: JSON.stringify(definition),
    });
  } catch (err) {
    return { status: "error", message: err instanceof Error ? err.message : "network error" };
  }
  if (res.status === 200) {
    return { status: "ok", version: stripQuotes(res.headers.get("ETag") ?? "") };
  }
  if (res.status === 422) {
    const body = (await res.json()) as ValidateResult;
    return { status: "invalid", errors: body.errors ?? [] };
  }
  if (res.status === 409 || res.status === 412 || res.status === 428) {
    return {
      status: "conflict",
      message:
        "The pipeline changed on disk since you opened it. Reload to get the latest, then re-apply your edits.",
    };
  }
  const text = await res.text();
  return { status: "error", message: `Server returned ${res.status}: ${text.slice(0, 200)}` };
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

// --- Readiness (UX-02) ------------------------------------------------------

interface ReadinessWire {
  status?: "ready" | "not_ready";
  checks?: Array<{ name?: string; status?: "ok" | "failed"; error?: string }>;
}

/** Probe liveness AND readiness, which are different questions.
 *
 * The design handoff names these endpoints `/healthz` and `/readyz`; this
 * server actually serves `/api/v1/health` and `/api/v1/ready`. The real paths
 * are used — a screen annotated with a route that does not exist is how a
 * console ends up reporting a permanent outage.
 *
 * Readiness returns 503 with a body when a check fails, so a non-OK status is
 * still a meaningful answer and must be parsed rather than thrown away. */
/* Memoized per request render for the same reason as getIdentity, and for one
 * more: `checkedAt` is stamped inside. Two independent calls would put two
 * different "last refresh" times on one screen. */
export const getServerStatus = cache(async function getServerStatus(): Promise<ServerStatus> {
  const checkedAt = new Date().toISOString();
  const key = await getAuthKey();
  const headers: Record<string, string> = { Accept: "application/json" };
  if (key) headers.Authorization = `Bearer ${key}`;

  const probe = async (path: string): Promise<Response | null> => {
    try {
      return await fetch(`${baseUrl()}${path}`, { cache: "no-store", headers });
    } catch {
      return null;
    }
  };

  const health = await probe("/api/v1/health");
  if (health === null) {
    // Cannot reach the process at all. Readiness is not false — it is unknown.
    return {
      liveness: "unreachable",
      readiness: "unknown",
      checks: [],
      version: null,
      checkedAt,
      stale: true,
    };
  }

  let version: string | null = null;
  try {
    const body = (await health.json()) as { version?: string };
    version = body.version ?? null;
  } catch {
    version = null;
  }

  const ready = await probe("/api/v1/ready");
  if (ready === null) {
    return {
      liveness: "process-up",
      readiness: "unknown",
      checks: [],
      version,
      checkedAt,
      stale: false,
    };
  }

  let wire: ReadinessWire = {};
  try {
    wire = (await ready.json()) as ReadinessWire;
  } catch {
    wire = {};
  }

  const checks: ReadinessCheck[] = (wire.checks ?? []).map((c) => ({
    name: c.name ?? "unnamed",
    status: c.status === "failed" ? "failed" : "ok",
    error: c.error,
  }));

  // Trust the status field when present; fall back to the HTTP code.
  const isReady = wire.status ? wire.status === "ready" : ready.ok;
  return {
    liveness: "process-up",
    readiness: isReady ? "ready" : "not-ready",
    checks,
    version,
    checkedAt,
    stale: false,
  };
});

// --- Pipeline execution (UX-05) -------------------------------------------

/** One node's result. `response` is nullable: a node that never ran, or ran and
 * failed, is reported with a null response rather than being omitted. */
export interface PipelineNodeResult {
  node_id: string;
  agent_name: string;
  response: {
    agent_name?: string;
    model?: string;
    content?: string;
    scenario_name?: string;
    finish_reason?: string;
    refusal?: string;
    tool_calls?: unknown[];
  } | null;
  /** NANOSECONDS. Go marshals time.Duration as an integer count of ns; reading
   * it as milliseconds would under-report by 1e6. Use `nsToMs`. */
  latency: number;
}

export interface PipelineRunResult {
  pipeline_name: string;
  topology: string;
  nodes: PipelineNodeResult[] | null;
  /** Nanoseconds — see PipelineNodeResult.latency. */
  latency: number;
}

/** Outcome of an explicit pipeline run.
 *
 * "partial" is a first-class outcome, not an error: a 422 means the pipeline
 * was valid but a node could not execute, and the body still carries whatever
 * nodes completed. Throwing that away would discard the most useful evidence a
 * failed run produces. */
export type PipelineRunOutcome =
  | { status: "ok"; result: PipelineRunResult }
  | { status: "partial"; error: string; result: PipelineRunResult | null }
  /** A node names an agent that is not loaded (epic §10:
   * `blocked-missing-dependency`). Distinct from `partial` because the remedy
   * is different — load the definition, rather than fix a scenario.
   *
   * NOTE the run still STARTED. Refs resolve at node-execution time, so any
   * node before the missing one has already executed and advanced its session
   * state; `result` carries those. The design prototype's "the run was not
   * started" is false against this server. */
  | { status: "blocked"; error: string; result: PipelineRunResult | null }
  | { status: "denied"; message: string }
  | { status: "invalid"; message: string }
  | { status: "unavailable"; message: string }
  /** The response never arrived. The run may or may not have happened. */
  | { status: "unknown"; message: string };

// Duration formatting lives in lib/duration.ts: it must be importable from
// client components, and this module is server-only (next/headers).

/** Execute a registered pipeline against the ACTIVE runtime.
 *
 * This is not a preview and not an isolated run: it invokes the same engine the
 * provider adapters use, against the definitions currently loaded, and it
 * advances per-node session state. `sessionId` should be fresh for each run so
 * turns are not accidentally reused — but a fresh session does NOT pin the
 * definitions or isolate fixtures (epic §8.1). Release B adds the isolated
 * boundary.
 *
 * A transport failure is reported as "unknown", never as failure: the run is
 * stateful, so the caller must not silently retry it. */
export async function runPipeline(
  name: string,
  input: string,
  sessionId: string,
): Promise<PipelineRunOutcome> {
  const key = await getAuthKey();
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (key) headers.Authorization = `Bearer ${key}`;

  let res: Response;
  try {
    res = await fetch(`${baseUrl()}/api/v1/pipelines/${encodeURIComponent(name)}/run`, {
      method: "POST",
      cache: "no-store",
      headers,
      // The server decodes with DisallowUnknownFields — send exactly these.
      body: JSON.stringify({ input, session_id: sessionId }),
    });
  } catch {
    return {
      status: "unknown",
      message:
        "The server could not be reached, so it is unknown whether this run executed. " +
        "Check the interaction logs before running it again — a pipeline run is not idempotent.",
    };
  }

  if (res.status === 200) {
    return { status: "ok", result: (await res.json()) as PipelineRunResult };
  }
  if (res.status === 422) {
    // Valid request, unrunnable pipeline. The body preserves completed nodes.
    const body = (await res.json().catch(() => ({}))) as {
      error?: string;
      code?: string;
      result?: PipelineRunResult;
    };
    // The server classifies the failure so this does not have to match on the
    // message. An older server sends no code; everything is then "partial",
    // which is what this client did before the field existed.
    const status = body.code === "missing_dependency" ? "blocked" : "partial";
    return {
      status,
      error: body.error ?? "A node could not execute.",
      result: body.result ?? null,
    };
  }
  if (res.status === 401 || res.status === 403) {
    return {
      status: "denied",
      message:
        res.status === 401
          ? "Your session is no longer valid. Sign in again to run pipelines."
          : "You do not have permission to run pipelines on this server.",
    };
  }
  if (res.status === 503) {
    return {
      status: "unavailable",
      message: "This server has pipeline execution disabled, so there is nothing to run.",
    };
  }
  if (res.status === 404) {
    return { status: "invalid", message: `No pipeline named "${name}" is registered.` };
  }
  const text = await res.text().catch(() => "");
  return { status: "invalid", message: `Server returned ${res.status}: ${text.slice(0, 200)}` };
}

// --- Agent revisions (UX-03) ----------------------------------------------

/** An agent document plus the revision needed to write it back safely. */
export interface AgentSource {
  name: string;
  /** Canonical YAML of the definition the engine is serving. NOT the bytes of
   * the source file: comments and formatting from a hand-authored file are not
   * represented, and a save replaces them. */
  yaml: string;
  /** Opaque revision to echo back as If-Match. */
  revision: string;
  /** Hash of the running definition. */
  effective: string;
  /** Hash of the backing file, when there is a readable one. */
  source: string | null;
  /** True when the file on disk means something different from what is
   * running — someone edited the YAML without reloading. */
  drifted: boolean;
}

/** Load an agent as canonical YAML together with its revision, in one request.
 * Returns null when the agent does not exist. */
export async function getAgentSource(name: string): Promise<AgentSource | null> {
  const key = await getAuthKey();
  const headers: Record<string, string> = { Accept: "application/yaml" };
  if (key) headers.Authorization = `Bearer ${key}`;

  const res = await fetch(`${baseUrl()}/api/v1/agents/${encodeURIComponent(name)}`, {
    cache: "no-store",
    headers,
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    const body = await res.text();
    throw new APIError(res.status, `GET agent ${name}: ${res.status} ${body.slice(0, 200)}`);
  }

  const effective = res.headers.get("X-Mockagents-Revision-Effective") ?? "";
  const source = res.headers.get("X-Mockagents-Revision-Source");
  return {
    name,
    yaml: await res.text(),
    revision: stripQuotes(res.headers.get("ETag") ?? ""),
    effective,
    source,
    // Drift is a SERVER judgement we cannot make here: the two hashes are
    // computed over different things, so comparing them client-side would
    // report drift for every hand-authored file. The server reports the
    // difference in the 412 message; this stays false until the API exposes
    // an explicit flag.
    drifted: false,
  };
}

/** Outcome of a conditional agent save. Each case needs a different response
 * from the UI, so they are distinct rather than a boolean plus a message. */
export type ConditionalSaveResult =
  | { status: "ok"; created: boolean; persisted: boolean; file?: string; revision: string }
  | { status: "invalid"; errors: ValidationError[] }
  | { status: "conflict"; message: string; currentRevision: string }
  | { status: "denied"; message: string }
  | { status: "error"; message: string };

/** Save an agent conditionally.
 *
 * `ifMatch` is the revision the editor loaded — the write is refused if the
 * agent moved since. Pass `"*"` as `ifNoneMatch` instead to create without ever
 * overwriting. Sending either precondition also opts into strict field
 * checking, so an unsupported field is reported rather than silently dropped.
 */
export async function saveAgentConditional(
  name: string,
  yaml: string,
  precondition: { ifMatch: string } | { ifNoneMatch: "*" },
): Promise<ConditionalSaveResult> {
  const key = await getAuthKey();
  const headers: Record<string, string> = { "Content-Type": "application/x-yaml" };
  if (key) headers.Authorization = `Bearer ${key}`;
  if ("ifMatch" in precondition) headers["If-Match"] = `"${stripQuotes(precondition.ifMatch)}"`;
  else headers["If-None-Match"] = "*";

  let res: Response;
  try {
    res = await fetch(`${baseUrl()}/api/v1/agents/${encodeURIComponent(name)}`, {
      method: "PUT",
      cache: "no-store",
      headers,
      body: yaml,
    });
  } catch (err) {
    // Transport failure: the outcome is UNKNOWN, not failed. The write may or
    // may not have been applied, so the caller must not silently retry.
    return {
      status: "error",
      message:
        "The server could not be reached, so the outcome of this save is unknown. " +
        "Reload to see the current state before trying again — do not assume it failed.",
    };
  }

  if (res.status === 200 || res.status === 201) {
    const body = (await res.json()) as {
      persisted?: boolean;
      file?: string;
      revision?: string;
    };
    return {
      status: "ok",
      created: res.status === 201,
      persisted: body.persisted === true,
      file: body.file,
      revision: body.revision ?? stripQuotes(res.headers.get("ETag") ?? ""),
    };
  }
  if (res.status === 422) {
    const body = (await res.json()) as ValidateResult;
    return { status: "invalid", errors: body.errors ?? [] };
  }
  if (res.status === 412) {
    const body = (await res.json().catch(() => ({}))) as { error?: string };
    return {
      status: "conflict",
      message: body.error ?? "The agent changed since it was loaded.",
      currentRevision: stripQuotes(res.headers.get("ETag") ?? ""),
    };
  }
  if (res.status === 401 || res.status === 403) {
    return {
      status: "denied",
      message:
        res.status === 401
          ? "Your session is no longer valid. Sign in again — your draft is kept."
          : "You do not have permission to change agents (this needs the editor role).",
    };
  }
  const text = await res.text();
  return { status: "error", message: `Server returned ${res.status}: ${text.slice(0, 200)}` };
}

// --- Agent write API (FB-06: persist edits from the console via the FB-04 API) ---

export interface SaveResult {
  ok: boolean;
  /** "created" | "updated" on success; "error" otherwise. */
  status: "created" | "updated" | "error";
  message: string;
  /** Populated on a 422 so the editor can render the same per-field errors. */
  errors?: ValidationError[];
}

/** extractAgentName finds the agent's kebab-case metadata.name to build the PUT
 * path. It is a focused YAML-aware scanner (not a full parser, to avoid a
 * browser-bundle dependency): it handles flow-style `metadata: {name: x}`, only
 * matches `name:` at the metadata block's own child-indent level, and skips the
 * (deeper-indented) content of block scalars like `description: |` so a `name:`
 * line inside a description/prose value isn't mistaken for the key. Returns null
 * when no name is found. */
function extractAgentName(yaml: string): string | null {
  // Flow-style mapping: metadata: { name: x, ... }
  const flow = yaml.match(/^metadata:\s*\{([^}]*)\}/m);
  if (flow) {
    const m = flow[1].match(/(?:^|,)\s*name:\s*["']?([a-z0-9][a-z0-9-]*)/);
    if (m) return m[1];
  }

  const lines = yaml.split("\n");
  let inMeta = false;
  let baseIndent = -1; // indent of metadata's direct children
  let skipDeeperThan = -1; // inside a block scalar: skip lines more indented than this
  for (const raw of lines) {
    const line = raw.replace(/\t/g, "  ");
    if (/^metadata:\s*(#.*)?$/.test(line)) {
      inMeta = true;
      continue;
    }
    if (!inMeta) continue;
    if (line.trim() === "") continue;
    if (/^\S/.test(line)) break; // a dedented (top-level) key ends the metadata block
    const indent = line.length - line.trimStart().length;
    if (skipDeeperThan >= 0) {
      if (indent > skipDeeperThan) continue; // block-scalar content line
      skipDeeperThan = -1; // back to a metadata sibling
    }
    if (baseIndent < 0) baseIndent = indent;
    if (indent !== baseIndent) continue; // only direct children of metadata
    if (/:\s*[|>][+-]?\d*\s*(#.*)?$/.test(line)) {
      skipDeeperThan = indent; // a block scalar value begins; skip its content
      continue;
    }
    const m = line.match(/^\s+name:\s*["']?([a-z0-9][a-z0-9-]*)["']?\s*(#.*)?$/);
    if (m) return m[1];
  }
  return null;
}

/** saveAgentYAML create-or-replaces an agent from a YAML document via the FB-04
 * write API (PUT /api/v1/agents/{name}). Runs server-side so the auth cookie is
 * threaded upstream. A 422 returns the validator errors for inline display. */
export async function saveAgentYAML(yaml: string): Promise<SaveResult> {
  const name = extractAgentName(yaml);
  if (!name) {
    return {
      ok: false,
      status: "error",
      message: "Could not find a valid metadata.name in the document.",
    };
  }
  const key = await getAuthKey();
  const headers: Record<string, string> = { "Content-Type": "application/x-yaml" };
  if (key) headers.Authorization = `Bearer ${key}`;
  const res = await fetch(`${baseUrl()}/api/v1/agents/${encodeURIComponent(name)}`, {
    method: "PUT",
    cache: "no-store",
    headers,
    body: yaml,
  });
  if (res.status === 200 || res.status === 201) {
    const created = res.status === 201;
    return {
      ok: true,
      status: created ? "created" : "updated",
      message: `Agent "${name}" ${created ? "created" : "updated"}.`,
    };
  }
  const text = await res.text();
  if (res.status === 422) {
    try {
      const body = JSON.parse(text) as { errors?: ValidationError[] };
      return { ok: false, status: "error", message: "Validation failed.", errors: body.errors ?? [] };
    } catch {
      /* fall through to the generic message */
    }
  }
  return {
    ok: false,
    status: "error",
    message: `Server rejected the save (HTTP ${res.status}): ${text.slice(0, 200)}`,
  };
}

/** deleteAgentByName removes an agent via DELETE /api/v1/agents/{name}. The
 * upstream error detail is logged server-side; the browser sees a clean,
 * status-aware message (no raw upstream body — GUI-07). */
export async function deleteAgentByName(name: string): Promise<{ ok: boolean; message: string }> {
  try {
    await fetchJSON<void>(`/api/v1/agents/${encodeURIComponent(name)}`, { method: "DELETE" });
    return { ok: true, message: `Agent "${name}" deleted.` };
  } catch (err) {
    console.error("deleteAgentByName: upstream request failed:", err);
    if (err instanceof APIError) {
      const reason =
        err.status === 403
          ? "you don't have permission (editor role required)"
          : err.status === 404
            ? "it no longer exists"
            : `the server returned HTTP ${err.status}`;
      return { ok: false, message: `Could not delete "${name}": ${reason}.` };
    }
    return { ok: false, message: `Could not delete "${name}": the server is unreachable.` };
  }
}
