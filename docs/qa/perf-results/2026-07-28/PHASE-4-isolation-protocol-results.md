# Cycle 2 — Phase 4: isolation + protocol (2026-07-28)

**Cases:** TC-PERF-08, 09, 10, **12 (new)** · **All PASS.** Power plan: Balanced.

## TC-PERF-08 — chaos isolation: PASS (regression clean)

Latency-only chaos target (`perf-slow`, uniform 100–300 ms), sanity-probed at
133 ms before the run; healthy and chaos load in separate processes.

| Healthy phase | n | p50 | p95 |
|---|---|---|---|
| solo-before | 106,382 | 5.28 | 6.76 |
| overlap | 378,965 | 5.47 | **7.51** |
| solo-after | 99,008 | 5.31 | 6.79 |

**p95 ratio overlap/solo = 1.111** (band 0.75–1.25; cycle 1: 1.102 — stable).
Chaos role confirmed in its configured band: 11,700 requests, p50 205 /
p95 296 ms. Zero errors both roles.

## TC-PERF-09 — Realtime WS concurrency: PASS (regression clean)

50 sessions, 50 ms dial stagger, 5 s hold: **50/50 completed, 50/50 correct
event order, 1,400 events, 0 error events**, 7.57 s wall — identical to
cycle 1. Full GA ladder verified in the spot-checked transcripts.

## TC-PERF-10 — replay throughput: PASS (regression clean)

| Target | RPS | p50 | p95 | errors |
|---|---|---|---|---|
| replay (cassette) | 4,208 | 9.98 | 17.88 | 0 |
| live agent | 3,982 | 11.26 | 18.55 | 0 |

Replay marginally faster than the live engine (hash lookup vs scenario
match), matching cycle 1's 4,140 vs 3,974.

## TC-PERF-12 — MCP tools/call throughput (NEW): PASS, no session bottleneck

`mockagents mcp --transport http --port 8081` serving `weather-mcp`;
`initialize` handshake verified (session id issued, protocolVersion echoed
`2025-06-18`), then `tools/call get_forecast{city:"tokyo"}` at 50 workers:

| Mode | RPS | p50 | p95 | JSON-RPC errors |
|---|---|---|---|---|
| single shared session | 4,133 | 10.04 | 18.08 | **0** |
| one session per worker | 4,210 | 9.83 | 18.01 | **0** |

**Ratio 1.02 — no session-lock bottleneck**, and MCP dispatch (JSON-RPC +
per-session state + `inputSchema` validation on every call) sustains the same
~4.2k RPS as the raw HTTP adapters. Zero protocol errors across ~500k calls;
every response carried a matching `id`. MCP baseline established.

## Phase-4 verdict

Four for four, no regressions, and the second new cycle-2 case (12) lands
clean. Combined with phases 2–3: **cycle 2 is complete — TC-PERF-01..12,
12/12 pass, zero open product defects.**
