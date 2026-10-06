# Review 03 — Protocols and storage (MCP, mcpadmin, A2A, Realtime, recording, storage, runner, contract, drift)

Target: worktree `mock-agents-qloop` at `origin/main 9fa4db7`. Read-only review. Every "VERIFIED" item was reproduced with a throwaway test injected through `go test -overlay`, so no repo files were touched. The repro sources are in `scratchpad/review/repro-protocols/*_repro_test.go`, with `overlay*.json` beside them.

Baseline for prior work: `docs/reviews/2026-09-03-production-readiness-audit.md`. Mediums are counted as fixed except where this review found otherwise (see P-05, P-07 and P-16).

Severity key: S1 = crash, security, data loss or false pass. S2 = significant incorrectness or spec violation. S3 = minor, maintainability or missing test.

---

## 1. New findings

| ID | Sev | File:line | Finding | Failure scenario | Recommended fix | Recommended test | Effort |
|---|---|---|---|---|---|---|---|
| **P-01** | **S1** | `internal/config/loader.go:176-178`, `:313-316` (non-strict `doc.Decode` for TestSuite); `internal/config/testsuite_validator.go:127-129` (only present assertions are checked, so zero assertions pass); `internal/runner/runner.go:225-230` (`Passed = len(Failures)==0`); `runner.go:399-401` (`len(wantArgs)==0` matches any args); the schema `schema/mockagents-v1-testsuite.json:44,65` says `additionalProperties:false`, but nothing enforces it at runtime | **The test runner reports a false pass when a key is misspelled.** Unknown YAML keys in a TestSuite are dropped silently. The validator accepts the result and the runner reports PASS. | **VERIFIED** (`runner_repro_test.go`). A `tool_call` assertion written with `args:` instead of `arguments:` and a deliberately wrong `id: WRONG-ID` passes. A case written with `assertion:` instead of `assertions:` has zero assertions and passes against `value: THIS-TEXT-IS-NOT-IN-THE-RESPONSE`. Run through the real `config.LoadAllDocuments` → `ValidateTestSuite` → `RunSuite` path: `passed=2 failed=0`. This is the same path `mockagents test` uses (`cmd/mockagents/test.go:135-150`). A CI gate goes green while asserting nothing. | (a) Decode TestSuite documents strictly. Re-encode the node and use `yaml.NewDecoder(...).KnownFields(true)`, or walk the `yaml.Node` against the struct tags, and report a `ValidationError` with a line number. (b) Reject a case with zero assertions, or require an explicit opt-in such as `smoke: true`. (c) Add a schema-parity test that runs `schema/mockagents-v1-testsuite.json` over the same fixtures as the Go validator. | Table test of misspelled keys (`args`, `assertion`, `asserts`, `max-ms`, `node`), each expecting a validation error, plus a zero-assertion case expecting rejection. Keep the repro as a regression test. | M |
| P-02 | S2 | `internal/recording/import_vcr.go:165-188` (gunzips `base64_string` bodies), `:249` + `flattenHeaders` `:277` (keeps every non-secret response header); `internal/recording/replay.go:160-162` (copies stored headers verbatim) | **Imported vcrpy cassettes with gzip bodies do not replay.** The importer decompresses the body but keeps `Content-Encoding: gzip` and the compressed `Content-Length`. | **VERIFIED** (`recording_vcr_repro_test.go`). Replay announces `CE=gzip CL=118` and then writes the 126-byte plain JSON. Both the raw client and Go's default client get `unexpected EOF` with a 0-byte body. vcrpy stores compressed bodies with their `Content-Encoding` by default (`decode_compressed_response=False`), so this hits common OpenAI cassettes. | When the importer decodes a body, drop `Content-Encoding`, `Content-Length`, `Transfer-Encoding` and the other hop-by-hop headers. Replay should never copy `Content-Length` or `Content-Encoding` from stored headers; let `net/http` frame the body it actually writes. | Import a gzip vcr cassette, replay it through `httptest.NewServer`, and assert the body equals the plain JSON. | S |
| P-03 | S2 | `internal/recording/proxy.go:162` (forwards the client's `Accept-Encoding`, which turns off the Transport's transparent decompression), `:189` (reads the raw compressed bytes), `:19-26` (`Content-Encoding` is not in `DefaultCaptureHeaders`); `replay.go:171`; `redact.go:68-69` (cannot see secrets inside base64 gzip) | **Recording through the proxy stores compressed bodies, and replay serves them as plain JSON.** | **VERIFIED** against a gzip-honouring upstream (`recording_repro_test.go`): the stored encoding is `base64`, the captured headers are `{Content-Type}`, and replay returns bytes starting `1f8b0800` with no `Content-Encoding`. httpx (openai-python) and undici send `Accept-Encoding: gzip` by default. Whether the real providers then compress is PLAUSIBLE, since most CDN fronts do. Every replay of such a recording is undecodable, and `--redact` silently fails to mask secrets inside it. | Strip `Accept-Encoding` before forwarding so the Transport adds its own and transparently decompresses. Alternatively, decode `Content-Encoding` before `EncodeBody`. Record decoded bodies only and never persist `Content-Encoding`. | Proxy test with a client sending `Accept-Encoding: gzip`: assert the cassette body is plain JSON and replay is byte-identical. | S |
| P-04 | S2 | `internal/recording/cassette.go:350-352` (`flushed` advances **before** the write), `:361-369` (on a write error, `rewrite` is not set) | **A failed cassette flush loses interactions for good, and a short write can corrupt the cassette.** | **VERIFIED** (`TestRepro_FailedFlushLosesInteraction`): first append fails with ENOENT; once the directory exists, the second append succeeds; in-memory count is 2, on-disk count is 1. A short write (for example ENOSPC) leaves a torn line. The next O_APPEND write concatenates onto it, so the corrupt line is no longer the last one. `Load` then fails the whole cassette (`cassette.go:215-216`): one transient disk error costs the entire recording. | Advance `flushed` only after a successful write. On any open or write error, set `c.rewrite = true` so the next flush rebuilds atomically from memory. Return a typed error so the proxy surfaces it in `X-Mockagents-Record-Error`, which it already does. | Inject a failing path, then a recovering one, and assert disk == memory. Fault-inject a short write through an `os.File` seam and assert `Load` still succeeds. | S |
| P-05 | S2 | `internal/mcp/bidirectional.go:77-80` | **Audit M-27 is still present.** The audit's Medium tier is described as fixed, but no commit touches this. A stealing `Subscribe` closes the old channel without draining its buffered messages back into `outbound`. | **VERIFIED** (`TestRepro_M27StealDropsBuffered`). A notification buffered for a dead stream never reaches the new subscriber. For a buffered `sampling/createMessage`, the admin trigger blocks for up to 60 s and then returns 504. | Do the audit's own fix: in the steal branch, drain the old channel and prepend the drained messages to `b.outbound` before replaying. Better still, use the redesign in §4.3. | Steal with buffered messages; assert the new subscriber receives them in order. | S |
| P-06 | S2 | `internal/mcp/bidirectional.go:85-97` (on a partial replay the tail stays in `outbound`), `:125-133` (when the channel overflows, `enqueue` buffers into `outbound` while a subscriber is attached); nothing drains `outbound` until the next `Subscribe`; `:216-226` (requests that time out stay queued) | **Messages get stranded behind a live subscriber and are delivered out of order.** | **VERIFIED** (`TestRepro_BidirectionalStrandedMessages`, buffer 2): 4 queued notifications, then a fifth emitted. Delivered order is `[m1 m2 m5]`; m3 and m4 stay stranded until the client reconnects. A burst of more than 16 messages (the default buffer) strands server-initiated `sampling`/`roots` requests until timeout. Requests that already timed out are later delivered to a new subscriber, whose replies get 404 from `/mcp/response`. | Use one ordered queue owned by a pump (§4.3). When `outbound` is non-empty, new messages must append behind it, never jump ahead. Remove a request from `outbound` when its `SendRequest` times out. | Burst more messages than the buffer while subscribed; assert all arrive in FIFO order. Assert a timed-out request is never delivered. | M |
| P-07 | S2 | `internal/realtime/session.go:155,163,169` (fields `history`, `itemOrder`, `items`), `:632-693` (`conversation.item.create` appends without any cap and accepts client ids of any length), `:1018-1024`; `internal/adapter/realtime.go:30` (16 MiB frames), `:409` (`expires_at` is reported but never enforced) | **Audit M-32 is still present.** Per-connection realtime memory is unbounded, and the session never expires. | **VERIFIED** (`TestRepro_M32UnboundedSessionMemory`): 20,000 `item.create` events give `items=history=itemOrder=20000`, about 166 MiB retained. Each 16 MiB frame of text is stored twice, in history and in items. One socket can exhaust the process, and the advertised 1 h expiry never closes it. | Cap items, history and itemOrder (count plus bytes). Past the cap, either drop the oldest items or reject with a GA-shaped error. Bound client item id length (GA uses 32 characters). Enforce `expires_at` by closing with a session-expired error. | Item-create flood, then assert caps hold. Use a fake clock past expiry and assert the close. | M |
| P-08 | S2 | `internal/realtime/vad.go:196-203` (always 48 bytes/ms, PCM16 at 24 kHz), `session.go:482-489`, `:517-519` (100 ms floor), `:529` (`*48` slice offset); `audio.input.format` is accepted at `session.go:1804,1818` but never consulted | **G.711 (`audio/pcmu`, `audio/pcma`, beta `g711_ulaw`/`g711_alaw`) is accepted but every duration is computed as PCM16.** | **VERIFIED** (`TestRepro_G711DurationMiscomputed`): a valid 500 ms μ-law commit is rejected with "buffer only has 83.33ms". VAD energy also reads μ-law bytes as int16 samples, which gives garbage turn detection, and transcription usage seconds are 6× too small. Telephony integrations such as Twilio Media Streams use exactly this format. | Add `bytesPerMs(format)` (pcm16 = 48, pcmu/pcma = 8) and use it for duration, the floor and the window slicing. Decode μ-law/A-law samples for the energy calculation. | Commit floor per format; VAD speech detection on μ-law speech and silence fixtures. | S |
| P-09 | S2 | `internal/a2a/server.go:479-483` (deletes the expiry for non-terminal states), `:487-497` (prune only walks `taskExpiry`), `:473-475` (cap rejects every new task) | **Non-terminal A2A tasks (`input-required`, `working`, `auth-required`, …) never expire, so the server ends up permanently at capacity.** | **VERIFIED** (`TestRepro_A2ANonTerminalTasksExhaustCapacity`, MaxTasks=3, clock advanced 1 h per call): the fourth `message/send` returns `-32603 mock task capacity exceeded`. With defaults, 10,000 abandoned multi-turn conversations (a long CI run or a load test) disable the mock until it restarts. | Track last-touched time for every task. Expire idle non-terminal tasks after the TTL, or evict the least recently used non-terminal task when at the cap. | Clock-driven test: abandoned `input-required` tasks are reclaimed and new sends succeed. | S |
| P-10 | S2 | `internal/mcpadmin/manager.go:292-299` (`resolveTarget` returns the existing source path), `:283` (writes canonical **YAML**); the same pattern is in `internal/server/agent_write_handlers.go:337-350`; `internal/config/loader.go:488-490` (`isJSON` decides by extension) | **`put_agent` (MCP) or `PUT /api/v1/agents/{name}` on an agent loaded from `foo.json` writes YAML into `foo.json`.** | **VERIFIED** (`mcpadmin_repro_test.go`): after `put_agent`, `config.LoadFile(support.json)` fails with `invalid JSON: invalid character 'a'`. The agent then disappears or fails on the next start or `--watch` reload, even though the write API reported "updated". | If the target extension is `.json`, write canonical JSON. Otherwise, write `<name>.yaml` and remove the old source inside the same critical section. | `put` over a JSON-sourced agent, then reload from disk and compare. | S |
| P-11 | S3 | `internal/mcp/streamable.go:714-719` | **A slow GET-stream subscriber silently loses events.** When its 64-slot buffer overflows, events are dropped but the stream stays open. The comment says the client recovers through `Last-Event-ID`, but nothing forces a reconnect. | **VERIFIED** (`TestRepro_StreamSubscriberOverflowSilentlyDrops`): after 100 broadcasts the channel holds 64. The client sees a gap it cannot detect, because SSE has no gap detection. | On overflow, close that subscriber's channel. The handler returns, the client reconnects with `Last-Event-ID`, and the replay log already has the events. | Overflow, then assert the stream closes; reconnect and assert full replay. | S |
| P-12 | S3 | `internal/mcp/jsonrpc.go:45-47` (`id:null` treated as a notification); `server.go:216-217` (missing `method` gives -32601); `server.go:138-140` (a notification with a bad version gets a response); `streamable.go:232-244` (parse error or batch answered with HTTP 200) | **Several JSON-RPC envelope edge cases are handled incorrectly.** MCP forbids a null id, which should be -32600, but the request is dropped silently. `{"id":1}` with no method should be -32600 (Invalid Request), not -32601. An invalid notification should get no reply. On Streamable HTTP, the official SDKs answer malformed input with HTTP 400 plus the JSON-RPC error; the mock answers 200. Ids that are objects, arrays or booleans are echoed instead of rejected. | **VERIFIED** (`TestRepro_NullIDTreatedAsNotification`, `TestRepro_StreamableParseErrorStatus`). Mostly a fidelity problem: a client's error-path tests pass against the mock but fail against real servers. | Centralise envelope validation (§4.1). Reject a null id or an id of the wrong type with -32600, treat a missing method as -32600, and return HTTP 400 on the streamable parse-error and batch paths. | Envelope table test across all transports. | S |
| P-13 | S3 | `internal/mcp/server.go:601-613` | **`resources/subscribe` accepts URIs that were never declared** into a process-global map with no bound (relates to L-40). | **VERIFIED**: 5,000 distinct undeclared URIs gives a map of 5,000. The total is limited only by the body cap times the number of requests. | Return `-32002` for an unknown URI, as `resources/read` already does, and cap the set. | Subscribe to an unknown URI and expect -32002. | S |
| P-14 | S3 | `internal/storage/sqlite.go:456-488` (prefixes `sk-` and `key-` are matched as plain substrings); used by `recording/redact.go:125,211` | **`SanitizeBody` over-redacts ordinary words.** | **VERIFIED**: `a risk-based task-list for the turkey-dinner desk-lamp` becomes `a risk-*** task-*** for the turkey-*** desk-***`. Rows logged with `MOCKAGENTS_LOG_BODIES=sanitized` are corrupted, and `record --redact` cassettes replay altered assistant content. The hash still matches, because it is computed before redaction, so the corruption is silent. | Require a word boundary before the prefix and a credential-shaped tail, for example `(^|[^A-Za-z0-9])sk-[A-Za-z0-9_-]{16,}`. | Negative corpus of English hyphenated words; positive corpus of real key shapes. | S |
| P-15 | S3 | `internal/recording/cassette.go:412-429` together with `proxy.go:202,214` and `replay.go:131` (path only) | **The request hash ignores the query string.** Gemini `:streamGenerateContent?alt=sse` (SSE) and the same path without `alt` (JSON array) collide; so do Anthropic `?beta=true` variants. | **VERIFIED**: a non-SSE caller receives the recorded SSE interaction (`hit-streaming`, `text/event-stream`). | Hash a canonicalised query (sorted keys, credential parameters such as `key` excluded). Keep path-only matching as a legacy fallback for old cassettes. | Two recordings that differ only by `alt=sse`; assert each caller gets its own. | S |
| P-16 | S3 | `internal/mcp/streamable.go:537-541` | **The fix for audit M-30 is incomplete.** Idle sessions are now swept first, but FIFO eviction still drops live sessions: 257 anonymous `initialize` calls inside the TTL evict the oldest active session, whose client then gets 404 mid-conversation. | By reading. Easy to reproduce with `newSessionManager(1)`. | When at the cap with no idle session to reclaim, refuse new sessions (503 with `Retry-After`) instead of evicting live ones, or cap sessions per remote address. | 257-initialize burst; assert the original session survives. | S |
| P-17 | S3 | `internal/a2a/server.go:369-372` vs `:459` | **`message/stream` reports a different message id than the stored history.** When the client omits `messageId`, `StreamResults` mints a second id, so the history of the streamed "working" Task does not match `tasks/get`. | By reading. | Reuse the id stored on the task's last user message. | Stream without `messageId`, then compare against `tasks/get` history. | S |
| P-18 | S3 | `internal/contract/contract.go:130-155` (`Name` is never compared); `cmd/mockagents/contract.go:66-71` (lenient JSON decode) | **The contract gate can pass when comparing the wrong files.** Pointing `contract diff` at agent A's baseline and agent B's YAML reports "No changes" if their shapes match. A baseline JSON with a misspelled `tool` instead of `tools` decodes as zero tools, so every current tool shows as additive and the gate passes. | By reading. | Diff `Name` as a breaking change. Decode contract JSON with `DisallowUnknownFields`. | Different names are breaking; an unknown key is rejected. | S |
| P-19 | S3 | `internal/storage/sqlite.go:253-260` (lexical `timestamp >= ?`), the default at `:28` (`datetime('now')` gives `YYYY-MM-DD HH:MM:SS`), callers `server/log_handlers.go:458` (RFC3339) and `server/pipeline_recorder.go:55` (RFC3339Nano) | **Since/Until filters can misorder rows at boundaries.** One column holds three timestamp formats and the filters compare them lexically: `…T10:00:00.5Z` sorts before `…T10:00:00Z`, so pipeline rows at sub-second boundaries are misfiltered. This is the same class as L-22. | PLAUSIBLE: boundary rows only. | Write one fixed-width UTC layout everywhere, normalise `Since`/`Until`, and drop the SQL default or make it match. | Mixed-precision rows plus boundary filters. | S |
| P-20 | S3 | `internal/runner/runner.go:405` (`tool_call` uses strict `reflect.DeepEqual`) vs `:428` (`tool_call_args` uses `looseEqual`) | **The two argument assertions compare numbers differently.** `arguments: {qty: 2.0}` fails against an agent's `qty: 2` in `tool_call` but passes in `tool_call_args`. This produces false failures, not false passes. | By reading. | Use `looseEqual` in `hasToolCall`. | int vs float argument equality for both assertion types. | S |
| P-21 | S3 | Missing tests on risky branches (see §3) | The cassette flush error path, `StreamableNotifyHandler.ServeHTTP` (47.1 %), replay `serveStreaming` cancel (68 %), the stdio chaos-plus-EOF branch, `contract.Validate` (0 % in-package) and drift `FilterFindings`/`IgnoreEnumPaths`/`MergeFindings` (0 %) have no tests. | Regressions like P-04 go unnoticed. | Add targeted tests. | — | S |

