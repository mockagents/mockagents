# REF-08 — SaaS-tier Multi-tenancy — Design / Scope

**Status:** Scoped (design), not started · **Priority:** P2 · **Estimate:** 21 pts (4 sub-slices)
**Date:** 2026-06-05 · Closes the "Future SaaS slices" rows in PROGRESS.md §6.

This document scopes the four sub-projects bundled under REF-08. Each is an
independent, separately-shippable slice; this is the design hand-off, not an
implementation.

## 1. Decisions locked in

| Decision | Choice |
| -------- | ------ |
| Scope | All four sub-areas designed to build-ready depth: per-tenant agent collisions, Postgres tenancy store, quotas & enforcement, SSO/OAuth. |
| Postgres | **Pluggable, tenancy-only first.** A `pgx` `Store` impl selected by `MOCKAGENTS_TENANCY_DSN`; SQLite stays the default. Interaction logs + audit stay SQLite for this slice. |
| Quotas/billing | **Enforcement only** — per-tenant rate + spend caps → 429/402 with refill windows. No payment provider, no invoices. |

## 2. Current state (what we build on)

Verified against the tree (2026-06-05):

- **`tenancy.Store` is already an interface** (`internal/tenancy/store.go`) with `SQLiteStore` as the sole impl. Tenant + API-key CRUD, rotation, and `Resolve(plaintext) → *Principal` are all defined on it. → Postgres is "add one impl", not a refactor.
- **API keys are production-grade**: `mak_<8hex>_<secret>` format, bcrypt(cost 10), prefix-indexed lookup, timing-oracle defense, a SHA-256-keyed bounded TTL **auth cache** (`auth_cache.go`) flushed on mutation. RBAC roles `viewer<editor<admin<platform`; platform is bootstrap-only and `IsAssignableViaAPI()` blocks self-escalation.
- **Auth is API-key only** — `Authorization: Bearer` / `X-Api-Key`. A grep for `oauth|oidc|jwt|saml|session` returns **zero** Go matches. The GUI stores the raw key in an HttpOnly cookie. SSO is fully greenfield.
- **Agents are keyed globally by `name`** (`engine/agent_registry.go`: `agents map[string]*Def`). A `visibleTo(def, tenantID)` filter + a `byModelTenant` index give tenant-scoped *visibility*, but two tenants **cannot** both own `weather-agent` — the second `Register` overwrites the first.
- **Cost is tracked, never enforced.** `pricing/` computes USD; `GET /api/v1/costs` aggregates per tenant. The only rate-limit is the per-**agent** chaos injector (`engine/chaos.go`), not per-tenant.
- **All stores are pure-Go SQLite** (`modernc.org/sqlite`), no cgo. `pgx`, `go-oidc`, and `x/oauth2` are all pure-Go too, so the no-cgo constraint holds.
- Multi-tenant mode is `MOCKAGENTS_MULTI_TENANT=1` (`cmd/mockagents/start.go`), using `.mockagents-tenancy.db`.

## 3. Sub-slice A — Per-tenant agent-name collisions  (~5 pts, foundational)

**Goal.** Two tenants can each own an agent named `weather-agent`; a global
(unowned) agent remains a shared fallback. Single-tenant mode is unchanged.

**Design.**
- Re-key the registry from `name` to a composite. Keep it readable:
  `agents map[string]map[string]*Def` (`tenantID → name → def`), with `""` the
  global bucket. The existing `byModelTenant map[model]map[tenantID]*Def` stays.
- `Register(def)` writes under `def.Metadata.TenantID`. `RegisterWithSource`
  parallel (mirrors the pipeline registry).
- `GetForTenant(name, tenantID)`: look up `agents[tenantID][name]`, fall back to
  `agents[""][name]` (global). `GetByModelForTenant` already does this — keep.
- `ListForTenant(tenantID)`: tenant's agents ∪ globals (tenant wins on name tie).
- `Remove(name, tenantID)`; `ReloadAgent` matches `(tenant, name)` (it already
  compares `existing.Metadata.TenantID`).
- **Back-compat:** single-tenant agents all live under `""`; every current path
  resolves identically. The change is observable only when `tenant_id` is set.

**Open question A1 — how do per-tenant agents get loaded?** Today agents load
from a directory at boot (global). Per-tenant agents presuppose either (a) the
`tenant_id:` YAML field + a shared dir, or (b) a future per-tenant agent-upload
API. This slice delivers the *registry keying*; the upload/management path is
called out as a dependency for a later slice, not built here.

