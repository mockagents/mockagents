# Multi-Tenant Mode & Control-Plane Operations

## Credential authority and revocation

Key mutations authorize both caller and target. Tenant admins may manage
ordinary keys in their tenant, but cannot rotate, demote, delete, or
bulk-rotate a platform key. Platform operations require a platform caller. The
store repeats target-role and credential-version checks inside the mutation
transaction; concurrent drift fails the operation and bulk rotation is atomic.

Cached authentication still checks the authoritative store for existence,
tenant, role, and credential version. Rotation, deletion, or demotion takes
effect on the next authorization lookup on every instance sharing the store. A
request authorized before commit may finish. Authority-store outages fail
closed. Drain older instances during rollout before relying on these semantics.
SQLite and PostgreSQL add and backfill `credential_version` at startup.

MockAgents includes an optional SaaS-style control plane: API-key auth, tenants,
RBAC, key rotation, and an audit log over the management API (`/api/v1/*`).

> **You do not need any of this for the core use case.** Single-tenant mode is
> the default — run `mockagents start` and the management API is open, exactly
> like a local dev tool. This guide is for platform/DevEx teams running a shared
> MockAgents instance for many users. The LLM endpoints
> (`/v1/chat/completions`, `/v1/messages`, Gemini `generateContent`) are
> **always unauthenticated** in both modes.

## Enabling multi-tenant mode

It is opt-in — set `MOCKAGENTS_MULTI_TENANT=1` before `mockagents start` to
enable it. When the flag is off everything behaves exactly as today.

On first boot with the flag set, MockAgents creates a `default` tenant and a
`bootstrap-admin` API key with the `platform` role. The plaintext is never
written to the log stream (in a container, stderr is the pod log and ends up in
every log aggregator). You receive it one of two ways:

- **Supply it yourself** with `MOCKAGENTS_BOOTSTRAP_KEY=mak_<8 hex>_<secret>`
  (secret: 24+ URL-safe base64 characters). This is the path for Kubernetes,
  where the value comes from a Secret. Nothing secret is printed.
- **Let MockAgents generate it.** The key is written, mode 0600, to
  `MOCKAGENTS_BOOTSTRAP_KEY_FILE` (default `<MOCKAGENTS_DATA_DIR>/bootstrap-admin.key`)
  and stderr shows only the path and the public prefix:

  ```
  ================================================================
  MockAgents multi-tenant mode enabled.
  Bootstrap platform key (prefix mak_1c3a9e0f) written to:
    /data/bootstrap-admin.key
  Read it once, store it in your password manager, then delete the file.
  Use it via:  Authorization: Bearer <key>   or   X-Api-Key: <key>
  ================================================================
  ```

  If that file cannot be written (read-only filesystem), startup fails with
  instructions instead of falling back to printing the secret.

The key is bcrypt-hashed immediately; there is no recovery path if you lose it.

Every `MOCKAGENTS_*` variable is parsed strictly: a value that is set but not
valid (`MOCKAGENTS_MULTI_TENANT=maybe`, `MOCKAGENTS_PORT=808O`,
`MOCKAGENTS_DEFAULT_RATE_PER_SEC=10rps`) is a startup error, never a silent
default. Booleans accept `1/0`, `true/false`, `yes/no`, `on/off`.

## Roles & route authorization

Four roles, ordered by privilege: `viewer` < `editor` < `admin` < `platform`.
**`platform`** is the cross-tenant operator role and the only one allowed to
manage the tenant *collection*; it is minted **only** by the CLI bootstrap, and
the management API refuses to assign it — so a per-tenant `admin` cannot
self-escalate. Roles gate the control-plane routes:

| Route                                     | Min role |
| ----------------------------------------- | -------- |
| `GET  /api/v1/health`                     | open     |
| `GET  /api/v1/ready`                      | open     |
| `GET  /api/v1/identity`                   | open     |
| `GET  /api/v1/agents`, `/api/v1/logs`     | viewer   |
| `GET  /metrics`                           | platform |
| `GET  /api/v1/pipelines[/{name}]`         | viewer   |
| `POST /api/v1/pipelines/{name}/run`       | viewer   |
| `POST /api/v1/agents/{name}/reload`       | editor   |
| `POST /api/v1/agents` (create)            | editor   |
| `PUT  /api/v1/agents/{name}` (replace)    | editor   |
| `DELETE /api/v1/agents/{name}`            | editor   |
| `POST /api/v1/keys/me/rotate`             | viewer   |
| `POST /api/v1/keys/me/burn`               | viewer   |
| `GET  /api/v1/tenants/{id}/keys`          | editor   |
| `POST /api/v1/config/validate`            | editor   |
| `PUT  /api/v1/pipelines/{name}`           | editor   |
| `POST /api/v1/tenants/{id}/keys`          | admin    |
| `POST /api/v1/tenants/{id}/keys/rotate`   | admin    |
| `PATCH /api/v1/keys/{id}`                 | admin    |
| `POST /api/v1/keys/{id}/rotate`           | admin    |
| `DELETE /api/v1/keys/{id}`                | admin    |
| `DELETE /api/v1/logs` (purge tenant log)   | admin    |
| `GET  /api/v1/audit`                      | admin    |
| `GET  /api/v1/logs/stream/metrics`        | admin    |
| `GET  /api/v1/tenants`                    | platform |
| `POST /api/v1/tenants`, `DELETE ...`      | platform |

