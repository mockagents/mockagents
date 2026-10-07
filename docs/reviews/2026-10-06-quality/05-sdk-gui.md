# Review 05: client-facing packages (SDKs + GUI)

Scope: `sdk/python`, `sdk/typescript`, `sdk/go/mockagents`, `sdk/npx`, `sdk/vitest` and `gui/`, at worktree `mock-agents-qloop` @ `9fa4db7` (origin/main).
Reviewer: #5 of 6. Repo was treated as read-only; every repro ran in `scratchpad/review/repro-sdk/`.

## Evidence base

| Check | Result |
|---|---|
| Python SDK `pytest tests` (scratch copy) | 148 passed |
| TS SDK `vitest run` / `tsc --noEmit` / `npm run build` | 77 passed / clean / ok |
| Vitest helper `vitest run` | 9 passed, 3 skipped (e2e needs a binary) |
| npx `node --test` | 5 passed. **`npm ci` fails: EUSAGE, no lockfile** (K-08) |
| Go SDK `go test ./sdk/go/... -count=1` | ok |
| GUI `vitest run` / `tsc --noEmit` | 329 passed (17 files) / clean. **`npm run lint` is broken** (K-21) |
| Packed-consumer test (`npm pack` both packages, then `require()`) | `ERR_PACKAGE_PATH_NOT_EXPORTED` for `@mockagents/vitest/jest` and `@mockagents/sdk` (K-09) |
| Wheel build (`pip wheel`) and METADATA inspection | No License-File, no Project-URL, no `py.typed` (K-20) |
| **Live runs against a real binary** (built with `go build -o scratch/bin` from the repo; no repo writes) | Confirmed K-01, K-04 and K-05 end to end, including in multi-tenant mode |

Repro scripts are in `repro-sdk/repro/`: `py_repro.py`, `py_utf8.py`, `py_live.py` and `py_mt.py`. Also used: `repro-sdk/typescript/repro*.mjs`, `repro-sdk/gorepro/repro_test.go` and `repro-sdk/gui/test/zz_repro.test.ts`.

Note for the coordinator: an untracked file `coverage` (990 KB, mtime 09:43) appeared in the shared worktree root. It predates my first Go run and was not created by this reviewer.

---

## 1. New findings

Severity counts: S1 = 3, S2 = 7, S3 = 16 (26 in total).