**Risks.** Hot-path lookup change — keep `GetByModelForTenant` O(1). Breaking
for anyone relying on cross-tenant name reuse (nobody, by construction).

**Tests.** Two tenants, same name → independent resolution; global fallback;
tenant agent shadows a global of the same name; single-tenant unchanged.

## 4. Sub-slice B — Postgres tenancy store  (~6 pts)

**Goal.** A `PostgresStore` implementing `tenancy.Store`, selected by DSN;
SQLite remains the zero-dependency default.

**Design.**
- Add `github.com/jackc/pgx/v5` (pure-Go) + `pgxpool`.
- `internal/tenancy/postgres_store.go` implements every `Store` method against
  the same logical schema (`tenants`, `api_keys`). Dialect deltas: `TEXT`/`SERIAL`
  vs SQLite, `$1` placeholders, `ON CONFLICT`, `TIMESTAMPTZ`. Inline
  `CREATE TABLE IF NOT EXISTS` migrations (same pattern as SQLite).
- **Concurrency:** SQLite serializes via `MaxOpenConns=1` for read-modify-write.
  Postgres uses real transactions with `SELECT … FOR UPDATE` for
  `UpdateAPIKeyRole` / `Rotate*` / `last_used`, so it scales past one writer.
- **Shared auth cache + bcrypt:** extract the SHA-256 TTL cache (currently a
  field on `SQLiteStore`) into a small reusable type both stores embed, so
  `Resolve` parity (prefix lookup → cache → bcrypt → timing defense) is identical.
- **Selection:** `start.go` reads `MOCKAGENTS_TENANCY_DSN`; set → `NewPostgresStore(dsn)`,
  unset → `NewSQLiteStore` (default). Refactor `bootstrapTenancy` to take
  `tenancy.Store` (interface), not `*SQLiteStore`.
- **Helm:** document the DSN env + a Secret for credentials; logs/audit stay SQLite.

**Risks.** SQL dialect divergence; Resolve parity. Tests need a real Postgres.

**Tests.** A **Store conformance suite** (table-driven, runs against any `Store`)
exercised against SQLite always, and against Postgres when `MOCKAGENTS_TEST_PG_DSN`
is set (CI service container / testcontainers; skipped locally without it).

## 5. Sub-slice C — Per-tenant quotas & enforcement  (~5 pts)

**Goal.** Enforce, per tenant: a **request rate** cap and a **monthly spend**
cap. Over rate → `429` + `Retry-After`; over spend → `402`. Enforcement only —
no payment provider.

**Design.**
- **Quota config per tenant:** a `tenant_quotas` table (`tenant_id`,
  `rate_per_sec`, `rate_burst`, `monthly_spend_usd`) in the tenancy store, plus
  env defaults (`MOCKAGENTS_DEFAULT_RATE`, `…_SPEND`). A management endpoint
  `PUT /api/v1/tenants/{id}/quota` (admin/platform) edits it; `GET` reads it.
- **Enforcement middleware** on the LLM endpoints (`/v1/*`), after auth so the
  principal/tenant is known and before the engine runs:
  - *Rate:* an in-memory token-bucket per tenant (generalize the chaos
    `rateBucket`, keyed by tenantID). Reject with `429` + `Retry-After`.
  - *Spend:* an in-memory running spend counter per tenant per UTC month,
    **seeded at startup** from interaction-log cost sums and incremented as the
    log worker computes each response's cost. Over cap → `402`. Resets at the
    month boundary.
- **Observability:** `GET /api/v1/quota` returns the caller tenant's limits +
  current usage; surfaced in the GUI costs page.

**Risks / explicit limits.**
- **Multi-replica:** in-memory counters are per-process, so across replicas
  enforcement is approximate. For accurate horizontal scale, back the counters
  with Postgres atomic increments (`INSERT … ON CONFLICT DO UPDATE … RETURNING`)
  — available once sub-slice B lands. This slice ships **single-process-accurate**;
  the Postgres-backed counter is a documented follow-on (gated on B).
- Spend lags by the async-logging window (seconds) and streaming-response cost
  finalization — caps are soft by a small margin. Documented, not silently hidden.

**Tests.** Rate bucket → 429 + Retry-After; spend counter seed + increment +
402 + month reset; quota CRUD authz; single-tenant mode unaffected (no tenant → no quota).

## 6. Sub-slice D — SSO / OAuth login  (~5 pts, largest surface)

**Goal.** Operators sign in via OIDC; the GUI uses a **session** instead of
storing a raw API key. API keys remain for programmatic/CLI access.

