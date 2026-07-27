# TC-PERF-07 — Multi-tenant auth + quota under load (2026-07-27)

**Build:** `8a26f15` · **Verdict: PASS** (all criteria)

## Setup

Native binary, `MOCKAGENTS_MULTI_TENANT=1`, `MOCKAGENTS_LOG_BODIES=sanitized`,
`--log-level warn`, **no default rate caps at start** (see plan-fix note
below). Bootstrap platform key parsed from server log by the driver (never
echoed); `editor` key minted via `POST /api/v1/tenants/{id}/keys` (201).
Driver: stdlib-Python (`mt_perf_test.py`), non-streaming `perf-echo-model`.

## Results

**Phase A — bcrypt cold vs warm (5 sequential requests):**
`57.92, 7.47, 7.44, 7.76, 7.79` ms. Request 1 pays bcrypt (~50 ms); the auth
cache makes repeats ~7.5 ms. Order-of-magnitude criterion met.

**Phase B — warm authenticated throughput (50 threads × 60 s, uncapped):**
247,246 ok, **0 errors, 4,116 RPS**, p50 10.4 / p95 18.2 / p99 48.9 ms.
Vs the single-tenant baseline on the same machine (3,796 RPS, p95 20.5 ms):
**warm-path auth overhead ≈ 0 ms** (within run-to-run noise; criterion
< 10 ms).

**Phase C — quota burst correctness (cap 100/s, burst 200, applied at
runtime via `PUT /api/v1/tenants/{id}/quota` → 200; then 200 threads × 30 s):**

| Status | Count |
|---|---|
| 200 | 3,251 (≈ 108/s ≈ cap + burst allowance) |
| 429 | 129,739 — **`Retry-After` present on all 129,739** |
| 5xx / connection errors | **0** |

Server stable throughout; no restarts.

## Plan fix folded into MA-QA-PTP-001 (v1.1)

As written, TC-PERF-07 step 1 set `MOCKAGENTS_DEFAULT_RATE_PER_SEC=100` at
server start — which would cap Phase B's throughput measurement at 100 RPS
and make the overhead comparison meaningless. Corrected sequence (executed
above, now in the plan): start uncapped → measure warm overhead → apply the
cap **at runtime** through the quota API → burst. Also exercises the
runtime-override surface itself.