| ID | Sev | File:line | Finding | Failure scenario (verified unless marked PLAUSIBLE) | Recommended fix | Recommended test | Effort |
|---|---|---|---|---|---|---|---|
| K-01 | **S1** | py `client.py:66-77,100-110,154-168,275-290,299-323` (only `run_pipeline` uses `api_key`, `:336-337`); TS `client.ts:255-285` (`requestSSE` never adds `Authorization`; `requestJSON` does at `:358-360`); Go `client.go:20-24` (no API-key option at all) | The configured credential is silently dropped. Python drops it on every LLM and management call. TS drops it on every streaming call. Go has no way to set one. In multi-tenant mode the LLM endpoints accept anonymous callers, so the request **succeeds against a different agent**. | Live test, MT mode with a tenant-owned agent `model: tenant-model` (created through the write API): TS `chat(..., {model:"tenant-model"})` with `apiKey` returned `"tenant agent reply"`, but TS `iterStream` with the same options returned `"café ☕ 日本"`, which is the global fallback agent. Python `MockAgentClient(api_key=key).chat(model="tenant-model")` also returned the fallback agent's text. `list_agents()` returned 401. A negative assertion such as "agent made no tool call" passes against the wrong agent. | Put auth-header injection in one place, the request helper, for every path including SSE. When a key is set, Anthropic calls should send it rather than the `X-Api-Key: mock-api-key` placeholder. Add `APIKey` to Go `ClientOptions`. | Per-SDK MT integration test: a tenant agent reached through chat, message, chat-stream, message-stream and iter-stream with a key, plus a negative test that the call fails when the key is wrong. | M |
| K-02 | **S1** | py `assertions.py:72-86, 210-236` | Python outcome assertions (`to_have_response_containing`, `to_have_status`, `to_have_finish_reason`) pass if **any** turn matches. The YAML runner (`internal/runner/runner.go:241-261`), TS (`assertions.ts:72,90-115`) and Go (`expect.go:41-57`) read the **final** turn. | `py_repro.py` K2: turn 1 says "error: payment failed" and the final turn says "all good". `expect(result).to_have_response_containing("error")` passes in Python and fails in TS, Go and YAML. Likewise `to_have_status(200)` passes when the final turn failed. | Align with the runner: outcome assertions read `last_response`. Offer `to_have_any_response_containing` if "any turn" is wanted. | Cross-SDK golden test: one scenario, one assertion, same verdict in all three SDKs and in YAML. | S |
| K-03 | **S1** | py `assertions.py:110-112`; `types.py:161-171`; TS `types.ts:121-131,159-166`; Go `types.go:183-201` | Python argument matching uses `tc.arguments.get(k) == v`, so an **absent** argument matches an expected `None`. TS and Go correctly fail on a missing key. Separately, every SDK collapses malformed or non-object tool arguments to `{}` or `nil` and exposes no raw string. The FB-03 `raw_arguments` fixture (`adapter/openai.go:438-440`) therefore cannot be observed, and Python crashes (`AttributeError`) on non-dict JSON such as `"[1]"`. | `py_repro.py` K1: `to_have_tool_call("search", {"filter": None})` passes when `filter` was never sent. With malformed `arguments`, the parsed args are `{}`, so `{"x": None}` also passes. | Use a sentinel (`k in args and args[k] == v`). Keep `raw_arguments: str` on `ToolCall` in all three SDKs, and add `to_have_malformed_tool_arguments()` / `toolCall.argumentsValid`. | Unit tests: missing key vs explicit null; malformed args are surfaced and do not become `{}`. | S |
| K-04 | S2 | py `assertions.py:192-208`; documented at `site/docs/sdk/python-sdk.md:255` | `to_have_tool_error(code)` reads `raw["tool_results"]`, but no wire response contains that field. It exists only on the engine-internal `Response` (`engine/response_generator.go:26`), and the adapters drop it (`adapter/openai.go:427-460`). The documented assertion **can never pass** against a real server, and the test suite has no test for it. | `py_live.py` against the real binary, with a `lookup_order` error fixture `NOT_FOUND`: the tool call fires and `raw` keys are `[id, object, created, model, choices, usage]`. The assertion raises "none found". | Either surface tool results on the wire (for example an `X-Mockagents-Tool-Errors` header, or the logs API keyed by request id) and read them from there, or delete the assertion and its doc. Implement the same assertion in TS and Go once a wire contract exists. | Live test: fixture error leads to the assertion passing; a success leads to it failing. | M |
| K-05 | S2 | py `client.py:113,171,397,486`; `mcp.py:131` (`iter_lines(decode_unicode=True)`); server `internal/streaming/sse_writer.go:37` (`text/event-stream`, no charset) | `requests` defaults `text/*` without a charset to ISO-8859-1, so **every Python streaming path mangles non-ASCII text**. Non-streaming calls are correct, so streamed and non-streamed results differ. MCP event params are affected the same way. | Live: non-stream `'café ☕ 日本'` vs stream `'cafÃ© â\x98\x95 æ\x97¥æ\x9c¬'`. `expect(stream_resp).to_have_response_containing("café")` fails (also reproduced with `responses` in `py_utf8.py`). | Set `resp.encoding = "utf-8"` before `iter_lines`, or iterate bytes and decode UTF-8 explicitly. Also have the server send `text/event-stream; charset=utf-8`. | Streaming test with multi-byte UTF-8 split across chunk boundaries (Python, plus a TS/Go check). | S |
| K-06 | S2 | Go `expect.go:176-184`; `types.go:183-201` | `ToHaveToolCall` compares with `reflect.DeepEqual` against `encoding/json`-decoded `float64` values. Any integer expectation (`map[string]any{"limit": 5}`) **always fails** (a false fail), and the failure message is self-contradictory. | `gorepro/repro_test.go`: the error reads `expected tool call "search" with args map[limit:5], got [search(map[limit:5])]`. `5.0` passes. | Normalise both sides through a JSON round-trip, or compare numbers numerically. Print `%#v` in the message. | Unit test: int, int64, float and `json.Number` expectations all match a `5` argument. | S |
| K-07 | S2 | py `scenario.py:92-119, 31` | `run_scenario` sends a request for **every** step, system and assistant included. It appends the server reply after that step, which produces out-of-order histories. It drops assistant turns that have empty content (tool-call turns), accepts `steps=[]`, and keeps `model="gpt-4o"` when `protocol="anthropic"`. TS (`scenario.ts:72-89`) and Go (`scenario.go:113-138`) send only on user steps and always append the assistant turn. | `py_repro.py` K2: two user turns produced **3** requests, with role lists `[['system'], ['system','assistant','user'], [...]]`. Interaction count, and so `to_have_tool_call_count`, `_sequence` and `latency_ms`, differ from TS and Go for the same script. The Anthropic path sends `role: system` inside `messages`. An empty scenario passes `to_have_tool_call_count(0)` vacuously. | Mirror TS and Go: send only on `role == "user"`, keep the assistant turn including its `tool_calls`, reject empty steps, and choose the default model per protocol. | Contract test with `responses`: the sequence of request bodies is asserted for a system + user + assistant + user script. | S |
| K-08 | S2 | `.github/workflows/verify.yml:62-75`; `release.yml:22,87-88`; `sdk/npx/` has no `package-lock.json` | `npm ci` in `sdk/npx` exits 1 (`EUSAGE … can only install with an existing package-lock.json`). `verify.yml` is the reusable gate that `release.yml` calls (added after v0.5.0), so **the next release cannot pass verification**. | Reproduced in a scratch copy: `npm ci` gives `EXIT=1`. | Commit a (dependency-free) `sdk/npx/package-lock.json`, or use `npm test` without `ci` for this zero-dependency package. | `release-workflow.test.sh` should assert that every `npm ci` directory has a lockfile. | XS |
| K-09 | S2 | `sdk/vitest/package.json:9-18`; `sdk/typescript/package.json:9-18`; `scripts/packed-js-consumer.test.sh:13` | Both exports maps expose only the `import` condition. Jest's default CommonJS runtime (and any `require`) gets `ERR_PACKAGE_PATH_NOT_EXPORTED`, so the advertised **Jest entry `@mockagents/vitest/jest` is unusable** without `--experimental-vm-modules`. The packed-consumer test only does an ESM `import()`. | Packed tarballs installed in a scratch consumer on Node 24: `require('@mockagents/vitest/jest')` and `require('@mockagents/sdk')` both throw `ERR_PACKAGE_PATH_NOT_EXPORTED`. README (`vitest/README.md:127-142`) shows plain Jest usage. | Ship a CJS build (`require` condition), or document ESM-only Jest setup explicitly. | Packed-consumer test that runs a real `jest` (CJS) suite importing the `/jest` entry. | M |
| K-10 | S2 | `sdk/npx/lib/binary.js:146-151` | Extraction runs `execFileSync('tar', …)`, which resolves the first `tar` on PATH. In Git Bash on Windows that is GNU tar. GNU tar parses `C:\…` as a remote host and cannot read `.zip` files, so **`npx mockagents` fails on Windows Git Bash shells**. | On this machine: `where tar` gives `Git\usr\bin\tar.exe` first. The extraction call yields `tar: Cannot connect to C: resolve failed`. | On win32, call `%SystemRoot%\System32\tar.exe` (bsdtar) by absolute path, or unzip in JS (as the Python SDK does with `zipfile`). | Windows CI job for the npx launcher running under Git Bash. | S |
| K-11 | S3 | py `server.py:52,63` (stored, never used); `:116`; `:105` | The `config_path` constructor option is silently ignored. `from_config([a/x.yaml, b/y.yaml])` serves only `a/` (all of it). Multi-document YAML, which the server loads, is rejected as `ConfigError`. | Code-verified: `start()` (`:143-149`) never passes `config_path`. `yaml.safe_load` raises on `---` multi-docs. | Remove the option or pass it as `--config`. Use `safe_load_all`, and reject multi-directory inputs or copy them into a temp dir. | Unit tests for each case. | S |
| K-12 | S3 | Go `streaming.go:212-226`; TS `client.ts:417-423` | SSE splitter edge cases. Go prefers `\n\n` over an **earlier** `\r\n\r\n`, which merges frames when line endings are mixed. TS misses the `\n\r\n` blank line and CR-only endings. In both cases the merged frame fails JSON parsing and both events are silently dropped. | TS: `findFrameBoundary("data: a\n\r\ndata: b\n\n")` returns `{end:17}`, i.e. one frame for both events. Low likelihood with this server, plausible behind proxies. | Normalise CRLF and CR to LF before splitting (spec-compliant line parsing). | Table test: LF, CRLF, CR, mixed, and a boundary split across chunks. | S |
| K-13 | S3 | Go `inprocess.go:37-39, 91-100` | Doc claims "same … management-API surface", but only 4 routes are mounted. `ListAgents`, `GetAgent` and `ReloadAgent` return 404. There is no Gemini or Responses support, and the global chaos policy from `start.go:220` is not applied, so in-process and subprocess behaviour diverge. | Code-verified. | Mount through `server.New` with an in-memory config, or correct the doc. | `NewInProcessClient(...).ListAgents()` returns the loaded agents. | M |
| K-14 | S3 | py `client.py:403-406, 494-497`; TS `client.ts:134-135`; Go `streaming.go:113-116` | Injected stream faults (the `truncateAfter` and `malformed` frames from `streaming/pacing.go:166-175`) look like normal completions in every SDK: no error, `finish_reason` is `""`, and nothing is counted. | `py_repro.py` K6 and the TS repro: a truncated plus malformed stream returns `'Hel'` with no exception. | Expose `StreamResult.truncated` and `malformedFrames`, or raise a `StreamError` when the stream ends without `[DONE]` or `message_stop`. | Fault-pacing agent leads to the flag being set in all SDKs. | S |
| K-15 | S3 | py `mcp.py:277-288` with `:233-234`; Go `mcp.go:263-265` | If a Python MCP handler returns `None`, the handler runs and then `send_response` raises `ValueError`. **No reply is posted**, so the server-side `SendRequest` blocks until its timeout. Go coerces `nil` to `{}`. | Code-verified. | Coerce `None` to `{}` as Go does. | Unit test: a handler returning `None` posts `result:{}`. | XS |
| K-16 | S3 | TS `client.ts:114-199, 255-290` | Streaming has no caller cancellation (no `AbortSignal` option) and no idle timeout once headers arrive. A server that stalls mid-stream hangs `for await` until the test runner's own timeout. Python has a 30 s per-read timeout and Go uses `ctx`. | Code-verified (the header-only timer is cleared at `:290`). | Accept `signal` in `ChatOptions`, `MessageOptions` and `iterStream`, plus an optional `idleTimeoutMs`. | Fake fetch whose body never enqueues: the stream rejects after the idle timeout. | S |
| K-17 | S3 | GUI `lib/yamlPath.ts:203-214, 262-276` | `decodeScalar` claims to strip trailing comments but does not. The guided form shows `gpt-4o  # prod model` as the model, and editing the field writes `model: "gpt-4o  # prod model" # prod model`. This is distinct from the prior bullet, which covered a comment on a *mapping* key. | `zz_repro.test.ts`: `READ spec.model -> "gpt-4o  # prod model"`. A write-back corrupts the value. The mandatory diff preview mitigates. | Strip `\s+#.*$` outside quotes in `decodeScalar`. | Add the case to `yamlPath.test.ts`. | XS |
| K-18 | S3 | GUI `app/login/page.tsx:42-46`; `account/page.tsx`; `admin/tenants/*` | An arbitrary `?error=` text is rendered verbatim inside a "Login failed." banner. React escapes it, but a crafted link can show phishing copy ("key expired, paste it at …") under the product's chrome. | Code-verified. | Map error codes to fixed messages, or pass errors through the flash store (`lib/flash.ts`) as already done for secrets. | Component test: an unknown `error` value renders a generic message. | S |
| K-19 | S3 | TS `server.ts:43`; Go `server.go:63,196`; vs server `DefaultHost = "127.0.0.1"` (`internal/server/server.go:30`) | TS and Go server helpers build `http://localhost:<port>`. Python deliberately uses `127.0.0.1` (`server.py:245-253`), and the GUI README (`gui/README.md:204-206`) documents the failure. Node 18/19 (still within `engines >=18`) have no `autoSelectFamily`, so the health check never reaches an IPv4-only server where `localhost` resolves to `::1` first. | PLAUSIBLE for Node 18/19. On Node 24, `fetch` falls back successfully (tested here). | Use `127.0.0.1` everywhere, as Python does. | Server URL unit test. | XS |
| K-20 | S3 | `sdk/*/package.json`, `sdk/python/pyproject.toml`, the built wheel | Packaging metadata gaps. **No LICENSE file** in any npm tarball or in the wheel (no `License-File`), although Apache-2.0 requires distributing it. TS and vitest have no `repository`, `homepage` or `bugs`. Python has no `[project.urls]` and no `py.typed`, despite full type hints. `sdk/python/.coverage` is committed. `*.d.ts.map` point to an unshipped `src/`. | Inspected the `npm pack` listing and wheel METADATA. | Add LICENSE copies (or `files` entries), URLs, and `py.typed` with `Typing :: Typed`. Delete `.coverage` and add it to `.gitignore`. Ship `src` or disable `declarationMap`. | `twine check` / `npm pack --dry-run` assertions in `verify.yml`. | XS |
| K-21 | S3 | GUI `package.json:13` | `"lint": "next lint"` was removed in Next 16: it prints `Invalid project directory … gui\lint`. ESLint is not a dependency, so react-hooks rules (several `eslint-disable` comments) are unenforced. | Ran in a scratch copy. | Install `eslint` with `eslint-config-next` flat config, run `eslint .`, and add it to the GUI CI job. | CI job. | S |
| K-22 | S3 | Python `_binary.py:146` (`MOCKAGENTS_BINARY` or `MOCKAGENTS_BIN`); npx `binary.js:59` (`MOCKAGENTS_BINARY` only); TS `server.ts:213` and Go `server.go:208` (`MOCKAGENTS_BIN` only) | Binary discovery is inconsistent. The env-var names differ per SDK. TS and Go silently execute a `mockagents` found in cwd's ancestor directories (`server.ts:217-220`, `server.go:218-223`) before PATH, which can run a stale binary in nested checkouts. Neither consults the versioned cache that Python and npx share. | Code-verified. | Accept both names everywhere. Drop the ancestor search, or put it behind an explicit opt-in. | Unit test for the env-name matrix. | S |
| K-23 | S3 | TS `server.ts:88-104`; Go `server.go:136-141` | Startup does not detect an early child exit. A bad agents dir or port collision waits the full timeout (10 s) instead of failing fast with logs (Python checks `poll()`, `server.py:288`). With an explicit `port`, a stale server already on that port answers `/health`, so `start()` "succeeds" against the wrong process (all three SDKs; PLAUSIBLE). | Code-verified (TS rejects only on `'error'`). | Race the health poll against the `exit` event / `done` channel. Verify identity, for example a nonce passed by env and echoed in `/health`. | Fake child that exits immediately: rejects within 1 s. | S |
| K-24 | S3 | py `client.py:127,241`; TS `client.ts:85,150`; Go `client.go:120`, `streaming.go:273` | The default Anthropic model differs: Python uses `claude-sonnet-4-20250514`, TS and Go use `claude-3-5-sonnet-latest`. The same script routes to different agents per language when agents are matched by model. | Code-verified. | Make one shared constant and document it. | Parity test that reads defaults from each SDK. | XS |
| K-25 | S3 | TS `types.ts:28-34`; Go `types.go:25-30` | TS and Go `ChatMessage` cannot carry assistant `tool_calls`, and content is string-only. You can send a `tool` result but not the assistant call that precedes it, so an OpenAI tool round trip under strict-tools id validation is impossible from TS or Go (Python takes raw dicts). | Code-verified. | Add `tool_calls?` and `content: string \| Part[]`, mirroring the provider wire types. | Round-trip test against an in-process strict-tools agent. | M |
| K-26 | S3 | npx `bin/mockagents.js:198` | `spawnSync` blocks Node's event loop, so a `SIGTERM` to the npx process (process supervisor, CI step cancel) kills Node but not the Go child. The orphaned server keeps holding the port. | PLAUSIBLE (standard Node semantics, not run). | Use `spawn` and forward `SIGINT`, `SIGTERM` and `SIGHUP`; mirror the exit code. | Linux test: send SIGTERM to the launcher and assert the child is gone. | S |

