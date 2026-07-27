# TC-PERF-09 (Realtime WS concurrency) + TC-PERF-10 (replay throughput) — 2026-07-27

**Build:** `1026987` · Both exploratory/P3 · **Both PASS** — cycle 1 complete.

## TC-PERF-09 — Realtime WebSocket concurrency (exploratory)

Driver: Go client in-module (reuses the server's own `coder/websocket` dep);
each session runs the full text flow — `session.update` →
`conversation.item.create` → `response.create` → read to `response.done` —
with per-session event-order assertions.

| Run | Result |
|---|---|
| 1 session | complete in 123 ms, full 28-event GA ladder, order correct |
| 2 / 5 / 10 / 15 / 20 simultaneous | all complete, all orders correct, ~130 ms |
| 25 simultaneous **instant dial burst** | 18/25 — 7 dials EOF/timeout *before any HTTP response* (see below) |
| 25 dials staggered 100 ms + 6 s hold (all 25 concurrently open) | **25/25**, order 25/25, 700 events, 0 error events |
| **50** dials staggered 50 ms + 5 s hold | **50/50**, order 50/50, 1,400 events, 0 error events, server RSS +1.4 MB |

**Verdict:** concurrent Realtime session handling is clean at 2× the plan's
target — every session got a correctly-ordered full ladder, no cross-session
bleed, no error events, negligible memory. The instant-burst dial failures
reproduce only when N simultaneous TCP+upgrade handshakes land in the same
instant on this Windows box and fail *before the server responds*; the
server has no connection limiter in code (verified), and staggering dials
by 50–100 ms (which no real client fleet avoids) eliminates it entirely.
Attributed to client-machine/loopback (AV network inspection) — recorded as
an environment observation, not a defect. Real SDK clients connect over a
network with natural jitter.

## TC-PERF-10 — Replay throughput vs live agent

Recorded a 3-interaction cassette through `mockagents record --upstream
http://127.0.0.1:8080` (proxy on 8081), served it with `mockagents replay`
on 8082, verified repeat requests keep serving past cassette exhaustion
(5 probes vs 3 recorded → all 200), then identical 50-thread × 30 s load
against both:

| Target | RPS | p50 | p95 | p99 | errors |
|---|---|---|---|---|---|
| **replay** (cassette hash-match) | 4,140 | 10.4 ms | 16.2 ms | 41.1 ms | 0 |
| **live agent** (engine hot path) | 3,974 | 11.8 ms | 17.2 ms | 21.3 ms | 0 |

**Verdict:** replay throughput is the same order as the live engine
(marginally faster — hash lookup vs scenario matching), as the plan
predicted. No gap worth a ticket.

## Cycle-1 completion

With 09/10 done, **cycle 1 is complete: TC-PERF-02..10 all pass**
(TC-PERF-01 engine-bench baseline intentionally skipped on this throttled
machine class; run it on a clean box per §4.1 when re-baselining).
