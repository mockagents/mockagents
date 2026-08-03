# MockAgents Performance Cycle 2 — Summary Report

**Date:** 2026-07-28 · **Plan:** MA-QA-PTP-001 v1.2 · **Builds:** `b3df416` → `75dd153`
**Scope:** TC-PERF-01..12 — **12/12 executed, 12/12 PASS** · **Zero regressions · Zero open product defects**

## 1. Executive summary

Cycle 2 was the first true **regression** cycle: every repeated case gated
against the committed 2026-07-27 baselines. Nothing regressed. The two new
cases closed cycle 1's coverage gaps and both came back clean — **the
Anthropic and Gemini adapters carry load within 1% of OpenAI**, and **MCP
`tools/call` sustains the same ~4.2k RPS with no session-lock bottleneck**.
The extended 90-minute soak did what cycle 1's 45-minute run couldn't: it
observed **45 minutes of post-TTL steady state** and confirmed memory
plateaus and then *declines* under continuous load. 4.6M requests, zero
errors.

## 2. Scoreboard

| Case | Cycle 2 | vs baseline | Verdict |
|---|---|---|---|
| **01** engine bench | allocs/op 17/17 exact; ns/op outliers proven to be P/E-core suite noise (isolated `-count=5` stable ±3%) | no regression | ✅ |
| **02** throughput | k6 ramp 556,636 reqs, **0.00% failed**, p95 45.4 ms < 50 gate; Python 4,085 RPS / p95 16.96 ms | ≥ 3,974 RPS / 17.2 ms | ✅ |
| **03** streaming | p95 2.22 s @20 VU · 2.32 s @100 VU, 0% errors | ≪ 8 s gate | ✅ |
| **04** TTFT | p50 370/360 ms · p95 1400/1400 ms @20u/@100u | bands hold at 5× | ✅ |
| **05** logging | none 3,833 / sanitized 3,948 / full 4,006 RPS | within noise | ✅ |
| **06** soak (90 min) | **4,623,954 reqs, 0 errors**; plateau t=40.5, drift +16.5% peak then declining; p95 12.86–16.69 ms flat; server max 98 ms | no leak | ✅ |
| **07** multi-tenant | bcrypt 66.0→7.4 ms; 3,953 RPS / p95 19.49; 122,380 × 429 all with `Retry-After`, 0 × 5xx | within 4% | ✅ |
| **08** chaos isolation | healthy p95 ratio **1.111** (cycle 1: 1.102) | stable | ✅ |
| **09** Realtime WS | 50/50 sessions, 50/50 correct order, 0 error events | identical | ✅ |
| **10** replay | 4,208 RPS ≥ live 3,982 RPS | matches | ✅ |
| **11** adapter parity **(new)** | OpenAI 3,998 · Anthropic 4,039 (**+1%**) · Gemini 4,039 (**+1%**), 0 errors | baseline set | ✅ |
| **12** MCP throughput **(new)** | single-session 4,133 / per-worker 4,210 RPS, **ratio 1.02**, 0 protocol errors over ~500k calls | baseline set | ✅ |

## 3. What cycle 2 learned that cycle 1 couldn't

- **Steady state exists.** 45 minutes past the TTL boundary, private bytes
  oscillate around ~4.7 GB and the final three samples *decline*. The
  session-TTL sawtooth is a bounded working set, not a slow leak.
- **Adapters are not a differentiator.** Anthropic and Gemini match OpenAI
  to within 1% — the shared engine dominates; per-adapter codecs are noise.
  Cycle 1's OpenAI-only measurements were representative after all, now
  proven rather than assumed.
- **MCP is as fast as raw HTTP** and does not serialize on session state.

## 4. Measurement-quality findings (fed back into plan v1.2)

| Finding | Rule now in the plan |
|---|---|
| Full-suite bench `ns/op` swings ±30–113% on sub-µs rows on P/E-core CPUs with identical code; isolated `-count=5` is ±3% | Verify any bench ns/op outlier isolated before filing; `allocs/op` stays the primary gate (§4.1) |
| HTTP tail is power-plan-sensitive at identical RPS (p95 27.1 ms High-perf vs 16.96 ms Balanced) | Record the active plan per HTTP run; compare matched plans only; primary-box HTTP baselines are **Balanced** (§4.1) |
| Log retention is a **bounded sawtooth**: 63,513 rows against a 50,000 cap because the pruner runs every 60 s | Expect cap + up to one minute of writes; unbounded growth is the real defect (§TC-PERF-05) |
| Body-capture cost is below the noise floor at ~4k RPS (modes within 4.5%, inverted order) | Don't file on mode ordering alone (§TC-PERF-05) |

No product code changed this cycle — every finding was about how we measure.

## 5. Open items for cycle 3

1. **Second-baseline box** ([#34](https://github.com/mockagents/mockagents/issues/34), carried from cycle 2 phase 1): run TC-PERF-01 on
   a non-AV, fixed-clock machine so bench ns/op has a trustworthy anchor.
2. **CI trend-tracking** of `docs/benchmarks/latest.json` — still the highest-
   leverage automation available; catches engine drift between QA cycles.
3. **Deferred coverage** now worth scheduling: A2A streaming load, Batch API
   throughput, `kind: Pipeline` execution under load, Postgres-backed tenancy.
   *(Outcome: A2A + Batch baselined in cycle 3; pipelines have no HTTP
   execution surface — [#33](https://github.com/mockagents/mockagents/issues/33);
   Postgres still blocked — [#35](https://github.com/mockagents/mockagents/issues/35).)*
4. Consider a **sustained-throughput variant** of TC-PERF-11 (parity under a
   long run, not a 60 s window) once cycle-3 scope is set.

## 6. Artifact index

`PHASE-1-machine-profile.md` · `PHASE-2-regression-core-results.md`
(+ `TC-PERF-02-k6.json`, `TC-PERF-03{a,b}-k6.json`, `TC-PERF-04{a,b}*.csv`) ·
`PHASE-3-endurance-results.md` (+ `soak2-samples.csv`, `soak2-summary.txt`) ·
`PHASE-4-isolation-protocol-results.md`. Tracker: `../../test-execution-tracker.csv`
(`*-C2` rows + 11/12). Plan: `../../PERFORMANCE-TEST-PLAN.md` §7 cycle table.
