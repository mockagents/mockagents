# Management API

The management API provides server introspection and control. All endpoints are prefixed with `/api/v1/`.

> **Authentication.** In single-tenant mode (default) the management API is open.
> With `MOCKAGENTS_MULTI_TENANT=1`, every route requires an API key
> (`Authorization: Bearer <key>` or `X-Api-Key: <key>`) whose role meets the
> route's floor: `viewer < editor < admin < platform`. The `platform` role
> (bootstrap-only) is the only one allowed to manage the tenant *collection*.
> The protocol endpoints (`/v1/*`) are always unauthenticated. The full machine-
> readable contract is in [`docs/api-spec.yaml`](https://github.com/mockagents/mockagents/blob/main/docs/api-spec.yaml).

## Health Check

```
GET /api/v1/health
```

```bash
curl http://localhost:8080/api/v1/health
```

```json
{ "status": "ok", "version": "0.1.0", "uptime": "5m32s" }
```

## List Agents

```
GET /api/v1/agents
```

```bash
curl http://localhost:8080/api/v1/agents
```

```json
[
  {
    "name": "customer-support",
    "description": "Customer support mock agent",
    "model": "gpt-4o",
    "protocol": "openai-chat-completions",
    "scenario_count": 3,
    "tool_count": 2,
    "tags": ["support"]
  }
]
```

## Get Agent Detail

```
GET /api/v1/agents/{name}
```

```bash
curl http://localhost:8080/api/v1/agents/customer-support
```

Returns the full agent definition as JSON. Returns 404 with available agent names if not found.

## Reload Agent

```
POST /api/v1/agents/{name}/reload
```

Re-reads the agent's YAML file from disk, re-validates, and replaces the in-memory definition.

```bash
curl -X POST http://localhost:8080/api/v1/agents/customer-support/reload
```

```json
{ "status": "reloaded", "agent": "customer-support" }
```

## Create Agent

```
POST /api/v1/agents
```

Registers a **new** agent at runtime — it serves immediately, no restart. The
body is a YAML or JSON `Agent` definition (same schema as a file on disk); it is
run through the same validator the CLI uses. When the server was started with an
agents directory, the definition is also persisted there so it survives a
restart. Returns `409` if an agent of that name already exists (use `PUT` to
replace). Requires the **editor** role in multi-tenant mode.

```bash
curl -X POST http://localhost:8080/api/v1/agents \
  -H 'Content-Type: application/yaml' --data-binary @my-agent.yaml
```

```json
{ "status": "created", "agent": "my-agent", "persisted": true, "file": "my-agent.yaml" }
```

## Create or Replace Agent

```
PUT /api/v1/agents/{name}
```

Create-or-replace (upsert). The path `{name}` is authoritative — the body's
`metadata.name` must match. Replacing an existing agent is live immediately. The
response `status` is `created` (HTTP 201) for a new agent or `updated` (HTTP
200) for a replacement. Editor role.

```bash
curl -X PUT http://localhost:8080/api/v1/agents/my-agent \
  -H 'Content-Type: application/json' --data-binary @my-agent.json
```

## Delete Agent

```
DELETE /api/v1/agents/{name}
```

Removes the agent from the engine (it stops serving immediately) and deletes its
persisted file when one exists. Returns `404` if no such agent is visible to the
caller. Editor role.

```bash
curl -X DELETE http://localhost:8080/api/v1/agents/my-agent
```

```json
{ "status": "deleted", "agent": "my-agent", "file": "my-agent.yaml" }
```

> Validation, ownership, and audit: a write that fails validation returns `422`
> with the same error report as `POST /api/v1/config/validate` and changes
> nothing. In multi-tenant mode the new agent is owned by the caller's tenant (a
> body-supplied `tenant_id` is ignored), and every create/replace/delete is
> recorded in the audit log (`agent.created` / `agent.updated` / `agent.deleted`).

## List Interaction Logs

```
GET /api/v1/logs
```

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `agent` | string | | Filter by agent name |
| `session_id` | string | | Filter by session ID |
| `since` | string | | ISO 8601 timestamp lower bound |
| `until` | string | | ISO 8601 timestamp upper bound |
| `limit` | int | 50 | Max results (1-1000) |
| `offset` | int | 0 | Pagination offset |

