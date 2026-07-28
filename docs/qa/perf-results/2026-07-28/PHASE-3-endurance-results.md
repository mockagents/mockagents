# Cycle 2 — Phase 3: endurance + behavior (2026-07-28)

**Cases:** TC-PERF-06 (90-min soak), 05, 07, **11 (new)** · **All PASS.**
Power plan: Balanced (matched to the HTTP baselines per plan §4.1).

## TC-PERF-06 — 90-minute soak: PASS, steady state proven

**4,623,954 requests** (4,587,476 non-streaming + 36,478 streamed), **0 errors**,
~855 RPS average rising to ~1,055 RPS in later windows.

Private bytes (the v1.2 metric — cycle 1's working set was unusable):

| t (min) | 0.5 | 10.5 | 20.5 | 30.6 | 35.6 | **40.5** | 50.6 | 60.5 | 70.5 | 75.5 | 80.6 | 85.6 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| GB | 0.13 | 1.63 | 2.58 | 3.28 | 4.03 | **4.09** | 4.28 | 4.53 | 4.76 | 4.78 | 4.74 | 4.73 |

- Linear climb to the 30-min TTL boundary, then **growth stops at t=40.5** —
  the same inflection cycle 1 saw, now confirmed with a clean metric.
- **45 minutes of post-plateau steady state** (the whole point of extending to
  90 min): drift from onset peaks at **+16.5%** (t=75.5) and then *declines*
  for the final three samples, ending +15.8%. Inside the ±20% criterion, and
  part of that drift tracks load — window throughput rose ~25% over the same
  span, and the live session set scales with RPS.
- **No leak.** Memory oscillates around ~4.7 GB rather than trending.

Latency was flat-to-improving across all 18 windows: non-streaming p95
**12.86–16.69 ms** (best window at the highest load), streamed p95
2,176–2,428 ms (paced, by design). Server-side truth from the interaction log:
**max non-streaming 98 ms** — every multi-second client-side `max` in this run
was again client/OS-side (MA-DEF-006 pattern).

**Retention characterized (not a defect):** the table ended at 63,513 rows
against `MOCKAGENTS_LOG_MAX_ROWS=50000`. The pruner runs on a
**60-second interval** (`DefaultLogPruneInterval`, `internal/server/log_pruner.go`),
so the table sawtooths between the cap and cap + one minute of persisted
writes (~13.5k rows/min here ≈ 225 writes/s). The cap is a **bounded
sawtooth, not a hard ceiling** — expected behavior; documented so QA doesn't
file it. Cycle 1 read exactly 50,000 purely by prune-tick timing.

## TC-PERF-05 — logging pipeline: PASS

| Body mode | RPS | p50 | p95 |
|---|---|---|---|
| `none` | 3,833 | 10.95 | 20.54 |
| `sanitized` | 3,948 | 10.70 | 19.36 |
| `full` | 4,006 | 10.94 | 18.47 |

Zero errors in all three. **Finding: body-capture mode has no measurable
throughput cost at ~4k RPS** — the spread is 4.5% and runs *inverted* vs the
documented `none ≥ sanitized ≥ full` expectation, i.e. the cost is below the
run-to-run noise floor on this hardware. Consistent with the architecture
(capture is async, off the request path). Plan text softened accordingly.

## TC-PERF-07 — multi-tenant auth + quota: PASS (regression clean)

| Measure | Cycle 2 | Baseline (2026-07-27) |
|---|---|---|
| bcrypt cold → warm | 66.0 → ~7.4 ms | 57.9 → 7.5 ms |
| Authenticated throughput | 3,953 RPS, p95 19.49 ms | 4,116 RPS, p95 18.2 ms |
| Burst under 100/s cap | 3,256 × 200 · 122,380 × 429 · **Retry-After on 122,380/122,380** · **0 × 5xx** | same shape |

Within 4% of baseline on every axis.

## TC-PERF-11 — adapter parity (NEW): PASS, near-perfect

Identical 60 s × 50-worker loads through each adapter, response-shape verified:

| Adapter | Endpoint | RPS | p50 | p95 | vs OpenAI |
|---|---|---|---|---|---|
| OpenAI | `/v1/chat/completions` | 3,998 | 10.71 | 19.01 | — |
| Anthropic | `/v1/messages` | 4,039 | 10.61 | 18.31 | **+1%** |
| Gemini | `generateContent` | 4,039 | 10.60 | 18.52 | **+1%** |

Zero errors on all three. **The cycle-1 blind spot is closed: the Anthropic
and Gemini adapters carry the same load as OpenAI to within 1%** — the shared
engine dominates, per-adapter encode/decode is not a differentiator. This is
now the parity baseline.

**Next:** phase 4 — TC-PERF-08, 09, 10, 12 (MCP).