### Cross-reference: the prior SDK bullet, re-evaluated for impact

Python `chat(stream=True)` / `message(stream=True)` still skip `raise_for_status()` (see §4). Its impact is worse than the original Low rating. A failed streamed request (500 JSON body) returns `ChatResponse(status_code=500, content="", tool_calls=[])` (verified, `py_repro.py` K3). Negative trajectory assertions such as `to_have_tool_call_count(0)` and `to_have_tool_call_sequence([])` therefore **false-pass on an error**. Treat that fix as S1.

---

## 2. SDK parity matrix

Legend: ✔ present, ✘ absent, ≠ semantic difference (see note).

### Client surface

| Capability | Python `MockAgentClient` | TS `MockAgentClient` | Go `Client` | Note |
|---|---|---|---|---|
| OpenAI chat (non-stream) | `chat()` ✔ | `chat()` ✔ | `Chat()` ✔ | |
| Anthropic messages | `message()` ✔ | `message()` ✔ | `Message()` ✔ | ≠ default model (K-24) |
| Aggregated streamed `ChatResponse` | `chat(stream=True)`, `message(stream=True)` ✔ | ✘ | ✘ | Python only. It skips `raise_for_status` (prior bullet) and has the UTF-8 bug (K-05) |
| Raw event stream | `chat_stream`, `message_stream` ✔ | `chatStream`, `messageStream` ✔ | `ChatStream`, `MessageStream` ✔ | |
| Protocol-agnostic `iter_stream` | ✔ | ✔ | ✔ (`ChunkStream`) | Same normaliser semantics, verified by reading |
| Gemini / Responses / Realtime / A2A | ✘ | ✘ | ✘ | The server supports them; the SDK clients do not |
| `health`, `list_agents`, `get_agent`, `reload_agent` | ✔ (name **not** URL-encoded, `client.py:313,320`) | ✔ (encoded) | ✔ (encoded) | ≠ |
| `run_pipeline` → `PipelineResult` | ✔ | ✔ | ✘ | Go gap |
| `RotateMyAPIKey` | ✘ | ✘ | ✔ | Go only |
| API key option | `api_key` (used only by `run_pipeline`) | `apiKey` (JSON calls only, not SSE) | ✘ | **K-01** |
| Timeout semantics | 30 s per connect/read (stream-safe) | 30 s total for JSON; headers-only for SSE, then unbounded | 30 s total for JSON; none for SSE (ctx) | ≠ (K-16) |
| Cancellation | close the generator | break the loop only (no signal) | `ctx`, `Close()` | ≠ |
| Non-2xx on JSON path | `requests.HTTPError` | `HTTPError{status, body}` | `*HTTPError{Status, Body}` | Typed in TS and Go only |
| Non-2xx on stream path | **swallowed** when `stream=True`; raised in `chat_stream` | `HTTPError` | `*HTTPError` | ≠ (prior bullet) |
| SSE `data:` without a space | dropped (requires `"data: "`) | ✔ | ✔ | ≠ |
| SSE CRLF | ✔ (line based) | ✔ (but `\n\r\n` and CR-only fail) | ✔ (mixed endings merge) | K-12 |
| Multi-line `data:` | ✘ (each line parsed separately, dropped) | ✔ joined | ✔ joined | ≠ |
| Malformed or truncated frames | silently skipped | silently skipped | silently skipped | K-14 |
| Anthropic multi-text-block join | `" "` non-stream / `""` stream | `" "` | `" "` (skips empty) | Python is inconsistent with itself |
| `ChatMessage` shape | raw dicts (anything) | string content; `tool` role only | string content only | K-25 |
| MCP bidirectional client | ✔ | ✔ | ✔ | Python `None` handler result ≠ Go (K-15) |

