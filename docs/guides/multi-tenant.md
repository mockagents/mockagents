# Multi-Tenant Mode & Control-Plane Operations

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
`bootstrap-admin` API key, then prints the plaintext **exactly once** to stderr
so you can capture it:

```
================================================================
MockAgents multi-tenant mode enabled.
Bootstrap admin key (shown once): mak_1c3a9e0f_MXh6A2ci8RaWGpQBxLHFhRRacKvKnovL
Store this in your password manager. Use it via:
  Authorization: Bearer <key>   or   X-Api-Key: <key>
================================================================
```

The key is bcrypt-hashed immediately; there is no recovery path if you lose it.

## Roles & route authorization

Four roles, ordered by privilege: `viewer` < `editor` < `admin` < `platform`.
**`platform`** is the cross-tenant operator role and the only one allowed to
manage the tenant *collection*; it is minted **only** by the CLI bootstrap, and
the management API refuses to assign it — so a per-tenant `admin` cannot
self-escalate. Roles gate the control-plane routes:

| Route                                     | Min role |
| ----------------------------------------- | -------- |
| `GET  /api/v1/health`                     | open     |
| `GET  /api/v1/agents`, `/api/v1/logs`     | viewer   |
| `POST /api/v1/agents/{name}/reload`       | editor   |
| `POST /api/v1/agents` (create)            | editor   |
| `PUT  /api/v1/agents/{name}` (replace)    | editor   |
| `DELETE /api/v1/agents/{name}`            | editor   |
| `POST /api/v1/keys/me/rotate`             | viewer   |
| `POST /api/v1/keys/me/burn`               | viewer   |
| `GET  /api/v1/tenants/{id}/keys`          | editor   |
| `POST /api/v1/config/validate`            | editor   |
| `POST /api/v1/tenants/{id}/keys`          | admin    |
| `POST /api/v1/tenants/{id}/keys/rotate`   | admin    |
| `PATCH /api/v1/keys/{id}`                 | admin    |
| `POST /api/v1/keys/{id}/rotate`           | admin    |
| `DELETE /api/v1/keys/{id}`                | admin    |
| `GET  /api/v1/audit`                      | admin    |
| `GET  /api/v1/logs/stream/metrics`        | admin    |
| `GET  /api/v1/tenants`                    | platform |
| `POST /api/v1/tenants`, `DELETE ...`      | platform |

**The LLM endpoints (`/v1/chat/completions`, `/v1/messages`, `/v1/models`,
`/v1/engines/*`) deliberately remain unauthenticated** — clients send their own
provider API keys which MockAgents ignores, and forcing a second layer of
credentials would break every existing SDK.

```bash
# Mint a viewer key for a read-only CI bot:
curl -H "Authorization: Bearer $ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"ci-bot","role":"viewer"}' \
  http://localhost:8080/api/v1/tenants/$TENANT_ID/keys
```

## Rotation and role changes

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
