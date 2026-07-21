# MockAgents — Performance Test Plan

**Document ID:** MA-QA-PTP-001
**Version:** 1.0
**Status:** Ready for execution
**Owner:** QA
**Applies to build:** `main` @ `a806dab` or later
**Last updated:** 2026-07-16
**Companions:** `MANUAL-TEST-PLAN.md` (MA-QA-TP-001, functional), `TROUBLESHOOTING.md` (MA-QA-TS-001)

---

## 1. Purpose & philosophy

MockAgents' pitch is *"your agent tests stop costing a dollar a run and stop
flaking"* — which only holds if the mock itself is fast and steady enough to
never be the bottleneck in a customer's test suite or load test. This plan
measures that.

Two kinds of performance matter, and they are **asserted differently**:

1. **Raw speed** (throughput, latency, allocations) — where *faster is
   better* and the assertion is "no regression vs the recorded baseline."
2. **Pacing fidelity** (streaming TTFT / inter-token latency) — where the
   mock *intentionally slows down* to simulate a real provider, and the
   assertion is "the delivered latency distribution matches the configured
   one, even under load." A mock that streams too fast under load is
   **broken**, not fast.

Confusing these two is the most common way to misread a result. Every case
below states which kind it asserts.

### First cycle = baseline capture

MockAgents has a committed **engine micro-benchmark** baseline
(`docs/benchmarks/latest.{json,md}`) but no committed **HTTP end-to-end**
baseline yet. For the first execution cycle, the HTTP-level cases (TC-PERF-02
onward) run in *baseline-capture* mode: the provisional gates below flag only
gross problems; the recorded numbers become the baseline that later cycles
regress against (>20% degradation = defect, see §8).

## 2. References

| Ref | Location |
|---|---|
| Engine benchmark baseline + refresh recipe | `docs/benchmarks/README.md`, `latest.{json,md}` |
| Ready-made load scripts | `examples/loadtest/k6.js`, `examples/loadtest/locustfile.py` |
| Load-test target agent (paced streaming) | `examples/load-target-agent.yaml` |
| Load-testing user guide | `site/docs/guides/load-testing.md` |
| Architecture (hot path, log worker, stores) | `ARCHITECTURE.md` |
| Functional test plan / tracker | `docs/qa/MANUAL-TEST-PLAN.md`, `test-execution-tracker.csv` |

## 3. Scope

**In scope:** engine micro-benchmarks; HTTP non-streaming throughput/latency;
SSE streaming under load incl. pacing fidelity; the async logging pipeline
under burst; memory/DB growth under soak; multi-tenant auth + quota overhead;
chaos-agent isolation. Optional/exploratory: Realtime WebSocket concurrency,
replay throughput.

**Out of scope:** GUI rendering performance; Kubernetes/Helm horizontal
scaling; network-limited scenarios (all tests are localhost); the SDKs'
client-side overhead.

## 4. Environment & setup

### 4.1 Hardware / OS ground rules

- **Record the machine**: CPU model, core count, RAM, OS, power plan — in the
  results file (§7). Numbers from different machines are not comparable.
- **Windows:** the Balanced power plan throttles benchmarks ~1.4× *uniformly*
  (documented in `docs/benchmarks/README.md`). Switch to **High performance**
  (`powercfg /setactive SCHEME_MIN`) for every timed run; restore afterwards.
