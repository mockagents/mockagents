# Load-test your LLM app for free

Load-testing an app that calls an LLM is painful: every run burns tokens, trips
rate limits, and racks up a bill — and generic HTTP load tools mis-measure LLMs
because they don't understand **time-to-first-token (TTFT)** or **inter-token
latency (ITL)**.

MockAgents flips it around: it's the **target**, not the generator. Point
[k6](https://k6.io) or [Locust](https://locust.io) at the mock and it serves
**realistic, long-tailed streaming latency** — so your client sees
production-like timing with **zero tokens and zero cost**.

> Don't build a load *generator* — use the one you already know (k6/Locust) and
> let the mock supply the LLM physics.

## 1. Define a realistic target

Give a streaming agent **latency percentiles** instead of a single fixed delay.
The mock samples each delay from a lognormal fit to your p50/p95, matching the
long-tailed shape of real LLM latency:

```yaml
streaming:
  enabled: true
  chunk_size: 1
  ttft_p50_ms: 350      # median time-to-first-token
  ttft_p95_ms: 1400     # p95 TTFT (the long tail)
  itl_p50_ms: 20        # median inter-token latency (per token)
  itl_p95_ms: 60        # p95 inter-token latency
```

These override the fixed `ttft_ms` / `tokens_per_sec`. A ready-made target ships
at [`examples/load-target-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/load-target-agent.yaml).

```bash
mockagents start --agents-dir examples
```

## 2. Point your load tool at it

### k6

```bash
k6 run examples/loadtest/k6.js
# tune: VUS=50 DURATION=1m BASE=http://localhost:8080 k6 run examples/loadtest/k6.js
```

The [script](https://github.com/mockagents/mockagents/blob/main/examples/loadtest/k6.js)
streams `model: load-target-model` and records the **total streamed completion
time** (`http_req_duration` = TTFT + every inter-token delay) as `stream_total_ms`,
asserting every response is an SSE stream that ends with `[DONE]`. Thresholds fail
the run if the error rate exceeds 1% or p95 total latency exceeds 8s.

> k6's plain HTTP client buffers the whole response, so it can't isolate TTFT
> from the full stream (TTFB/`http_req_waiting` is the 200 headers, which flush
> *before* the time-to-first-token delay). Use Locust below if you need TTFT
> broken out.

### Locust

```bash
locust -f examples/loadtest/locustfile.py --host http://localhost:8080
# headless:
locust -f examples/loadtest/locustfile.py --host http://localhost:8080 \
       --headless -u 20 -r 5 -t 30s
```

The [locustfile](https://github.com/mockagents/mockagents/blob/main/examples/loadtest/locustfile.py)
consumes the stream line-by-line. Because the mock applies the configured TTFT
*before* the first SSE frame, the first line arrives after ≈TTFT — Locust reports
that as a dedicated **`TTFT`** entry in the stats table (alongside the default
full-stream response time), and fails any request whose stream doesn't terminate
with `[DONE]`.

## 3. Turn up the pressure with chaos

Because it's the same mock, you can compose load with [failure injection](yaml-schema.md#named-presets):
add `chaos: { preset: rate-limited }` to see how your retry/backoff behaves
under 429s at load, or `preset: flaky` to test recovery. You're now load-testing
the **unhappy path** too — something you can't safely do against a real provider.

## What this does and doesn't do

- ✅ Realistic TTFT + inter-token timing, streamed, at any concurrency your load
  tool drives — free and reproducible.
- ✅ Compose with chaos/faults to load-test error handling.
- ❌ Not a load *generator*: bring k6/Locust (or Gatling, Vegeta, …).
- ❌ Not a model: token *counts* are approximate; this measures your app's HTTP
  + streaming path, not model quality.