### Assertions / matchers

| Matcher | Python `expect()` | TS `expect()` | Go `Expect` / `ExpectScenario` | Note |
|---|---|---|---|---|
| Failure mode | raises | throws | `t.Errorf` (soft, continues) | ≠ by design |
| Response contains | **any turn** | last turn | last turn | **K-02** |
| Finish reason | **any turn** | last | last | K-02 |
| Status code | **any turn** | last | last | K-02. Non-2xx can only be asserted on the Python stream path, because JSON paths throw first |
| Tool call (name plus partial args) | shallow partial; missing key == `None` **passes**; `5 == 5.0` | deep-equal per key; missing ≠ null | `reflect.DeepEqual`; int ≠ float64 (**false fail**) | K-03, K-06 |
| Tool-call count (total) | ✔ | ✔ | ✔ | |
| Tool-call count by name | ✔ | ✔ | ✘ | Go gap |
| Tool-call sequence (exact) | ✔ | ✔ | ✔ | Prior Go gap FIXED |
| Tool error | ✔ but **never passes** against a real server | ✘ | ✘ | K-04 |
| Pipeline node sequence | ✔ | ✔ | ✘ | Go gap |
| Latency | `ValueExpect.to_be_less_than` | `toHaveLatencyLessThan` | `ToHaveLatencyLessThanMs` | |
| Generic value matchers | `to_equal`, `to_contain` (uses `str(value)`, so `expect({"status":1}).to_contain("status")` and `expect(None).to_contain("None")` pass), `to_be_greater_than` | ✘ | ✘ | Python's `to_contain` is loose |
| Empty-needle contains | passes | passes | passes | All vacuous |
| Empty scenario | allowed; count/sequence assertions pass vacuously | constructor throws | `NewScenario` panics; `ExpectScenario` fatals | ≠ (K-07) |