- Close background heavy processes (browsers with many tabs, indexers,
  containers you're not using). Laptop on AC power, not battery.
- **Run the server natively, not in Docker, for all timed cases.** Docker
  Desktop / Rancher Desktop on Windows/macOS interposes a VM: its virtualized
  network and filesystem skew both throughput and latency. Docker parity is a
  functional concern (covered by the manual plan's ENV suite), not a
  performance one. Build once:

  ```bash
  cd mockagents
  go build -o mockagents.exe ./cmd/mockagents     # drop .exe on macOS/Linux
  ```

### 4.2 Load tools

| Tool | Install | Used by |
|---|---|---|
| k6 | `winget install k6` / `choco install k6` / `brew install k6` | TC-PERF-02/03, 08 |
| Locust | `pip install locust` | TC-PERF-04 (TTFT fidelity) — and a full fallback if k6 can't be installed (corporate proxy) |
| `jq` | winget/choco/brew | result extraction |

If the corporate proxy blocks k6's installer, every k6 case has a Locust
fallback noted inline — Locust installs from PyPI, which works on the QA
machines.

### 4.3 Test agents

Stage the example agents plus one **dedicated perf agent** (distinct model
name → no model-collision ambiguity, no streaming pacing, no chaos — pure
hot-path):

```bash
mkdir -p agents perf-results && cp examples/*.yaml agents/
cat > agents/perf-echo-agent.yaml <<'EOF'
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: perf-echo
  description: Non-streaming hot-path target for throughput tests.
spec:
  protocol: openai-chat-completions
  model: perf-echo-model
  behavior:
    scenarios:
      - name: default
        response:
          content: A short deterministic completion used for throughput measurement.
EOF
```

### 4.4 Server start recipe (baseline configuration)

Unless a case says otherwise, start the server like this and **wait for the
banner** before applying load:

```bash
MOCKAGENTS_LOG_BODIES=sanitized ./mockagents.exe start --agents-dir agents --log-level warn
```

- `--log-level warn` — INFO-per-request logging costs real throughput.
- `MOCKAGENTS_LOG_BODIES=sanitized` — the representative middle ground.
  TC-PERF-05 measures `full` vs `none` explicitly.
- Single-tenant mode (no `MOCKAGENTS_MULTI_TENANT`) except TC-PERF-07.

**Between cases:** restart the server (fresh session store, fresh SQLite WAL)
and delete `.mockagents.db*` for a clean logging table:
`rm -f .mockagents*.db*`.

## 5. Metrics — what to measure and how to read it

| Metric | Definition | Where it comes from |
|---|---|---|
| RPS | completed requests/sec sustained | k6 `http_reqs` rate / Locust RPS |
| p50/p95/p99 latency | full request duration, non-streaming | k6 `http_req_duration` percentiles |
| `stream_total_ms` | full streamed completion (TTFT + every inter-token delay) | custom Trend in `k6.js` |
| TTFT | time to **first SSE data line** | Locust only (`locustfile.py` measures first-line arrival; k6's plain client buffers the body, so k6 **cannot** see TTFT — do not report k6 `http_req_waiting` as TTFT; the 200 headers flush before the first-token delay) |
| Error rate | non-200 or malformed responses | k6 `http_req_failed` / Locust failures |
| Log completeness | interaction-log rows written vs requests sent | `GET /api/v1/logs` row count; the log worker's `submitted`/`dropped` counters are printed at server shutdown — capture the shutdown log line |
| Memory | server process RSS over time | Task Manager / `ps` sampled every 5 min (record, don't eyeball) |
| DB growth | `.mockagents.db*` file sizes over time | `ls -la` sampled with RSS |

## 6. Test cases

> Effort key: each case lists an estimated wall-clock duration. Run TC-PERF-01
> first (it validates the machine), then in any order; TC-PERF-06 (soak) can
> run over lunch.

---

### TC-PERF-01 — Engine micro-benchmark baseline (P1, ~15 min)

*Kind: raw speed. The only case with a committed baseline today.*

**Steps**

1. High-performance power plan active (§4.1). No other load on the machine.
2. `make bench-report` (regenerates `docs/benchmarks/latest.{json,md}`).
3. Diff the regenerated table against the committed one (`git diff docs/benchmarks/`).

**Pass criteria**

- **`allocs/op` and `B/op` must match the committed baseline exactly** for
  every benchmark. These are deterministic — hardware-independent — so *any*
  change is a code-level regression (or improvement): file a defect either
  way so the baseline gets consciously re-committed.
- `ns/op` within **±25%** of baseline per row. Uniform drift across all rows
  ≈ machine/power-plan variance (re-check §4.1); a *single* row blowing past
  25% while others hold ≈ genuine hot-path regression.
- Do **not** commit the regenerated files; discard with
  `git checkout -- docs/benchmarks/` unless Eng asks for a refresh.

**Reference magnitudes** (baseline 2026-06-04): static-response engine pass
≈ 550 ns/op (~1.8M ops/sec), scenario match ≈ 28 ns/op, registry lookup
≈ 12 ns/op. If your numbers are 10× off, the environment is wrong — stop and
fix §4.1 before running anything else.

---

### TC-PERF-02 — Non-streaming HTTP throughput ramp (P1, ~20 min)

*Kind: raw speed. Establishes the HTTP end-to-end baseline.*

**Steps**

1. Server per §4.4. Save as `perf-nostream.js`:

   ```javascript
   import http from 'k6/http'
   import { check } from 'k6'
   export const options = {
     scenarios: {
       ramp: {
         executor: 'ramping-vus',
         stages: [
           { duration: '60s', target: 10 },
           { duration: '60s', target: 50 },
           { duration: '60s', target: 200 },
         ],
       },
     },
     thresholds: {
       http_req_failed: ['rate<0.001'],
       http_req_duration: ['p(95)<50'],
     },
   }
   export default function () {
     const res = http.post(
       `${__ENV.BASE || 'http://localhost:8080'}/v1/chat/completions`,
       JSON.stringify({ model: 'perf-echo-model',
         messages: [{ role: 'user', content: 'hello' }] }),
       { headers: { 'Content-Type': 'application/json', Authorization: 'Bearer mock' } },
     )
     check(res, { 'status 200': (r) => r.status === 200 })
   }
   ```

2. `k6 run --summary-export perf-results/TC-PERF-02.json perf-nostream.js`
3. Record per-stage RPS and p50/p95/p99 from the summary.

**Provisional gates (first cycle):** error rate < 0.1%; p95 < 50 ms across
the whole ramp on a 4-core-or-better machine. **Sanity floor:** if sustained
RPS at 50 VUs is below ~1,000 on modern hardware, something environmental is
wrong (§4.1, antivirus scanning the SQLite file, debug logging left on).
Record all numbers as the baseline regardless of gate outcome.

**Locust fallback:** reuse `locustfile.py` with the payload's model changed
to `perf-echo-model` and `stream` removed; `--headless -u 50 -r 10 -t 3m`.

---

### TC-PERF-03 — Streaming load, encoded thresholds (P1, ~10 min)

*Kind: raw speed under streaming concurrency (the pacing itself is asserted
in TC-PERF-04).*

**Steps**

1. Server per §4.4 (example agents include `load-target`).
2. Stock run — the script's thresholds (**<1% errors, p95
   `stream_total_ms` < 8000**) are the pass gate:
   `k6 run --summary-export perf-results/TC-PERF-03a.json examples/loadtest/k6.js`
3. Heavy run: `VUS=100 DURATION=120s k6 run --summary-export perf-results/TC-PERF-03b.json examples/loadtest/k6.js`

**Pass criteria:** both runs meet the script's thresholds (k6 exits non-zero
on a threshold breach — the exit code *is* the verdict). 100 concurrent
paced SSE streams is ~100 parked goroutines — trivial for Go; a breach here
is a real finding, not an expected limit.

---

### TC-PERF-04 — TTFT / pacing fidelity under load (P2, ~20 min)

*Kind: **pacing fidelity** — the "too fast = broken" case.*

The `load-target` agent promises TTFT p50≈350 ms / p95≈1400 ms and ITL
p50≈20 ms / p95≈60 ms (lognormal). This case checks the *delivered*
distribution matches at 20 users, then that it **doesn't collapse or blow
up** at 100.

**Steps**

1. Server per §4.4.
2. `locust -f examples/loadtest/locustfile.py --host http://localhost:8080 --headless -u 20 -r 5 -t 2m --csv perf-results/TC-PERF-04a`
3. Repeat with `-u 100 -r 20 -t 2m --csv perf-results/TC-PERF-04b`.
4. Read the **TTFT** metric rows from the Locust stats CSVs (the script
   reports TTFT as its own named metric, separate from full-stream time).

**Pass criteria**

- At 20 users: TTFT p50 within **250–500 ms**, p95 within **900–2100 ms**
  (±~30–50% band around the configured lognormal — it's a sampled
  distribution, not a constant).
- At 100 users: the same bands still hold. TTFT p50 *dropping far below*
  350 ms means pacing broke under load (mock streaming unrealistically
  fast) — that is a **defect**, not a win. TTFT p95 inflating well past the
  band means the pacer is starving — also a defect.
- Full-stream p95 consistent between the two runs (±25%).

---

### TC-PERF-05 — Logging pipeline under burst (P2, ~25 min)

*Kind: raw speed + graceful-degradation semantics.*

The interaction log is written by a small async worker pool with a bounded
queue; on overflow it **drops entries rather than blocking the response
path** (by design — `ARCHITECTURE.md`). This case quantifies logging's cost
and verifies overflow degrades exactly as documented.

**Steps**

1. Three identical 60 s runs of the TC-PERF-02 script at 50 VUs, one per
   config (restart server + `rm -f .mockagents*.db*` between runs):
   - a) `MOCKAGENTS_LOG_BODIES=none`
   - b) `MOCKAGENTS_LOG_BODIES=sanitized`
   - c) `MOCKAGENTS_LOG_BODIES=full`