Areas reviewed with no new defect found:
- `internal/drift`: the comparison is fail-closed, exceptions are strict and exact-match, and the baseline is decoded strictly.
- `contract.schemaConstraintsTightened` is conservative and correct apart from P-18.
- `recording.Load` torn-tail handling.
- Redaction SSE framing.
- MCP stdio frame cap.
- Realtime WebSocket reader/writer goroutine lifecycle (`adapter/realtime.go:442-507`): no leak; `readErr` is buffered and `done` is closed.

---

## 2. LOW-tier re-verification (audit 2026-09-03)

| ID | Status | Current location | Evidence |
|---|---|---|---|
| L-32 (out of area, quick check) | STILL-PRESENT | `internal/audit/recorder.go:40` | `_ = r.Store.Append(...)`: the error is still discarded without logging. |
| L-34 | STILL-PRESENT (latent) | `internal/storage/sqlite.go:198-210`; schema default `:28` | `entry.Timestamp` is inserted verbatim, so a blank value bypasses `DEFAULT (datetime('now'))`. Current callers do set it (`log_handlers.go:458`, `pipeline_recorder.go:55`). See also P-19 for the format mix. |
| L-36 | STILL-PRESENT | `internal/a2a/server.go:802-811` | `X-Forwarded-Proto` and `r.Host` are copied verbatim into the card `url`. |
| L-37 | FIXED | `internal/a2a/server.go:402-411`, `:433` | `tasks/get` snapshots with `cloneTask` under `s.mu`; cancel returns a clone. |
| L-38 | STILL-PRESENT | `internal/mcp/server.go:407`; `internal/mcp/stdio.go:80` | Handlers still run on `context.Background()`. There is no `recover` anywhere in `internal/mcp`, so a panicking `ToolHandler` kills a stdio session. |
| L-39 | STILL-PRESENT | `internal/a2a/server.go:255-276`, `:243-247` | **VERIFIED**: an id-less `message/send` returns a full Task result with `"id":null`. A batch returns -32700 instead of -32600. A2A chaos also answers notifications (`applyChaos`). |
| L-40 | STILL-PRESENT | `internal/mcp/server.go:28-32` | `inited`, `logLvl`, `pending` and `subscribed` are process-global. `inited` is written at `:180` and never read (dead state). `pending` is drained by **any** streamable session's SSE POST (`streamable.go:296`), which leaks notifications across sessions. See P-13. |
| L-41 | STILL-PRESENT | `internal/mcpadmin/manager.go:225-245` | **VERIFIED**: `create_agent` with an unknown `behaviour_typo:` key succeeds ("created") and the field disappears. The HTTP write API is strict (`server/agent_strict_fields.go:54`), so the two paths disagree. |
| L-42 (out of area, quick check) | STILL-PRESENT for the named-directory case | `internal/cli/scaffold.go:114-119` | In-place init is skipped. `init --force <dir>` still runs `RemoveAll` on `<dir>/agents` and `<dir>/tests`. |
| L-43 | PARTIAL | fixed: `cmd/mockagents/record.go:91-93`, `replay.go:152-154`; open: `cmd/mockagents/mcp.go:176-180`, `cmd/mockagents/a2a.go:108` | record and replay now set `ReadTimeout` and `IdleTimeout`. The standalone MCP and A2A servers still set only `ReadHeaderTimeout`. |
| L-44 | STILL-PRESENT | `internal/realtime/session.go:482` (`audioEnergy` decodes) and `:485` (decodes again); `vad.go:198` | Each append is still base64-decoded twice. |

