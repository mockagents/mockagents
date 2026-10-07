# Action register: 2026-10-06 quality review

Every finding from the [review summary](2026-10-06-quality-review-summary.md), with the
iteration in the [resolution plan](2026-10-06-quality-resolution-plan.md) that closes it.
The finding text is the first sentence of the full entry; the per-area appendices under
[2026-10-06-quality/](2026-10-06-quality/) hold the failure scenario, reproduction,
recommended fix and recommended test for each ID.

Status values: **Open**, **Fixed** (with the pull request), **Maintainer** (needs repository
or registry owner access), **Deferred** (with the reason). Iteration `Maintainer` means the
action is in the plan's maintainer-only table.

## New findings

| ID | Sev | Area | Location | Finding | Iteration | Status |
|---|---|---|---|---|---|---|
| E-01 | S2 | Hot path | `adapter/responses.go:651-659` | `responsesSessionID` returns `"resp-thread-"+prev` for a chained turn and `"sess-"+random` when no header is sent. | 3 | Open |
| E-02 | S2 | Hot path | `streaming/openai.go:270-283` | Streamed tool-call arguments are cut every 20 bytes. | 3 | Open |
| E-03 | S2 | Hot path | `adapter/anthropic.go:502` | The Messages response renders `"content": null` when there is no text, tool_use or refusal block. | 3 | Open |
| E-04 | S2 | Hot path | `adapter/gemini.go:182-188` | Any `:method` other than `streamGenerateContent` is served as `generateContent`. | 2 | Open |
| E-05 | S2 | Hot path | `adapter/gemini.go:319-327` | Gemini `contents` with no `role` are valid (Google's own REST quickstart omits it), but the adapter keeps `role:""`. | 3 | Open |
| E-06 | S2 | Hot path | `adapter/batches.go:337,442-500,530-549` | The OpenAI and Anthropic batch endpoints replay up to 50,000 / 100,000 requests synchronously inside the create handler. | 2 | Open |
| E-07 | S2 | Hot path | `engine/strict.go:255` | Under strict `tool_choice` forcing, `finish_reason:"stop"` is chosen from the agent's declared `spec.protocol`, not from the wire surface. | 3 | Open |
| E-08 | S2 | Hot path | `adapter/responses_stream.go:42-52,129,200` | The stream-physics and fault model (TTFT, tokens/sec, jitter, ITL/TTFT distributions, `truncate_after_chunks`, `malformed`) is applied only to content chunks on Chat, Anthropic and Gemini. | 3 | Open |
| E-09 | S2 | Hot path | `adapter/anthropic.go:282-283 vs :292-314` | Residual of M-18. | 3 | Open |
| E-10 | S3 | Hot path | `adapter/responses.go:611-617` | Any Responses input item type not modelled here (`reasoning`, `item_reference`, `custom_tool_call_output`, `mcp_*`, …) falls to the default branch and becomes `role:"user"` with empty content. | 3 | Open |
| E-11 | S3 | Hot path | `adapter/responses.go:302-320` | `instructions` are ignored when `previous_response_id` is set (the stored prior system message is replayed instead) and on any non-empty conversation. | 3 | Open |
| E-12 | S3 | Hot path | `adapter/bedrock.go:290-303` | `HandleConverseStream` runs `HandleConverse` against an in-memory `bedrockCapture` writer that cannot be hijacked. | 3 | Open |
| E-13 | S3 | Hot path | `engine/chaos.go:438-452,325-335` | The validator allows chaos `timeout_ms` and latency up to 60,000 ms, but non-streaming responses do not bump the write deadline (only SSE frames do, per the M-16 fix). | 5 | Open |
| E-14 | S3 | Hot path | `adapter/openai.go:207-208` | Since M-19 an engine 500 has the correct status, but OpenAI, Anthropic and Responses still render `type:"invalid_request_error"`. | 3 | Open |
| E-15 | S3 | Hot path | `adapter/responses.go:166-201` | In-memory stores are bounded by count only, never by bytes. | 3 | Open |
| E-16 | S3 | Hot path | `engine/chaos.go:397-408 vs :417-419,443-445` | The engine caps normal-distribution latency and timeouts at 60 s as defence in depth, but `fixed` and `uniform` latency are uncapped. | 5 | Open |
| E-17 | S3 | Hot path | `toolschema/validator.go:143,150` | `minLength` / `maxLength` compare `len(str)` (bytes). | 3 | Open |
| E-18 | S3 | Hot path | `toolschema/strict_subset.go:44-52` | Strict-schema errors are collected while ranging over a Go map, and the engine reports `errs[0]`. | 3 | Open |
| E-19 | S3 | Hot path | `conversion/aimock.go:132-138` | The converter accepts `turnIndex >= 0`, but the validator requires `turn_number >= 1`. | 5 | Open |
| E-20 | S3 | Hot path | `adapter/openai.go:269-284` | `/v1/models` emits one entry per agent: duplicate ids when agents share a model (six shipped examples claim `gpt-4o`), `id:""` for agents with no model, and `created` set to now on every call. | 3 | Open |
| E-21 | S3 | Hot path | `adapter/conversations.go:317-320,379-382,577-579` | Update and create-items map `*http.MaxBytesError` to 400, while every other route returns 413. | 3 | Open |
| E-22 | S3 | Hot path | `adapter/strict.go:18-23` | The warn-mode `X-Mockagents-Strict-Violation` header joins every client-supplied unanswered tool id with no size bound. | 3 | Open |
| E-23 | S3 | Hot path | `engine/pipeline.go:342` | `PipelineExecutor.RunContext(..., sessionID:"")` scopes nodes as `"::<pipeline>::<node>"`. | 3 | Open |
| S-01 | S1 | Control plane | withheld | Tenant-isolation bypass on one adapter family (details withheld until the advisory is published). | 2 | Open |
| S-02 | S2 | Control plane | `internal/adapter/anthropic_batches.go:319-374,488-533` | Batch endpoints are unmetered and fan one request out to up to 100k in-process engine executions. | 2 | Open |
| S-03 | S2 | Control plane | `internal/server/pipeline_handlers.go:106-164` | Pipeline run drives N agent executions with no rate/spend enforcement. | 2 | Open |
| S-04 | S2 | Control plane | `internal/server/log_handlers.go:363-368,489-495` | Monthly spend cap is silently un-enforceable for batch sub-requests and pipeline nodes. | 2 | Open |
| S-05 | S3 | Control plane | `internal/server/realtime_wiring.go:34-50,116-132` | Realtime WebSocket cookie-principal provenance — only code-traced, not live-verified here. | 2 | Open |
| S-06 | S3 | Control plane | `internal/server/log_handlers.go:772-779` | `/api/v1/logs/stream/metrics` snapshot is not tenant-scoped. | 2 | Open |
| S-07 | S3 | Control plane | `internal/server/oidc_handlers.go:86-90` | OIDC domain→tenant map + single default role gives every verified user in a mapped domain the same (possibly elevated) role, with no per-user downgrade path. | 2 | Open |
| P-01 | S1 | Protocols | `internal/config/loader.go:176-178, :313-316` | The test runner reports a false pass when a key is misspelled. | 1 | Open |
| P-02 | S2 | Protocols | `internal/recording/import_vcr.go:165-188` | Imported vcrpy cassettes with gzip bodies do not replay. | 4 | Open |
| P-03 | S2 | Protocols | `internal/recording/proxy.go:162` | Recording through the proxy stores compressed bodies, and replay serves them as plain JSON. | 4 | Open |
| P-04 | S2 | Protocols | `internal/recording/cassette.go:350-352` | A failed cassette flush loses interactions for good, and a short write can corrupt the cassette. | 4 | Open |
| P-05 | S2 | Protocols | `internal/mcp/bidirectional.go:77-80` | Audit M-27 is still present. | 4 | Open |
| P-06 | S2 | Protocols | `internal/mcp/bidirectional.go:85-97` | Messages get stranded behind a live subscriber and are delivered out of order. | 4 | Open |
| P-07 | S2 | Protocols | `internal/realtime/session.go:155,163,169` | Audit M-32 is still present. | 4 | Open |
| P-08 | S2 | Protocols | `internal/realtime/vad.go:196-203` | G.711 (`audio/pcmu`, `audio/pcma`, beta `g711_ulaw`/`g711_alaw`) is accepted but every duration is computed as PCM16. | 4 | Open |
| P-09 | S2 | Protocols | `internal/a2a/server.go:479-483` | Non-terminal A2A tasks (`input-required`, `working`, `auth-required`, …) never expire, so the server ends up permanently at capacity. | 4 | Open |
| P-10 | S2 | Protocols | `internal/mcpadmin/manager.go:292-299` | `put_agent` (MCP) or `PUT /api/v1/agents/{name}` on an agent loaded from `foo.json` writes YAML into `foo.json`. | 4 | Open |
| P-11 | S3 | Protocols | `internal/mcp/streamable.go:714-719` | A slow GET-stream subscriber silently loses events. | 4 | Open |
| P-12 | S3 | Protocols | `internal/mcp/jsonrpc.go:45-47` | Several JSON-RPC envelope edge cases are handled incorrectly. | 4 | Open |
| P-13 | S3 | Protocols | `internal/mcp/server.go:601-613` | `resources/subscribe` accepts URIs that were never declared into a process-global map with no bound (relates to L-40). | 4 | Open |
| P-14 | S3 | Protocols | `internal/storage/sqlite.go:456-488` | `SanitizeBody` over-redacts ordinary words. | 4 | Open |
| P-15 | S3 | Protocols | `internal/recording/cassette.go:412-429 together with proxy.go:202,214 and replay.go:131` | The request hash ignores the query string. | 4 | Open |
| P-16 | S3 | Protocols | `internal/mcp/streamable.go:537-541` | The fix for audit M-30 is incomplete. | 4 | Open |
| P-17 | S3 | Protocols | `internal/a2a/server.go:369-372 vs :459` | `message/stream` reports a different message id than the stored history. | 4 | Open |
| P-18 | S3 | Protocols | `internal/contract/contract.go:130-155` | The contract gate can pass when comparing the wrong files. | 1 | Open |
| P-19 | S3 | Protocols | `internal/storage/sqlite.go:253-260` | Since/Until filters can misorder rows at boundaries. | 4 | Open |
| P-20 | S3 | Protocols | `internal/runner/runner.go:405` | The two argument assertions compare numbers differently. | 4 | Open |
| P-21 | S3 | Protocols | `Missing tests on risky branches` | The cassette flush error path, `StreamableNotifyHandler.ServeHTTP` (47.1 %), replay `serveStreaming` cancel (68 %), the stdio chaos-plus-EOF branch, `contract.Validate` (0 % in-package) and drift `FilterFindings`/`IgnoreEnumPaths` | 4 | Open |
| C-01 | S1 | Authoring/CLI | `internal/config/loader.go:93` | Multi-document YAML is silently truncated to its first document. | 1 | Open |
| C-02 | S2 | Authoring/CLI | `cmd/mockagents/mcp.go:88-93` | `mockagents mcp` and `mockagents a2a` serve documents without running their validators. | 1 | Open |
| C-03 | S1 | Authoring/CLI | `internal/cli/scaffold.go:66-69` | tests)`) | 1 | Open |
| C-04 | S2 | Authoring/CLI | `Non-strict decode at internal/config/loader.go:140,161,177,193,209,224,239,269,300-345` | Typo'd / unknown fields are silently dropped by `validate`, `start`, `ValidateBytes` (GUI editor) and `mockagents add`. | 1 | Open |
| C-05 | S2 | Authoring/CLI | `internal/server/log_handlers.go:324-333` | `MOCKAGENTS_LOG_BODIES` fails open. | 2 | Open |
| C-06 | S2 | Authoring/CLI | `cmd/mockagents/logs.go:38` | `mockagents logs` ignores `MOCKAGENTS_DATA_DIR`, creates an empty DB in the cwd, and reports "No interaction logs found." with exit 0. | 1 | Open |
| C-07 | S2 | Authoring/CLI | `cmd/mockagents/agent_cmd.go:108-123` | `mockagents rm` permanently deletes the agent's hand-authored source file (any subdirectory) with no prompt, `--yes`, or `--dry-run`, and the response claims `"persisted": false`. | 1 | Open |
| C-08 | S2 | Authoring/CLI | `internal/config/validator.go:244-270` | Streaming timing is unbounded and negative values are accepted, unlike chaos (capped at 60 s for exactly this reason, `validator.go:272-276`). | 5 | Open |
| C-09 | S2 | Authoring/CLI | `internal/config/validator.go:185-242` | Response templates are never parsed at validation time. | 5 | Open |
| C-10 | S2 | Authoring/CLI | `cmd/mockagents/validate.go:211-231` | `validate --format json` is not machine-readable. | 1 | Open |
| C-11 | S2 | Authoring/CLI | `cmd/mockagents/validate.go:205-232` | `validate` exits 0 when it found nothing to validate, so a CI step pointed at the wrong (existing) directory is green. | 1 | Open |
| C-12 | S2 | Authoring/CLI | `internal/config/cross_document_validator.go:69-121` | Duplicate Pipeline names (and MCPServer / A2AServer / TestSuite names) are not detected. | 1 | Open |
| C-13 | S3 | Authoring/CLI | `internal/config/lint.go:14-34 vs internal/engine/agent_registry.go:190-199` | Model collisions are only reported at `start`, never by `validate` (even `--strict`). | 5 | Open |
| C-14 | S3 | Authoring/CLI | `cmd/mockagents/validate.go:88-117` | Single-file `validate` reports the wrong error for non-Agent kinds. | 1 | Open |
| C-15 | S3 | Authoring/CLI | `internal/config/loader.go:86-90,492-498` | JSON definitions get wrong line numbers and BOM-prefixed JSON is rejected. | 1 | Open |
| C-16 | S3 | Authoring/CLI | `cmd/mockagents/validate.go:13-16,62-65,227-229` | Exit-code/error-output contract drift. | 1 | Open |
| C-17 | S3 | Authoring/CLI | `cmd/mockagents/start.go:203-214` |  | 2 | Open |
| C-18 | S3 | Authoring/CLI | `cmd/mockagents/start.go:118-122` | `--json-logs` and `--log-level` do not apply to package-level `slog` calls. | 1 | Open |
| C-19 | S3 | Authoring/CLI | `internal/config/validator.go:314-318,357-360` |  | 5 | Open |
| C-20 | S2 | Authoring/CLI | `schema/mockagents-v1-mcpserver.json` | The hand-maintained JSON schemas reject documents the Go loader accepts and serves. | 5 | Open |
| C-21 | S3 | Authoring/CLI | `cmd/mockagents/contract.go:55-74` | `contract extract/diff` cannot read JSON agent definitions (a supported authoring format): any file starting with `{` is assumed to be an extracted contract. | 1 | Open |
| C-22 | S3 | Authoring/CLI | `internal/config/loader.go:361-369,396-398` | Subdirectories named `build`, `dist`, `target`, `vendor`, `venv`, `node_modules`, `__pycache__` or starting with `.` are silently skipped by `validate`, `start`, `test`, `mcp`, `a2a` — including invalid documents inside them. | 1 | Open |
| C-23 | S3 | Authoring/CLI | `cmd/mockagents/validate.go:55-119` | False duplicate-name errors. | 1 | Open |
| C-24 | S3 | Authoring/CLI | `internal/cli/scaffold.go:91,135-147` | `init` scaffolds `.mockagents.yaml` (port, host, agents_dir, logging) that no command reads. | 1 | Open |
| C-25 | S3 | Authoring/CLI | `cmd/mockagents/test.go:226-236` | CLI output hygiene. | 1 | Open |
| C-26 | S3 | Authoring/CLI | `cmd/mockagents coverage 38.9 %` | No test pins the CI-facing exit-code contract. | 1 | Open |
| C-27 | S3 | Authoring/CLI | `tools/benchguard/main.go` | The CI perf gate is untested. | 8 | Open |
| K-01 | S1 | SDK/GUI | `py client.py:66-77,100-110,154-168,275-290,299-323` | The configured credential is silently dropped. | 6 | Open |
| K-02 | S1 | SDK/GUI | `py assertions.py:72-86, 210-236` | Python outcome assertions (`to_have_response_containing`, `to_have_status`, `to_have_finish_reason`) pass if any turn matches. | 6 | Open |
| K-03 | S1 | SDK/GUI | `py assertions.py:110-112` | Python argument matching uses `tc.arguments.get(k) == v`, so an absent argument matches an expected `None`. | 6 | Open |
| K-04 | S2 | SDK/GUI | `py assertions.py:192-208` | `to_have_tool_error(code)` reads `raw["tool_results"]`, but no wire response contains that field. | 6 | Open |
| K-05 | S2 | SDK/GUI | `py client.py:113,171,397,486` | `requests` defaults `text/*` without a charset to ISO-8859-1, so every Python streaming path mangles non-ASCII text. | 6 | Open |
| K-06 | S2 | SDK/GUI | `Go expect.go:176-184` | `ToHaveToolCall` compares with `reflect.DeepEqual` against `encoding/json`-decoded `float64` values. | 6 | Open |
| K-07 | S2 | SDK/GUI | `py scenario.py:92-119, 31` | `run_scenario` sends a request for every step, system and assistant included. | 6 | Open |
| K-08 | S2 | SDK/GUI | `.github/workflows/verify.yml:62-75` | `npm ci` in `sdk/npx` exits 1 (`EUSAGE … can only install with an existing package-lock.json`). | 6 | Open |
| K-09 | S2 | SDK/GUI | `sdk/vitest/package.json:9-18` | Both exports maps expose only the `import` condition. | 6 | Open |
| K-10 | S2 | SDK/GUI | `sdk/npx/lib/binary.js:146-151` | Extraction runs `execFileSync('tar', …)`, which resolves the first `tar` on PATH. | 6 | Open |
| K-11 | S3 | SDK/GUI | `py server.py:52,63` | The `config_path` constructor option is silently ignored. | 6 | Open |
| K-12 | S3 | SDK/GUI | `Go streaming.go:212-226` | SSE splitter edge cases. | 6 | Open |
| K-13 | S3 | SDK/GUI | `Go inprocess.go:37-39, 91-100` | Doc claims "same … management-API surface", but only 4 routes are mounted. | 6 | Open |
| K-14 | S3 | SDK/GUI | `py client.py:403-406, 494-497` | Injected stream faults (the `truncateAfter` and `malformed` frames from `streaming/pacing.go:166-175`) look like normal completions in every SDK: no error, `finish_reason` is `""`, and nothing is counted. | 6 | Open |
| K-15 | S3 | SDK/GUI | `py mcp.py:277-288 with :233-234` | If a Python MCP handler returns `None`, the handler runs and then `send_response` raises `ValueError`. | 6 | Open |
| K-16 | S3 | SDK/GUI | `TS client.ts:114-199, 255-290` | Streaming has no caller cancellation (no `AbortSignal` option) and no idle timeout once headers arrive. | 6 | Open |
| K-17 | S3 | SDK/GUI | `GUI lib/yamlPath.ts:203-214, 262-276` | `decodeScalar` claims to strip trailing comments but does not. | 6 | Open |
| K-18 | S3 | SDK/GUI | `GUI app/login/page.tsx:42-46` | An arbitrary `?error=` text is rendered verbatim inside a "Login failed." banner. | 6 | Open |
| K-19 | S3 | SDK/GUI | `TS server.ts:43` | TS and Go server helpers build `http://localhost:<port>`. | 6 | Open |
| K-20 | S3 | SDK/GUI | `sdk/*/package.json, sdk/python/pyproject.toml, the built wheel` | Packaging metadata gaps. | 6 | Open |
| K-21 | S3 | SDK/GUI | `GUI package.json:13` | `"lint": "next lint"` was removed in Next 16: it prints `Invalid project directory … gui\lint`. | 6 | Open |
| K-22 | S3 | SDK/GUI | `Python _binary.py:146` | Binary discovery is inconsistent. | 6 | Open |
| K-23 | S3 | SDK/GUI | `TS server.ts:88-104` | Startup does not detect an early child exit. | 6 | Open |
| K-24 | S3 | SDK/GUI | `py client.py:127,241` | The default Anthropic model differs: Python uses `claude-sonnet-4-20250514`, TS and Go use `claude-3-5-sonnet-latest`. | 6 | Open |
| K-25 | S3 | SDK/GUI | `TS types.ts:28-34` | TS and Go `ChatMessage` cannot carry assistant `tool_calls`, and content is string-only. | 6 | Open |
| K-26 | S3 | SDK/GUI | `npx bin/mockagents.js:198` | `spawnSync` blocks Node's event loop, so a `SIGTERM` to the npx process (process supervisor, CI step cancel) kills Node but not the Go child. | 6 | Open |
| O-01 | S1 | OSS delivery | `deploy/helm/mockagents/values.yaml:20` | Install commands point at public package names that nobody has claimed. | Maintainer | Maintainer |
| O-02 | S1 | OSS delivery | `.goreleaser.yml:36-38` | Released binaries and the image ship with no third-party licence notices. | 7 | Open |
| O-03 | S2 | OSS delivery | `gui/lib/icons.tsx:1-14 and the 35 icons that follow` | Lucide icon geometry copied without attribution. | 7 | Open |
| O-04 | S2 | OSS delivery | `sdk/python/` | Published SDK packages would contain no licence text. | 7 | Open |
| O-05 | S2 | OSS delivery | `.github/workflows/verify.yml:65,69-70,74-75` | The release gate has never run (0 runs), and it fails on its first job. | 7 | Open |
| O-06 | S2 | OSS delivery | `cmd/mockagents/main.go:11` | The install-path monitor has been red every day for 26 days (first failure 2026-09-11). | 7 | Open |
| O-07 | S2 | OSS delivery | `gh api …/rulesets/22323775` | No review or status gate on `main`, and nothing stops a write-access account from publishing a release. | Maintainer | Maintainer |
| O-08 | S2 | OSS delivery | `PR #143` | The response promise to contributors is broken, and dependency PRs are going stale. | Maintainer | Maintainer |
| O-09 | S2 | OSS delivery | `gh api …/dependabot/alerts` | Known vulnerabilities have no remediation path. | Maintainer | Maintainer |
| O-10 | S2 | OSS delivery | `gh api …/pages → {"build_type":"legacy","source":{"branch":"main","path":"/docs"}}` | The published docs site serves the wrong content. | Maintainer | Maintainer |
| O-11 | S2 | OSS delivery | `Missing: .github/CODEOWNERS, GOVERNANCE.md, MAINTAINERS.md, .github/PULL_REQUEST_TEMPLATE.md, SUPPORT.md` | Standard governance and community artifacts are missing, and the bus factor is 1 (one collaborator). | 7 | Open |
| O-12 | S2 | OSS delivery | `CONTRIBUTING.md` | No inbound-contribution licensing policy. | 7 | Open |
| O-13 | S2 | OSS delivery | `CODE_OF_CONDUCT.md:29-32` | Conduct reports are routed to the security-advisory form. | 7 | Open |
| O-14 | S2 | OSS delivery | `.goreleaser.yml` | Release artifacts carry no signatures, SBOM or provenance, and npm publishing uses a long-lived token. | 7 | Open |
| O-15 | S2 | OSS delivery | `ci.yml:327-364` | No formatting or linter gate. | 7 | Open |
| O-16 | S2 | OSS delivery | `ci.yml has no sdk/typescript, sdk/vitest or sdk/npx job` | SDK changes are merged untested. | 7 | Open |
| O-17 | S2 | OSS delivery | `CHANGELOG.md:13` | The changelog and release-notes process is broken. | 7 | Open |
| O-18 | S2 | OSS delivery | `hooks/pre-push:21-40` | The tracked maintainer-only push policy breaks the documented fork workflow. | 7 | Open |
| O-19 | S2 | OSS delivery | `Dockerfile:2,6,20,22-24,13-17` | The base image is end-of-life and nothing is pinned. | 7 | Open |
| O-20 | S3 | OSS delivery | `deploy/helm/mockagents/values.yaml:44-55` | The chart misses PSS "restricted" and supply-chain options. | 7 | Open |
| O-21 | S3 | OSS delivery | `.goreleaser.yml:4-6,7-25` | GoReleaser builds are not reproducible and can mutate the tree. | 7 | Open |
| O-22 | S3 | OSS delivery | `docs.yml:40` | Some dependencies are unpinned and versions drift. | 7 | Open |
| O-23 | S3 | OSS delivery | `sdk/python/.coverage` | Repo hygiene. | 7 | Open |
| O-24 | S3 | OSS delivery | `docker-compose.yml:8,11` | The compose file is broken out of the box and unhardened. | 7 | Open |
| O-25 | S3 | OSS delivery | `0 func Fuzz in 488 Go files` | No fuzzing, even on the parsers that take untrusted input: the YAML loader, request decoders, SSE parsers and the tool-schema validator. | 8 | Open |
| O-26 | S3 | OSS delivery | `README badges` | Discoverability and trust signals are missing. | Maintainer | Maintainer |
| O-27 | S3 | OSS delivery | `SECURITY.md:15-38` | The security policy is thin. | 7 | Open |
| O-28 | S3 | OSS delivery | `Secret-scanning alert #1` | A synthetic test fixture has been left as an open public secret alert. | Maintainer | Maintainer |
| O-29 | S3 | OSS delivery | `0 timeout-minutes in ci.yml` | Workflow robustness. | 7 | Open |
| O-30 | S3 | OSS delivery | `See §4` | Docs accuracy cluster: wrong asset names, unpublished install commands without warnings, a wrong Node minimum, ruff, the Makefile macOS comment, "all 5 kinds", RELEASING staleness, and stale workflow comments. | 7 | Open |
| O-31 | S3 | OSS delivery | `CONTRIBUTING.md:28-31 vs ci.yml:354-364` | CI enforces only part of `make docs-check`. | 7 | Open |
| O-32 | S3 | OSS delivery | `release.yml:348,381` | Remaining `${{ … }}` interpolation inside `run:`. | 7 | Open |
| O-33 | S3 | OSS delivery | `gui/package-lock.json` | Licence and support-matrix notes. | 7 | Open |
| K-27 | S1 | SDK/GUI | `sdk/python/mockagents/client.py:80-84` | Python chat(stream=True)/message(stream=True) skip raise_for_status, so negative assertions false-pass on a server error (carried from 2026-09-03, promoted). | 6 | Open |

## 2026-09-03 LOW tier and carried Mediums, re-verified

| Item | Re-verified status | Iteration |
|---|---|---|
| L-01 GET agent re-parses the source per request | Still present | 9 (R7) |
| L-02 Revision hash omitted for a global agent read by a tenant | Still present | 2 |
| L-03 `MaxBodyBytes == 0` rejects every body | Fixed | - |
| L-04 `writeJSON` encode failure loses request id / not logged | Partial (adapter half has a fallback body) | 3 |
| L-05 Three error envelopes; `http.Error` sends text/plain | Still present | 9 (R3) |
| L-06 `DeleteAgent` guesses a path | Still present | 1 |
| L-07 `atomicWriteFile` has no fsync; wrong temp prefix | Still present | 4 |
| L-08 Two conditional-write contracts | Not re-checked | 9 |
| L-09 Strict decode inspects only the first YAML document | Still present | 1 |
| L-10 `auth.denied` hidden from every multi-tenant reader | Still present | 2 |
| L-11 Spend hook parses the body twice, synchronously | Still present | 9 (R2) |
| L-12 Watcher lifecycle owned by `cmd`, not `Server` | Still present | 9 |
| L-13 `TemplateContext.Timestamp` never set; `Vars` never written | Still present | 3 |
| L-14 Streaming drops `refusal` when content is also set | Still present (reproduced) | 3 |
| L-15 SSE headers flushed before the TTFT sleep | Still present | 9 (R4) |
| L-16 Chaos injector locks up to five times per request | Still present | 9 |
| L-17 Template/regex caches never evicted | Still present | 9 (R5) |
| L-18 Token estimates ignore tools, args, images | Still present | 3 |
| L-19 Goroutine per tool call under the session lock | Partial (two or fewer run inline) | 9 |
| L-20 Adapters re-resolve the agent after the engine | Still present | 9 (R3) |
| L-21 `WithTenantID` doc names the dead tenant header | Still present | 3 |
| L-22 Audit `Since` lexical compare over variable-width time | Still present | 4 |
| L-23 Bulk rotate clobbers a concurrent rotate | Fixed (credential-version guard) | - |
| L-24 Global auth-cache flush on any key mutation | Still present | 2 |
| L-25 Sessions snapshot role; no revocation | Still present | 2 |
| L-26 Expired sessions never garbage-collected | Still present | 2 |
| L-27 `Retry-After` truncated | Fixed | - |
| L-28 Spend fallback overwritten by the next refresh | Still present | 2 |
| L-29 Postgres pool has no `ConnMaxLifetime`; unserialised DDL | Still present | 9 |
| L-30 Non-deterministic `ORDER BY created_at` | Fixed | - |
| L-31 Unknown models priced at $0 | Still present | 2 |
| L-32 Audit `Recorder.Record` swallows append failures | Still present | 4 |
| L-33 Metrics cardinality ceiling unconfigurable | Still present | 9 |
| L-34 `storage.Log` writes an empty timestamp | Partial (latent) | 4 |
| L-35 `tracingEnabled` bare global | Fixed | - |
| L-36 A2A card URL trusts `X-Forwarded-Proto` | Still present | 4 |
| L-37 A2A `tasks/get` marshals outside the lock | Fixed | - |
| L-38 MCP handlers on `context.Background()`; panic kills stdio | Still present | 4 |
| L-39 A2A notification and batch rules | Still present (reproduced) | 4 |
| L-40 MCP session state is process-global | Still present | 4 |
| L-41 `mcpadmin` round-trips YAML non-strictly | Still present (reproduced) | 1 |
| L-42 `init --force` removes `agents/` and `tests/` | Still present, worse on Windows (C-03) | 1 |
| L-43 Standalone servers set only `ReadHeaderTimeout` | Partial (record/replay fixed; mcp, a2a open) | 4 |
| L-44 Realtime decodes each audio frame twice | Still present | 4 |
| M-27 Bidirectional MCP steal drops buffered messages | **Still present** (P-05) | 4 |
| M-30 Streamable MCP session eviction | Partial (P-16) | 4 |
| M-32 Unbounded realtime session memory | **Still present** (P-07) | 4 |

## Status summary

| Status | Count |
|---|---:|
| Open | 131 |
| Maintainer | 7 |
| Fixed | 0 |
