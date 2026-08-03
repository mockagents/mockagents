# MockAgents Performance Cycle 1 — Summary Report

**Date:** 2026-07-27 · **Plan:** MA-QA-PTP-001 v1.1 · **Builds:** `ad67121` → `0b826be`
**Scope:** TC-PERF-01..10 — **all 10 executed, all 10 PASS** · **Open product defects: 0**

## 1. Executive summary

The mock is fast enough and steady enough to never be the bottleneck in a
customer's test suite: **~3,800–4,100 RPS sustained on laptop-class
hardware with p95 ≤ 20 ms**, flat latency over a 45-minute 2.18M-request
soak, zero errors across every run, correct 429/`Retry-After` behavior
under quota pressure, and streaming pacing that stays realistic (not
"conveniently fast") at 100 concurrent users. The one issue reported
during testing (a 36 s latency spike) was proven to be a client-side
measurement artifact, not a server defect. Two small test-infrastructure
defects were found and fixed along the way; the engine benchmark baseline
was consciously refreshed after ~8 weeks of feature work.

## 2. Results scoreboard

| Case | What it proves | Key numbers | Verdict |
|---|---|---|---|
| **01** Engine bench baseline | No hot-path regression | allocs/op **17/17 exact** vs baseline; control benches flat; matcher/generator ns+B growth = shipped-feature cost → baseline re-committed (2026-07-27) | ✅ |
| **02** Non-streaming throughput | Raw HTTP speed | p95 **7.6–8.4 ms**, 0% errors up to 200 VUs (k6); repro run: 3,796–3,974 RPS @50 threads | ✅ |
| **03** Streaming load | SSE under concurrency | p95 stream-total well under the 8 s gate at 20 and 100 VUs, 0% errors | ✅ |
| **04** TTFT / pacing fidelity | Mock stays *realistically slow* under load | TTFT p50 ≈ **330 ms** (band 250–500), holds at 100 users | ✅ |
| **05** Logging pipeline | Observability never taxes requests | bodies none/sanitized/full measured; row cap held; at 3.8k RPS the bounded queue dropped 69% **with request p95 unchanged** — drops buy stability, as designed | ✅ |
| **06** 45-min soak | No leaks, no drift | **2,180,503 requests, 0 errors**; p95 flat 13–17 ms all windows; DB capped at exactly 50k rows; session-TTL RSS sawtooth peaks 4.4 GB → declines under load → idle decay 3.95→1.69 GB by +15 min | ✅ |
| **07** Multi-tenant auth + quota | Auth is cheap warm, correct under pressure | bcrypt cold 57.9 ms → warm **7.5 ms**; authenticated 4,116 RPS ≈ **0 ms overhead**; burst: only 200s+429s, `Retry-After` on **129,739/129,739** 429s, zero 5xx | ✅ |
| **08** Chaos isolation | Faulty agents can't hurt healthy ones | healthy p95 overlap/solo ratio **1.102** (band 0.75–1.25) vs a latency-only chaos agent; chaos role verified in its 100–300 ms band | ✅ |
| **09** Realtime WS concurrency | Voice sessions scale | **50/50 sessions** (2× target) with correct full event ladders, 0 error events, RSS +1.4 MB | ✅ |
| **10** Replay throughput | Cassettes are as fast as live | replay **4,140 RPS** ≥ live 3,974 RPS, p95 16.2 vs 17.2 ms | ✅ |

## 3. Issues investigated — and what they actually were

| Report | Finding | Outcome |
|---|---|---|
| 36 s max-latency spike, hypothesized SQLite lock contention + 4-part fix proposal | Architecturally impossible (log writes are async, post-response, drop-on-overflow; WAL/busy_timeout/write-timeout already in code). Reproduced the shape: client max 5.6 s while **server-side max was 461 ms over 683k requests** | **MA-DEF-006 closed — client/OS loopback artifact.** No code change. Triage rule now in TROUBLESHOOTING §5: always cross-check server-side `latency_ms`; gate on percentiles, never `max` |
| TC-PERF-08 run 1 nominal fail (p95 ratio 1.92) | The example chaos-agent's 20/min rate cap turned chaos load into a ~2,000 RPS fast-429 flood — a load-scaling test, not isolation | Plan re-targeted to a latency-only chaos agent; corrected run passed at 1.102 |
| TC-PERF-09 instant 25-dial burst failures | Dials died **before any HTTP response**; server has no connection limiter in code; 50–100 ms dial stagger → 50/50 clean | Client-machine/AV loopback artifact, recorded as environment observation |
| benchreport captured 15/17 benchmarks silently | Per-collision registry WARN floods bench output; parser drops corrupted rows | **Fixed** (`silenceSlog` in engine benches) + README note |

## 4. Test-infrastructure improvements shipped by this cycle

- **Plan v1.0 → v1.1** (three defects found by executing it): TC-PERF-07
  re-sequenced (measure auth overhead uncapped, apply quota cap at runtime
  via `PUT /quota`); TC-PERF-08 re-targeted (latency-only chaos agent,
  separate load processes, chaos-band verification); TC-PERF-06 memory
  metric switched to private bytes (working set swings by GBs under OS
  trimming).
- **TROUBLESHOOTING.md** grew a Performance section: max-outlier triage,
  log-row drops explanation, chaos-YAML nesting (`spec.behavior.chaos` +
  `enabled: true` — a mis-nested block validates clean but never fires),
  Windows `localhost` ≈ +200 ms IPv6-fallback per curl request, don't pipe
  the bootstrap-key banner through `grep`/`head`.
- **ARCHITECTURE.md** chaos diagram label corrected.
- **Engine bench baseline refreshed** off-governor per the documented
  recipe; future cycles regress against 2026-07-27.

## 5. Capacity notes for users (documented in plan §9)

- Single instance, laptop-class HW: ~4k RPS non-streaming, p95 < 20 ms.
- **Sessionless traffic holds memory by design**: each request without
  `X-Session-Id` creates a 30-min-TTL session (~2–2.5 KB) → ~4 GB peak at
  ~800 RPS. Reuse a session id to keep memory flat.
- Interaction logging degrades gracefully: beyond ~1–2k RPS the bounded
  queue samples rather than stalls; row counts < request counts under
  burst are expected.

## 6. Recommendations for cycle 2

1. Regress against the 2026-07-27 baselines (>20% = defect, per plan §8).
2. Sample **private bytes** in the soak; consider a 90-min soak to observe
   a full post-TTL steady state under load.
3. ([#34](https://github.com/mockagents/mockagents/issues/34)) Run TC-PERF-01 additionally on a non-AV, non-throttled machine to get a
   second baseline point (this cycle's ran on the primary dev box,
   off-governor).
4. Consider CI trend-tracking of `docs/benchmarks/latest.json` (schema v1
   is built for it) so baseline drift is caught between QA cycles.
5. If Realtime dial-burst behavior matters for a customer scenario, retest
   from a second machine over a real network to remove the loopback/AV
   variable.

## 7. Artifact index (this directory)

`TC-PERF-01-bench-baseline-results.md` · `adhoc-max-outlier-investigation.md`
· `TC-PERF-06-soak-results.md` + `soak-samples.csv` + `soak-summary.txt`
· `TC-PERF-07-multitenant-results.md` · `TC-PERF-08-chaos-isolation-results.md`
· `TC-PERF-09-10-results.md` · Tracker: `../../test-execution-tracker.csv`
(TC-PERF rows) · Plan: `../../PERFORMANCE-TEST-PLAN.md` (§7 cycle table).
