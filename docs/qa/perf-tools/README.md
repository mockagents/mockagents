# QA performance drivers

Load drivers for the cases in [`../PERFORMANCE-TEST-PLAN.md`](../PERFORMANCE-TEST-PLAN.md)
(MA-QA-PTP-001). They live in the repo so a case can be reproduced without
rebuilding scripts, and so the published numbers are auditable.

Every Python driver is **stdlib-only** — no k6, no Locust, no pip install.
k6 and Locust are used where they add something (encoded thresholds, TTFT
measurement); everything else runs with a bare Python 3.

| Driver | Case | What it measures |
|---|---|---|
| `load_driver.py` | TC-PERF-02 and any throughput check | RPS + p50/p95/p99/max against `/v1/chat/completions` |
| `k6-nostream.js` | TC-PERF-02 | 10 → 50 → 200 VU ramp with pass/fail thresholds encoded in the script |
| `a2a_perf.py` | TC-PERF-13 | A2A `message/send` + `message/stream`, id-echo and `final:true` checks |
| `batch_perf.py` | TC-PERF-14 | Batch fan-out at N = 100 / 1,000 / 5,000 + the OpenAI file-based lifecycle |
| `mcp_perf.py` | TC-PERF-12 | MCP `tools/call`, single shared session vs one per worker |
| `chaos_isolation.py` | TC-PERF-08 | Whether a latency-injected agent disturbs a healthy one |

## Usage

Start the server first (see plan §4.4 — `--log-level warn`, sanitized bodies,
a scratch `MOCKAGENTS_DATA_DIR`), then:

```bash
python docs/qa/perf-tools/load_driver.py 8080 60 50      # port, seconds, workers
python docs/qa/perf-tools/a2a_perf.py 8083               # needs: mockagents a2a
python docs/qa/perf-tools/batch_perf.py 8080
python docs/qa/perf-tools/mcp_perf.py 8081               # needs: mockagents mcp
k6 run docs/qa/perf-tools/k6-nostream.js
```

`chaos_isolation.py` runs as **two processes** so the load roles don't share a
GIL — start `chaos` about 30 s after `healthy`:

```bash
python docs/qa/perf-tools/chaos_isolation.py healthy &
sleep 30 && python docs/qa/perf-tools/chaos_isolation.py chaos
```

## Traps these scripts already avoid

Each was written after something cost real time in an earlier cycle. Worth
knowing if you write another driver:

- **Use `127.0.0.1`, never `localhost`.** On Windows curl and some clients
  resolve IPv6 first against an IPv4-bound server, adding ~200 ms per request —
  which can masquerade as injected latency.
- **A chaos target must be latency-only for isolation tests.** The example
  `chaos-agent` has a 20/min rate cap, which turns the chaos role into a
  ~2,000 RPS fast-429 flood and measures load scaling, not isolation.
- **`chaos:` nests under `spec.behavior`, not `spec`**, and needs
  `enabled: true`. A mis-nested block validates cleanly and silently never
  fires — always sanity-probe the target before trusting a pass.
- **Cross-check any big client-side `max` against the server's own numbers**
  before filing: `SELECT MAX(latency_ms) FROM interaction_logs`. A multi-second
  client max with a millisecond server max is a loopback artifact
  (see [`../TROUBLESHOOTING.md`](../TROUBLESHOOTING.md) §5).
- **Gate on percentiles, never on `max`.**

## Related

- Engine micro-benchmarks are **not** here — they're `go test -bench` via
  `tools/benchreport`, gated in CI by `tools/benchguard`
  (`.github/workflows/perf-guard.yml`).
- Results and per-cycle summaries: `../perf-results/`.
