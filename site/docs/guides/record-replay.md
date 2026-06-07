# Record & Replay (the fastest on-ramp)

The quickest way to a deterministic test double is **not** to hand-write YAML —
it's to record your real provider traffic once and replay it forever. Reach for
hand-authored agents only for the cases you can't easily record (synthetic edge
cases, faults, future scenarios).

```
record real traffic  ──►  replay offline, deterministically  ──►  (optional) hand-write YAML for synthetic cases
```

## 1. Record

Put MockAgents in front of the real API and run your app through the flow you
want to freeze. Your API key stays on your machine — it is **never** written to
the cassette.

```bash
# Start recording against a real upstream
mockagents record \
  --upstream https://api.openai.com \
  --cassette fixtures/checkout-flow.jsonl \
  --api-key "$OPENAI_API_KEY"

# In your app, point at the recorder and run your scenario once:
export OPENAI_BASE_URL=http://localhost:8080/v1
python run_checkout_flow.py
```

Cassettes are JSON-lines — safe to `git diff`, review, and check in. SSE
(streaming) responses are captured and replayed faithfully.

## 2. Replay

Serve the cassette with no upstream, no key, no network — same requests get the
same recorded responses every time.

```bash
mockagents replay --cassette fixtures/checkout-flow.jsonl
# point your tests at http://localhost:8080 — fast, free, offline, deterministic
```

Request matching canonicalizes JSON (sorted keys), so SDK field reordering still
hits the cassette. An unknown request during replay returns `404` with the
SHA-256 prefix of the miss, so you can see exactly what wasn't recorded.

## 3. Graduate to YAML for what you can't record

Record/replay is perfect for "make my real flow deterministic." For cases that
are awkward or impossible to capture from a live provider — a specific tool-call
routing, a `429`, a truncated stream, a malformed response — define a small
[`kind: Agent`](yaml-schema.md) with scenarios and chaos/streaming faults. The
two compose: replay your happy path, hand-author the failure paths.

| Use | Reach for |
|---|---|
| Freeze an existing real flow | **record → replay** |
| Assert tool-call routing / scenarios | [hand-written agent](testing-agents.md) |
| Inject 429 / timeout / truncated / malformed | [chaos + streaming faults](yaml-schema.md) |

## CI

Commit the cassette and run `mockagents replay` (or the
[`setup-mockagents` action](https://github.com/mockagents/mockagents/tree/main/deploy/actions/setup-mockagents))
as a service in your pipeline — zero token cost, zero flakiness.