### Scenario runner

| Behaviour | Python | TS | Go |
|---|---|---|---|
| Sends on | **every** step | user steps | user steps |
| Appends assistant reply | only if content non-empty | always | always |
| Assistant `tool_calls` echoed into history | ✘ | ✘ | ✘ |
| Default model under anthropic | `gpt-4o` | client default | client default |

### Server / process helpers

| Behaviour | Python | TS | Go |
|---|---|---|---|
| URL host | 127.0.0.1 | localhost | localhost |
| Early-exit detection | ✔ | ✘ | ✘ |
| Log capture | 8 MiB ring | 8 MiB ring | 8 MiB ring |
| Stop | TERM, then 5 s, then KILL; raises if unreaped | TERM, then KILL, waits for exit | SIGINT (doc says SIGTERM), then KILL |
| Binary env var | `MOCKAGENTS_BINARY` or `_BIN` | `_BIN` | `_BIN` |
| Auto-download | ✔ (opt-in) | ✘ | ✘ |
| Test-framework integration | pytest plugin (env patch per test) | `@mockagents/vitest` (env patch per file; Jest entry broken from CJS, K-09) | ✘ |

---

## 3. Packaging / metadata checklist

| Item | `mockagents` (PyPI) | `@mockagents/sdk` | `@mockagents/vitest` | `mockagents` (npx) | Go SDK |
|---|---|---|---|---|---|
| Version | 0.5.0 (pyproject and `__version__` both checked by `scripts/release-preflight.sh:30-31`) | 0.5.0 | 0.5.0; peer `^0.5.0` ✔ (DS-04 fixed) | 0.5.0 | Root-module tag `v0.5.0` |
| License field | `License-Expression: Apache-2.0` ✔ | ✔ | ✔ | ✔ | repo LICENSE |
| LICENSE file shipped | ✘ (no `License-File`) | ✘ | ✘ | ✘ | n/a |
| Repository / homepage / bugs | ✘ (no `[project.urls]`) | ✘ | ✘ | ✔ repository, homepage | n/a |
| Keywords / classifiers | ✔ (no `Typing :: Typed`) | ✔ | ✔ | ✔ | — |
| Runtime floor | `requires-python >=3.10`; CI matrix 3.10 and 3.13 ✔ | `engines node >=18` (Node 18 EOL; K-19) | `>=18` | `>=18` | `go 1.26.1` forced on consumers |
| Type info | hints present, **no `py.typed`** | `.d.ts` ✔; `.d.ts.map` → unshipped `src` | ✔ | n/a (CJS JS) | n/a |
| Module format | — | ESM only; no `require`/`default` condition | ESM only; **`/jest` unusable from CJS Jest** (K-09) | CJS | — |
| Optional deps declared | extras `langchain`, `crewai` ✔ | adapters dynamically `import()` `@langchain/*` and `@ai-sdk/openai` with no `peerDependencies`/`peerDependenciesMeta` (resolution fails under Yarn PnP; PLAUSIBLE) | `vitest` optional peer ✔ | none | n/a |
| Lockfile | n/a | ✔ | ✔ | **✘, breaks `npm ci` in verify/release** (K-08) | go.sum (root) |
| README accuracy | Now present (DS-12 fixed). Accurate on lifecycle. Site docs advertise `to_have_tool_error` (K-04) | Accurate, except the Jest path in the sibling package | Jest section misleading (K-09) | ✔ | ✔. `inprocess.go` doc overclaims (K-13); doc says SIGTERM (`server.go:144`), code sends SIGINT (`server.go:171`) |
| Stray files | `sdk/python/.coverage` committed | — | — | — | — |
| Dev toolchain skew | — | TS ^7, vitest ^5, @types/node ^26 | TS ^5.6, vitest ^2.1, @types/node ^22 | — | — |
| GUI (`mockagents-gui`, private 0.1.0) | — | — | — | — | No `engines` (Next 16 needs Node ≥20.9). `lint` broken (K-21). Next 16.3.4 / React 19.2 |

