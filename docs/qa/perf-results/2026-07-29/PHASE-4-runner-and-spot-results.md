# Cycle 3 — Phase 4: runner pipelines + spot regressions (2026-07-29)

**Cases:** TC-PERF-16 (new), TC-PERF-08, TC-PERF-12 · **All PASS.**
Balanced power plan; build `c1d9429`.

## TC-PERF-16 — Test-runner pipeline execution (NEW): PASS, with a caveat

Fixtures authored for this case (committed to `agents/` staging, reproducible
from `examples/`): a **parallel** pipeline (`perf-parallel-pipeline`, same two
nodes as `research-pipeline`) — `examples/` ships only a sequential topology —
plus two 20-case timing suites (`perf-seq-suite`, `perf-par-suite`).

| Suite | Wall clock ×3 | Cases | Per-case execution (from JSON) |
|---|---|---|---|
| sequential | 311 / 218 / 261 ms | 20/20 pass | p50 ~0.000 ms, max 1.619, **total 1.6 ms** |
| parallel | 286 / 258 / 304 ms | 20/20 pass | p50 ~0.000 ms, max 1.734, **total 2.8 ms** |

**Headline: pipeline execution is effectively free — the CLI is the cost.**
Twenty pipeline cases execute in **1.6–2.8 ms total**, while the whole
`mockagents test` invocation takes **~220–310 ms**. Over 98% of the wall clock
is process startup plus agent-directory loading, not pipeline work. For users
this means: if a `mockagents test` run feels slow, look at how many YAML docs
the agents dir holds, not at pipeline complexity.

Run-to-run variance: sequential 218–311 ms (±18%), parallel 258–304 ms (±8%) —
both inside the <20% criterion, and both dominated by that fixed startup cost.

**Caveat on the topology comparison (recorded, not papered over):** the case
asked whether parallel is slower than sequential for the same node count.
Measured, parallel totals 2.8 ms vs sequential 1.6 ms — but at sub-millisecond
per-case execution this difference is goroutine-scheduling noise on a
zero-latency workload, not a topology finding. **A meaningful comparison needs
nodes that actually take time** (e.g. chaos latency-injected agents so each
node costs ~100 ms); then parallel should finish in ~1 node-time while
sequential takes N×. Recommended for cycle 4.

## TC-PERF-08 — Chaos isolation: PASS (spot regression)

Latency-only chaos target sanity-probed first (182–261 ms, inside the
configured 100–300 ms band).

| Healthy phase | n | p50 | p95 |
|---|---|---|---|
| solo-before | 100,962 | 5.35 | 8.31 |
| overlap | 375,493 | 5.35 | **8.30** |
| solo-after | 91,530 | 5.60 | 7.92 |

**p95 ratio overlap/solo = 0.999** — the tightest result across three cycles
(1.102 → 1.111 → 0.999). Healthy p50 identical during overlap. Chaos role
behaved: 11,643 requests, p50 206.6 / p95 297.1 ms. Zero errors both roles.

## TC-PERF-12 — MCP tools/call: PASS (spot regression)

| Mode | RPS | p50 | p95 | JSON-RPC errors |
|---|---|---|---|---|
| single shared session | 3,808 | 10.72 | 22.81 | **0** |
| one session per worker | 3,833 | 11.05 | 21.30 | **0** |

**Ratio 1.01** (cycle 2: 1.02) — still no session-lock bottleneck. Throughput
is ~8% below cycle 2's 4,133/4,210 RPS, well inside the 20% gate and in line
with this cycle's slightly lower HTTP numbers generally (3,849 vs 4,085 RPS on
TC-PERF-02), i.e. a whole-machine level difference rather than an MCP-specific
one. Zero protocol errors across ~460k calls.

## Tooling

`chaos_isolation.py` and `mcp_perf.py` joined the committed toolset
(`docs/qa/perf-tools/`), each carrying the setup gotchas that cost time in
earlier cycles (latency-only chaos target, separate load processes,
`spec.behavior.chaos` + `enabled: true`, 127.0.0.1 not localhost).

**Cycle 3 execution complete** — rollup next.
