# Cycle 3 — Phase 3: new protocol surfaces (2026-07-29)

**Cases:** TC-PERF-13 (A2A), TC-PERF-14 (Batch fan-out) — **both PASS, both are
first-ever baselines.** Balanced power plan; single-tenant; build `9252189`.

## TC-PERF-13 — A2A throughput: PASS

`mockagents a2a --agents-dir agents --server weather-a2a` (port 8083).
Agent card probed first: **200**, `name: "Weather Agent"`, 492 bytes.

| Method | workers × dur | RPS | p50 | p95 | p99 | errors |
|---|---|---|---|---|---|---|
| `message/send` | 50 × 60 s | **3,953** | 11.62 | 19.43 | 27.18 | **0** |
| `message/stream` | 20 × 60 s | **3,083** | 5.80 | 10.66 | 17.18 | **0** |

- **Zero JSON-RPC id mismatches** across 237,469 `message/send` calls — the
  id-echo contract holds under load (this is the surface where cycle-1's
  INT-2 fix lived, so it's worth asserting).
- **Every stream terminated with `final:true`** — 0 truncated streams across
  185,143 streams and 740,572 SSE frames (exactly 4 frames per stream).
- **vs the OpenAI adapter baseline** (3,849–4,085 RPS): `message/send` lands
  at 3,953 RPS — **within 3%**, far inside the 2× criterion. Task-lifecycle
  bookkeeping costs nothing measurable.
- `message/stream` is *faster per call* than `message/send` (5.8 vs 11.6 ms
  p50) because A2A streams are short and **unpaced** — 4 frames delivered
  immediately, unlike the token-paced `load-target` chat streams. Not a
  comparison to TC-PERF-03.

## TC-PERF-14 — Batch fan-out: PASS, sub-linear

### Part A — Anthropic inline (3 runs per size, best per-request shown)

| N | total_ms (3 runs) | per-request ms | succeeded / errored |
|---|---|---|---|
| 100 | 33.7 / 16.4 / 17.5 | 0.1644 | 100 / 0 |
| 1,000 | 66.5 / 41.8 / 43.6 | **0.0418** | 1,000 / 0 |
| 5,000 | 136.9 / 145.3 / 154.8 | **0.0274** | 5,000 / 0 |

**Per-request cost falls 6× as N grows 50×** (0.164 → 0.027 ms) — the pass
criterion asked for flat-or-improving, and fan-out is comfortably sub-linear:
fixed per-batch overhead amortizes and the per-item path stays cheap. At
N=5,000 the mock dispatches ~**36,500 requests/second** of fan-out work, an
order of magnitude above the ~4k RPS per-request HTTP path (no HTTP
round-trip per item).

### Part B — OpenAI file-based lifecycle (N=1,000)

| Phase | ms |
|---|---|
| upload (multipart JSONL) | 32.7 |
| create batch | 33.3 |
| poll to `completed` | 5.4 (**1 poll** — instant completion, delay=0) |
| fetch output file | 13.2 |

`status=completed`, `request_counts={total:1000, completed:1000, failed:0}`,
**output_lines = 1,000 = input count**, every `custom_id` echoed. Whole
lifecycle ≈ 85 ms for a thousand requests.

## Notes for future cycles

- Both surfaces are now baselined; cycle 4 can gate them at >20%.
- The `X-Mockagents-Batch-Delay-Ms` header was deliberately **not** used —
  these runs measure pure fan-out. A lifecycle-timing test that exercises the
  `in_progress` → `completed` transition should set it explicitly.

**Next:** phase 4 — TC-PERF-16 (runner pipelines) + spot regressions 08, 12.