**Mediums the audit lists as fixed that are not:**
- **M-27** (P-05): unchanged, and no commit references it.
- **M-32** (P-07): no caps exist.
- **M-30**: partially fixed (P-16).

---

## 3. Coverage

Collected with `go test -coverprofile` across the nine packages; all pass. The full per-function list is in `scratchpad/review/repro-protocols/cover-func.txt`.

| Package | Statement coverage |
|---|---|
| internal/mcp | 86.9 % |
| internal/mcpadmin | 74.7 % |
| internal/a2a | 91.2 % |
| internal/realtime | 92.9 % |
| internal/recording | 89.8 % |
| internal/storage | 84.8 % |
| internal/runner | 85.1 % |
| internal/contract | 81.1 % |
| internal/drift | 84.0 % |

Low or zero coverage on risky code:
- **mcp:**
  - `StreamableNotifyHandler.ServeHTTP` 47.1 % (origin and method rejection untested)
  - `handleGet` 74.0 % (resume and 409 paths partly covered)
  - `sse.go` `ServeHTTP` 72.7 %, `writeSSEMessage` 61.5 %
  - `ServeStdioWithFaults` 75.6 % (chaos-plus-EOF branch)
  - `Sample` and `ListRoots` 0 %
  - `handlePromptsList` 33.3 %
