# Cycle 3 — Phase 2: regression core (2026-07-29)

**Cases:** TC-PERF-01, 02, 03, 04 · **All PASS** — no regression against either
the 2026-07-27 or 2026-07-28 baselines. Engine code unchanged since `0b826be`.
Power plan: High-performance for 01 (restored to Balanced immediately after),
Balanced for all HTTP cases — matching the baselines per §4.1.

## Results vs two prior cycles

| Case | Cycle 3 | Cycle 2 | Cycle 1 | Verdict |
|---|---|---|---|---|
| **01** bench | **allocs/op 17/17 exact** | 17/17 exact | 17/17 exact | ✅ |
| **02** k6 ramp | 507,906 reqs · **0.00% failed** · p95 46.4 ms (<50 gate) · med 10.9 ms | 556,636 · 0.00% · p95 45.4 ms | p95 7.6–8.4 ms¹ | ✅ |
| **02** Python cross-check | 3,849 RPS · p95 18.59 ms | 4,085 RPS · p95 16.96 ms | 3,974 RPS · p95 17.2 ms | ✅ within 6% |
| **03a** streaming @20 VU | p95 **2.36 s** · 0% errors | 2.22 s | ≪ 8 s | ✅ |
| **03b** streaming @100 VU | p95 **2.24 s** · 0% errors (8,856 streams) | 2.32 s | ≪ 8 s | ✅ |
| **04a** TTFT @20 users | p50 **380 ms** · p95 1,500 ms · 0% fail (874) | 370 / 1,400 | ~330 | ✅ in band |
| **04b** TTFT @100 users | p50 **360 ms** · p95 1,500 ms · 0% fail (4,415) | 360 / 1,400 | — | ✅ holds at 5× |

¹ Cycle-1's p95 was measured on a shorter, lighter profile; cycles 2–3 use the
full 10/50/200 ramp, so compare cycle 3 to cycle 2 for that row.

Pacing fidelity holds: TTFT p50 sits mid-band (250–500 ms) at both 20 and 100
users, and p95 (1.5 s) is inside the 900–2,100 ms band — the mock is not
speeding up under load.

## TC-PERF-01: the ns/op gate is now definitively unusable on this machine

The `allocs/op` gate passed exactly for the **third consecutive cycle** — that
remains the reliable signal. `ns/op` again flagged six >25% outliers, so the
v1.2 rule was applied (verify isolated with `-count=5` before filing). The
verification produced a stronger result than expected:

| Benchmark | Committed baseline | Cycle-3 in-suite | Cycle-3 isolated ×5 | Cycle-2 isolated ×5 |
|---|---|---|---|---|
| `ResponseGenerator_Static` | 110.8 | 232.0 | **76.7–93.6** | **49.1–52.2** |
| `ScenarioMatcher_ContentContains` | 39.9 | 27.5 | 46.6–50.2 | — |
| `ProcessRequest_MultipleToolCalls` | 6,608 | 4,063 | 4,064–5,069 | — |

`ResponseGenerator_Static` measures 49–52 ns one day and 77–94 ns the next
**in isolation, on identical code** — a ~60% day-to-day swing — while its
in-suite value is a third number again (232). The isolated-run escape hatch
from v1.2 does not rescue sub-microsecond benchmarks; it only narrows the
spread.

**Recommendation (plan change):** stop gating on `ns/op` for sub-µs benchmarks
on this machine class entirely — report them as informational. Keep the gate
for the µs-scale rows (e.g. `ProcessRequest_*`, which behave sensibly), and
keep `allocs/op` + `B/op` as the real regression signal. This escalates the
cycle-2 "second-baseline box" item from *nice-to-have* to **required for any
ns/op gating at all**.

## Durable tooling added

The load drivers were being wiped from scratch storage at every session
boundary, so they now live in the repo: `docs/qa/perf-tools/k6-nostream.js`
and `load_driver.py` (stdlib-only, no k6 required). QA can run the identical
tests without reconstructing scripts.

**Next:** phase 3 — TC-PERF-13 (A2A) and TC-PERF-14 (Batch fan-out).