2. After each run: note k6's total request count, then count stored rows
   (`curl -s 'http://localhost:8080/api/v1/logs?limit=500' | jq '.logs | length'`
   is a spot check; the authoritative numbers are in step 3).
3. Stop the server with Ctrl-C and capture the shutdown log line with the
   worker's `submitted` / `dropped` counters.
4. Retention: re-run (b) with `MOCKAGENTS_LOG_MAX_ROWS=1000`; after the run
   confirm the row count settles at ≤ ~1000 and the DB file is not growing
   unboundedly.

**Pass criteria**

- Throughput ordering `none ≥ sanitized ≥ full`; record the deltas (this is
  the documented cost of body capture — baseline data, no hard gate).
- `submitted + dropped == requests sent` (every request accounted once).
- Zero drops at 50 VUs is the expectation on local hardware; if drops occur,
  p95 latency must **not** have degraded vs run (a) — drops must buy
  stability, never accompany it degrading. Drops with stable latency =
  documented behavior; record the count.
- Retention run: row cap enforced, server stable.

---

### TC-PERF-06 — Soak / resource stability (P2, ~60 min wall clock, mostly unattended)

*Kind: stability. Catches leaks that short runs can't.*

**Steps**

1. Server per §4.4 plus `MOCKAGENTS_LOG_MAX_ROWS=50000`.
2. Mixed moderate load for **45 minutes**: run the TC-PERF-02 script with a
   constant 20 VUs (`--vus 20 --duration 45m` on a simple script without the
   ramp scenario), and in a second terminal the streaming k6.js with
   `VUS=10 DURATION=45m`.