- **mcpadmin:**
  - `sanitizeFilenamePart` 0 % (tenant path-safety helper)
  - `definitionBytes` 50 % (object-form definition)
  - `atomicWriteFile` 66.7 %
  - `agentFilePath` 69.2 % (the escape guard is never exercised)
  - `handleDelete` 71.4 %
- **recording:**
  - `writeCassette` 62.5 % (rewrite error paths)
  - `Replay.serveStreaming` 68 % (ctx-cancel during preserved delays)
  - `flush` 85.7 %, but the open/write **error path** that carries P-04 is untested
  - `redactJSONValue` 66.7 %
- **runner:**
  - `runCase` 70.2 % (pipeline-not-found, engine-error mid-turn)
  - `hasToolCall` 71.4 %
  - `shortFailureMessage` 40 %
- **a2a:**
  - `RPCHandler` 72.7 % (body-read error and 204 notification paths)
  - `serveStream` 77.3 %
  - `requestBaseURL` 66.7 %
- **storage:**
  - `NewSQLiteStore` 61.5 %, `migrate` 69.4 % (legacy-schema migration branches)
  - `Ping` 0 %
- **contract:**
  - `Validate` 0 % in-package (covered only from `cmd`)
  - `schemaNumber` 25 %
- **drift:**
  - `FilterFindings`, `IgnoreEnumPaths` and `MergeFindings` 0 %
  - `ensureJSONEOF` 50 %
