# MockAgents — Performance Test Plan

**Document ID:** MA-QA-PTP-001
**Version:** 1.4

> **v1.4 changes (cycle-4 planning): the cycle model changes.** Cycles 1–3 ran
> the full suite on a schedule; `git log` shows **no product-code change
> between the cycle-1 baselines and cycle 3**, so cycles 2 and 3 re-measured an
> identical binary and found — as they must — no regression. Scheduled
> full re-runs have therefore stopped paying. §9 replaces the phase plan with a
> **change-triggered model**: a small CI perf-guard catches drift continuously,
> and manual effort goes only to new surfaces, changed code, and the questions
> earlier cycles could not answer. The case list is also **demoted, not
> grown** — see §9.3.

> **v1.3 changes (cycle-3 planning):** §9 replaced with the cycle-3 execution
> plan. Four new cases take the protocol surfaces cycle 2 deferred:
> TC-PERF-13 (A2A), TC-PERF-14 (Batch fan-out), TC-PERF-15 (Postgres tenancy),
> TC-PERF-16 (test-runner pipeline execution). **Scope correction:** "pipeline
> execution under load" as deferred in v1.2 is **not executable over HTTP** —
> `PipelineExecutor.Run` is reachable only from `internal/runner` via
> `mockagents test`; there is no pipeline execution endpoint (list/get/update
> only). TC-PERF-16 measures the runner path instead, and the missing surface
> is raised with Eng rather than papered over.

> **v1.2 changes (cycle-2 planning):** new §10 cycle-2 execution plan. Two new
> cases close cycle-1 coverage gaps: TC-PERF-11 (adapter parity — every cycle-1
> HTTP number was measured through the OpenAI adapter only) and TC-PERF-12
> (MCP `tools/call` throughput — the MCP surface had zero perf coverage).
> TC-PERF-06 gains a 90-minute variant with private-bytes sampling. Cycle 2
> is a **regression cycle**: every repeated case gates against the committed
> 2026-07-27 baselines per §8 (>20% = defect).

> **v1.1 changes (from cycle-1 execution):** TC-PERF-07 re-sequenced — start
> the server **uncapped**, measure warm auth overhead, then apply the rate cap
> **at runtime** (`PUT /api/v1/tenants/{id}/quota`) before the burst phase
> (the v1.0 env-var cap at startup would have limited the overhead
> measurement itself to 100 RPS). TC-PERF-06 memory metric switched to
> private bytes (`tasklist` working set proved too noisy — the OS trims it).
> the environment-caveats section gains the sessionless-traffic memory note.
**Status:** Ready for execution
**Owner:** QA
**Applies to build:** `main` @ `c722eaa` or later
**Last updated:** 2026-07-29
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

### Baseline status (updated each cycle)

| Surface | Baseline | Captured |
|---|---|---|
| Engine micro-benchmarks | `docs/benchmarks/latest.{json,md}` | 2026-07-27, re-verified 2026-07-28 |
| HTTP (throughput, streaming, TTFT, logging, soak, multi-tenant, chaos, Realtime, replay) | `perf-results/2026-07-27/` + `2026-07-28/` | cycles 1–2 |
| Adapter parity (Anthropic/Gemini), MCP | `perf-results/2026-07-28/` | cycle 2 |
| A2A, Batch fan-out, runner pipelines | `perf-results/2026-07-29/` | cycle 3 |
| Postgres tenancy | — | **blocked** — no Postgres reachable (cycle-3 §5) |

Repeated cases gate against the recorded numbers (>20% degradation = defect,
see §8). A case with no baseline yet runs in *capture* mode: its provisional
gates flag only gross problems, and its numbers become the anchor.

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
under burst; memory/DB growth under soak; multi-tenant auth + quota overhead
(SQLite **and** Postgres stores); chaos-agent isolation; adapter parity
across OpenAI/Anthropic/Gemini; MCP and A2A protocol throughput; Batch API
fan-out; replay throughput; Realtime WebSocket concurrency; runner-side
pipeline execution.

