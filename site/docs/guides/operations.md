# Operations

Running MockAgents as shared infrastructure rather than on a laptop. Every
procedure here has been checked against the current code; where a capability
does not exist, this guide says so instead of implying one.

The short version: MockAgents keeps its state in three SQLite files (or
Postgres for tenancy), holds exactly one credential you cannot regenerate from
the API, and keeps several runtime stores per process even when tenancy uses a
shared backend.

## What has state

| Store | Default path | Holds | Lost if deleted |
| --- | --- | --- | --- |
| Interaction log | `.mockagents.db` | Every request/response the server handled | Dashboard history and cost aggregates |
| Audit trail | `.mockagents-audit.db` | Who created, changed or deleted what, plus auth denials | Your record of administrative actions |
| Tenancy store | `.mockagents-tenancy.db`, or Postgres | Tenants, API key hashes, roles, SSO users and sessions, quota overrides and the spend ledger | Every credential; nobody can authenticate |

All three land in the working directory unless `MOCKAGENTS_DATA_DIR` points
elsewhere. Agent, pipeline, MCP and A2A definitions are plain YAML in the
agents directory — those belong in version control, not in a backup job.

Only the tenancy store is unrecoverable. The other two are observability data:
losing them costs you history, not access.

## Backup and restore

SQLite is a live database, so copying the file while the server is writing can
capture a torn page. Use the online backup, which is consistent without
stopping the server:

```bash
sqlite3 .mockagents-tenancy.db ".backup '/backups/tenancy-$(date +%F).db'"
sqlite3 .mockagents-audit.db   ".backup '/backups/audit-$(date +%F).db'"
sqlite3 .mockagents.db         ".backup '/backups/interactions-$(date +%F).db'"
```

The databases run in WAL mode, so a plain `cp` of the `.db` file alone can miss
committed transactions still in the `-wal` sidecar. If you must copy files,
stop the server first and copy the `.db`, `-wal` and `-shm` files together.

Restore is the reverse, with the server stopped:

```bash
# stop the server first
cp /backups/tenancy-2026-09-08.db .mockagents-tenancy.db
rm -f .mockagents-tenancy.db-wal .mockagents-tenancy.db-shm
# start the server
```

For Postgres tenancy, back up with your normal `pg_dump` schedule; MockAgents
adds no requirements beyond the connection.

Back up the tenancy store on the same schedule as anything else holding
credentials. The audit trail deserves the same treatment if you rely on it for
compliance; the interaction log usually does not.

## Recovering a lost platform key

The platform role is the cross-tenant operator: it manages the tenant
collection. The management API refuses to mint it, so a per-tenant admin
cannot escalate to it — which also means there is no "reissue" endpoint.

Bootstrap mints a platform key **only when the default tenant has no
platform-role key at all**. Restarting the server does not produce a new one.

Two recovery paths, in order of preference:

**1. Rotate it with another platform key.** Rotation regenerates
a key's secret in place, preserving its id, name, role and tenant, and returns
the new plaintext once:

```bash
curl -sX POST http://localhost:8080/api/v1/keys/<platform-key-id>/rotate \
  -H "Authorization: Bearer $PLATFORM_KEY"
```

Find the id with `GET /api/v1/tenants/<default-tenant-id>/keys` using the same
platform key. Tenant admins cannot mutate platform credentials.

**2. Delete the row and re-bootstrap.** If no platform key survives, stop the
server, remove the platform key row, and start it again with
the plaintext you want:

```bash
sqlite3 .mockagents-tenancy.db "DELETE FROM api_keys WHERE role='platform';"
MOCKAGENTS_BOOTSTRAP_KEY="$(openssl rand -hex 24)" mockagents start
```

Supplying `MOCKAGENTS_BOOTSTRAP_KEY` is better than letting the server generate
one, because a generated key is written to
`MOCKAGENTS_BOOTSTRAP_KEY_FILE` (default `<data dir>/bootstrap-admin.key`) and
you have to fetch it off the filesystem. In Kubernetes, source it from a Secret
through the chart's `existingSecret` or `extraEnvFrom` values.

Keys are stored bcrypt-hashed. Nobody, including the operator, can read an
existing key back out of the database.

## Rotating the OIDC client secret

The provider secret lives only in `MOCKAGENTS_OIDC_CLIENT_SECRET`; MockAgents
does not persist it.