- **realtime:**
  - `storedItemToMessage` 50 %
  - `parseItem` 60 %

---

## 4. Design observations and refactor proposals

### 4.1 One JSON-RPC 2.0 core for MCP and A2A
Two hand-written envelopes have already drifted apart. MCP fixed the notification and batch rules in round 10 (R10-8, R10-12); A2A still has L-39; both have gaps such as P-12. Proposal: create a package `internal/jsonrpc`.
```go
type Message struct{ JSONRPC string; ID json.RawMessage; Method string; Params, Result, Error json.RawMessage }
func Decode(body []byte) (Message, *Error)          // parse(-32700) / batch(-32600) / id type+null(-32600) / method(-32600)
func (m Message) Kind() Kind                         // Request | Notification | Response
type Handler func(ctx context.Context, m Message) (result any, err *Error)
func Serve(ctx, body, h) (out []byte, httpStatus int) // never answers a Notification; 400 on envelope errors
```
- MCP `Server.Handle` and A2A `dispatch` become method tables behind `Serve`.
- Panic recovery (L-38) and `ctx` propagation live in `Serve`.
- One table test covers every transport.

### 4.2 Per-session MCP state (L-40, P-13)
- Add `type session struct{ logLevel string; subscribed boundedSet; pending ring; initialized bool }`.
- Change `Server.Handle(ctx, *session, *Request)`.
- Streamable `streamSession` embeds one. stdio and the legacy POST transport use `Server.defaultSession`.
- `EmitNotification(sessionID|all)` routes explicitly, so notifications stop leaking between sessions.

