# Review Summary — `internal/server`

- **Target:** directory `internal/server/` (12 source files, full-file review)
- **Reviewed at:** 2026-06-02 00:21  ·  **Depth:** standard (all passes; S0/S1 adversarially verified)
- **Scope:** 12 `.go` files, ~5,777 LOC + 12 test files. Excluded: vendored deps, generated code. Cross-package reads (Pass 2): `internal/tenancy/{middleware,store,types}.go`, `cmd/mockagents/start.go`, `internal/config/validate_bytes.go`, `internal/adapter/openai.go`.
- **Reviewer:** multi-pass-review skill (Pass 1 fanned out across 7 subagents; Pass 2 + synthesis single-threaded)

## Verdict

> **GO WITH FIXES** — one **S0 cross-tenant IDOR** in the management API blocks any multi-tenant deployment and must be fixed before the next multi-tenant release; the single-tenant LLM-mock path is sound. The S0 is real but the LLM endpoints (`/v1/chat/completions`, `/v1/messages`) and single-tenant mode are unaffected.

## Findings by severity

| Severity | Count | Notable IDs |
|----------|-------|-------------|
| S0 Blocker | 1 | `X-SEC-001` (tenant-IDOR cluster, 6 sites) |
| S1 High    | 3 | `X-DOS-001` (unbounded body), `F-HD-001` (ReloadAgent ungated), `F-LW-001` (send-on-closed-channel race) |
| S2 Medium  | 14 | `X-AUTHZ-001`, `X-SHUT-001`, `X-ERR-001`, `F-LH-001`, `F-WT-001`, `F-TN-007`, `F-LH-002`, … |
| S3 Low     | ~20 | `F-LH-009` (dead `init()`), `F-SV-006`, doc-drift, magic numbers, … |

## Top risks (the 5 that matter)

1. **Cross-tenant IDOR in the management API** (`X-SEC-001`, S0) — `RequireRole` checks only the role *level*, never that the caller owns the `{id}`/`{tenantID}` in the path; the handlers don't either; the store keys by global id. A tenant-A **admin** can read/rotate/delete/create tenant-B's API keys and bulk-rotate B's keys. Exploitable on any deployment with ≥2 tenants (the entire point of multi-tenant mode). Self-service `/keys/me/*` routes are correctly principal-scoped — only the `{id}`-addressed routes are affected.
2. **Unbounded request bodies → memory DoS** (`X-DOS-001`, S1) — `validate_handler` does `io.ReadAll(r.Body)` with no `MaxBytesReader`; the tenancy handlers `json.Decode` with no cap either. A single large POST OOMs the process. Reachable by an `editor` (validate) or `admin` (tenancy).
3. **`ReloadAgent` is not role-gated** (`F-HD-001`, S1) — every other mutation uses `RequireRole`, but `POST /api/v1/agents/{name}/reload` (a disk-touching, registry-mutating op) is mounted bare. A `viewer` can trigger reloads in multi-tenant mode; it's unauthenticated in single-tenant mode.
4. **Async log worker: send-on-closed-channel race** (`F-LW-001`, S1) — `Submit` checks an atomic `stopped` flag then sends, while `Shutdown` sets the flag and `close()`s the queue non-atomically. A `Submit` concurrent with `Shutdown` can panic the process. Untested (and `-race` is unavailable here), so the logic fix is the only guard.
5. **Inconsistent route authorization & input limits** (`X-AUTHZ-001` / `X-LIMIT-001`, S2) — `costs` and `pipelines` have no role gate while `audit`/`validate`/`tenancy` do; `limit` is clamped in `costs` (10000), unclamped in `audit`/`logs`. The gating/clamping policy is ad-hoc per route, which is how the ReloadAgent gap (#3) slipped in.

## Coverage & confidence

- Passes run: 0, 1 (per-file, 7 subagents), 2 (cross-file), 3 (synthesis), 4 (reports).
- **S0/S1 adversarially verified:** yes. `X-SEC-001` confirmed by reading `tenancy/middleware.go` (`RequireRole` is role-only), `tenancy/types.go` (`Principal.TenantID` exists, keys are per-tenant), `cmd/start.go` (admin key minted *into* a tenant; `CreateTenant` lets admins spawn tenants) — there is **no** super-admin tier and **no** `WithPrincipalTenantScope` (that cross-file note was speculative; the symbol does not exist). `X-DOS-001`, `F-HD-001`, `F-LW-001` each confirmed against the exact lines.
- **Not covered / blind spots:** (a) the `tenancy` and `storage` packages were read only enough to resolve the seams — they are **not** themselves reviewed here (e.g. whether `SQLiteStore.Query` truly parameterizes every filter, whether `yaml.Unmarshal` bounds alias expansion for billion-laughs — flagged as dependencies, not verified). (b) `conformance_test.go` (cross-adapter suite) was not deeply audited. (c) No live concurrency/stress run was performed (`-race` unavailable, no cgo); concurrency findings are by inspection. (d) The adapter handlers' own correctness is out of scope (reviewed only at the seam).

## Where to act

→ Execute **`03-ACTION-PLAN.md`** (P0 first — the IDOR gates the next multi-tenant release). Per-file detail in `01-PER-FILE.md`; cross-file relationships + the blast-radius map in `02-INTEGRATION.md`.
