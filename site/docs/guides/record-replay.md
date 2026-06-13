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

### Redact secrets before they hit the cassette

Your `--api-key` is never written, but request/response **bodies** can still
carry tokens (an `Authorization` echoed in an error, a secret in your prompt).
Add `--redact` to mask common secret formats — `sk-*`, `key-*`, `Bearer`
tokens, AWS `AKIA…`, GitHub `ghp_/github_pat_…`, Slack `xox…`, Google `AIza…`,
and JWTs — before each interaction is stored:

```bash
mockagents record \
  --upstream https://api.openai.com \
  --cassette fixtures/checkout-flow.jsonl \
  --api-key "$OPENAI_API_KEY" \
  --redact \
  --redact-pattern 'cust_[0-9]+'        # repeatable; add your own formats
```

Redaction is **structure-preserving** — it rewrites JSON string *values* only,
so a pattern can never break the cassette's framing, rename a key, or corrupt an
SSE frame, and replay still matches because the request hash is taken from the
original body. Coverage is best-effort: review a cassette before committing it.

## 2. Replay

Serve the cassette with no upstream, no key, no network — same requests get the
same recorded responses every time.

```bash
mockagents replay --cassette fixtures/checkout-flow.jsonl
# point your tests at http://localhost:8080 — fast, free, offline, deterministic
```

Request matching canonicalizes JSON (sorted keys), so SDK field reordering still
hits the cassette. An unknown request during replay returns a `404` whose JSON
body tells you what drifted, instead of just a hash:

```json
{
  "error": "no cassette match",
  "method": "POST",
  "path": "/v1/chat/completions",
  "hash": "3f2a91bc04e7",
  "nearest": {
    "hash": "7a1c3e9f2b05",
    "similarity": 0.75,
    "diff": [
      {"field": "messages", "kind": "changed",
       "cassette_value": "...", "request_value": "..."}
    ]
  }
}
```

`nearest` is the closest recorded interaction **on the same method+path**, scored
by top-level field overlap; the `diff` lists `changed` / `missing_in_request` /
`extra_in_request` fields (bounded, with long values truncated). A drifted prompt
now points you straight at the field that changed.

### Ignore replay-time fields

SDKs and frameworks often inject sampling fields (`temperature`, `seed`,
`stream`, `metadata`) that weren't in the recorded request, or vary them per run.
Use `--match-ignore` (repeatable) to ignore those top-level fields when matching:

```bash
mockagents replay \
  --cassette fixtures/checkout-flow.jsonl \
  --match-ignore temperature \
  --match-ignore seed \
  --match-ignore stream
```

Ignored fields are **replay-time only** — the cassette on disk and each stored
hash are unchanged, and exact-hash matching stays the default when no
`--match-ignore` is given. Sequenced playback (the Nth request → the Nth recorded
response) is preserved across ignored-field differences.

### Record on miss: `--record-mode`

`replay` can fall through to a real upstream and record what it didn't have,
turning it into a record/replay hybrid — pass `--upstream` plus `--record-mode`:

```bash
# Replay what's recorded; record anything new from the real API on a miss
mockagents replay \
  --cassette fixtures/checkout-flow.jsonl \
  --record-mode new_episodes \
  --upstream https://api.openai.com \
  --api-key "$OPENAI_API_KEY" \
  --redact
```

| Mode | On a hit | On a miss | Use it to |
|---|---|---|---|
| `none` *(default)* | replay | 404 with diff | run fully offline |
| `new_episodes` | replay | forward + record | grow a cassette as new requests appear |
| `once` | replay | forward + record **only if the cassette is empty/new** | record a flow the first time, replay forever after |
| `all` | — (never replays) | forward + record every request | re-record a whole session / use as a recording proxy |

Record-on-miss reuses the same `--api-key`, `--redact`, and `--redact-pattern`
flags as `mockagents record`, and never caches a transient failure: a 4xx/5xx
response — or a stream that breaks mid-flight — is returned to your client but
**not** written to the cassette (so a flake can't poison your fixtures). `all`
mode, being a faithful re-record, *does* capture error responses.

> `all` rewrites the whole cassette file on each record and does not de-duplicate
> repeated requests — keep `all` sessions short, or use `new_episodes` for a
> long-lived hybrid server.

### Import an existing cassette

Already have recordings from another tool? Convert them instead of re-recording:

```bash
# A vcrpy (Python) YAML cassette
mockagents import vcr fixtures/openai.yaml -o cassette.jsonl

# An OpenAI stored-completions JSONL export
mockagents import openai-stored-completions export.jsonl -o cassette.jsonl

# Then serve it like any cassette
mockagents replay --cassette cassette.jsonl
```

`import vcr` understands vcrpy's body shapes (plain string, `base64_string`,
gzip'd) **and** parsed-JSON request bodies (vcrpy's JSON serializer). By default
it keeps only POSTs to the LLM endpoints (pass `--all` for everything), drops
credential-bearing headers, and skips anything it can't import with a printed
reason. `import openai-stored-completions` accepts either an envelope
(`{"request":…,"response":…}`) or a flat stored completion and reconstructs the
request; this format is a MockAgents-defined contract, so a raw export may need a
small pre-massage.

> Secrets inside request/response **bodies** are not redacted on import — review
> the cassette before committing, or re-record through
> [`mockagents record --redact`](#redact-secrets-before-they-hit-the-cassette).

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