---

## 4. Re-verification of prior findings

### Production-readiness audit §2.4, "Ops/docs/GUI/SDK"

| Prior bullet | Status | Current evidence |
|---|---|---|
| CI `cancel-in-progress` cancels `main` | **FIXED** | `ci.yml:19`, PR-only |
| Image-size gate only warns; Helm not linted | **FIXED** | `ci.yml:380-383` (`exit 1`); `ci.yml:411-418` (helm lint and template) |
| `alpine:3.19` EOL; UID not pinned vs chart 100; no digest pins | **STILL-PRESENT** | `Dockerfile:20,24` (`adduser -S` with no `-u`); `values.yaml:46` |
| docker-compose at `debug`, no hardening | **STILL-PRESENT** | `docker-compose.yml:12` |
| SA token auto-mounted; NetworkPolicy off | **PARTIAL** | `deployment.yaml:59` `automountServiceAccountToken: false` is fixed; `values.yaml:249` still has `networkPolicy.enabled: false` |
| GoReleaser `go mod tidy` before-hook | **STILL-PRESENT** | `.goreleaser.yml:6-7` |
| Composite actions default to `@latest` | **STILL-PRESENT** | `deploy/actions/mockagents-test/action.yml:20,88` |
| OIDC vars undocumented; BOOTSTRAP_KEY / REALTIME_GA_DEFAULTS documented but not read; "API keys only" | **PARTIAL** | OIDC is documented in `site/docs/reference/configuration.md`. `MOCKAGENTS_BOOTSTRAP_KEY` is now exercised (`cmd/mockagents/env_test.go:196,243`). `docs/guides/multi-tenant.md:209` still says "API keys only". GA_DEFAULTS remains a "Later" note (`docs/design/realtime-server-vad.md:170`). Spot-checked only |
| GUI: SSE 401 probe uses HEAD on a GET-only route and opens a real subscriber | **STILL-PRESENT** | `gui/app/logs/LogsConsole.tsx:175`. Next auto-maps HEAD to the GET handler (`next/dist/server/route-modules/app-route/helpers/auto-implement-methods.js:39-44`), so each probe runs a full upstream `/api/v1/logs/stream` fetch |
| GUI: prod CSP `'unsafe-inline'`, no HSTS | **STILL-PRESENT** | `gui/next.config.ts:14-15`; the header list at `:34-45` has no `Strict-Transport-Security` |
| GUI: `saveAgentYAML` forwards raw upstream error bodies | **STILL-PRESENT (wider)** | `gui/lib/api.ts:1309`, and the same pattern at `:817`, `:1067` and `:1208`. `APIError` messages embed `body.slice(0,200)` (`api.ts:198-200`) and are placed in `?error=` redirect URLs (`app/admin/tenants/[id]/page.tsx:127,142,166,184,200,231`; `admin/tenants/page.tsx:100,115`) |
| GUI: inconsistent agent-name path encoding | **STILL-PRESENT** | `gui/app/logs/[id]/page.tsx:55` |
| GUI: editor has no navigation guard | **STILL-PRESENT (mitigated)** | No `beforeunload` anywhere in `gui/app`. The promise at `AgentEditor.tsx:7-9` stands. Export-draft (`lib/download.ts`) now provides a manual escape |
| GUI: `yamlPath.ts` misparses `spec:  # comment`, quoted keys, `1e3` / `.inf` | **STILL-PRESENT** | Reproduced. `spec:  # comment` gives "holds a scalar". `"name": a` reads as `null`. `encodeScalar` leaves `1e3`, `.inf`, `0x1F` and `1_000` unquoted (`yamlPath.ts:230-241`). A new adjacent bug is K-17 |
| SDK: Python `chat(stream=True)` skips `raise_for_status()` | **STILL-PRESENT, impact raised** | `sdk/python/mockagents/client.py:80-81, 293-294`. Verified: a 500 returns an empty `ChatResponse(status 500)`, so negative assertions false-pass (§1 cross-reference) |
| SDK: Python requires `data: ` with a space | **STILL-PRESENT** | `client.py:114, 182, 398, 491` |
| SDK: Go lacks `ToHaveToolCallSequence` | **FIXED** | `sdk/go/mockagents/expect.go:137-151` |
| SDK: Go lacks `ToHaveToolError`; TS lacks `toHaveToolError` | **STILL-PRESENT** | The Python version cannot work either (K-04), so the parity target itself is broken |
| CHANGELOG `[Unreleased]` spans 65 entries | **FIXED** | Finalised as `## [0.5.0]` (`CHANGELOG.md:13`). There is no `[Unreleased]` section for post-0.5.0 changes such as `verify.yml` |
| API spec drift | **PARTIAL** | `/api/v1/pipelines/{name}/run` is now in `docs/api-spec.yaml:1848`. `/api/v1/logs/stream` and `/v1/engines/process` are still absent. The logs `limit` maximum is 1000 in the spec (`:516,670`) vs `maxListLimit = 10000` (`internal/server/handlers.go:27`) |

