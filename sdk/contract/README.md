# Cross-SDK contract suite

One agent fixture, one shared list of cases, three runners. Each runner — Python
(pytest), TypeScript (vitest) and Go (`testing`) — loads `cases.json`, executes
every case through **its own SDK's public client and assertion API** against a
single real `mockagents` server, and checks that each assertion's verdict equals
the verdict the case file expects. Because all three read the same expected
verdicts, a matcher that behaves differently in one language fails that
language's runner.

| Path | What it is |
| --- | --- |
| `sdk/contract/agents/` | Agent fixtures the server loads (`--agents-dir`). |
| `sdk/contract/tenant/contract-tenant-agent.yaml` | Tenant-owned twin of the global agent, registered over the API in the multi-tenant leg. Deliberately outside `agents/`. |
| `sdk/contract/cases.json` | The shared, data-driven cases. |
| `sdk/contract/provision-tenant.sh` | Multi-tenant setup: tenant + tenant key + tenant agent via the management API. |
| `sdk/python/tests/contract/test_contract.py` | Python runner. |
| `sdk/typescript/tests/contract.test.ts` | TypeScript runner. |
| `sdk/go/mockagents/contract_test.go` | Go runner. |
| `.github/workflows/sdk-contract.yml` | CI: builds the binary once and runs both legs. |

Every runner **skips** unless `MOCKAGENTS_CONTRACT_URL` is set, so the ordinary
SDK test suites are unaffected.

## Verdicts

Each assertion expects one of:

- `pass` — the SDK matcher accepted the result.
- `fail` — the SDK matcher rejected it (Python `AssertionError`, TypeScript
  `AssertionError`, Go `t.Errorf` — the Go runner hands the matcher a recording
  `testing.TB`, so a failing matcher is captured instead of failing the test).
- `error` — executing the case **raised** (HTTP error, broken stream). Every
  assertion of that case then has the verdict `error`. This is what proves a
  negative assertion cannot pass vacuously on a server error.

## Assertion kinds

Only kinds that exist in all three SDKs are part of the contract.

| Kind | Args | Python | TypeScript | Go |
| --- | --- | --- | --- | --- |
| `response_contains` | `text` | `to_have_response_containing` | `toHaveResponseContaining` | `ToHaveContentContaining` |
| `tool_call` | `name`, `arguments?` | `to_have_tool_call` | `toHaveToolCall` | `ToHaveToolCall` |
| `tool_call_count` | `count`, `name?` | `to_have_tool_call_count` | `toHaveToolCallCount` | `ToHaveToolCallCount` / `ToHaveToolCallCountByName` |
| `tool_call_sequence` | `names` | `to_have_tool_call_sequence` | `toHaveToolCallSequence` | `ToHaveToolCallSequence` |
| `finish_reason` | `reason` | `to_have_finish_reason` | `toHaveFinishReason` | `ToHaveFinishReason` |
| `status_code` | `code` | `to_have_status` | `toHaveStatusCode` | `ToHaveStatusCode` |
| `stream_text_matches_nonstream` | — | runner check over `iter_stream` | runner check over `iterStream` | runner check over `IterStream` |

Semantics shared by all three: outcome kinds (`response_contains`,
`finish_reason`, `status_code`) read the **final** turn; trajectory kinds
(`tool_call*`) read the aggregate of **all** turns; `tool_call` argument matching
is partial, compares JSON values (`5` matches `5`, not `"5"`), and an expected
`null` matches only an explicit `null`, never a missing key.

Left out because they are not available in every SDK:

- any-turn response contains — Python only (`to_have_any_response_containing`).
- latency — TypeScript `toHaveLatencyLessThan` and Go `ToHaveLatencyLessThanMs`
  only (and timing-dependent).
- malformed tool arguments, tool errors — Python only
  (`to_have_malformed_tool_arguments`, `to_have_tool_error`).
- pipeline node sequence — Python and TypeScript only (the Go SDK has no
  pipeline client).

## Streaming cases

