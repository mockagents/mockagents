# Chaos & Fault Injection

Your app's unhappy paths — retries, backoff, timeouts, circuit breakers, SSE
parser recovery — are the code you can't safely test against a real provider:
you can't ask OpenAI for a 429 on demand. MockAgents can inject every one of
those faults deterministically, per agent, in the provider's **real error
shapes**, so a client SDK reacts exactly as it would in production.

Two fault surfaces compose:

- **`spec.behavior.chaos`** — request-level faults: latency, HTTP errors, rate
  limits, timeouts, and connection-layer (TCP) faults.
- **`spec.behavior.streaming`** — mid-*stream* faults and timing physics:
  TTFT/inter-token pacing, truncated streams, malformed SSE frames.

Field-by-field reference lives in the
[YAML schema guide](yaml-schema.md#specbehaviorchaos); this page is the
task-oriented tour.

## Quickstart: one-line presets

The fastest way to a failure mode is a named preset:

```yaml
spec:
  behavior:
    chaos:
      preset: rate-limited     # every request -> 429 + Retry-After
    scenarios:
      - name: default
        response: { content: "You won't often see this." }
```

| Preset | Expands to |
|---|---|
| `server-down` | every request → 503 "the server is temporarily unavailable" |
| `rate-limited` | every request → 429 "rate limit exceeded" (+ `Retry-After`) |
| `access-denied` | every request → 403 |
| `unauthorized` | every request → 401 |
| `flaky` | first 2 requests → 503, then recover (`fail_first: 2`) |
| `slow` | 2–5 s uniform latency on every response |
| `connection-reset` | every request → TCP RST before any HTTP response |

A preset only fills the sub-sections you leave unset, so it composes with
explicit overrides — `preset: flaky` plus your own `latency:` block works.
See [`examples/access-denied-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/access-denied-agent.yaml).

## The explicit knobs

[`examples/chaos-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/chaos-agent.yaml)
exercises three at once:

```yaml
chaos:
  enabled: true
  latency:
    distribution: uniform     # fixed | uniform | normal
    min_ms: 100
    max_ms: 300
  errors:
    rate: 0.1                 # 10% of requests fail...
    status_codes: [503, 504]  # ...with one of these (or a single status_code)
    message: "Upstream provider temporarily unavailable"
  rate_limit:
    requests: 20              # rolling-window token bucket:
    window_ms: 60000          # 21st request in a minute -> 429 + Retry-After
```

- **`latency`** — `fixed` (min_ms), `uniform` (min_ms–max_ms), or `normal`
  (mean_ms/stddev_ms, clipped at 60 s). Applied after generation, so it
  composes with streaming timing.
- **`errors`** — probability-gated (`rate`) or deterministic (`fail_first: N`
  fails the first N requests then recovers — the retry/backoff fixture; see
  [`examples/flaky-then-healthy-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/flaky-then-healthy-agent.yaml)).
  `timeout: true` + `timeout_ms` holds the request open then returns a
  synthetic 504. Injected 429s always carry a `Retry-After` header.
- **`rate_limit`** — a real rolling window per agent; `Retry-After` is the
  window remainder, so well-behaved backoff code recovers on schedule.
- **`connection`** — faults the TCP connection itself, before any HTTP
  response, for the transport errors an error *body* can't simulate:

    ```yaml
    chaos:
      connection:
        mode: reset      # reset | empty | random
        rate: 1.0        # or fail_first: N
    ```

    `reset` = connection reset by peer (RST), `empty` = EOF with no bytes,
    `random` = unparseable garbage bytes. See
    [`examples/connection-fault-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/connection-fault-agent.yaml).

Order per request: rate limit → errors → connection faults, then latency on
the way out.

## Provider-faithful error shapes

An injected error is rendered in each protocol's own envelope — an OpenAI
client sees `{"error": {"type": "server_error", ...}}`, an Anthropic client
sees `{"type": "error", "error": {"type": "overloaded_error", ...}}`, a Gemini
client sees `{"error": {"code": 503, "status": "UNAVAILABLE", ...}}` — with
status-appropriate `type`/`code`/`status` values (401 →
`invalid_api_key` / `authentication_error` / `UNAUTHENTICATED`, and so on).
The full mapping table is in the
[YAML schema guide](yaml-schema.md#specbehaviorchaos). Your SDK's typed error
classes, retry predicates, and `Retry-After` handling all fire for real.

## Mid-stream faults & timing physics

HTTP errors arrive before the body; the nastier production failures happen
**mid-stream**. Those live in the `streaming` block
([`examples/stream-faults-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/stream-faults-agent.yaml)):

```yaml
streaming:
  enabled: true
  ttft_ms: 200               # time-to-first-token delay
  tokens_per_sec: 20         # paced output (overrides chunk_delay_ms)
  jitter_ms: 30              # deterministic +/- jitter per chunk
  truncate_after_chunks: 3   # stream dies after 3 chunks - no finish frame, no [DONE]
  # malformed: true          # or: emit one invalid-JSON SSE frame, then end
```

- `truncate_after_chunks` reproduces the dropped-connection-mid-answer case:
  your client gets content, then silence — no `finish_reason`, no `[DONE]`.
- `malformed` feeds your SSE parser an invalid-JSON frame.
- For **load testing**, replace the fixed timing with latency *percentiles*
  (`ttft_p50_ms`/`ttft_p95_ms`, `itl_p50_ms`/`itl_p95_ms`) and the mock samples
  each request from a lognormal fit — realistic long-tailed LLM latency. That
  workflow (k6/Locust scripts included) has its own guide:
  [Load Testing](load-testing.md).

## Semantic errors: well-formed but wrong

Not every failure is transport-level. A scenario response can plant a
*successful* response that's wrong in the ways real models are wrong —
truncation (`finish_reason: length`), refusals, malformed tool-call argument
JSON (`raw_arguments`), and [hallucination fixtures](hallucination-testing.md).
See [Semantic error modes](yaml-schema.md#semantic-error-modes).

## Composing it all

Because chaos is per-agent YAML, you can dial failure modes into an otherwise
realistic fixture and drive it with your real test suite:

```yaml
# A support agent that rate-limits under load and drops 10% of streams
chaos:
  rate_limit: { requests: 5, window_ms: 1000 }
streaming:
  enabled: true
  truncate_after_chunks: 8
```

Pair with [`mockagents test`](testing-agents.md) assertions or your load tool
([guide](load-testing.md)) to make the unhappy path a permanent CI citizen.

## Troubleshooting

- **Chaos never fires** — a `chaos:` block with any sub-section is enabled
  automatically, but check you didn't set `enabled: false`; and remember
  `errors.rate` is a probability — use `rate: 1.0` or `fail_first` for
  deterministic tests.
- **`fail_first` keeps failing / never resets** — the counter is per-agent and
  resets only on server restart. For test isolation, restart the mock (or use
  distinct agents) between suites.
- **Connection faults return 502 instead of resetting** — over HTTP/2 the
  server can't hijack the TCP connection; it falls back to a 502. Use
  HTTP/1.1 (the default for `http://` URLs) to exercise real transport faults.
- **My client "recovers" from truncation silently** — assert on completeness
  explicitly (e.g. final frame / `[DONE]` seen); many SDKs surface truncation
  only as a shorter-than-expected message.