### 2026-09-10 delivery-sdk review (DS-*), items in my area

| ID | Status | Evidence |
|---|---|---|
| DS-01 Go log race | **FIXED** | `sdk/go/mockagents/server.go:258-288` (a single locked `logBuffer`, bounded to 8 MiB) |
| DS-02 Python pipe drain / startup cleanup | **FIXED** | `server.py:161-172` (reader threads plus try/stop); `pytest_plugin.py:88-92` |
| DS-03 TS `proc.killed` | **FIXED** | `server.ts:117-143` (`hasExited` plus SIGKILL escalation and an exit wait) |
| DS-04 vitest peer `^0.4.0` | **FIXED** | `sdk/vitest/package.json` peer `^0.5.0`, enforced by `release-preflight.sh:41-43` |
| DS-05 release gate / JS jobs | **PARTIAL** | `verify.yml` now runs the TS, vitest and npx jobs and is called by `release.yml:22`, but the npx `npm ci` cannot succeed (K-08) |
| DS-08 unversioned cache, non-atomic extraction | **FIXED** | `_binary.py:74-78, 205-216`; `binary.js:44-48, 139-171` |
| DS-12 Python README missing | **FIXED** | `sdk/python/README.md` exists; wheel METADATA has `Description-Content-Type: text/markdown` |

---

## 5. Test coverage gaps

**SDKs**

- **No SDK test drives a real server.** That covers the wire contract, encoding and auth. Python and TS tests use mocked parsers and fake fetch. `run_scenario` has no test at all (`tests/test_scenario.py` only builds dataclasses). The vitest e2e skips when the binary is absent, and the Go in-process test omits management routes. This gap is why K-01, K-04, K-05 and K-07 were not caught.
  - Recommendation: a CI job that builds the binary once and runs a shared cross-SDK contract suite (one YAML fixture plus the same scenario and assertions in Python, TS and Go, with identical expected verdicts).