**The LLM endpoints (`/v1/chat/completions`, `/v1/messages`, `/v1/models`,
`/v1/engines/*`) deliberately remain unauthenticated** — clients send their own
provider API keys which MockAgents ignores, and forcing a second layer of
credentials would break every existing SDK.

`GET /api/v1/identity` reports the calling principal and what it may do:

```json
{
  "mode": "multi_tenant",
  "authenticated": true,
  "tenant_id": "t_01HZY4",
  "key_id": "k_01HZY9",
  "role": "editor",
  "capabilities": ["agents.read", "agents.write", "pipelines.run.write"],
  "server": { "version": "0.4.0" }
}
```

It carries **no secret** — `key_id` is an identifier, never key material.
It is open to every authenticated role on purpose: a viewer must be able to
discover its own identity, and a client that cannot do so ends up probing a
privileged endpoint to guess its role. An anonymous caller still gets 401.

Capabilities are derived from the role-floor table above **intersected with
the routes this process actually mounts** — `/api/v1/audit` and
`/api/v1/costs` only exist when their stores are configured — so an
advertised capability never names a route that would 404. They are advisory,
for deciding what a UI renders; every request is still authorized server-side.

`GET /api/v1/health` and `GET /api/v1/ready` are unauthenticated for the same
class of reason: a kubelet or load balancer carries no API key, and gating the
probes would take every pod out of rotation. Neither response contains agent,
tenant, or configuration data. `GET /metrics` is **not** in that group — agent
and scenario names appear there as metric labels — so a Prometheus scrape
config needs a dedicated platform key:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: mockagents
    authorization: { credentials_file: /etc/prometheus/mockagents.key }
    static_configs:
      - targets: ["mockagents:8080"]
```

```bash
# Platform keys are bootstrap-managed. Put the dedicated scrape key in a
# narrowly readable secret; do not reuse an application tenant credential.
```

## Rotation and role changes

For a metrics key, provision its replacement and update Prometheus first,
confirm successful authenticated scrapes, and only then revoke the old key.
This ordering avoids a monitoring gap while preserving immediate revocation
once the old credential is removed.

`POST /api/v1/keys/{id}/rotate` regenerates an existing key's secret in place.
The key id, name, role, and tenant stay stable so every consumer that references
the key by id keeps working — only the plaintext changes. The old hash is
replaced atomically inside a transaction, the auth cache is flushed, and an
`api_key.rotated` audit event is emitted with both the old and new prefixes so
operators can correlate a rotation with a specific compromised credential.
`PATCH /api/v1/keys/{id}` changes the role with the same audit semantics.

## Audit logging

Every control-plane mutation (tenant create/delete, API key create/delete,
agent reload) is appended to a dedicated SQLite file (`.mockagents-audit.db`)
and exposed for query at `GET /api/v1/audit`. Audit is always on — there's no
flag to enable it because the cost is a handful of SQLite writes per admin
action.

```bash
# Fetch all recent events
curl -H "Authorization: Bearer $ADMIN_KEY" http://localhost:8080/api/v1/audit

# Filter by kind + time window
curl -H "Authorization: Bearer $ADMIN_KEY" \
  "http://localhost:8080/api/v1/audit?kind=api_key.created&since=2026-04-13T00:00:00Z&limit=50"
```

Supported `kind` values: `tenant.created`, `tenant.deleted`, `api_key.created`,
`api_key.deleted`, `api_key.role_changed`, `api_key.rotated`, `agent.reloaded`,
`agent.created`, `agent.updated`, `agent.deleted`, `pipeline.saved`,
`auth.denied`. Additional filters: `actor` (exact-match actor name), `since`
(RFC3339 lower bound), `limit` (default 100, max 1000).

Each event records the authenticated principal's tenant id, key id, role, and
remote IP. In single-tenant mode the actor is `"anonymous"`. Plaintext API keys
are never written to the audit log — the `api_key.created` event carries only
the key's opaque id, its public prefix, its name, and its role.

When multi-tenant mode is enabled, `GET /api/v1/audit` requires the admin role;
in single-tenant mode it is open (matching the rest of the management API).

## What's deliberately deferred

- **Tenant-scoped agent data isolation per name.** Agents can carry
  `metadata.tenant_id` and the engine resolves with tenant visibility, but
  agents still share a global name namespace — two tenants can't both own an
  `echo` agent. Needs the Postgres slice.
- **Postgres backend.** The tenancy store is pure-Go SQLite
  (`.mockagents-tenancy.db`); the `Store` interface makes a Postgres
  implementation straightforward once it's needed.
- **Billing, quotas, usage metering.** SaaS primitives — separate slice.
- **SSO / OAuth.** API keys only for now.

> Several of the "deferred" items above (Postgres backend, per-tenant quotas +
> monthly spend caps, OIDC SSO) have since landed — see the `tenancy`, `quota`,
> and `oidcauth` packages and the `CHANGELOG`.
