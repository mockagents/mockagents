# MockAgents — Manual QA Test Plan

**Document ID:** MA-QA-TP-001
**Version:** 1.2
**Status:** Ready for execution
**Owner:** QA
**Applies to build:** `main` @ `888b59d` or later (Docker image `mockagents:latest`)
**Last updated:** 2026-07-15

> **v1.2 changes:** Covers the fixes for the cycle-1 QA validation report (`888b59d`):
> TC-ENV-05 rewritten — data persistence via the `/data` volume **was broken before this build**
> (DBs were silently written to the unwritable `/`; logging was disabled with a misleading
> SQLite "out of memory (14)" warning) and must now be positively verified; new TC-ENV-06
> (container workdir + `MOCKAGENTS_DATA_DIR` relocation); new TC-CLI-03 (`init` inside the
> container); new TC-RUN-03 (in-process glob expansion in `mockagents test`); §4.1 gains zsh
> glob-quoting guidance; §4.2 documents the model-collision tie-break warning (`wins=` field);
> §13 gains GUI port-conflict recovery; §16 regression table extended with TC-REG-11..14;
> tracker updated with the new case rows.
>
> **v1.1 changes:** Realtime suite expanded from 3 to 8 cases with copy-paste payloads (the
> Realtime surface gained server VAD, barge-in/cancellation, item ops, ephemeral keys, and
> session-update semantics across fidelity rounds 3–5, PRs #17–#23); §16 regression table now
> covers rounds 2–5; concrete request bodies added to Gemini/tool-call cases; Windows shell
> guidance added (§4.1); tracker updated with the new case rows.

---

## 1. Purpose

This plan lets a manual QA engineer validate the MockAgents server **end-to-end on a local
machine** using a **Docker-based deployment** and the bundled **demo/sample applications**.
Each test case is self-contained: preconditions, steps, expected results, and verification
steps. A companion tracker (`test-execution-tracker.csv`) records execution status.

MockAgents is a drop-in mock of the OpenAI / Anthropic / Gemini APIs (plus MCP, A2A, Realtime,
batches, conversations) driven by declarative YAML agents. There are **no real LLM calls** —
every response is deterministic and canned, which makes expected results predictable.

## 2. References

| Ref | Location |
|---|---|
| **QA troubleshooting guide (symptom → fix)** | `docs/qa/TROUBLESHOOTING.md` |
| Project overview & architecture | `CLAUDE.md` |
| CLI reference | `site/docs/guides/cli-reference.md` |
| Management API | `site/docs/guides/management-api.md` |
| Testing agents (runner, tools, MCP) | `site/docs/guides/testing-agents.md` |
| Record & replay | `site/docs/guides/record-replay.md` |
| Multi-tenant / control plane | `docs/guides/multi-tenant.md` |
| YAML schema | `schema/mockagents-v1-agent.json`, `site/docs/guides/yaml-schema.md` |
| Example agents | `examples/*.yaml` |
| Demo apps | `demo/customer-support-agent*/`, `demo/responses-api-agent/` |

## 3. Scope

### 3.1 In scope
Docker deployment; OpenAI/Anthropic/Gemini chat + streaming + tool calls + structured output +
vision; embeddings; moderations; batch APIs (OpenAI + Anthropic); Conversations + Responses;
Realtime WebSocket; chaos/fault injection; streaming faults; hallucination injection; MCP server;
A2A server; management API (agent CRUD, logs, costs, audit, validate, pipelines); multi-tenancy
(API keys, RBAC, quota); record/replay; `mockagents test` runner; contract extract/diff; CLI;
GUI console; demo applications end-to-end.

### 3.2 Out of scope
- Load/performance benchmarking (covered separately by `examples/loadtest/` + `make bench`).
- Automated unit/integration tests (`make test-all`) — this plan is **manual** validation.
- Kubernetes/Helm deployment (`deploy/helm`), OIDC/SSO against a real IdP.
- Source-level security review.

## 4. Test environment

| Item | Value |
|---|---|
| OS | Windows 11 / macOS / Linux with Docker Desktop or Docker Engine |
| Container runtime | Docker 24+ with Compose v2 (`docker compose`) |
| Tools on host | `curl`, `jq` (recommended), a WebSocket client (`websocat` or `wscat`), a browser |
| Optional | Node 20+ (GUI), Python 3.11+ (Python SDK/demo), Go 1.26+ (Go SDK) |
| Server image | Built locally via `make docker` (multi-stage, pure-Go, no cgo) |
| Default bind | `127.0.0.1:8080` in-container `0.0.0.0:8080` (compose sets `MOCKAGENTS_HOST=0.0.0.0`) |
| Health endpoint | `GET /api/v1/health` |
| Agents dir (container) | `/agents` (compose mounts `./agents:/agents:ro`) |

> **Note on the agents directory.** The compose file mounts `./agents`. For this plan, copy the
> example agents into `./agents` before starting (see TC-ENV-02), so every feature agent is
> available: `mkdir -p agents && cp examples/*.yaml agents/`.

### 4.1 Shell & quoting guidance (read before executing)

All commands in this plan are written for a **POSIX shell** (Git Bash on Windows, or any
macOS/Linux shell). If you must use PowerShell:

- Use `curl.exe` (not the `curl` alias for `Invoke-WebRequest`).
- Inline JSON quoting differs; **prefer writing request bodies to files** and passing
  `--data @body.json` — this also makes evidence capture reproducible. Example:
  ```bash
  cat > /tmp/chat.json <<'EOF'
  {"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}
  EOF
  curl -s -X POST http://localhost:8080/v1/chat/completions \
       -H 'Content-Type: application/json' --data @/tmp/chat.json
  ```
- For WebSocket cases install `websocat` (https://github.com/vi/websocat/releases — a single
  binary; add it to PATH). `wscat` (`npm i -g wscat`) also works; the plan shows `websocat`.
- **Glob patterns in zsh.** zsh aborts on an unmatched glob *before* the command runs
  (`zsh: no matches found: /tests/*.yaml`). Quote the pattern — `mockagents test 'tests/*.yaml'`
  — the CLI expands globs itself (since `888b59d`), so quoted patterns behave identically on
  every shell, including inside `docker run` arguments (see TC-RUN-03).

### 4.2 Canonical trigger phrases (example agents)

Scenario matching is substring-based on the latest user message. These inputs are used
throughout the plan:

| Agent (model) | Input | Matched scenario / effect |
|---|---|---|
| `customer-support-agent` (`gpt-4o`) | `hello` | greeting scenario (canned text) |
| `customer-support-agent` | `order status` | order-status scenario |
| `weather-agent` (`gpt-4o`) | any text containing `weather` | tool-call scenario → `get_weather` |
| `weather-agent` | anything else | default scenario (plain text) |
| `hallucination-agent` | per-type prompts in the YAML | planted hallucination + header |
| `chaos-agent` | any | probabilistic latency/errors |

> Multiple example agents share `model: gpt-4o`; the engine picks by model **and** scenario
> match. When a case says "the weather scenario", include the word `weather` in the message.
>
> **Model-collision tie-break (since `888b59d`).** When several agents claim the same model,
> API requests resolve to the **lexicographically smallest agent name** — deterministic, not
> random. The server logs a startup WARN per collision that names the winner explicitly
> (`wins=<agent>`) plus a `hint`. QA does **not** need to isolate agents into a backup folder
> to identify the routing target: read the `wins=` field. Isolation remains useful only when
> a case must exercise a *losing* agent's scenarios via its shared model.

## 5. Test approach

- **Deterministic-first.** Because responses are canned, assert on exact/near-exact content,
  headers (`X-Mockagents-*`), status codes, and SSE frame structure — not on "plausible LLM text".
- **One feature per suite.** Suites are independent; run in any order after the ENV suite passes.
- **Evidence.** Capture the command, the raw response (`curl -i` for headers), and any relevant
  log/GUI screenshot into the tracker's *Actual Result* column.
- **Verification steps** cross-check the same behavior through a second surface (e.g. confirm a
  chat call also appears in `GET /api/v1/logs` and the `/logs` GUI page).

### 5.1 Entry criteria
- Docker image builds (TC-ENV-01) and the container reports healthy (TC-ENV-03).
- Example agents are mounted and validated (TC-ENV-02).

### 5.2 Exit criteria
- 100% of **P1** cases executed; ≥ 95% pass.
- No open **Sev-1/Sev-2** defects. All failures logged in the defect log (§9) with a linked ID.

### 5.3 Priority & severity
- **Priority** P1 = core drop-in path (must pass to ship), P2 = important feature, P3 = edge/nice-to-have.
- **Severity** (defects) Sev-1 = crash/data-loss/blocks a protocol, Sev-2 = wrong result users hit,
  Sev-3 = minor/cosmetic.

### 5.4 Status values (tracker)
`Not Run` · `Pass` · `Fail` · `Blocked` · `N/A`

---

## 6. Environment setup suite (ENV)

| ID | TC-ENV-01 | Priority | P1 |
|---|---|---|---|
| **Title** | Build the Docker image | | |
| **Preconditions** | Repo checked out; Docker running | | |
| **Steps** | 1. From repo root run `make docker` (or `docker build -t mockagents:latest .`). | | |
| **Expected** | Image builds without error; `docker images` lists `mockagents:latest`. | | |
| **Verification** | `docker run --rm mockagents:latest --help` prints CLI usage. | | |

| ID | TC-ENV-02 | Priority | P1 |
|---|---|---|---|
| **Title** | Stage and validate example agents |
| **Preconditions** | TC-ENV-01 passed |
| **Steps** | 1. `mkdir -p agents && cp examples/*.yaml agents/`  2. `docker run --rm -v "$PWD/agents:/agents:ro" mockagents:latest validate /agents`. |
| **Expected** | Validator reports all agent/pipeline/suite files valid (exit 0). |
| **Verification** | Intentionally corrupt one file (bad YAML), re-run, confirm non-zero exit + a clear error pointing to the file/line; then restore. |

| ID | TC-ENV-03 | Priority | P1 |
|---|---|---|---|
| **Title** | Start via Docker Compose and confirm health |
| **Preconditions** | TC-ENV-02 passed |
| **Steps** | 1. `make docker-up` (or `docker compose up -d`).  2. `curl -s http://localhost:8080/api/v1/health`. |
| **Expected** | JSON `{"status":"ok","version":...,"uptime":...}`; HTTP 200. Compose healthcheck shows `healthy` in `docker ps`. |
| **Verification** | `docker compose logs mockagents` shows startup with the mounted agents loaded and the listen address `0.0.0.0:8080`. |

| ID | TC-ENV-04 | Priority | P2 |
|---|---|---|---|
| **Title** | Config via environment variables |
| **Preconditions** | Compose file editable |
| **Steps** | 1. Set `MOCKAGENTS_LOG_LEVEL=debug` and `MOCKAGENTS_LOG_BODIES=sanitized` in compose env.  2. `docker compose up -d --force-recreate`.  3. Make one chat call (TC-OAI-01).  4. `GET /api/v1/logs/{id}`. |
| **Expected** | Server honors debug logging; the captured response body is redacted/sanitized per `MOCKAGENTS_LOG_BODIES`. |
| **Verification** | Compare log body vs `full` mode — sanitized mode masks/omits body while keeping agent grouping. |

| ID | TC-ENV-05 | Priority | **P1** (regression — was broken before `888b59d`) |
|---|---|---|---|
| **Title** | Graceful shutdown / restart / data persistence via `/data` |
| **Steps** | 1. `docker compose logs mockagents` — confirm startup logs say `interaction logging enabled db=.mockagents.db` and `audit logging enabled` (**no** `logging disabled` WARNs).  2. Make several chat calls.  3. `docker compose exec mockagents ls -la /data` — the `.mockagents.db` / `.mockagents-audit.db` files (+ WAL/SHM) are present.  4. `make docker-down` then `make docker-up`.  5. `GET /api/v1/logs`. |
| **Expected** | Logging is ENABLED at startup; DB files live in `/data` (the compose volume), not `/`; container stops cleanly; on restart prior interactions persist. |
| **Verification** | Log IDs from before the restart are still queryable. **Failure signature of the old bug:** WARN `interaction logging disabled ... unable to open database file: out of memory (14)` — that message means the DB path is unwritable (SQLITE_CANTOPEN), *not* actual memory exhaustion; it must not appear on `888b59d`+. |

| ID | TC-ENV-06 | Priority | P2 |
|---|---|---|---|
| **Title** | Writable workdir + `MOCKAGENTS_DATA_DIR` relocation |
| **Preconditions** | TC-ENV-03 passed |
| **Steps** | 1. Confirm the image workdir: `docker run --rm --entrypoint pwd mockagents:latest` → `/data`.  2. Relocation: add `MOCKAGENTS_DATA_DIR=/data/state` to compose env, `docker compose up -d --force-recreate`, make a chat call.  3. `docker compose exec mockagents ls /data/state`.  4. Negative: run with a read-only root and NO data dir override pointing at a writable path, e.g. `docker run --rm --read-only -v "$PWD/agents:/agents:ro" mockagents:latest start --agents-dir /agents` and observe startup logs. |
| **Expected** | (1) `pwd` is `/data`. (2)(3) all three SQLite DBs are created under `/data/state` (directory auto-created).  (4) Server still starts and serves traffic; the `logging disabled` WARNs appear and each carries the DB path and the actionable `hint` naming `MOCKAGENTS_DATA_DIR` — logging failure is degraded-mode, never a crash. |
| **Verification** | With the override removed, DBs return to `/data` (the default). `GET /api/v1/logs` works in (2) and returns rows. |

---

## 7. Core protocol suites

### 7.1 OpenAI Chat Completions (OAI)

| ID | TC-OAI-01 | Priority | P1 |
|---|---|---|---|
| **Title** | Non-streaming chat completion |
| **Preconditions** | Server healthy; `weather-agent`/`customer-support-agent` loaded |
| **Steps** | `curl -s -X POST http://localhost:8080/v1/chat/completions -H 'Content-Type: application/json' -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}'` |
| **Expected** | HTTP 200; OpenAI-shaped body: `id`, `object:"chat.completion"`, `choices[0].message.content` (canned), `usage` with token counts, `finish_reason`. |
| **Verification** | Interaction appears in `GET /api/v1/logs?limit=1` with the matched agent/scenario. |

| ID | TC-OAI-02 | Priority | P1 |
|---|---|---|---|
| **Title** | Streaming chat completion (SSE) |
| **Steps** | `curl -N -X POST .../v1/chat/completions -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}'` |
| **Expected** | `Content-Type: text/event-stream`; a sequence of `data: {chat.completion.chunk}` frames with incremental `delta.content`, terminated by `data: [DONE]`. |
| **Verification** | Concatenated deltas equal the non-streaming content for the same prompt/agent. |

| ID | TC-OAI-03 | Priority | P1 |
|---|---|---|---|
| **Title** | Simulated tool / function call |
| **Preconditions** | `weather-agent` (or `tool-routing-agent`) loaded |
| **Steps** | `curl -s -X POST http://localhost:8080/v1/chat/completions -H 'Content-Type: application/json' -d '{"model":"gpt-4o","messages":[{"role":"user","content":"What is the weather in NYC?"}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"location":{"type":"string"}}}}}]}'` |
| **Expected** | Response `choices[0].message.tool_calls[]` with `function.name=get_weather` and a JSON `arguments` string; `finish_reason:"tool_calls"`. |
| **Verification** | For an agent using `raw_arguments`, the emitted `arguments` string is the **verbatim** planted value (incl. malformed JSON) — cross-check with the agent YAML. |

| ID | TC-OAI-04 | Priority | P2 |
|---|---|---|---|
| **Title** | Structured output (JSON schema strict) |
| **Preconditions** | `structured-output-agent` loaded |
| **Steps** | Send a chat request with `response_format:{type:"json_schema", json_schema:{...}}`. |
| **Expected** | `message.content` is valid JSON conforming to the requested schema; refusal path returns a `refusal` field when the agent scenario dictates. |
| **Verification** | Parse the content with `jq`; validate against the schema keys. |

| ID | TC-OAI-05 | Priority | P2 |
|---|---|---|---|
| **Title** | Vision / image input matching |
| **Preconditions** | `vision-agent` loaded |
| **Steps** | Send a chat request with a `content` array containing an `image_url` part. |
| **Expected** | The `has_image` scenario matches; response header `X-Mockagents-Image-Count: 1`. |
| **Verification** | Send a text-only request → header absent/0 and a different scenario matches. |

| ID | TC-OAI-06 | Priority | P3 |
|---|---|---|---|
| **Title** | Azure OpenAI URL surface |
| **Steps** | POST to `/openai/deployments/{deployment}/chat/completions?api-version=...` with a chat body. |
| **Expected** | Routed to the OpenAI handler; same completion shape returned. |
| **Verification** | Response equivalent to TC-OAI-01. |

### 7.2 Anthropic Messages (ANT)

| ID | TC-ANT-01 | Priority | P1 |
|---|---|---|---|
| **Title** | Non-streaming Anthropic message |
| **Steps** | `curl -s -X POST .../v1/messages -H 'anthropic-version: 2023-06-01' -d '{"model":"claude-3-opus","max_tokens":100,"messages":[{"role":"user","content":"hello"}]}'` |
| **Expected** | HTTP 200; Anthropic-shaped body: `type:"message"`, `role:"assistant"`, `content:[{type:"text",text:...}]`, `stop_reason`, `usage`. |
| **Verification** | Logged under the matched agent in `/api/v1/logs`. |

| ID | TC-ANT-02 | Priority | P1 |
|---|---|---|---|
| **Title** | Streaming Anthropic message |
| **Steps** | Same as TC-ANT-01 with `"stream":true`, `curl -N`. |
| **Expected** | SSE event sequence `message_start` → `content_block_start` → `content_block_delta`* → `content_block_stop` → `message_delta` → `message_stop`. |
| **Verification** | Deltas reassemble to the non-streaming text. |

| ID | TC-ANT-03 | Priority | P2 |
|---|---|---|---|
| **Title** | Anthropic tool use |
| **Steps** | Send a messages request with `tools` and a prompt that triggers a tool scenario. |
| **Expected** | `content` includes a `tool_use` block with `name` + `input`; `stop_reason:"tool_use"`. |
| **Verification** | Matches the agent's tool spec. |

### 7.3 Google Gemini (GEM)

| ID | TC-GEM-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Gemini generateContent |
| **Preconditions** | `gemini-agent` loaded (protocol `google-gemini`; check its `model:` in the YAML — the URL segment must match it) |
| **Steps** | `curl -s -X POST 'http://localhost:8080/v1beta/models/<model>:generateContent' -H 'Content-Type: application/json' -d '{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}'` |
| **Expected** | Gemini-shaped response (`candidates[].content.parts[].text`, `usageMetadata` with `promptTokenCount`/`candidatesTokenCount`). |
| **Verification** | Streaming variant `POST .../models/<model>:streamGenerateContent` (`curl -N`) emits Gemini stream chunks; concatenated parts equal the non-stream text. |

### 7.4 Embeddings & Moderations (EMB / MOD)

| ID | TC-EMB-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Embeddings endpoint |
| **Steps** | `POST /v1/embeddings -d '{"model":"text-embedding-3-small","input":"hello world"}'` |
| **Expected** | `data[0].embedding` is a numeric vector (deterministic, unit-normalized); `usage` present. |
| **Verification** | Same input → identical vector (deterministic); different input → different vector. |

| ID | TC-MOD-01 | Priority | P3 |
|---|---|---|---|
| **Title** | Moderations endpoint |
| **Steps** | `POST /v1/moderations -d '{"model":"omni-moderation-latest","input":"some text"}'` |
| **Expected** | `results[0]` with `flagged` boolean and 14 category scores. |
| **Verification** | Response shape matches OpenAI moderation schema. |

---

## 8. Advanced API suites

### 8.1 Batch APIs (BATCH)

| ID | TC-BATCH-01 | Priority | P2 |
|---|---|---|---|
| **Title** | OpenAI Batch API lifecycle |
| **Steps** | 1. `POST /v1/files` (purpose=batch) uploading a JSONL of chat requests.  2. `POST /v1/batches` with `input_file_id`, `endpoint:/v1/chat/completions`, `completion_window:24h`.  3. Poll `GET /v1/batches/{id}` until `completed`.  4. `GET /v1/files/{output_file_id}/content`. |
| **Expected** | File upload returns a file id; batch created with `status`; polling transitions to `completed`; output JSONL contains one canned response per input line keyed by `custom_id`. |
| **Verification** | Line count of output == input; each `custom_id` echoed. |

| ID | TC-BATCH-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Anthropic Message Batches (inline) |
| **Steps** | `POST /v1/messages/batches` with an inline `requests:[{custom_id, params:{...}}]` array; poll for results. |
| **Expected** | Batch accepted without file upload; results retrievable, one per `custom_id`. |
| **Verification** | Each result is a valid Anthropic message. |

### 8.2 Conversations & Responses (CONV)

| ID | TC-CONV-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Conversations + Responses stateful turn |
| **Preconditions** | `demo/responses-api-agent/` agents loaded |
| **Steps** | 1. `POST /v1/conversations`.  2. `POST /v1/responses` with the `conversation` id and a message.  3. `GET /v1/responses` / conversation items. |
| **Expected** | Conversation id issued; response items returned (typed events for streaming: `response.created`, `response.output_text.delta`, `response.completed`); state threaded via previous item ids. |
| **Verification** | A second turn referencing the conversation reflects prior context. |

### 8.3 Realtime WebSocket (RT)

> **Driving recipe.** The Realtime API is served by the MAIN server (no separate process):
> `websocat -t "ws://localhost:8080/v1/realtime?model=gpt-4o"` — the `model` query parameter
> selects the agent (use `gpt-4o` so the weather/customer-support scenarios apply). Type one
> single-line JSON event per line; server events print one per line. Keep the connection open
> across the steps of a case.
>
> **Audio chunks for VAD cases.** Server VAD decides speech vs silence from decoded PCM16
> energy. Generate base64 chunks once and reuse them (any machine with Python 3):
> ```bash
> python3 - <<'EOF'
> import base64
> mk = lambda amp, ms: base64.b64encode(
>     amp.to_bytes(2, "little", signed=True) * (ms * 24)).decode()
> print("SPEECH (200ms):", mk(20000, 200))
> print("SILENCE (600ms):", mk(0, 600))
> EOF
> ```
> `SPEECH` is 200 ms of loud audio (detected as speech at the default threshold 0.5);
> `SILENCE` is 600 ms of digital silence (crosses the default 500 ms end-of-turn window).
> Below, `<SPEECH>` / `<SILENCE>` mean those base64 strings.

| ID | TC-RT-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Realtime session establishment (GA session object) |
| **Preconditions** | `websocat` installed; server healthy |
| **Steps** | 1. `websocat -t "ws://localhost:8080/v1/realtime?model=gpt-4o"`.  2. Observe the greeting events. |
| **Expected** | First event `session.created` with the GA session object: `type:"realtime"`, `model:"gpt-4o"`, top-level `output_modalities:["audio"]`, `instructions` (server default text when unset), `tools:[]`, `tool_choice:"auto"`, `max_output_tokens:"inf"`, `expires_at` (≈ now+3600), and a nested `audio` block (`audio.output.voice:"alloy"`, `audio.output.speed:1`, `audio.input.turn_detection:null`). **No** top-level `voice`/`modalities` (beta fields). Second event `conversation.created`. Every event carries a unique `event_id`. |
| **Verification** | Send `{"type":"session.update","session":{"instructions":"be brief"}}` → `session.updated` echoes the FULL effective session object with the new instructions. Send garbage (`not json`) → an `error` event with `type:"invalid_request_error"`, `param:null`. |

| ID | TC-RT-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Text turn + mid-session function call |
| **Preconditions** | TC-RT-01 connection open (`model=gpt-4o`, `weather-agent` loaded) |
| **Steps** | 1. Send `{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_text","text":"What is the weather in NYC?"}]}}`.  2. Send `{"type":"response.create"}`. |
| **Expected** | Step 1: `conversation.item.added` then `conversation.item.done` (GA pair), echoing the item with `previous_item_id:null` for the first item. Step 2: `response.created` → `rate_limits.updated` (exactly once) → function-call ladder `response.output_item.added` → `conversation.item.added` (mirror) → `response.function_call_arguments.delta`* → `.done` → `response.output_item.done` → `conversation.item.done`, then `response.done` whose `output` lists the `function_call` item with `name:"get_weather"`, a `call_id`, assembled `arguments`, and `usage` (input/output token details). |
| **Verification** | Reassembled `arguments` deltas == the `.done` `arguments` string. Send the tool result back: `{"type":"conversation.item.create","item":{"type":"function_call_output","call_id":"<call_id>","output":"{\"temp\":72}"}}` then `{"type":"response.create"}` → a normal assistant message ladder follows (tool loop closes). |

| ID | TC-RT-03 | Priority | P3 |
|---|---|---|---|
| **Title** | Text-only modality + response.create overrides |
| **Steps** | 1. Send `{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`.  2. Send `{"type":"response.create","response":{"output_modalities":["text"],"metadata":{"purpose":"qa"}}}`. |
| **Expected** | Text ladder: `response.content_part.added` (part `type:"text"`) → `response.output_text.delta`* → `response.output_text.done` → `response.content_part.done`; **no** `response.output_audio*` events. `response.done`'s response echoes `output_modalities:["text"]` and `metadata:{"purpose":"qa"}`. |
| **Verification** | An out-of-band variant `{"type":"response.create","response":{"conversation":"none"}}` returns a full burst ladder whose `response.done` has `conversation_id:null`, emits **no** `conversation.item.*` mirror events, and its output item is NOT retrievable via `conversation.item.retrieve` (`item_not_found`). |

| ID | TC-RT-04 | Priority | P3 |
|---|---|---|---|
| **Title** | Ephemeral keys — GA client_secrets vs legacy sessions |
| **Steps** | 1. `curl -s -X POST http://localhost:8080/v1/realtime/client_secrets -H 'Content-Type: application/json' -d '{"session":{"model":"gpt-realtime","audio":{"output":{"voice":"verse"}}}}'`.  2. `curl -s -X POST http://localhost:8080/v1/realtime/sessions -H 'Content-Type: application/json' -d '{"model":"gpt-4o-realtime-preview","voice":"verse"}'`. |
| **Expected** | (1) GA envelope `{value:"ek_...", expires_at, session}` — `expires_at` ≈ now+600 s; session is GA-shaped (voice under `audio.output`, `client_secret` mirrored inside). (2) Legacy = the BETA session object itself (no envelope): top-level `voice:"verse"`, `modalities:["audio","text"]`, `input_audio_format:"pcm16"`, `temperature:0.8`, `max_response_output_tokens:"inf"`, `client_secret.expires_at` ≈ now+**60 s**; **no** GA `audio` block. |
| **Verification** | (1) with `{"expires_after":{"anchor":"created_at","seconds":900}}` → expires_at ≈ now+900 (clamped to 10..7200). |

| ID | TC-RT-05 | Priority | P2 |
|---|---|---|---|
| **Title** | Server VAD voice turn (turn detection end-to-end) |
| **Preconditions** | Fresh connection (`model=gpt-4o`); `<SPEECH>`/`<SILENCE>` chunks generated (recipe above) |
| **Steps** | 1. Send `{"type":"session.update","session":{"audio":{"input":{"turn_detection":{"type":"server_vad"}}}}}`.  2. Send `{"type":"input_audio_buffer.append","audio":"<SPEECH>"}`.  3. Send `{"type":"input_audio_buffer.append","audio":"<SILENCE>"}`. |
| **Expected** | Step 2: `input_audio_buffer.speech_started` pre-announcing an `item_id`. Step 3 (end of turn): `input_audio_buffer.speech_stopped` → `input_audio_buffer.committed` with the SAME `item_id` → `conversation.item.added`/`.done` for the user item → the auto-response ladder (`response.created`, `rate_limits.updated`, message ladder events arriving paced, `response.done` status `completed`). |
| **Verification** | Invalid config is rejected whole: `{"type":"session.update","session":{"audio":{"input":{"turn_detection":{"type":"server_vad","threshold":1.5}}}}}` → `error` with `code:"invalid_value"` and `param:"session.audio.input.turn_detection.threshold"`, and no `session.updated`. With `idle_timeout_ms:5000` configured, ~5 s of inactivity after a completed response fires `input_audio_buffer.timeout_triggered` + a `[silence]`-driven re-prompt response — exactly once (no self-prompt loop). |

| ID | TC-RT-06 | Priority | P2 |
|---|---|---|---|
| **Title** | Barge-in & cancellation close-out (delta invariant) |
| **Preconditions** | VAD session as in TC-RT-05 |
| **Steps** | 1. Complete one voice turn (TC-RT-05 steps 2–3) but **while the response ladder is still streaming**, send `{"type":"input_audio_buffer.append","audio":"<SPEECH>"}` (barge-in). Alternatively/additionally: start another turn and send `{"type":"response.cancel"}` right after `response.created`. |
| **Expected** | Barge-in: `input_audio_buffer.speech_started` followed by the interrupted item's close-out (`.done` events) and `response.done` with `status:"cancelled"`, `status_details.reason:"turn_detected"` (client cancel → reason `client_cancelled`). **Invariant:** every `.done` payload (`output_audio_transcript.done`, `content_part.done`, the item) carries exactly the concatenation of the deltas that were actually received — never the full unstreamed transcript; a content part whose `content_part.added` never arrived is absent from the close-out; `usage.output_tokens` counts only streamed words (0 for an immediate cancel, with `output:[]`). |
| **Verification** | The interrupted item is retrievable with `status:"incomplete"`; the NEXT turn's `input_audio_buffer.committed` chains (`previous_item_id`) off the last item the client actually saw — never an unannounced id. `response.cancel` with nothing in flight → `error` code `response_cancel_not_active`. |

| ID | TC-RT-07 | Priority | P3 |
|---|---|---|---|
| **Title** | Conversation item ops — client ids, delete chain repair, truncate errors |
| **Steps** | 1. Create with a client id: `{"type":"conversation.item.create","item":{"id":"cli_1","type":"message","role":"user","content":[{"type":"input_text","text":"one"}]}}`.  2. `{"type":"conversation.item.retrieve","item_id":"cli_1"}`.  3. Create `cli_2` (same shape, text "two").  4. `{"type":"conversation.item.delete","item_id":"cli_2"}`.  5. Create `cli_3` (text "three").  6. `{"type":"conversation.item.truncate","item_id":"cli_1","content_index":0,"audio_end_ms":100}`. |
| **Expected** | (2) `conversation.item.retrieved` echoes the item. (4) `conversation.item.deleted`. (5) `conversation.item.added` shows `previous_item_id:"cli_1"` — the chain is REPAIRED after deleting the tail, not dangling at `cli_2`. (6) `error` with `code:null`, `param:null`, message exactly `Only model output audio messages can be truncated`. |
| **Verification** | Duplicate id (`cli_1` again) → `error` `invalid_value` with `param:"item.id"`. Unknown `previous_item_id` on create → `item_not_found` with `param:"previous_item_id"`. Retrieve of a deleted id → `item_not_found`. |

| ID | TC-RT-08 | Priority | P2 |
|---|---|---|---|
| **Title** | session.update semantics — mid-speech safety, voice lock, validation |
| **Preconditions** | VAD session with `<SPEECH>`/`<SILENCE>` chunks |
| **Steps** | 1. Send `<SPEECH>` (speech_started arrives).  2. **Mid-speech**, send `{"type":"session.update","session":{"instructions":"be brief"}}`.  3. Send `<SILENCE>`.  4. After any response that produced audio, send `{"type":"session.update","session":{"audio":{"output":{"voice":"cedar"}}}}`.  5. Send `{"type":"session.update","session":{"max_output_tokens":-5}}`. |
| **Expected** | (2) `session.updated` — and the in-progress speech cycle SURVIVES: (3) still yields `speech_stopped` + `committed` with the item id pre-announced in step 1, plus the auto-response (the turn is not dropped). (4) `error` `code:"cannot_update_voice"`, message `Cannot update a conversation's voice if assistant audio is present.`, `param:null`; NO `session.updated`; the old voice remains effective. (5) `error` `invalid_value` with `param:"session.max_output_tokens"`. |
| **Verification** | Re-sending the SAME voice (with other fields) after audio → accepted (`session.updated`). Changing `turn_detection` itself mid-speech DOES reset the speech cycle (expected: the config changed). |

---

## 9. Fault-injection & fidelity suites

### 9.1 Chaos / fault injection (CHAOS)

| ID | TC-CHAOS-01 | Priority | P1 |
|---|---|---|---|
| **Title** | Injected latency |
| **Preconditions** | `chaos-agent` loaded |
| **Steps** | Send repeated chat calls to the chaos model; measure round-trip time. |
| **Expected** | Responses are delayed per the agent's `chaos.latency` (bounded, ctx-cancellable); still return 200 with valid body. |
| **Verification** | Latency visible in `/api/v1/logs` timing; a client timeout/cancel aborts cleanly. |

| ID | TC-CHAOS-02 | Priority | P1 |
|---|---|---|---|
| **Title** | Injected error responses (per provider) |
| **Steps** | Send many calls to the chaos model; observe error frequency and shape. |
| **Expected** | A fraction return provider-correct error bodies with correct HTTP status (e.g. 503/504/429) and `Retry-After` where applicable. |
| **Verification** | Error JSON shape matches the provider being called (OpenAI vs Anthropic vs Gemini). |

| ID | TC-CHAOS-03 | Priority | P2 |
|---|---|---|---|
| **Title** | Rate-limit chaos (429) |
| **Preconditions** | `chaos-agent` (rate 20/min) |
| **Steps** | Burst > the configured rate within a minute. |
| **Expected** | Excess requests get HTTP 429 with `Retry-After`. |
| **Verification** | After the window, requests succeed again. |

| ID | TC-CHAOS-04 | Priority | P2 |
|---|---|---|---|
| **Title** | Flaky-then-healthy (FailFirst-N) |
| **Preconditions** | `flaky-then-healthy-agent` loaded |
| **Steps** | Send N+ sequential calls. |
| **Expected** | First N calls fail per config, then subsequent calls recover to 200. |
| **Verification** | Recovery boundary matches the configured N. |

| ID | TC-CHAOS-05 | Priority | P2 |
|---|---|---|---|
| **Title** | Chaos presets (403/401/server-down) |
| **Preconditions** | `access-denied-agent` / `unauthorized` preset agents |
| **Steps** | Call each preset agent. |
| **Expected** | Correct status (403 / 401 / 5xx) and body per preset. |
| **Verification** | Matches `config/chaos_presets.go` semantics. |

| ID | TC-CHAOS-06 | Priority | P3 |
|---|---|---|---|
| **Title** | Connection-layer faults |
| **Preconditions** | `connection-fault-agent` loaded |
| **Steps** | Drive traffic; observe transport behavior. |
| **Expected** | Connection drop/reset behavior per config (client sees a connection error, not a clean HTTP error). |
| **Verification** | Reproducible under repeated attempts. |

### 9.2 Streaming faults (STREAM)

| ID | TC-STREAM-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Stream timing physics (TTFT + tokens/sec) |
| **Preconditions** | `stream-faults-agent` loaded |
| **Steps** | `curl -N` a streaming request to the faulty stream model; time first byte and inter-token gaps. |
| **Expected** | First chunk arrives ~TTFT (e.g. 200ms); subsequent chunks paced per tokens/sec + jitter. |
| **Verification** | Timing within configured envelope; deterministic under fixed seed. |

| ID | TC-STREAM-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Mid-stream truncation / malformed frame |
| **Steps** | Stream from an agent configured with `truncateAfter` / `malformed`. |
| **Expected** | Stream cuts off after N tokens (no `[DONE]`) or emits a malformed frame, exactly as configured. |
| **Verification** | Client-side parser observes the injected fault; matches YAML. |

| ID | TC-STREAM-03 | Priority | P3 |
|---|---|---|---|
| **Title** | Load-target latency distribution |
| **Preconditions** | `load-target-agent` loaded |
| **Steps** | Send many streaming requests; sample TTFT/ITL. |
| **Expected** | Per-request lognormal TTFT/ITL draws consistent with configured p50/p95. |
| **Verification** | Distribution roughly matches targets over a sample. |

### 9.3 Hallucination injection (HALL)

| ID | TC-HALL-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Deterministic hallucination planting |
| **Preconditions** | `hallucination-agent` loaded |
| **Steps** | Send prompts matching each hallucination type (fabricated_fact, fabricated_citation, ungrounded, bad_tool_result). |
| **Expected** | Planted bad output returned deterministically; response header `X-Mockagents-Hallucination: <type>`. |
| **Verification** | Each type yields its documented payload; header present. |

---

## 10. Protocol server suites

### 10.1 MCP server (MCP)

| ID | TC-MCP-01 | Priority | P2 |
|---|---|---|---|
| **Title** | MCP over HTTP — tools/resources/prompts |
| **Preconditions** | `weather-mcp` document available |
| **Steps** | 1. Start `mockagents mcp --transport http --port 8081 --bind 0.0.0.0 --agents-dir /agents` (a second container or `docker compose run`; `--bind 0.0.0.0` is required to reach it from outside the container — the default bind is loopback-only per the MCP spec).  2. `POST /mcp` JSON-RPC `initialize`, then `tools/list`, `resources/list`, `prompts/list`, `tools/call`. |
| **Expected** | JSON-RPC 2.0 responses with correct `id` echo; tools/resources/prompts enumerated; `tools/call` returns the canned result. |
| **Verification** | Malformed JSON-RPC yields a proper error object with correct code. |

| ID | TC-MCP-02 | Priority | P3 |
|---|---|---|---|
| **Title** | MCP bidirectional SSE |
| **Steps** | `GET /mcp/events` (SSE) to receive server-initiated requests; trigger a sample/roots via admin; reply through `POST /mcp/response`. |
| **Expected** | Server-initiated request delivered over SSE; client reply routed back and resolved. |
| **Verification** | Round-trip completes; notification queue drains. |

| ID | TC-MCP-03 | Priority | P3 |
|---|---|---|---|
| **Title** | MCP over stdio |
| **Steps** | `docker run -i ... mcp --transport stdio --server weather-mcp` and pipe a JSON-RPC `initialize` on stdin. |
| **Expected** | JSON-RPC response on stdout. |
| **Verification** | Sequence of calls works over stdio framing. |

### 10.2 A2A server (A2A)

| ID | TC-A2A-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Agent Card discovery |
| **Preconditions** | `a2a-server.yaml` (`weather-a2a`) available |
| **Steps** | Start `mockagents a2a --agents-dir /agents --server weather-a2a`; `GET /.well-known/agent-card.json`. |
| **Expected** | Agent Card JSON with `name`, `url`, `protocolVersion`, `preferredTransport:"JSONRPC"`, `capabilities.streaming:true`, and defaulted `version`/`description`/`skills` (each skill's `tags` renders as `[]` never null). |
| **Verification** | Author-set fields are not clobbered by defaults; the card matches the YAML. |

| ID | TC-A2A-02 | Priority | P2 |
|---|---|---|---|
| **Title** | message/send (Task + bare Message) |
| **Steps** | `POST /` JSON-RPC `message/send` with a text part. |
| **Expected** | JSON-RPC result is a Task (or a bare Message when the matched response sets `as_message`); correct `id` echo. |
| **Verification** | file/data parts in the request propagate into the artifact parts. |

| ID | TC-A2A-03 | Priority | P2 |
|---|---|---|---|
| **Title** | message/stream SSE |
| **Steps** | `POST /` JSON-RPC `message/stream`; read the SSE stream. |
| **Expected** | `text/event-stream`; ordered `data:` frames each wrapping a JSON-RPC result (Task → status-update → artifact-update), the last a status-update with `final:true`. |
| **Verification** | A Data-part response propagates into the streamed artifact parts. |

| ID | TC-A2A-04 | Priority | P2 |
|---|---|---|---|
| **Title** | JSON-RPC error id compliance (INT-2 fix) |
| **Steps** | 1. POST a **malformed** JSON body.  2. POST a valid body with `"jsonrpc":"1.0"`.  3. POST a `message/stream` **without** an `id`. |
| **Expected** | (1)(2) return a JSON-RPC error object rendering `"id":null` (not omitted); (3) returns HTTP 204 (no id-less SSE stream). |
| **Verification** | `curl -i` shows `"id":null` in the error bodies and `204 No Content` for the id-less stream. |

---

## 11. Management & control-plane suites

### 11.1 Management API — agents & config (MGMT)

| ID | TC-MGMT-01 | Priority | P1 |
|---|---|---|---|
| **Title** | List & get agents |
| **Steps** | `GET /api/v1/agents`; `GET /api/v1/agents/weather-agent`. |
| **Expected** | Array of agent summaries; detail returns full definition (scenarios, tools, model, protocol). |
| **Verification** | Count matches the mounted `/agents` files. |

| ID | TC-MGMT-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Create / replace / delete agent (write API + CLI) |
| **Steps** | 1. `mockagents add agents/new-agent.yaml` (POST).  2. `mockagents add agents/new-agent.yaml --replace` (PUT).  3. Call the new model to confirm it's live.  4. `mockagents rm new-agent` (DELETE). |
| **Expected** | Create returns 201; replace updates in place; the new agent answers requests; delete removes it (subsequent calls no longer match). |
| **Verification** | `GET /api/v1/agents` reflects each change; an audit `agent.reloaded`/write event is recorded. |

| ID | TC-MGMT-03 | Priority | P2 |
|---|---|---|---|
| **Title** | Config validate endpoint |
| **Steps** | `POST /api/v1/config/validate` with valid then invalid YAML. |
| **Expected** | Valid → ok; invalid → structured parse/schema errors with locations. |
| **Verification** | Mirrors CLI `mockagents validate` behavior and the GUI `/editor`. |

| ID | TC-MGMT-04 | Priority | P2 |
|---|---|---|---|
| **Title** | Pipelines listing & DAG |
| **Preconditions** | `research-pipeline.yaml` loaded |
| **Steps** | `GET /api/v1/pipelines`; `GET /api/v1/pipelines/research-pipeline`. |
| **Expected** | Pipeline list; detail returns topology (nodes/edges). |
| **Verification** | Drive the pipeline via a request and see per-node execution in logs. |

### 11.2 Logs, costs, audit (OBS)

| ID | TC-OBS-01 | Priority | P1 |
|---|---|---|---|
| **Title** | Interaction log query |
| **Steps** | After making calls: `GET /api/v1/logs?limit=10`; `GET /api/v1/logs/{id}`. |
| **Expected** | Paginated entries (agent, model, latency, status, cost_usd); detail includes request/response bodies (per `LOG_BODIES`). |
| **Verification** | CLI `mockagents logs --agent weather-agent --since 1h` returns the same rows. |

| ID | TC-OBS-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Live log stream (SSE) |
| **Steps** | `curl -N http://localhost:8080/api/v1/logs/stream`; in another shell make a chat call. |
| **Expected** | A new SSE event appears for the call, carrying a row with a primary-key id. |
| **Verification** | The streamed id matches a `GET /api/v1/logs/{id}` fetch. |

| ID | TC-OBS-03 | Priority | P2 |
|---|---|---|---|
| **Title** | Cost dashboard endpoint |
| **Steps** | `GET /api/v1/costs` after varied traffic. |
| **Expected** | Aggregates by model and by agent, total USD, token counts, derived from the pricing table. |
| **Verification** | Override `MOCKAGENTS_PRICING` with a custom YAML → costs reflect the override. |

| ID | TC-OBS-04 | Priority | P2 |
|---|---|---|---|
| **Title** | Audit log |
| **Preconditions** | Multi-tenant mode (§11.3) for auth events |
| **Steps** | Perform key/tenant/agent operations; `GET /api/v1/audit?limit=50`. |
| **Expected** | Append-only entries for the nine event kinds (tenant.created, api_key.*, agent.reloaded, pipeline.saved, auth.denied, ...). |
| **Verification** | A denied auth attempt (wrong key) produces an `auth.denied` entry. |

### 11.3 Multi-tenancy, RBAC, quota (TENANT / QUOTA)

| ID | TC-TENANT-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Enable multi-tenant mode & bootstrap key |
| **Steps** | Start the container with `MOCKAGENTS_MULTI_TENANT=1`; capture the bootstrap (platform) key from stderr/logs. |
| **Expected** | Server boots in multi-tenant mode; a platform bootstrap key is printed once. |
| **Verification** | Management routes now require auth (unauth → 401). |

| ID | TC-TENANT-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Tenant & API key lifecycle |
| **Steps** | Using the platform key: `POST /api/v1/tenants`; then mint keys `POST /api/v1/tenants/{id}/keys` at roles viewer/editor/admin; `PATCH /api/v1/keys/{id}` role change; `POST /api/v1/keys/{id}/rotate`; `DELETE /api/v1/keys/{id}`. |
| **Expected** | Each operation succeeds for an authorized caller; rotate preserves id/name/role/tenant with a new secret; deleted keys stop working. |
| **Verification** | Each mutation appears in the audit log. |

| ID | TC-TENANT-03 | Priority | P1 |
|---|---|---|---|
| **Title** | RBAC enforcement & privilege boundaries |
| **Steps** | 1. Use a `viewer` key to attempt an editor action (create agent) → expect 403.  2. Use a per-tenant `admin` key to attempt to assign the `platform` role → expect refusal.  3. Attempt cross-tenant key management → expect refusal. |
| **Expected** | Ordered roles enforced (viewer<editor<admin<platform); platform is bootstrap-only; admins can't self-escalate or touch other tenants. |
| **Verification** | Denials produce `auth.denied` audit entries. |

| ID | TC-QUOTA-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Rate limit & monthly spend enforcement |
| **Steps** | Set `MOCKAGENTS_DEFAULT_RATE_PER_SEC`/`_RATE_BURST`/`_MONTHLY_SPEND_USD` low; burst LLM calls; `GET /api/v1/quota`. |
| **Expected** | Over-rate → 429 + `Retry-After`; over-spend → 402; quota endpoint shows current rate + spend. |
| **Verification** | `PUT /api/v1/tenants/{id}/quota` override persists and reloads after restart; empty/anonymous tenant is never limited. |

---

## 12. Tooling & workflow suites

### 12.1 Test runner (RUN)

| ID | TC-RUN-01 | Priority | P1 |
|---|---|---|---|
| **Title** | Run a TestSuite (assertions) |
| **Preconditions** | `tool-routing-suite.yaml` + its agents loaded |
| **Steps** | `mockagents test /agents/tool-routing-suite.yaml --agents-dir /agents`. |
| **Expected** | All assertions pass; exit 0. Covers `tool_call`, `scenario_matched`. |
| **Verification** | Introduce a failing assertion → exit 1 with a clear diff. |

| ID | TC-RUN-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Multi-turn + new assertion types |
| **Preconditions** | A suite exercising `tool_call_args`, `tool_error`, `handles_tool_error`, `latency_ms_lt` (max_ms), multi-turn steps |
| **Steps** | Run the suite. |
| **Expected** | Multi-turn steps replay and aggregate trajectory; `tool_call_args` matches nested dotted paths (type-tolerant); `tool_error`/`handles_tool_error` assert error trajectory; `latency_ms_lt` enforces `max_ms ≥ 1`. |
| **Verification** | `--format junit` emits valid JUnit XML; `--format json` machine-readable. |

| ID | TC-RUN-03 | Priority | P2 (new in `888b59d`) |
|---|---|---|---|
| **Title** | Glob pattern expansion in `mockagents test` |
| **Preconditions** | ≥ 2 TestSuite YAML files staged (e.g. under `/agents`) |
| **Steps** | 1. Quoted glob (zsh-safe): `mockagents test '/agents/*suite*.yaml' --agents-dir /agents`.  2. Same via Docker: `docker run --rm -v "$PWD/agents:/agents:ro" mockagents:latest test '/agents/*suite*.yaml' --agents-dir /agents`.  3. Negative: `mockagents test '/agents/*.nomatch' --agents-dir /agents`. |
| **Expected** | (1)(2) The CLI expands the pattern itself — every matching suite runs, in **sorted filename order** (deterministic), identical results on zsh/bash/PowerShell/in-container.  (3) Clear error `no test suite files match pattern ...` with a **non-zero exit** — an unmatched glob never silently runs zero suites. |
| **Verification** | A non-glob literal path that doesn't exist still reports the file-not-found error (pass-through unchanged). Compare run output of (1) against invoking each suite file explicitly — same cases, same results. |

### 12.2 Record & replay (REC)

| ID | TC-REC-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Record then replay a cassette |
| **Steps** | 1. `mockagents record --upstream <api> --cassette /data/cass.jsonl [--redact]` and drive a couple of calls through it.  2. Stop; `mockagents replay --cassette /data/cass.jsonl`.  3. Re-issue the same requests. |
| **Expected** | Cassette captures request/response (and SSE streams); replay serves identical responses offline, including streamed frames. |
| **Verification** | `--redact` masks secrets in the cassette. |

### 12.3 Contract (CONTRACT)

| ID | TC-CONTRACT-01 | Priority | P3 |
|---|---|---|---|
| **Title** | Extract & diff agent contract |
| **Steps** | `mockagents contract extract /agents/weather-agent.yaml -o /data/c.json`; modify the agent; `mockagents contract diff /data/c.json /agents/weather-agent.yaml`. |
| **Expected** | Extract produces a contract JSON; diff classifies changes as breaking/additive/info and exits non-zero on breaking. |
| **Verification** | An additive-only change exits 0. |

### 12.4 CLI (CLI)

| ID | TC-CLI-01 | Priority | P3 |
|---|---|---|---|
| **Title** | Scaffold via `init` templates |
| **Steps** | `mockagents init myproj --template customer-support` (and `--list-templates`). |
| **Expected** | Starter pack scaffolded; `--list-templates` shows basic/customer-support/rag/coding-agent/planner. |
| **Verification** | The scaffold validates and starts. |

| ID | TC-CLI-02 | Priority | P3 |
|---|---|---|---|
| **Title** | Hot-reload with `--watch` |
| **Steps** | Start with `--watch`; edit a mounted agent file; re-call. |
| **Expected** | Change is picked up without restart; an `agent.reloaded` audit entry appears. |
| **Verification** | New scenario behavior reflected in responses. |

| ID | TC-CLI-03 | Priority | P2 (regression — was broken before `888b59d`) |
|---|---|---|---|
| **Title** | `init` scaffolding inside the container |
| **Preconditions** | TC-ENV-01 passed |
| **Steps** | 1. Default workdir: `docker run --rm --entrypoint mockagents mockagents:latest init starter` (no volume mount).  2. Scaffold onto the host: `docker run --rm -v "$PWD/scaffold-out:/work" -w /work --entrypoint mockagents mockagents:latest init starter && ls scaffold-out/starter`. |
| **Expected** | (1) Scaffolds into `/data/starter` **without** `permission denied` — the image now runs from the writable `/data` (the old failure was `Error: creating directory for ".mockagents.yaml": mkdir /starter: permission denied`).  (2) Project lands on the host; `agents/`, `tests/`, `.mockagents.yaml` present. No `--force` needed on a fresh directory. |
| **Verification** | The scaffold from (2) validates: `docker run --rm -v "$PWD/scaffold-out/starter:/p:ro" mockagents:latest validate /p/agents` exits 0. |

---

## 13. GUI console suite (GUI)

> Requires the Next.js console: `make gui-dev` (port 3001) pointed at the server on 8080, or the
> deployed GUI. In single-tenant mode calls go through anonymously; in multi-tenant mode log in at `/login`.
>
> **Port 3001 busy?** (`Error: listen EADDRINUSE: address already in use :::3001`) — run on
> another port with `npx next dev --port 3002` (from `gui/`), or free 3001 first (Windows:
> `netstat -ano | findstr :3001` → `taskkill /PID <pid> /F`). See `gui/README.md` §Running.
> A port change does not affect any test expectation — the GUI talks to the API on 8080.

| ID | TC-GUI-01 | Priority | P2 |
|---|---|---|---|
| **Title** | Agent catalog & detail |
| **Steps** | Open `/`; click into an agent. |
| **Expected** | Cards show model/protocol/scenario/tool counts; detail shows scenarios, tools, raw JSON. |
| **Verification** | Matches `GET /api/v1/agents`. |

| ID | TC-GUI-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Live log feed & log detail |
| **Steps** | Open `/logs`, enable live mode; make a chat call; open a row. |
| **Expected** | New interaction streams in real time; detail shows bodies, latency, matched scenario, `cost_usd`. |
| **Verification** | Row id matches API. |

| ID | TC-GUI-03 | Priority | P3 |
|---|---|---|---|
| **Title** | Costs, audit, pipelines DAG, editor |
| **Steps** | Visit `/costs`, `/audit`, `/pipelines/[name]`, `/editor` (paste YAML). |
| **Expected** | Cost dashboard renders aggregates; audit lists events; DAG viewer renders topology; editor validates YAML inline. |
| **Verification** | Cross-check each against its API endpoint. |

| ID | TC-GUI-04 | Priority | P3 |
|---|---|---|---|
| **Title** | Multi-tenant admin pages |
| **Preconditions** | Multi-tenant mode |
| **Steps** | `/login` with a key; visit `/admin/tenants` and `/admin/tenants/[id]`; change a role; rotate a key. |
| **Expected** | Tenant list/create/delete; per-tenant key list with inline role change and Rotate button. |
| **Verification** | Actions reflected via API + audit log. |

---

## 14. SDK parity suite (SDK) — optional

| ID | TC-SDK-01 | Priority | P3 |
|---|---|---|---|
| **Title** | Python SDK against the container |
| **Steps** | Point the OpenAI/Anthropic Python SDK `base_url` at `http://localhost:8080`; call chat + `iter_stream`. |
| **Expected** | Existing SDK code works unchanged with `OPENAI_API_KEY=mock`; streaming iterates chunks. |
| **Verification** | `make test-python` green (in-process server). |

| ID | TC-SDK-02 | Priority | P3 |
|---|---|---|---|
| **Title** | TypeScript & Go SDK streaming |
| **Steps** | Use TS `iterStream` and Go `IterStream`/`NewInProcessClient`. |
| **Expected** | Protocol-agnostic streaming yields `StreamChunk`s; Go in-process client serves inline. |
| **Verification** | `make test-typescript` / Go SDK tests green. |

---

## 15. Demo application suite (DEMO) — end-to-end

> The demo apps are the primary "real client" driving the mock. Each has its own compose.

| ID | TC-DEMO-01 | Priority | P1 |
|---|---|---|---|
| **Title** | Customer-support demo — deterministic smoke |
| **Preconditions** | `demo/customer-support-agent/` |
| **Steps** | `cd demo/customer-support-agent && docker compose up --build -d mockagents && docker compose run --rm demo python -m app.deterministic_smoke`. |
| **Expected** | Demo runs its triage flow against the mock and prints deterministic, canned results (pass). |
| **Verification** | Interactions appear in the mock's `/api/v1/logs`. |

| ID | TC-DEMO-02 | Priority | P2 |
|---|---|---|---|
| **Title** | Customer-support demo — streaming |
| **Steps** | `docker compose run --rm demo python -m app.streaming_demo`. |
| **Expected** | Streaming responses render incrementally; completes without error. |
| **Verification** | SSE frames observed; final content coherent with the agent scenario. |

| ID | TC-DEMO-03 | Priority | P2 |
|---|---|---|---|
| **Title** | Customer-support demo — resilience (chaos) |
| **Steps** | `docker compose run --rm demo python -m app.resilience_demo`. |
| **Expected** | Demo exercises ret/backoff against injected faults and recovers per its logic. |
| **Verification** | Fault responses (429/5xx/latency) visible in logs; demo handles them. |

| ID | TC-DEMO-04 | Priority | P3 |
|---|---|---|---|
| **Title** | Claude & Google-ADK & Responses demos |
| **Steps** | Repeat the smoke flow for `demo/customer-support-agent-claude/`, `demo/customer-support-agent-google-adk/`, `demo/responses-api-agent/`. |
| **Expected** | Each demo works against its target protocol (Anthropic / Google ADK / Responses+Conversations). |
| **Verification** | Cross-check `demo/*/TESTING.md` steps where present. |

---

## 16. Regression focus — fidelity fixes (rounds 2–5)

These target shipped fidelity fixes; run them after any change to Realtime or A2A. Each row
links the manual case that exercises the fix.

| ID | Linked case | Focus (fix round) |
|---|---|---|
| TC-REG-01 | TC-RT-02 | Realtime function-call honors `raw_arguments` verbatim (round 2, INT-1). |
| TC-REG-02 | TC-A2A-04 | JSON-RPC error responses render `"id":null`; id-less `message/stream` → 204 (round 2, INT-2). |
| TC-REG-03 | TC-RT-01 | Every Realtime event carries a unique `event_id`; GA session object shape; `conversation.item.added`/`.done` pair (rounds 2–3). |
| TC-REG-04 | TC-A2A-01 | Agent Card defaults for `version`/`description`/`skills`; `tags` never null; no clobber (round 2). |
| TC-REG-05 | TC-RUN-02 | Runner `max_ms` (`latency_ms_lt`) requires ≥ 1; `tool_call_args` type-tolerant dotted paths (round 2). |
| TC-REG-06 | TC-RT-05 | Server VAD arc: real-audio detection at the GA threshold scale, `speech_started` pre-announces the committed item id, turn_detection validation with `param` paths, idle timeout fires once (rounds 3–4). |
| TC-REG-07 | TC-RT-06 | Cancellation close-out: delta-concatenation invariant, no phantom/unannounced items, `usage` bills only streamed words, interrupted item retrievable as `incomplete` (rounds 4–5). |
| TC-REG-08 | TC-RT-08 | `session.update` mid-speech keeps the turn; voice locked after assistant audio (`cannot_update_voice` verbatim); `max_output_tokens` range validation (round 5). |
| TC-REG-09 | TC-RT-04 | Legacy `/v1/realtime/sessions` = beta-flat shape + 60 s key expiry; GA `client_secrets` envelope + 600 s default (rounds 4–5). |
| TC-REG-10 | TC-RT-07 | Delete-tail chain repair (`previous_item_id` never dangles); truncate error shape (code null, verbatim message); OOB items not retrievable (round 5). |
| TC-REG-11 | TC-CLI-03 | Container `init` scaffolds without `permission denied` — runtime image runs from writable `/data` (`888b59d`, QA cycle-1 #1). |
| TC-REG-12 | TC-ENV-05 / TC-ENV-06 | Interaction + audit logging ENABLED in Docker; DBs land in the `/data` volume; `MOCKAGENTS_DATA_DIR` relocates state; disabled-logging WARNs carry path + hint (`888b59d`, QA cycle-1 #4). |
| TC-REG-13 | TC-RUN-03 | `mockagents test` expands quoted globs in-process; unmatched pattern errors instead of running zero suites (`888b59d`, QA cycle-1 #2). |
| TC-REG-14 | §4.2 note | Model-collision startup WARN names the winning agent (`wins=`) + fix hint; observe it by loading two agents with the same `model:` (`888b59d`, QA cycle-1 #3). |

---

## 17. Execution tracker

Track per-run status in `test-execution-tracker.csv` (same folder) — one row per test case.
Columns: `TestCaseID, Suite, Title, Priority, Status, ExecutedBy, Date, ActualResult, DefectID, Notes`.
Summarize each cycle here:

| Cycle | Date | Build | Total | Pass | Fail | Blocked | N/A | P1 pass % | Sign-off |
|---|---|---|---|---|---|---|---|---|---|
| 1 | | | | | | | | | |

## 18. Defect log

| Defect ID | Test Case | Severity | Summary | Steps to reproduce | Status | Owner |
|---|---|---|---|---|---|---|
| MA-DEF-001 | TC-CLI-03 | Sev-2 | `mockagents init` in container: `mkdir /starter: permission denied` (runtime image had no writable WORKDIR; ran from `/` as non-root) | `docker run --rm --entrypoint mockagents mockagents:<pre-888b59d> init starter` | **Fixed** `888b59d` (WORKDIR /data) — verify via TC-REG-11 | Eng |
| MA-DEF-002 | TC-ENV-05 | Sev-2 | Interaction + audit logging silently disabled in Docker: `unable to open database file: out of memory (14)` (= SQLITE_CANTOPEN, DBs written to unwritable `/`; log GUI pages empty) | Start pre-`888b59d` container; check startup WARNs; `/api/v1/logs` empty | **Fixed** `888b59d` (WORKDIR /data + `MOCKAGENTS_DATA_DIR` + hint in WARN) — verify via TC-REG-12 | Eng |
| MA-DEF-003 | TC-RUN-03 | Sev-3 | `mockagents test /tests/*.yaml` fails in zsh (`no matches found`) — CLI did not expand globs itself | Run an unquoted glob in zsh against a container path | **Fixed** `888b59d` (in-process expansion; quote the pattern) — verify via TC-REG-13 | Eng |
| MA-DEF-004 | §4.2 | Sev-3 | Multi-agent model-claim WARN did not say which agent wins; QA had to isolate agents to identify routing | Load two agents with the same `model:`; read the WARN | **Fixed** `888b59d` (WARN now has `wins=` + hint); behavior itself was by-design — verify via TC-REG-14 | Eng |
| MA-DEF-005 | §13 note | Sev-3 (env) | GUI `npm run dev` fails `EADDRINUSE :3001` when the port is taken | Occupy 3001; `npm run dev` in `gui/` | **Documented** (`gui/README.md` + §13) — env issue, not a product defect | QA |

## 19. Risks & assumptions

- Responses are canned; a test that asserts "LLM-quality" text is invalid — assert structure/exact content.
- MCP and A2A run as **separate processes/modes** (`mockagents mcp` / `mockagents a2a`), not the
  default `start` server — budget extra containers/commands. Realtime IS part of the main server
  (`GET /v1/realtime` on 8080).
- WebSocket and SSE cases need a client that doesn't buffer (`curl -N`, `websocat`).
- The mock has no STT: committed audio always becomes the fixed transcript `[audio input]`, so
  VAD-driven turns match the agent's **default** scenario. To exercise a specific scenario over
  Realtime, send text via `conversation.item.create` instead.
- Multi-tenant, quota, and audit-auth cases require `MOCKAGENTS_MULTI_TENANT=1` and are skipped in single-tenant runs.
- Timing-based cases (chaos latency, stream physics) are statistical — allow tolerance and repeat.

## 20. Sign-off

| Role | Name | Date | Result |
|---|---|---|---|
| QA Engineer | | | |
| QA Lead | | | |
| Eng Owner | | | |