1. Create the new secret at the identity provider, keeping the old one valid.
2. Update the environment (Secret, `.env`, whatever supplies it) and restart.
3. Retire the old secret at the provider.

Existing SSO sessions are unaffected: a session is an opaque token stored as a
SHA-256 hash in the tenancy store and validated against that hash, not against
the provider. To force everyone to sign in again, delete the sessions:

```bash
sqlite3 .mockagents-tenancy.db "DELETE FROM sessions;"
```

Session lifetime is `MOCKAGENTS_OIDC_SESSION_TTL` (default 24 hours), and
expired rows stop resolving on their own.

## Moving from SQLite to Postgres

Point `MOCKAGENTS_TENANCY_DSN` at Postgres and restart. The store creates its
own schema on first connection.

There is **no migration command**, and the two stores share no file format, so
the existing SQLite contents do not come with you. Because API keys are stored
as bcrypt hashes and cannot be exported as plaintext, moving means reissuing
credentials:

1. Stand up Postgres and set `MOCKAGENTS_TENANCY_DSN`.
2. Start the server with `MOCKAGENTS_BOOTSTRAP_KEY` set, which creates the
   default tenant and your platform key in the new store.
3. Recreate tenants (`POST /api/v1/tenants`) and keys
   (`POST /api/v1/tenants/{id}/keys`) with the platform key, and distribute the
   new plaintexts.
4. Re-apply per-tenant quota overrides with
   `PUT /api/v1/tenants/{id}/quota`.

Keep the SQLite file until the new store is verified: `GET /api/v1/tenants`
returning your tenant list is the check that it took.

The interaction log and audit trail stay on SQLite either way — only tenancy
has a Postgres backend.

## Upgrades

MockAgents opens its stores with `CREATE TABLE IF NOT EXISTS` at startup, so a
new version that adds a table or an index applies it on first boot. There is no
separate migration step and no down-migration.

A safe upgrade:

1. Back up the tenancy store (above). A schema change is applied in place.
2. Roll the new version out to one replica, or restart the single one.
3. Check `GET /api/v1/ready` — it reports `503` naming the failing dependency
   if a store did not open.
4. Confirm authentication still works with a real key, not just the health
   endpoint, since readiness does not exercise the tenancy store.

Downgrading after a schema change is not supported. Older instances do not
enforce credential-version revocation; drain them during upgrade and favor a
forward repair. Restore the pre-upgrade backup only as an incident procedure.

Agent YAML is validated at load time. `mockagents validate ./agents` before an
upgrade tells you whether your definitions still parse under the new version;
a definition the server rejects is logged and skipped, not fatal.

## Running more than one replica

Conversation turns commit state only after response generation succeeds. A
failed turn does not advance the counter, retain the user message, or keep
nested variable mutations. This state remains memory-backed and is lost on
restart. A2A task state is also bounded, process-local, and expires after a
terminal TTL; it is not a durable migration target.

The default deployment is a **single writer**. Two replicas sharing one SQLite
file on a shared volume will corrupt each other's writes.

| Replicas | What you need |
| --- | --- |
| 1 | Nothing. This is the default. |
| N, independent | Nothing shared. Each pod is its own mock with its own logs, audit trail and write-API state. The Helm chart makes you say so explicitly with `multiReplica.acknowledged=true`. |
| N, one system | `MOCKAGENTS_TENANCY_DSN` pointing every replica at the same Postgres. |

Even with shared tenancy, two things stay per-pod: the interaction log and the
audit trail are local SQLite files, and conversation session state lives in
memory. A client that depends on multi-turn session continuity needs sticky
routing, or it will land on a pod that has never seen its session.

Quota accounting is the exception that works properly across replicas: the
monthly spend cap goes through a shared ledger in the tenancy store, so
Postgres-backed replicas enforce one budget rather than N.

## What to watch

`GET /api/v1/ready` is the operational signal — it fails when the fixture
registry is empty or the interaction log stops answering. `GET /metrics` is a
Prometheus scrape target (platform-gated in multi-tenant mode). Both are covered
in [Observability](observability.md).

The audit trail is queryable at `GET /api/v1/audit` with an admin key. It
records tenant and key lifecycle, agent and pipeline changes, and
authentication denials — the trail to read after an incident.
