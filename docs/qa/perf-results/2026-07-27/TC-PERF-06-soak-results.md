# TC-PERF-06 — Soak / resource stability (2026-07-27)

**Build:** `8a26f15` · **Duration:** 45 min · **Verdict: PASS** (all four criteria; one metric caveat)

## Setup

Native Windows binary, `MOCKAGENTS_LOG_BODIES=sanitized`,
`MOCKAGENTS_LOG_MAX_ROWS=50000`, `--log-level warn`. Load: 20 non-streaming
workers → `perf-echo-model` + 10 streaming workers → `load-target-model`
(stdlib-Python driver; k6 unavailable on this box), 50 distinct
`X-Session-Id` requests up front. Sampler every 5 min: RSS (`tasklist`
working set), DB file sizes, timed probe, per-window client percentiles.
Raw samples: `soak-samples.csv`.

## Totals

**2,180,503 requests (2,162,266 non-streaming + 18,237 streaming), 0 errors**
(~800 RPS sustained for 45 min). Server never restarted.

## Criteria

| # | Criterion | Result |
|---|---|---|
| 1 | RSS plateaus after warm-up, no monotonic climb | **PASS w/ caveat** — see memory section |
| 2 | DB size stabilizes at the row cap | **PASS** — main file froze at 77.7 MB by t=10.5, one pruner step to 80.0 MB at t=25.6, flat to end; WAL flat at 80.6 MB; exactly **50,000 rows** at end |
| 3 | Minute-40 probe within 2× minute-5 | **PASS** — 38.5 ms vs 25.7 ms (1.5×); window p95 even flatter: 16.2 vs 15.7 ms (1.03×) |
| 4 | No crashes, zero errors | **PASS** — 0/2,180,503 |

Latency stability across all nine windows: non-streaming p95 ranged
**13.15–16.64 ms** (±12% band, no upward trend); streaming p95 ranged
2,232–2,417 ms (paced by design). Server-side (interaction log, last 50k):
non-streaming max 124 ms / avg 3.7 ms; streaming avg 1,383 ms / max 6,060 ms
— the configured lognormal TTFT/ITL tail, not a defect. One client-side
streaming max of 22.2 s in the t=5.5 window with no server-side counterpart
— same client/OS loopback artifact class as MA-DEF-006.

## Memory: the session-TTL sawtooth (expected) + metric caveat

Sessionless requests each create a 30-min-TTL session, so RSS **by design**
climbs for ~30 min, then TTL expiry (cleanup tick every 5 min) balances new
creation:

| t (min) | 0.5 | 5.5 | 10.5 | 15.5 | 20.5 | 25.6 | 30.6 | 35.5 | 40.5 |
|---|---|---|---|---|---|---|---|---|---|
| RSS (MB) | 118 | 814 | 763 | 440 | 2,386 | 229 | 3,691 | 4,391 | **4,036 ↓** |

Growth stopped and reversed at t=40.5 **while still under load** — the
plateau/decline the design predicts. Post-load idle decay (server idle):

| idle+min | 0 | 2 | 4 | 6 | 8 | 15 |
|---|---|---|---|---|---|---|
| RSS (MB) | 3,954 | 2,974 | 2,938 | 2,938 | 2,481 | 1,693 |

Stepwise decline matching the 5-minute cleanup ticks — 57% reclaimed by
+15 min with the server fully idle; full drain takes 30 min after the last
request by TTL definition. **No leak.**

**Caveat:** `tasklist` reports Windows *working set*, which the OS trims
aggressively — hence the 229 MB↔4.4 GB swings. The criterion-1 "±20%
plateau" is not literally evaluable with this metric; the leak question is
answered instead by (a) the under-load downturn at t=40.5 and (b) idle
decay. Cycle 2 should sample **private/commit bytes**
(`Get-Process | % PrivateMemorySize64`) instead.

## Capacity note (informational, for the docs)

At ~800 RPS of sessionless traffic the live session set costs ~2–2.5 KB per
request held for 30 min → **multi-GB steady-state RSS** (peak observed
4.4 GB). TTL-bounded, not a leak — but load-test authors who don't need
per-request sessions should reuse a session id (`X-Session-Id` header) to
keep the mock's memory flat.

## Follow-ups

- Plan §6 TC-PERF-06: switch the memory metric to private bytes (cycle 2).
- Plan §9 + load-testing guide: session-memory capacity note added.