### 4.3 Ordered outbound queue with a single pump (P-05, P-06, P-11)
Replace "channel plus overflow slice" with one slice per subscriber target, protected by a mutex, plus a `notify chan struct{}` of capacity 1.
- The SSE writer pops from the head in order.
- `Subscribe` (steal) transfers ownership of the slice. Nothing is ever split between two containers, so loss and reordering are impossible by construction.
- `SendRequest` removes its message on timeout.
- The streamable GET stream uses the same structure with its resumable log as the backing store. A slow consumer simply lags rather than losing events.

### 4.4 Strict decoding for every authored document (P-01, L-41, P-18; audit F3)
- Add `config.DecodeStrict(node *yaml.Node, out any) error`, which marshals the node and decodes it with `KnownFields(true)`, mapping the error back to a line number. Use it for TestSuite, Agent (MCP write path), Pipeline, MCPServer and A2AServer, and for contract JSON (`DisallowUnknownFields`).
- Add a parity test that validates `examples/` plus negative fixtures against both `schema/*.json` and the Go validators, and asserts they agree.
- This closes the main false-pass class for the test runner.

### 4.5 Recording normalisation layer (P-02, P-03, P-14, P-15)
- Add `recording.normalize(resp *http.Response) (headers map[string]string, body []byte)`. It decodes `Content-Encoding`, strips CE/CL/TE and hop-by-hop headers, and is used by the proxy, the VCR importer and the OpenAI importer.
- Replay never emits stored framing headers.
- `HashRequest(method, path, canonicalQuery, body)` with a v1→v2 compatibility lookup.
- Redaction then always runs on decoded plaintext with boundary-anchored patterns.

### 4.6 Bounded protocol state as a shared primitive
A2A tasks (P-09), realtime items (P-07), MCP subscriptions (P-13) and streamable sessions (P-16) each hand-roll a different, partial bound. Proposal:
- Add one small `internal/bounded` package: an LRU+TTL map with a byte budget and an eviction callback.
- Each protocol states its policy in a single line, e.g. `bounded.New(maxN, maxBytes, idleTTL, onEvict)`.
- A shared property test asserts that "never exceeds N/bytes" and "idle entries are reclaimed".

---

### Notes
- All repro tests were injected with `go test -overlay`, and nothing was written into the repository. `t.TempDir()` directories under the configured `GOTMPDIR` (`.gotmp`) are removed automatically.
- An untracked file `coverage` (990 KB) exists at the worktree root. It was created at 09:42 local time, before this reviewer's first `go` command at about 09:49, so it most likely came from a parallel reviewer. This reviewer left it untouched.
