# TC-PERF-08 — Chaos isolation (2026-07-27)

**Build:** `b83674d` · **Verdict: PASS** (corrected run; see run 1 note)

## Corrected run (the isolation claim, properly targeted)

Target: `perf-slow` — a **latency-only** chaos agent (uniform 100–300 ms,
`enabled: true`, no error/rate config), registered at runtime via
`mockagents add`. Healthy load: 20 workers → `perf-echo-model`, 180 s.
Chaos load: 20 workers → `perf-slow-model`, t=30–150 s, **run as a separate
OS process** (no shared client GIL). Native binary, sanitized logging.

| Healthy phase | n | p50 | p95 | p99 | max |
|---|---|---|---|---|---|
| solo-before (0–29 s) | 93,401 | 5.66 | 9.69 | 13.45 | 46.1 |
| overlap (36–144 s) | 338,225 | 5.69 | **10.68** | 15.42 | 48.4 |
| solo-after (153–180 s) | 90,037 | 5.63 | 8.55 | 11.62 | 51.2 |

**p95 ratio overlap/solo = 1.102** — inside the ±25 % band. Healthy p50
unchanged. Chaos role confirmed working as configured: 11,648 requests,
p50 206 / p95 297 / max 326 ms (the uniform band), ~97 RPS bounded by the
injected sleeps. Zero errors on both roles. Sleeping chaos goroutines do
not starve unrelated traffic.

## Run 1 (per plan v1.0 as written): nominal FAIL — but not an isolation failure

Targeting the example `chaos-agent` (`gpt-4o-flaky`) as the plan suggested:
its **20/min rate cap** turned the chaos load into a fast-429 flood —
240,089 × 429 in 120 s (~2,000 RPS, only 37 requests actually slept) — and
healthy overlap p95 rose to 1.92× solo. That measures *load scaling under
+2,000 RPS of extra traffic on one machine* (compounded by both loads
sharing one Python process/GIL in the first attempt), not chaos-sleep
starvation. Plan updated (v1.1) to specify a latency-only target and
separate load processes.

## Execution gotchas found (now documented)

1. **`chaos` nests under `spec.behavior`, not `spec`.** A chaos block under
   `spec.` validates clean (extra fields tolerated) but silently never
   fires. Also requires `enabled: true`. Verified: `internal/types/behavior.go`.
   ARCHITECTURE.md's cross-cutting diagram said `spec.chaos` — fixed in the
   same commit.
2. **Probe with `http://127.0.0.1`, not `localhost`.** On Windows, curl
   resolves `localhost` IPv6-first and pays a ~200 ms fallback per request
   (server binds IPv4) — which masqueraded as chaos latency during
   debugging. Same root cause as the GUI's documented IPv6 note.