- **Assertion tests check the positive path but not false-pass edges.** Missing are: missing key vs `None`, int vs float, last turn vs any turn, empty scenario, and an error response with negative assertions.
- **No streaming tests for** multi-byte UTF-8, `data:` without a space, multi-line `data`, mixed line endings, truncated or malformed fault frames, or non-2xx on `chat(stream=True)`.
- **No multi-tenant (`apiKey`) tests on streaming paths.** The TS `client.test.ts:163-168` checks only `requestJSON`.
- **Packaging tests** only ESM-import the Jest entry (`packed-js-consumer.test.sh:13`). Missing are a CJS Jest run, a wheel `twine check` / license-file assertion, and an npx lockfile check.
- **npx tests** (5) do not cover download, redirect, Windows extraction or signal forwarding.

**GUI** (17 test files, 329 tests, for about 50 source files; plus `e2e/smoke.spec.ts`, 45 Playwright tests)

- With no component or unit tests:
  - `lib/auth.ts` (login, rotate, burn, logout server actions)
  - `lib/guard.ts` and `app/api/logs/stream/route.ts` (the cross-site guard and 401/403 pass-through)
  - `lib/flash.ts`
  - `PipelineEditor.tsx` (529 LOC), `YamlEditor.tsx` (316), `DAGViewer.tsx`, `AgentTabs.tsx`, `ReportExport.tsx`, `CopyField.tsx`
  - every server-component page apart from what e2e touches
- `yamlPath.test.ts` lacks the comment, quoted-key and numeric-literal cases (prior bullet and K-17).
- There is no lint gate (K-21). The 30-odd ` as T` casts on `JSON.parse` results (for example `api.ts:204`) are unchecked at runtime. No `any`, `@ts-ignore` or non-null `!` was found, which is good.

**Test quality.** Most SDK tests assert behaviour, not just shape: lifecycle tests use real child processes, and normaliser tests check emitted chunks. The client tests call private `_parse_*` helpers directly, so request construction (headers, auth, URL encoding) is unverified in Python.

---

## 6. Design observations

1. **The SDKs are thin, divergent re-implementations with no shared contract.** Each SDK re-derives SSE parsing, argument decoding and assertion semantics by hand, and the code comments repeatedly cite past divergence (audit M-38, three times). A single machine-readable "assertion semantics" spec, plus a shared golden fixture set run by all three SDKs and the YAML runner, would stop K-02, K-03, K-06 and K-07 from recurring.
2. **Fault-injection features are invisible to the SDK layer.** Chaos (429/500), stream truncation, malformed frames and `raw_arguments` exist to test client resilience. The SDKs either throw before any assertion can run (non-2xx on JSON paths), so `toHaveStatusCode(429)` is unreachable, or silently normalise the fault away (K-14, K-03). Consider a `ChatResponse.error` / `StreamResult.fault` model rather than exceptions, so tests can assert on faults fluently.
3. **Auth is bolted on per call instead of per client** (K-01). Combined with anonymous access to LLM endpoints in multi-tenant mode and the single-agent fallback, any missed header turns into silent wrong-agent routing rather than a 401. The server could treat "a credential was presented but did not resolve" as fail-closed, and the SDKs should send the credential on every request.
4. **The Go SDK lives inside the server module.** `go get .../sdk/go/mockagents` pulls the whole server dependency graph (SQLite, pgx, OIDC, OTel) and forces `go 1.26.1` on consumers. A separate `sdk/go/go.mod`, with the in-process client in a sub-package, would decouple versioning and the dependency footprint.
5. **TS and vitest are ESM-only, while Jest is advertised** (K-09). Either commit to ESM (and document Jest ESM mode) or dual-publish.
6. **GUI security posture is generally good.**
   - The API key lives in an HttpOnly, SameSite=Strict cookie, Secure in production.
   - Server actions rely on Next's Origin check.
   - The SSE proxy has a `Sec-Fetch-Site` guard and passes 401/403 through generically.
   - `safeRedirect` uses URL parsing.
   - Secrets go through the in-memory flash store instead of URLs.
   - The HTML report export escapes everything (`lib/report.ts:402-409`).
   - There are no `dangerouslySetInnerHTML` sinks.

   Remaining hardening items:
   - nonce-based CSP and HSTS (prior)
   - generic error messages, not upstream bodies in URLs (prior, wider)
   - `?error=` content spoofing (K-18)
   - The internal `MOCKAGENTS_API_URL` is rendered in the shell for every visitor, including the unauthenticated `/login` page (`app/InstrumentStrip.tsx:64`, `app/Shell.tsx:157`). This is a minor information disclosure in proxied deployments.
7. **`lib/auth.ts` is a `"use server"` module.** Every export becomes a client-callable action, including `getAuthStatus`, which returns the caller's own key prefix and role. That is harmless today, but it is a footgun: keep read-only helpers in a non-action module, as `lib/flash.ts` already does deliberately.
8. **Minor maintainability items.**
   - `client.py:439` computes an unused `total_latency`.
   - `next.config.ts` has an empty `experimental: {}` under a comment claiming it disables static optimisation.
   - `LEGACY_ROLE_COOKIE` cleanup is pending (`auth.ts:43-47`).
   - `requireModule` (TS adapters) maps *any* import failure to "please install", which hides genuine module errors (the cause is preserved).