3. Every 5 minutes, append to `perf-results/TC-PERF-06-samples.csv`: RSS of
   the server process, sizes of `.mockagents.db*`, and one manual
   `curl -w '%{time_total}'` latency probe of each endpoint.
4. Send ~50 requests with **distinct `session_id`s** early on (session store
   entries expire after a 30-min TTL; a 45-min soak crosses it).

**Pass criteria**

- RSS reaches a plateau after warm-up and stays within **±20%** of that
  plateau for the remainder — no monotonic climb through the whole window.
- DB size stabilizes once the row cap is reached (WAL files fluctuate; the
  trend must be flat).
- The manual latency probes at minute 40 are within 2× of the minute-5
  probes.
- Server never restarts/crashes; zero errors in both k6 summaries.

---

### TC-PERF-07 — Multi-tenant auth + quota under load (P2, ~25 min)

*Kind: raw speed + correctness under pressure.*

API-key resolution runs **bcrypt** (~50–100 ms of deliberate CPU cost) on
cache miss; a TTL cache makes repeats cheap. Quota enforcement adds a token
bucket + spend check per request. This case measures the warm-path overhead
and proves the cold path and limit responses stay correct under burst.

**Steps**

1. Start multi-tenant with a modest rate limit:
   ```bash
   MOCKAGENTS_MULTI_TENANT=1 MOCKAGENTS_DEFAULT_RATE_PER_SEC=100 \
     MOCKAGENTS_DEFAULT_RATE_BURST=200 ./mockagents.exe start --agents-dir agents --log-level warn
   ```
   Capture the bootstrap platform key from stderr; mint one `editor` API key
   via the management API (see `docs/guides/multi-tenant.md`) and use **that**
   key for the load below.
2. **Cold vs warm:** send 5 sequential `curl -w '%{time_total}'` requests
   with the key. Request 1 pays bcrypt; 2–5 should be an order of magnitude
   cheaper. Record all five timings.
3. **Warm throughput:** TC-PERF-02 script at 50 VUs / 60 s with
   `Authorization: Bearer <editor-key>`. Compare RPS + p95 against the
   single-tenant TC-PERF-02 numbers → this delta is the authenticated-path
   overhead (record it; no hard gate first cycle).