**Out of scope:** GUI rendering performance; Kubernetes/Helm horizontal
scaling and multi-instance deployments; network-limited scenarios (all tests
are localhost); the SDKs' client-side overhead. **Not testable as of
`c722eaa`:** concurrent pipeline execution over HTTP — no execution endpoint
exists — raised as [#33](https://github.com/mockagents/mockagents/issues/33).

## 4. Environment & setup

### 4.1 Hardware / OS ground rules

- **Record the machine**: CPU model, core count, RAM, OS, power plan — in the
  results file (§7). Numbers from different machines are not comparable.
- **Windows:** the Balanced power plan throttles benchmarks ~1.4× *uniformly*
  (documented in `docs/benchmarks/README.md`). Switch to **High performance**
  (`powercfg /setactive SCHEME_MIN`) for **micro-benchmark** runs; restore
  afterwards. **HTTP-level runs: record the active plan and compare matched
  plans only** — cycle-2 measured p95 27.1 ms (High-perf) vs 16.96 ms
  (Balanced) on the identical build at identical RPS; the existing HTTP
  baselines for the primary box are **Balanced**.
- **Do not gate on `ns/op` for sub-microsecond benchmarks on this machine
  class — report them as informational only.** Cycle 2 found full-suite
  ns/op swinging ±30–113% on sub-µs rows with identical code; cycle 3 showed
  the isolated `-count=5` escape hatch doesn't rescue them either
  (`ResponseGenerator_Static`: 49–52 ns one day, 77–94 ns the next in
  isolation, 232 ns in-suite — same commit). Gate on **`allocs/op` and
  `B/op`** (exact, machine-independent, clean for three cycles) plus the
  µs-scale rows (`ProcessRequest_*`), which behave sensibly. Trustworthy
  ns/op gating **requires** the non-AV fixed-clock second-baseline box
  ([#34](https://github.com/mockagents/mockagents/issues/34)).
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

**Outlier triage rule:** before attributing a client-reported latency outlier
(a big `max` with healthy p95) to the server, cross-check the **server-side**
distribution from the interaction log —
`SELECT MAX(latency_ms) FROM interaction_logs`. On a single machine the load
generator and server share CPU and one loopback TCP stack; multi-second client
`max` values with a millisecond server-side max are a client/OS artifact, not
a server defect (see `TROUBLESHOOTING.md` §5 and the 2026-07-27
investigation in `perf-results/`).

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
   confirm the row count settles near 1000 and the DB file is not growing
   unboundedly. **The cap is a bounded sawtooth, not a hard ceiling** — the
   pruner runs every 60 s (`DefaultLogPruneInterval`), so a live table can sit
   at cap + up to one minute of persisted writes (cycle-2 soak: 63,513 rows
   against a 50,000 cap at ~225 persisted writes/s). Overshoot proportional
   to write rate is expected; unbounded growth is not.

**Pass criteria**

- Record the per-mode deltas (baseline data, no hard gate). Note: cycle-2
  measured the three modes within 4.5% of each other and in *inverted* order
  — at ~4k RPS the cost of body capture is below the run-to-run noise floor,
  which is consistent with capture being async and off the request path. Do
  not file a defect on ordering alone.
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
3. Every 5 minutes, append to `perf-results/TC-PERF-06-samples.csv`: the
   server process's **private bytes** (PowerShell:
   `(Get-Process mockagents).PrivateMemorySize64` — do *not* use
   `tasklist`'s working set, which the OS trims aggressively and swings by
   GBs; cycle-1 finding), sizes of `.mockagents.db*`, and one manual
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

1. Start multi-tenant **without** rate caps (they'd cap step 3's overhead
   measurement itself — v1.1 fix):
   ```bash
   MOCKAGENTS_MULTI_TENANT=1 ./mockagents.exe start --agents-dir agents --log-level warn
   ```
   Capture the bootstrap platform key from stderr (don't pipe the server
   output through `grep`/`head` — buffering can swallow the shown-once
   banner); mint one `editor` API key via the management API (see
   `docs/guides/multi-tenant.md`) and use **that** key for the load below.
2. **Cold vs warm:** send 5 sequential `curl -w '%{time_total}'` requests
   with the key. Request 1 pays bcrypt; 2–5 should be an order of magnitude
   cheaper. Record all five timings.
3. **Warm throughput:** TC-PERF-02 script at 50 VUs / 60 s with
   `Authorization: Bearer <editor-key>`. Compare RPS + p95 against the
   single-tenant TC-PERF-02 numbers → this delta is the authenticated-path
   overhead (record it; no hard gate first cycle).
4. **Limit correctness under burst:** first apply the cap **at runtime** —
   `PUT /api/v1/tenants/{id}/quota` with
   `{"rate_per_sec":100,"rate_burst":200}` (platform key; expect 200) —
   then raise to 200 VUs for 30 s. The cap must reject the excess with
   **429 + `Retry-After`** — never 500s, never connection errors. In k6,
   count statuses
   (`check(res, {'429 or 200': r => r.status === 200 || r.status === 429})`).

**Pass criteria:** warm-path p95 overhead vs single-tenant < 10 ms; burst
run contains only 200s and 429s (any 5xx = Sev-2 defect); server stable
throughout; a `Retry-After` header present on 429s.

---

### TC-PERF-08 — Chaos isolation (P3, ~10 min)

*Kind: isolation. A deliberately slow agent must not degrade a healthy one.*

**Steps**

1. Server per §4.4. Stage a **latency-only** chaos target (v1.1 — do *not*
   use the example `chaos-agent`: its 20/min rate cap turns the run into a
   ~2,000 RPS fast-429 flood that measures load scaling, not isolation):

   ```yaml
   # agents/perf-slow-agent.yaml — note: chaos nests under spec.behavior
   apiVersion: mockagents/v1
   kind: Agent
   metadata:
     name: perf-slow
   spec:
     protocol: openai-chat-completions
     model: perf-slow-model
     behavior:
       scenarios:
         - name: default
           response: {content: Deliberately slow canned completion.}
       chaos:
         enabled: true
         latency: {distribution: uniform, min_ms: 100, max_ms: 300}
   ```

   Sanity-probe it with `http://127.0.0.1:8080` (NOT `localhost` — Windows
   curl pays a ~200 ms IPv6-fallback that masquerades as chaos latency):
   expect 100–300 ms responses.
2. Terminal A: TC-PERF-02 script at 20 VUs against `perf-echo-model`,
   3 minutes.
3. Terminal B — a **separate load-generator process** (a shared process
   skews the healthy numbers), starting 30 s later: 20 VUs against
   `perf-slow-model` for 2 minutes. Its ~100–300 ms responses are the
   expected behavior; its RPS is bounded by the sleeps.
4. Compare terminal A's p95 during the overlap window vs before it.

**Pass criteria:** healthy-agent p95 during overlap within **±25%** of its
solo value; chaos-role latency distribution matches the configured band
(confirms the injection actually fired — a mis-nested chaos block
validates clean but never fires). Chaos latency is per-request goroutine
sleep — it must never starve unrelated traffic.

---

### TC-PERF-09 — Realtime WebSocket concurrency (P3, exploratory, ~20 min)

No hard gates — capture behavior. Open **25 concurrent** Realtime sessions
(`k6` ws module, or 25 backgrounded `websocat` loops per the manual plan
§8.3 recipe), each: `session.update` → one text `conversation.item.create` →
`response.create` → read to `response.done`. Record: all sessions complete,
event ordering stays correct per session (spot-check 3 transcripts), server
RSS before/after, and any error events. File defects only for crashes,
cross-session event bleed, or stuck sessions.

### TC-PERF-11 — Adapter parity: Anthropic + Gemini throughput (P2, new in cycle 2, ~25 min)

*Kind: raw speed. Cycle-1 blind spot: every HTTP-level number (02, 05, 06,
07, 10) was measured through `/v1/chat/completions` — the OpenAI adapter.
The Anthropic and Gemini adapters share the engine but have their own
decode/encode paths.*

**Steps**

1. Server per §4.4, plus two parity agents alongside `perf-echo` (protocol
   strings are exact — a typo fails validation):

   ```yaml
   # agents/perf-echo-ant-agent.yaml
   apiVersion: mockagents/v1
   kind: Agent
   metadata: {name: perf-echo-ant}
   spec:
     protocol: anthropic-messages
     model: perf-echo-ant-model
     behavior:
       scenarios:
         - name: default
           response: {content: A short deterministic completion for parity measurement.}
   ---
   # agents/perf-echo-gem-agent.yaml — same shape with
   # protocol: google-gemini, model: perf-echo-gem-model
   ```

2. Three identical 60 s / 50-worker runs (restart + clean DB between):
   - OpenAI: `POST /v1/chat/completions` with `perf-echo-model`
   - Anthropic: `POST /v1/messages` (header `anthropic-version: 2023-06-01`,
     body `{"model":"perf-echo-ant-model","max_tokens":100,"messages":[...]}`)
   - Gemini: `POST /v1beta/models/perf-echo-gem-model:generateContent`
3. Record RPS + p50/p95/p99 per adapter.

**Pass criteria:** Anthropic and Gemini RPS within **±30%** of the OpenAI
run and p95 within 2× (first cycle-2 run = parity baseline capture; the
band flags only a grossly slower adapter). Zero errors on all three.

### TC-PERF-12 — MCP tools/call throughput (P3, new in cycle 2, ~20 min)

*Kind: raw speed. The MCP surface (`mockagents mcp`) had no cycle-1 perf
coverage; it is its own process and dispatch path (JSON-RPC 2.0 +
per-session state + input-schema validation on every call).*

**Steps**

1. `mockagents mcp --transport http --port 8081 --agents-dir agents` with
   the `weather-mcp` example document (loopback bind is the default —
   probing from the same host is fine).
2. Initialize one session (`initialize` → capture `Mcp-Session-Id`), then
   50 workers × 60 s of `tools/call` POSTs on that session with valid
   arguments; a second run with 50 sessions (one per worker).
3. Record RPS, p95, and JSON-RPC error counts (id echo must always match).

**Pass criteria:** zero protocol errors; single-session vs per-worker-session
throughput within 2× of each other (flags a session-lock bottleneck);
numbers recorded as the MCP baseline (no absolute gate first run).

### TC-PERF-13 — A2A throughput: message/send + message/stream (P2, new in cycle 3, ~25 min)

*Kind: raw speed. A2A is its own package and its own process — never load-tested.*

**Steps**

1. `mockagents a2a --agents-dir agents --server weather-a2a` (defaults to
   **port 8083**; `--server` is required when several A2AServer docs are loaded).
2. Verify the card first: `GET /.well-known/agent-card.json` → 200.
3. 60 s × 50 workers of JSON-RPC `message/send` (`POST /`, a text part).
   Record RPS, p50/p95, and that every response echoes its request `id`.
4. 60 s × 20 workers of `message/stream`, reading each SSE stream to the
   `final:true` status-update. Record full-stream time and frame counts.

**Pass criteria:** zero JSON-RPC errors and zero id mismatches;
`message/send` RPS within **2×** of the OpenAI adapter baseline (TC-PERF-11)
— a large gap points at task-lifecycle bookkeeping, worth a ticket;
every stream terminates with `final:true` (no truncated streams). First run
is baseline capture.

### TC-PERF-14 — Batch API fan-out throughput (P2, new in cycle 3, ~25 min)

*Kind: raw speed under fan-out. A batch dispatches N engine calls from one
request — a different shape from per-request load.*

**Verified behavior:** a batch completes **immediately by default**
(`delay = 0`); the optional `X-Mockagents-Batch-Delay-Ms` header simulates
processing time (clamped to 600,000 ms). So a no-delay batch measures pure
fan-out cost, and the delay header is only for lifecycle tests.

**Steps**

1. Server per §4.4 (single-tenant).
2. **Anthropic inline** (no file upload needed — simplest fan-out probe):
   `POST /v1/messages/batches` with N inline `requests[]` at
   N = 100 / 1,000 / 5,000, targeting `perf-echo-ant-model`. Time
   create → first `completed` poll. Repeat each N three times.
3. **OpenAI file-based**: `POST /v1/files` (purpose=batch) with a JSONL of
   1,000 chat requests → `POST /v1/batches` → poll → fetch the output file.
   Time the whole lifecycle and the output fetch separately.
4. Compute per-request cost (total ms ÷ N) at each size.

**Pass criteria:** per-request fan-out cost is **flat or improving** as N
grows (a rising per-request cost means super-linear behavior — file it);
output line count == input count with every `custom_id` echoed; zero errors.
Baseline capture on first run.

### TC-PERF-15 — Postgres-backed tenancy vs SQLite (P2, new in cycle 3, ~30 min)

*Kind: raw speed on a swapped backend. Cycle 2 measured the auth/quota path
on the SQLite store only; `MOCKAGENTS_TENANCY_DSN` selects Postgres, which
uses `SELECT … FOR UPDATE` for read-modify-write.*

**Environment:** needs a Postgres instance —
`docker run -e POSTGRES_PASSWORD=… -p 5432:5432 postgres:16`. ⚠️ The
container runtime on the primary box has been unreliable (see
TROUBLESHOOTING); if Docker is unavailable, **skip and record as blocked**
rather than substituting a different store.

**Steps**

1. Start Postgres; start the mock with `MOCKAGENTS_MULTI_TENANT=1` and
   `MOCKAGENTS_TENANCY_DSN=postgres://…`.
2. Repeat **TC-PERF-07 exactly** (bcrypt cold/warm, warm 50×60 s throughput,
   runtime quota cap + 200-worker burst).
3. Compare against the same-day SQLite run of TC-PERF-07.

**Pass criteria:** warm-path throughput within **±20%** of SQLite (the auth
cache should make the backend nearly irrelevant on the hot path); cold
bcrypt unchanged; burst still yields only 200s/429s-with-`Retry-After` and
zero 5xx. A large warm-path gap indicates the cache isn't covering the
Postgres path — file it.

### TC-PERF-16 — Test-runner pipeline execution (P3, new in cycle 3, ~20 min)

*Kind: raw speed, CLI-side. **Scope note:** pipelines have **no HTTP
execution surface** — `PipelineExecutor.Run` is reachable only through
`internal/runner` from `mockagents test` (the management API exposes
list/get/update only). This case therefore measures the runner path; a
concurrent HTTP pipeline load test is **not possible** against the current
product. Raised as [#33](https://github.com/mockagents/mockagents/issues/33).*

**Steps**

1. Author a TestSuite targeting `research-pipeline` (sequential) with ~20
   cases, and a second targeting a parallel/graph topology if one exists.
2. `mockagents test <suite> --agents-dir agents --format json`, timed, ×3.
3. Record wall-clock per case and total; compare sequential vs parallel
   topology cost per node.

**Pass criteria:** run-to-run variance < 20%; per-case cost roughly constant
as case count grows; parallel topology is not slower than sequential for the
same node count. Baseline capture.

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
| 1 (partial: 02–05) | 2026-07-27 | `ad67121` | QA laptop, Windows 11 (details in perf-results/2026-07-27/) | pass — p95 7.6–8.4 ms, 0% err @200 VU (k6) | pass — well under 8 s @20/100 VU | pass — TTFT p50 ≈330 ms (in band) | pass — row cap held; deltas in raw artifacts | not run | **02–05 pass.** Ad-hoc 36 s client `max` investigated → client/OS-side, closed not-a-defect (MA-DEF-006; see perf-results/2026-07-27/). Cross-check repro: 3,796 RPS, client p95 20.5 ms, server-side max 461 ms over 683k reqs. 06+ pending. |
| 1 (cont.: 06–07) | 2026-07-27 | `8a26f15` | Same machine (native binary) | — | — | — | — | pass — cold 57.9 ms → warm 7.5 ms; ≈0 ms warm overhead (4,116 RPS auth vs 3,796 unauth); burst: only 200s+429s, `Retry-After` on all 129,739 429s, zero 5xx | **06–07 pass.** Soak: 2.18M reqs 0 err, p95 flat 13–17 ms, DB capped at 50k rows, session-TTL RSS sawtooth peaked 4.4 GB then declined under load; idle decay 3.95→1.69 GB by +15 min (no leak). Details: `perf-results/2026-07-27/TC-PERF-06-soak-results.md`, `TC-PERF-07-multitenant-results.md`. **08 pass** — healthy p95 ratio 1.102 vs latency-only chaos target (`TC-PERF-08-chaos-isolation-results.md`). **09 pass** — 50/50 concurrent Realtime sessions, correct ladders, RSS +1.4 MB (instant-dial-burst failure = client-machine artifact; stagger dials). **10 pass** — replay 4,140 RPS ≥ live 3,974 RPS. **Cycle 1 COMPLETE: 01–10 all executed, all pass.** ↓ **Cycle 2 (2026-07-28, `75dd153`, Balanced plan): 01–12 all executed, 12/12 pass, zero regressions.** k6 ramp 0.00% failed; 90-min soak 4.62M reqs 0 errors with 45 min of post-TTL steady state (no leak); adapter parity within **1%** (OpenAI/Anthropic/Gemini); MCP 4.1–4.2k RPS ratio 1.02. Four measurement-quality rules added (bench isolation, power-plan matching, retention sawtooth, body-capture noise floor). See `perf-results/2026-07-28/CYCLE-2-SUMMARY.md`. 01 ran off-governor per §4.1 recipe: allocs/op 17/17 exact, control benches flat; matcher/generator ns/B growth = shipped-feature cost since 2026-06-04 → baseline consciously re-committed (`TC-PERF-01-bench-baseline-results.md`; also found+fixed benchreport silently dropping 2 registry benches due to collision-WARN output pollution). |
| 3 | 2026-07-29 | `c722eaa` | same box, Balanced plan | k6 507,906 reqs 0.00% fail, p95 46.4 ms; Python 3,849 RPS / p95 18.59 ms | 2.36 s @20VU · 2.24 s @100VU | 380/360 ms · p95 1.5 s | modes within 4.5% (noise floor) | — | **11/11 executed pass · 0 regressions · 1 blocked.** New baselines: A2A send 3,953 RPS (within 3% of the OpenAI adapter); Batch fan-out sub-linear (0.164→0.027 ms/req as N goes 100→5,000); runner pipelines 1.6–2.8 ms execution vs ~250 ms invocation. ns/op gate retired for sub-µs rows. TC-PERF-15 blocked — no Postgres reachable. See `perf-results/2026-07-29/CYCLE-3-SUMMARY.md`. |

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

## 9. Cycle-4 plan — change-triggered, automation-first

### 9.1 Why the model changes

Three cycles produced **zero product defects and zero regressions**. The
reason is visible in the history: `git log -- internal/ cmd/` shows **no
product-code change between the cycle-1 baselines (`0b826be`, 2026-07-27) and
the end of cycle 3** — the only code touched was a test-only benchmark fix.
Cycles 2 and 3 re-measured the same binary. Their real value was
*measurement-quality* findings (power-plan sensitivity, the unusable sub-µs
`ns/op` gate, the retention sawtooth), and those are now written down.

Continuing to re-run a 10-case suite against unchanged code buys nothing and
costs hours. Cycle 4 therefore inverts the model:

| Old (cycles 1–3) | New (cycle 4 onward) |
|---|---|
| Full suite, on a schedule | Small guard in CI on **every push**; manual runs **triggered by change** |
| Regression is QA's job | Regression is CI's job; QA owns *new surfaces and open questions* |
| Plan grows each cycle | Plan **shrinks** — cases that pass on unchanged code get demoted |

### 9.2 Deliverable 1 (primary): CI perf-guard

A new job — suggested `.github/workflows/perf-guard.yml`, or a job inside
`ci.yml` — that runs on every push touching `internal/` or `cmd/`:

1. `go test -run '^$' -bench . -benchmem ./internal/engine/...` via
   `tools/benchreport`.
2. Compare against the committed `docs/benchmarks/latest.json`:
   - **Fail** on any `allocs/op` or `B/op` change (exact, machine-independent,
     stable across three cycles — this is the signal that actually works).
   - **Warn only** on `ns/op` for sub-µs rows; fail only if a **µs-scale** row
     (`ProcessRequest_*`) moves >25%. Runner CPUs vary; see §4.1.
3. On an intentional change, the PR updates `latest.json` in the same commit —
   the diff becomes the review artifact.

This closes the "engine drift between QA cycles" gap that has been the top
open item since cycle 1, and it is what makes scheduled manual regression
unnecessary rather than merely tedious.

**Note:** the guard needs *no* new tooling — `tools/benchreport` already emits
schema-v1 JSON built for exactly this.

### 9.3 Deliverable 2: demote the settled cases

Cases that passed three consecutive cycles on unchanged code, with no finding
attributable to the product, move to **on-demand** — run when their surface
changes, not on a schedule:

| Demoted to on-demand | Trigger to re-run |
|---|---|
| TC-PERF-02, 03, 04 (throughput / streaming / TTFT) | any change under `internal/adapter/`, `internal/streaming/`, `internal/engine/` |
| TC-PERF-05 (logging modes) | `internal/storage/`, `internal/server/log_*` |
| TC-PERF-08 (chaos isolation) | `internal/engine/chaos.go`, `internal/config/chaos_presets.go` |
| TC-PERF-09, 10, 12, 13 (Realtime, replay, MCP, A2A) | their own package |
| TC-PERF-01 (bench) | **now CI's job** — manual run only when re-baselining |

TC-PERF-06 (soak) stays **scheduled but rare**: once per release candidate,
not per cycle — leaks are time-dependent, not commit-dependent, so a
change-trigger would miss them.

### 9.4 Deliverable 3: the two open questions

Manual cycle-4 work is limited to what earlier cycles genuinely could not
answer:

1. **TC-PERF-16 redo with latency-injected nodes.** Cycle 3 could not compare
   parallel vs sequential topology because per-node execution is sub-millisecond
   — the delta was scheduling noise. Re-run with each node backed by a
   chaos-latency agent (~100 ms uniform) so a sequential 2-node pipeline should
   cost ~2 node-times and a parallel one ~1. That is a real answer about the
   executor.
2. **TC-PERF-15 (Postgres tenancy)** — still blocked ([#35](https://github.com/mockagents/mockagents/issues/35));
   runs the moment a reachable Postgres exists (§ cycle-3 summary lists four unblock paths).
   Compare against a **same-day** SQLite run.

### 9.5 Entry / exit

**Entry:** none for the CI guard (it is a code change, reviewable as a PR).
The two manual cases need their respective unblocks (a parallel-pipeline
fixture is already committed; Postgres is not).

**Exit:** perf-guard merged and demonstrably failing on a deliberate
allocs/op change (prove the gate works before trusting it); TC-PERF-16 redo
recorded with a defensible topology answer; TC-PERF-15 either executed or
still formally blocked with an owner.

### 9.6 Explicitly not doing

A full 12-case manual re-run. If the CI guard is green and no surface-specific
trigger fired, there is nothing a scheduled cycle would discover — and three
cycles of evidence say so.

### Deferred beyond cycle 4

Multi-instance / horizontal scaling, Helm-deployed cluster performance, GUI
rendering, sustained multi-hour parity runs.

> Cycle-3's execution plan is retired; its results live in
> `perf-results/2026-07-29/CYCLE-3-SUMMARY.md`.

## 10. Known environment caveats

- Windows Balanced power plan → ~1.4× uniform ns/op inflation (§4.1).
- Docker/Rancher Desktop VM → do not time through it (§4.1). Rancher's
  engine has also been observed to stop mid-session on the QA machine
  (see TROUBLESHOOTING.md) — another reason timed runs stay native.
- Corporate TLS proxy may block k6 installation → Locust fallbacks inline.
- Antivirus real-time scanning of `.mockagents.db*` can serialize SQLite
  writes; exclude the working directory for timed runs if policy allows.
- k6 buffers SSE bodies: k6 can gate on *total* stream time only. TTFT
  claims must come from Locust (§5). Never report k6 TTFB as TTFT.
- Single-machine loopback runs produce occasional multi-second **client-side**
  `max` outliers (load generator and server share CPU + one TCP stack) with no
  server-side counterpart. Apply the §5 outlier triage rule before filing;
  gate on percentiles, never on `max`, for pass/fail decisions.
- **Sessionless traffic holds memory by design:** each request without an
  `X-Session-Id` creates a 30-min-TTL session (~2–2.5 KB); at ~800 RPS the
  live set peaks at ~4 GB RSS before TTL expiry balances creation (cycle-1
  soak). Not a leak — but budget RAM for high-RPS runs, or reuse a session
  id to keep memory flat.