With `"stream": true` the runner sends each user step through the SDK's
protocol-agnostic stream iterator, concatenates the chunk text into one response
per turn (finish reason from the final chunk), and runs the ordinary matchers on
it. A stream that ends without a finished chunk counts as `error`.
`stream_text_matches_nonstream` additionally runs the same steps without
streaming and compares the final turn's text exactly (this is where multi-byte
UTF-8 split across SSE frames is checked). Streamed tool calls are not
assembled, so do not put `tool_call*` kinds on streaming cases.

## Multi-tenant leg

Cases with an `auth` field run only when `MOCKAGENTS_CONTRACT_TENANT_KEY` is
set; cases without one run only when it is not. `auth` selects the client
credential:

- `tenant` — the tenant editor key minted by `provision-tenant.sh`. Must reach
  the tenant-owned agent, on plain and streamed requests.
- `none` — no key. Must fall back to the global agent.
- `wrong` — `wrong_api_key` from `cases.json`, well-formed but never
  registered. The LLM endpoints are auth-exempt in multi-tenant mode (clients
  send their own provider keys), and the server resolves credentials there
  best-effort, so an unknown key is **not** a 401: the request continues
  anonymously and reaches the global agent. The management API, by contrast,
  answers the same key with 401.

## Running locally

From the repository root, in bash:

```bash
go build -o /tmp/contract/mockagents ./cmd/mockagents
/tmp/contract/mockagents validate --strict sdk/contract/agents
/tmp/contract/mockagents validate --strict sdk/contract/tenant

# Single-tenant leg. Run the server from a scratch directory: it writes its
# SQLite files into its working directory.
(cd /tmp/contract && ./mockagents start --agents-dir "$OLDPWD/sdk/contract/agents" --port 18080 &)
export MOCKAGENTS_CONTRACT_URL=http://127.0.0.1:18080

(cd sdk/python && PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 python -m pytest tests/contract -v)
(cd sdk/typescript && npm ci && npx vitest run tests/contract.test.ts)
go test ./sdk/go/mockagents -run Contract -count=1 -v
```

Multi-tenant leg (stop the first server or use another port). The agent write
API persists the tenant agent into the agents directory, so serve a **copy**:

```bash
mkdir -p /tmp/contract-mt/agents && cp sdk/contract/agents/*.yaml /tmp/contract-mt/agents/
export MOCKAGENTS_CONTRACT_PLATFORM_KEY="mak_$(openssl rand -hex 4)_$(openssl rand -hex 16)"
(cd /tmp/contract-mt && MOCKAGENTS_MULTI_TENANT=1 MOCKAGENTS_BOOTSTRAP_KEY="$MOCKAGENTS_CONTRACT_PLATFORM_KEY" \
  "/tmp/contract/mockagents" start --agents-dir /tmp/contract-mt/agents --port 18081 &)
export MOCKAGENTS_CONTRACT_URL=http://127.0.0.1:18081
export MOCKAGENTS_CONTRACT_TENANT_KEY="$(bash sdk/contract/provision-tenant.sh)"
# ...then the same three runner commands as above.
```

## Adding a case

1. If the case needs new server behaviour, add a scenario to
   `agents/contract-agent.yaml` (scenarios match on the latest user message, so
   pick a keyword no other step uses) and re-run `mockagents validate --strict`.
2. Append an object to `cases` in `cases.json`:

   ```json
   {
     "name": "short-kebab-name",
     "description": "What the case pins down and why.",
     "protocol": "openai",
     "stream": false,
     "model": "contract-main",
     "steps": ["first user message", "second user message"],
     "assertions": [
       {"kind": "response_contains", "args": {"text": "hello"}, "expect": "pass"}
     ]
   }
   ```

   `model` defaults to `default_model`; `auth` (`none` | `tenant` | `wrong`)
   moves the case to the multi-tenant leg. Pair every positive assertion with a
   negative one where it makes sense, so a matcher that always passes is caught.
3. Run all three runners. If they agree, you are done.

If an SDK gets a case **wrong**, do not change the expected verdict to match
it. Keep the correct verdict and add
`"known_divergence": "<python|typescript|go>: <one line>"` (or an array of such
strings) to the case: that runner skips the case and prints the note, the other
two keep enforcing it, and the note is the bug report. Remove it once the SDK is
fixed.