**Design.**
- **OIDC relying-party flow** (`coreos/go-oidc` + `x/oauth2`, both pure-Go):
  `GET /auth/login` → redirect to the IdP with `state` + **PKCE**;
  `GET /auth/callback` → verify `state`, exchange code, validate the ID token.
- **Identity → tenant/role mapping:** a new `users` table (`id`, `email`,
  `tenant_id`, `role`) lives in the tenancy store. Mapping policy (pick one,
  config-driven): email-domain → tenant, or admin-issued invitations. This is
  the genuinely new domain.
- **Sessions:** server-side **opaque** session tokens in a `sessions` table
  (`token_hash`, `user_id`, `tenant_id`, `role`, `expires_at`) — revocable,
  unlike a bare JWT. Set as an HttpOnly cookie. TTL + sliding refresh.
- **Auth middleware:** accept **either** an API key (existing `Resolve`) **or** a
  session cookie (new `ResolveSession`) → the same `*Principal`. One unified
  principal downstream, so every existing authz check is unchanged.
- **GUI:** `/login` gains "Sign in with SSO"; the session cookie replaces the
  raw-key cookie for SSO users. Raw-key login stays for key holders.
- **Config:** `MOCKAGENTS_OIDC_ISSUER` / `_CLIENT_ID` / `_CLIENT_SECRET` /
  `_REDIRECT_URL`. SSO is enabled only when these are set.

**Risks.** Security-critical and the largest lift: `state`/PKCE/CSRF on the
callback, session fixation/rotation, secret handling, the users/mapping domain.
Sequence it **last** so it builds on the Postgres store + a stable principal.
Warrants its own security review (extend `docs/SECURITY-REVIEW*.md`).

**Tests.** Callback `state` mismatch rejected; ID-token validation; session
issue/resolve/expire/revoke; API-key path unaffected; middleware accepts both.

## 7. Recommended sequencing

```
A. Per-tenant agent collisions   ── self-contained engine change; do first
        │
B. Postgres tenancy store         ── Store impl ready to drop in; unblocks C's
        │                            multi-replica counters and D's users/sessions
        ├──────────────► C. Quotas & enforcement  (single-process first;
        │                    Postgres-backed counters once B lands)
        └──────────────► D. SSO/OAuth  (largest; needs users/sessions tables — last)
```

Rationale: A is independent and high-leverage. B is low-risk (interface exists)
and is the substrate C (accurate counters) and D (users/sessions) both want. C
is useful even single-process. D is the heaviest and most security-sensitive, so
it goes last on the most stable base.

## 8. Cross-cutting

- **No cgo:** `pgx`, `go-oidc`, `x/oauth2` are pure-Go — constraint preserved;
  `goreleaser` cross-compile stays simple.
- **Single-tenant mode is untouched** in every slice (`tenantID == ""` paths
  resolve exactly as today). REF-08 only changes behavior under
  `MOCKAGENTS_MULTI_TENANT=1`.
- **Horizontal scale boundary:** the agent registry and the in-memory quota
  counters are per-process. True multi-replica isolation/enforcement needs
  shared state (Postgres) — documented per slice rather than implied.
- **Logs/audit stay SQLite** this cycle (per the Postgres decision). A real
  high-volume SaaS deployment would eventually want the interaction-log path on
  Postgres too (≈864M rows/day at 10k req/s) — a future slice, out of scope here.

## 9. Test & rollout strategy

- Each slice ships behind the existing `MOCKAGENTS_MULTI_TENANT` flag with its
  own env switches (`_TENANCY_DSN`, `_DEFAULT_RATE`/`_SPEND`, `_OIDC_*`), so each
  is independently enable-able and revertible.
- A `tenancy.Store` **conformance suite** is the spine for B (and guards SQLite
  from regressions).
- D extends the security-review docs before merge.
- Update `docs/api-spec.yaml` per slice (quota endpoints, `/auth/*`); `driftcheck`
  keeps the spec honest.

## 10. Open questions (carry into per-slice kickoff)

1. **A** — per-tenant agent *provisioning* (upload API vs shared-dir `tenant_id:`): which, and is it in REF-08 or a follow-on? (Recommend: keying now, provisioning later.)
2. **B** — do we want a single `MOCKAGENTS_TENANCY_DSN` or a generic `…_DSN` that later also routes logs/audit? (Recommend tenancy-specific now; rename if/when logs move.)
3. **C** — month boundary in UTC vs per-tenant timezone; and the soft-margin tolerance we advertise.
4. **D** — identity→tenant mapping policy (email-domain vs invitations) and whether SCIM/just-in-time provisioning is ever in scope.
