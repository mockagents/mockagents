# Cycle 2 — Phase 2: regression core (2026-07-28)

**Build:** `b3df416` (engine code identical to baseline `0b826be`) ·
**TC-PERF-01/02/03/04 + client cross-validation: all PASS — zero regressions.**
Two gate-calibration findings, both fed back into the plan.

## Results vs 2026-07-27 baselines

| Case | Cycle-2 numbers | Baseline | Verdict |
|---|---|---|---|
| **01** bench | allocs/op **17/17 exact**; suspect ns/op rows re-run isolated `-count=5`: stable ±3% (e.g. `ResponseGenerator_Static` 49–52 ns) | allocs table | ✅ no regression |
| **02** throughput | k6 ramp: 556,636 reqs, **0.00% failed**, 3,092 RPS avg, p95 45.4 ms < 50 gate, med 8.2 ms; Python @50 threads (Balanced): **4,085 RPS, p95 16.96 ms** | 3,796–3,974 RPS, p95 17.2–20.5 ms | ✅ equal/better |
| **03** streaming | stock: p95 2.22 s; heavy @100 VU/120 s: p95 2.32 s; 0% errors both | ≪ 8 s gate | ✅ |
| **04** TTFT | @20u: p50 370 / p95 1400 ms; @100u: p50 360 / p95 1400 ms; 0 failures (897 + 4,427 streams) | bands 250–500 / 900–2100 | ✅ pacing holds at 5× |
| Cross-validation | k6 med 8.2 ms ≈ Python p50 9.7–11.2 ms on the same build — clients agree to ~20% at the median; use one client consistently per metric | — | calibrated |

## Gate-calibration findings (no code implicated — engine byte-identical to baseline)

1. **Full-suite ns/op is untrustworthy on P/E-core CPUs.** The full bench
   suite showed ±30–113% swings on sub-µs rows in both directions with
   identical code; the same benches isolated with `-count=5` are stable
   within ±3% (and far faster: `ScenarioMatcher_Regex` 264–272 ns vs
   424–572 in-suite). **Rule:** on heterogeneous-core machines, verify any
   ns/op outlier with an isolated `-count=5` run before filing; `allocs/op`
   (exact) remains the primary gate. Strengthens the case for the non-AV
   fixed-clock second-baseline box (phase-1 open action).
2. **HTTP tail latency is power-plan-sensitive; throughput is not.** Same
   build, same fresh server: High-performance plan → p95 27.1 ms; Balanced →
   p95 16.96 ms; RPS identical (~4k). Cycle-1 HTTP baselines were captured
   under Balanced. **Rule:** record the active power plan with every HTTP
   run and compare matched plans only. Benches stay High-perf per §4.1;
   HTTP baselines for this box are Balanced.

Confound also ruled out en route: a warm session store (~800k live sessions
from the preceding k6 ramp) was eliminated as the cause via a fresh-server
rerun — the tail delta tracked the power plan, not the session store.

## Environment notes

AV-avoidance worked: per-run `MOCKAGENTS_DATA_DIR` scratch dirs (no DB
deletions), repo-local builds + `GOTMPDIR`. k6 v2.1.0 first production use
this phase. Raw artifacts: `TC-PERF-02-k6.json`, `TC-PERF-03{a,b}-k6.json`,
`TC-PERF-04{a,b}*.csv` in this directory.

**Next:** phase 3 — 90-min soak (private-bytes sampling) + 05, 07, 11.