```bash
curl "http://localhost:8080/api/v1/logs?agent=customer-support&limit=10"
```

## Get Log Detail

```
GET /api/v1/logs/{id}
```

Returns full request/response bodies for a single interaction.

```bash
curl http://localhost:8080/api/v1/logs/42
```

## Clear Logs

```
DELETE /api/v1/logs
```

```bash
curl -X DELETE http://localhost:8080/api/v1/logs
```

```json
{ "deleted_count": 150 }
```

## Live Log Feed (SSE)

```
GET /api/v1/logs/stream            # text/event-stream — one frame per new row
GET /api/v1/logs/stream/metrics    # admin: subscriber drop counts + buffer use
```

New interaction rows are pushed sub-second after the SQLite write. The web
console subscribes to this for its live feed.

## Costs

```
GET /api/v1/costs?since=<rfc3339>&until=<rfc3339>&limit=1000
```

Aggregates interaction-log usage against the configured price table; returns
totals plus by-model and by-agent breakdowns. Tenant-scoped in multi-tenant mode.

## Audit Log

```
GET /api/v1/audit?kind=<kind>&actor=<name>&since=<rfc3339>&limit=100   # admin
```

Append-only control-plane events. `kind` is one of `tenant.created`,
`tenant.deleted`, `api_key.created`, `api_key.deleted`, `api_key.role_changed`,
`api_key.rotated`, `agent.reloaded`, `agent.created`, `agent.updated`,
`agent.deleted`, `pipeline.saved`, `auth.denied`.

## Pipelines

```
GET /api/v1/pipelines             # list kind:Pipeline documents
GET /api/v1/pipelines/{name}      # detail incl. DAG nodes + edges
PUT /api/v1/pipelines/{name}      # editor — create-or-replace a pipeline
```

## Config Validation

```
POST /api/v1/config/validate      # editor — body is a YAML document
```

Runs the same validator as `mockagents validate` and returns `{ ok, kind, errors }`.

## Multi-tenant Control Plane

Available when `MOCKAGENTS_MULTI_TENANT=1`. Plaintext keys are returned **exactly
once** on mint/rotate.

| Method & Path | Min role | Description |
|---------------|----------|-------------|
| `GET /api/v1/tenants` | platform | List tenants |
| `POST /api/v1/tenants` | platform | Create a tenant |
| `DELETE /api/v1/tenants/{id}` | platform | Delete a tenant (cascades to keys) |
| `GET /api/v1/tenants/{id}/keys` | editor | List a tenant's keys (no secret) |
| `POST /api/v1/tenants/{id}/keys` | admin | Mint a key |
| `POST /api/v1/tenants/{id}/keys/rotate` | admin | Bulk-rotate (`?except=self`) |
| `PATCH /api/v1/keys/{id}` | admin | Change a key's role |
| `POST /api/v1/keys/{id}/rotate` | admin | Rotate a key's secret in place |
| `DELETE /api/v1/keys/{id}` | admin | Delete a key |
| `POST /api/v1/keys/me/rotate` | viewer | Self-service rotation |
| `POST /api/v1/keys/me/burn` | viewer | Rotate without returning plaintext |

### Quotas

```
GET /api/v1/quota                     # caller's rate + monthly-spend quota and usage
PUT /api/v1/tenants/{id}/quota        # set a per-tenant override (persisted)
```

Defaults come from `MOCKAGENTS_DEFAULT_RATE_PER_SEC`,
`MOCKAGENTS_DEFAULT_RATE_BURST`, and `MOCKAGENTS_DEFAULT_MONTHLY_SPEND_USD`.
Over-rate LLM requests get `429` (+ `Retry-After`); over-spend gets `402`.

```bash
# Mint a read-only CI key
curl -H "Authorization: Bearer $ADMIN_KEY" -H "Content-Type: application/json" \
  -d '{"name":"ci-bot","role":"viewer"}' \
  http://localhost:8080/api/v1/tenants/$TENANT_ID/keys
```