4. **Limit correctness under burst:** raise to 200 VUs for 30 s. The rate
   cap (100/s) must reject the excess with **429 + `Retry-After`** —
   never 500s, never connection errors. In k6, count statuses
   (`check(res, {'429 or 200': r => r.status === 200 || r.status === 429})`).

**Pass criteria:** warm-path p95 overhead vs single-tenant < 10 ms; burst
run contains only 200s and 429s (any 5xx = Sev-2 defect); server stable
throughout; a `Retry-After` header present on 429s.

---

### TC-PERF-08 — Chaos isolation (P3, ~10 min)

*Kind: isolation. A deliberately slow agent must not degrade a healthy one.*

**Steps**

1. Server per §4.4 (examples include `chaos-agent` and the slow presets).
2. Terminal A: TC-PERF-02 script at 20 VUs against `perf-echo-model`,
   3 minutes.
3. Terminal B, starting 30 s later: 20 VUs against the chaos/slow agent's
   model for 2 minutes (its injected latency/errors are the *expected*
   behavior).
4. Compare terminal A's p95 during the overlap window vs before it.

**Pass criteria:** healthy-agent p95 during overlap within **±25%** of its
solo value. Chaos latency is per-request goroutine sleep — it must never
starve unrelated traffic.

---

### TC-PERF-09 — Realtime WebSocket concurrency (P3, exploratory, ~20 min)

No hard gates — capture behavior. Open **25 concurrent** Realtime sessions
(`k6` ws module, or 25 backgrounded `websocat` loops per the manual plan
§8.3 recipe), each: `session.update` → one text `conversation.item.create` →
`response.create` → read to `response.done`. Record: all sessions complete,
event ordering stays correct per session (spot-check 3 transcripts), server
RSS before/after, and any error events. File defects only for crashes,
cross-session event bleed, or stuck sessions.

### TC-PERF-10 — Replay throughput (P3, optional, ~15 min)

Record a 5-interaction cassette against the live mock itself
(`mockagents record --upstream http://localhost:8080 ...`), then serve it
with `mockagents replay` and run TC-PERF-02's script (adjusting the payload
to a recorded request). Cassette matching is a hash lookup — throughput
should be the same order as TC-PERF-02. Record the number; a large gap is
worth a ticket, not a release blocker.

---

## 7. Reporting

- Raw artifacts (k6 `--summary-export` JSON, Locust CSVs, soak samples CSV,
  shutdown log lines) go in `docs/qa/perf-results/<YYYY-MM-DD>/` — commit
  them with the cycle.
- Summarize each cycle in the table below (append; keep history):

| Cycle | Date | Build | Machine (CPU/cores/RAM/OS/power plan) | TC-PERF-02 RPS@50VU / p95 | TC-PERF-03 p95 stream | TC-PERF-04 TTFT p50/p95 @20u | TC-PERF-05 none→full delta | TC-PERF-07 warm overhead | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| | | | | | | | | | |

- Track execution status in `test-execution-tracker.csv` (TC-PERF rows).
- Defects: log in MANUAL-TEST-PLAN §18 with severity per its §5.3.
  Suggested severities — pacing-fidelity break or 5xx under quota burst:
  Sev-2; baseline regression >20%: Sev-2; soak leak: Sev-2; missing
  provisional gate on first cycle: record, discuss with Eng before filing.

## 8. Regression policy (cycle 2+)

Once a cycle's numbers are committed as baseline **on the same machine
class**: >20% degradation in RPS or p95 on any P1/P2 case = defect;
allocs/op change in TC-PERF-01 = defect (either direction — improvements
must be consciously re-baselined); pacing-fidelity bands (TC-PERF-04) are
absolute, not relative — they never loosen with a slower baseline.

## 9. Known environment caveats

- Windows Balanced power plan → ~1.4× uniform ns/op inflation (§4.1).
- Docker/Rancher Desktop VM → do not time through it (§4.1). Rancher's
  engine has also been observed to stop mid-session on the QA machine
  (see TROUBLESHOOTING.md) — another reason timed runs stay native.
- Corporate TLS proxy may block k6 installation → Locust fallbacks inline.
- Antivirus real-time scanning of `.mockagents.db*` can serialize SQLite
  writes; exclude the working directory for timed runs if policy allows.
- k6 buffers SSE bodies: k6 can gate on *total* stream time only. TTFT
  claims must come from Locust (§5). Never report k6 TTFB as TTFT.
