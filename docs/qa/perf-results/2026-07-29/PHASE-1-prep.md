# Cycle 3 — Phase 1: prep (2026-07-29)

**Plan:** MA-QA-PTP-001 v1.3 §9 · **Status: complete** ·
**One case blocked up front: TC-PERF-15 (environmental).**

## Machine — unchanged from cycle 2, so baselines remain comparable

| Item | Value |
|---|---|
| CPU / RAM | Intel Core Ultra 9 285K, 24c/24t · 31.5 GB |
| OS | Windows 11 Pro 10.0.26200 |
| Power plan | **Balanced** (matches the HTTP baselines per §4.1; switch to High-performance only for the TC-PERF-01 bench run) |
| Go | go1.26.4 · k6 v2.1.0 · Locust 2.43.4 |
| Build | `mockagents-perf.exe` from `3ced5c3`; `agents/` staged (examples + `perf-echo`, `perf-echo-ant`); `validate` clean |

## TC-PERF-15 (Postgres tenancy): **BLOCKED — no Postgres obtainable**

Determined before the cycle started, per the plan's blocked-if-unavailable
rule. Three routes checked, all closed:

1. **Docker/Rancher Desktop — gone.** The CLI binary is absent and the
   `rancher-desktop` WSL distro is `Stopped`. (Cycle 1 saw this runtime die
   mid-session; it has since been removed from the box.)
2. **Podman — running, but no egress.** `podman-machine-default` is up
   (podman 5.8.5), so the runtime itself is fine, but `postgres:16` cannot be
   pulled: DNS fails inside the VM, and the **host** can't reach the registry
   either — `registry-1.docker.io` and `auth.docker.io` both fail TLS with
   `CRYPT_E_NO_REVOCATION_CHECK`. This is the same corporate-proxy
   interception that blocked Alpine `apk` in cycle 1, so repairing the VM's
   DNS would not have helped.
3. **Native Postgres — not installed.** `C:\Program Files\PostgreSQL\17`
   exists but contains only profiler/ODBC leftovers (`lib`, `share`, `data`);
   there is no `bin/` — no `postgres.exe`, `pg_ctl`, `initdb`, or `psql`.

**Not substituted.** Per plan §TC-PERF-15, a different store is not an
acceptable stand-in, so the case is recorded **Blocked** rather than
approximated. Cycle 3 continues with 13, 14, 16 and the regression set.

**To unblock (team action):** provide a reachable Postgres — an internal
registry mirror for `postgres:16`, a proxy exception for Docker Hub, a
native PostgreSQL 17 server install, or a shared dev Postgres instance
(`MOCKAGENTS_TENANCY_DSN` accepts any reachable DSN, so a remote instance
works — though a network-hop DSN should be noted in the results, since it
adds latency the SQLite comparison doesn't have).

## Fixtures verified for the new cases

- **TC-PERF-13 (A2A):** `examples/a2a-server.yaml` → `kind: A2AServer`,
  name `weather-a2a`, one skill. Serves on port **8083** by default.
- **TC-PERF-14 (Batch):** `perf-echo-ant` staged for the Anthropic inline
  path; default batch delay is 0 (instant completion) — no header needed.
- **TC-PERF-16 (runner pipelines):** `examples/research-pipeline.yaml`,
  `topology: sequential`. Only a sequential topology ships in `examples/`, so
  the sequential-vs-parallel comparison in the case needs a hand-authored
  parallel pipeline — will author one at execution time.

## Phase-2 entry: clear

Regression set (01–04) gates against both 2026-07-27 and 2026-07-28 numbers.
