# MockAgents Performance Cycle 3 — Summary Report

**Date:** 2026-07-29 · **Plan:** MA-QA-PTP-001 v1.3 · **Builds:** `ac14f81` → `c1d9429`
**Executed:** 11 cases — **11/11 PASS · 1 blocked (environmental) · 0 regressions · 0 open product defects**

## 1. Executive summary

Cycle 3 pushed into the surfaces nobody had load-tested: **A2A**, **Batch
fan-out**, and **runner-side pipelines** — all three baselined clean. The
regression set held for a third consecutive cycle. Two structural findings
came out of it: batch fan-out is **sub-linear** (per-request cost drops 6× as
batch size grows 50×), and pipeline execution is **effectively free** with
>98% of a `mockagents test` run spent on CLI startup rather than pipeline
work. One case (Postgres tenancy) was blocked before the cycle began, for
environmental reasons documented up front rather than discovered mid-run.

## 2. Scoreboard

| Case | Result | Verdict |
|---|---|---|
| **01** engine bench | allocs/op **17/17 exact** (3rd cycle); ns/op gate retired for sub-µs rows — see §4 | ✅ |
| **02** throughput | k6 507,906 reqs **0.00% failed**, p95 46.4 ms < 50 gate; Python 3,849 RPS / p95 18.59 ms | ✅ |
| **03** streaming | p95 2.36 s @20 VU · 2.24 s @100 VU, 0% errors | ✅ |
| **04** TTFT | p50 380/360 ms · p95 1.5 s @20u/@100u, 0 failures | ✅ |
| **08** chaos isolation | p95 ratio **0.999** (best of 3 cycles) | ✅ |
| **12** MCP | 3,808 / 3,833 RPS, ratio **1.01**, 0 protocol errors | ✅ |
| **13** A2A **(new)** | send **3,953 RPS** (within 3% of OpenAI adapter); 0 id mismatches / 237k; all 185k streams `final:true` | ✅ |
| **14** Batch fan-out **(new)** | per-request **0.164 → 0.027 ms** as N goes 100 → 5,000; ~36,500 req/s at N=5,000; file lifecycle 85 ms for N=1,000 | ✅ |
| **16** runner pipelines **(new)** | 20 cases execute in **1.6–2.8 ms**; invocation ~250 ms — startup-dominated | ✅ |
| **15** Postgres tenancy | **BLOCKED** — no Postgres obtainable (see §5) | ⛔ |

## 3. What cycle 3 established

- **A2A is not a slow path.** `message/send` runs within 3% of the OpenAI
  adapter; task-lifecycle bookkeeping is free at this scale. Both correctness
  invariants held under sustained load (id echo, `final:true` termination).
- **Batch fan-out is sub-linear and fast.** Bigger batches are *cheaper per
  request* — ~36,500 requests/second of dispatch at N=5,000, an order of
  magnitude above the per-request HTTP path.
- **Pipeline cost is startup, not execution.** If a customer's suite is slow,
  the lever is the size of the agents directory, not pipeline complexity.

## 4. The ns/op gate is retired (measurement finding)

Cycle 2 flagged full-suite `ns/op` as unstable on this P/E-core box and added
an isolated-`-count=5` verification rule. Cycle 3 ran that rule and found it
insufficient: `ResponseGenerator_Static` measures **49–52 ns one day, 77–94 ns
the next — in isolation, on identical code** (and 232 ns in-suite). Isolation
narrows the spread but doesn't make sub-µs `ns/op` a usable signal.

Plan §4.1 now gates on **`allocs/op` + `B/op`** (exact, machine-independent,
clean for three cycles) plus the µs-scale `ProcessRequest_*` rows, and treats
sub-µs `ns/op` as informational. **Consequence:** the second-baseline box
moves from *nice-to-have* to **required** for any ns/op gating at all.

## 5. TC-PERF-15 blocked — and not faked

Three routes checked before the cycle started: Rancher/Docker has been removed
from the box; podman is running but `postgres:16` can't be pulled (DNS fails
in the VM **and** the host can't reach `registry-1.docker.io` — TLS
`CRYPT_E_NO_REVOCATION_CHECK`, the same proxy interception that blocked Alpine
`apk` in cycle 1); and `C:\Program Files\PostgreSQL\17` has no server
binaries. Per plan, a different store is **not** an acceptable substitute, so
the case is `Blocked`, not approximated.

**Unblock options:** internal registry mirror for `postgres:16` · proxy
exception for Docker Hub · native PostgreSQL 17 install · a shared dev
Postgres via `MOCKAGENTS_TENANCY_DSN` (note the added network latency in the
results if so).

## 6. Durable tooling (new this cycle)

Load drivers kept evaporating from scratch storage at session boundaries, so
they now live in the repo at **`docs/qa/perf-tools/`**: `k6-nostream.js`,
`load_driver.py` (stdlib-only, no k6 needed), `a2a_perf.py`, `batch_perf.py`,
`chaos_isolation.py`, `mcp_perf.py`. Each carries the setup gotchas that cost
time in earlier cycles. QA can now reproduce any case without rebuilding
scripts.

## 7. Open items for cycle 4

1. **Second-baseline box** — now *required* for ns/op gating, not optional.
2. **Unblock TC-PERF-15** (see §5) — the only untested storage backend.
3. **Redo the TC-PERF-16 topology comparison with latency-injected nodes** —
   at sub-ms execution the parallel-vs-sequential question is unanswerable;
   with ~100 ms nodes it becomes meaningful.
4. **CI trend-tracking** of `docs/benchmarks/latest.json` — still the
   highest-leverage automation available.
5. Gate 13/14/16 at >20% against these baselines.

## 8. Artifact index

`PHASE-1-prep.md` · `PHASE-2-regression-results.md` (+ k6/Locust exports) ·
`PHASE-3-new-surfaces-results.md` · `PHASE-4-runner-and-spot-results.md` ·
this summary. Tracker: `*-C3` rows plus 13/14/15/16. Plan: §7 cycle table.
