# MockAgents

**Make your agent fail on purpose.** Simulate hallucinations, malformed tool calls, broken streams, and flaky MCP/A2A peers — offline, deterministically, with zero LLM calls.

[![CI](https://github.com/mockagents/mockagents/actions/workflows/ci.yml/badge.svg)](https://github.com/mockagents/mockagents/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mockagents/mockagents)](https://goreportcard.com/report/github.com/mockagents/mockagents)
[![MCP Conformance](https://github.com/mockagents/mockagents/actions/workflows/mcp-conformance.yml/badge.svg)](https://github.com/mockagents/mockagents/actions/workflows/mcp-conformance.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

MockAgents is a single pure-Go binary that stands in for the services your agent
talks to — the OpenAI, Anthropic, and Google Gemini HTTP APIs, MCP servers, and
A2A agents. Point your app's base URL at it and your existing SDK code just
works, returning **deterministic** responses, simulated tool calls, and real SSE
streams with **no real LLM calls**. Your tests stop costing a dollar a run, stop
flaking on model updates, and run fully offline.

That much is table stakes. The reason to reach for MockAgents is the other half:
it produces the failures a real model **won't produce on demand** — a
confidently-wrong answer, a tool call carrying malformed JSON arguments, a
response truncated mid-stream, a refusal, an MCP peer that starts rate-limiting
under load. Those are the paths your guardrails and retry logic exist for, and
the paths that are otherwise nearly impossible to cover in CI. You can't ask
GPT-4o to hallucinate on cue. You can ask MockAgents.

<!-- TODO(RR-03): embed a 12-second GIF of `mockagents start` + the web console streaming SSE. -->

## 60-second start

```bash
go install github.com/mockagents/mockagents/cmd/mockagents@latest   # needs Go 1.26+
mockagents start                                       # 1. run the mock
export OPENAI_BASE_URL=http://localhost:8080/v1        # 2. the ONLY change to your app
export OPENAI_API_KEY=mock
python my_existing_app.py                              # 3. it works — free, offline, deterministic
```

No Go toolchain? Grab a prebuilt binary for macOS / Linux / Windows (amd64 and
arm64) from [Releases](https://github.com/mockagents/mockagents/releases), then
skip to step 1.

That's the whole idea: **swap the base URL, change nothing else.** Works with
the official OpenAI / Anthropic / Google SDKs, LangChain, LlamaIndex, the Vercel
AI SDK — anything that talks these APIs over HTTP.

### Install

| Method | Command | |
|---|---|---|
| Go | `go install github.com/mockagents/mockagents/cmd/mockagents@latest` | ✅ |
| Binary | [GitHub Releases](https://github.com/mockagents/mockagents/releases) | ✅ |
| npx | `npx mockagents start` | ⏳ |
| pipx | `pipx run mockagents start` | ⏳ |
| Homebrew | `brew install mockagents/tap/mockagents` | ⏳ |
| Docker | `docker run -p 8080:8080 mockagents/mockagents` | ⏳ |

⏳ **Not yet published.** These packages are built and versioned in-tree, but the
v0.4.0 release pipeline failed partway through, so the registries have nothing to
serve — they will 404 until the next release completes. Use Go or a prebuilt
binary in the meantime. The
[install-paths workflow](https://github.com/mockagents/mockagents/actions/workflows/install-paths.yml)
checks every row on this table daily and is the source of truth for this column.

**SDKs** (client libraries, not the server) — the Go SDK works today via
`go get github.com/mockagents/mockagents/sdk/go/mockagents`. The Python
(`mockagents`) and TypeScript (`@mockagents/sdk`) packages are ⏳ pending the same
release.

**Test-runner helpers** (auto-spawn the server + redirect the provider SDKs):
`@mockagents/vitest` for [Vitest/Jest](sdk/vitest/README.md), and the bundled
`pytest` plugin in the Python SDK — both ⏳ pending publication.

```bash
mockagents init my-project && cd my-project   # scaffold an example agent
# ...or start from a curated pack:
mockagents init my-bot --template customer-support   # see `--list-templates`
mockagents start                              # prints your base URL + a ready-to-paste snippet
```

## Then write the test

Nothing to import and no fixture to wire up. The Python SDK registers a pytest
plugin, so the `mockagents` fixture exists in any test session: it starts one
server for the session against `./agents` and points the OpenAI / Anthropic /
Gemini SDKs at it. Your application code runs unchanged.

```python
def test_router_picks_the_weather_tool(mockagents):
    from openai import OpenAI                      # your real app code,
    reply = OpenAI().chat.completions.create(      # unchanged
        model="gpt-4o",
        messages=[{"role": "user", "content": "what's the weather?"}],
    )
    assert reply.choices[0].message.tool_calls[0].function.name == "get_weather"
```

Then assert the part that actually breaks in production — the **trajectory**:
the ordered shape of what the agent *did*, not just the text it ended with.

```python
from mockagents import Scenario, expect, run_scenario

def test_support_flow(mockagents_client):
    result = run_scenario(mockagents_client, Scenario(
        name="support",
        steps=[
            {"role": "user", "content": "what's the weather in London?"},
            {"role": "user", "content": "and where is my order?"},
        ],
    ))
    expect(result).to_have_tool_call_sequence(["get_weather", "search_orders"])
    expect(result).to_have_tool_call_count(2)
    expect(result).to_have_tool_call("get_weather", {"city": "London"})
```

Wrong tool, wrong order, one call too many: the three bugs a text assertion
cannot see. The same checks in TypeScript, where `setupMockAgents()` does the
spawn-and-redirect for Vitest or Jest:

```ts
import { Scenario, expect as expectAgent, runScenario } from "@mockagents/sdk";
import { setupMockAgents } from "@mockagents/vitest";
import { test } from "vitest";

const mock = setupMockAgents({ agentsDir: "./agents" });

test("support flow", async () => {
  const result = await runScenario(mock.client, new Scenario({
    name: "support",
    steps: [
      { role: "user", content: "what's the weather in London?" },
      { role: "user", content: "and where is my order?" },
    ],
  }));
  expectAgent(result)
    .toHaveToolCallSequence(["get_weather", "search_orders"])
    .toHaveToolCallCount(2)
    .toHaveToolCall("get_weather", { city: "London" });
});
```

Both read the **aggregate across every turn** and compare the sequence for full
equality — an unexpected extra call fails it — which is exactly what the
`tool_call_sequence` assertion in `kind: TestSuite` YAML does. A check written
in your test file transfers to `mockagents test` unchanged, and back.

The agent behind both samples is
[`examples/tool-routing-agent.yaml`](examples/tool-routing-agent.yaml). Put it
in `./agents` **by itself** and the tests above pass as written — over the
provider HTTP surfaces an agent is picked by `spec.model`, so two agents sharing
`gpt-4o` in one directory would make the routing ambiguous (the server warns and
picks the first name alphabetically). Both SDK packages are ⏳ pending
publication (see the table above); until then, install from the repo with
`pip install -e sdk/python`.

→ **[Testing AI Agents guide](site/docs/guides/testing-agents.md)** · **[Python SDK](site/docs/sdk/python-sdk.md)** · **[@mockagents/vitest](sdk/vitest/README.md)**

## Why MockAgents

### The failures a real model won't produce on demand

- **Hallucination fixtures** — return a deterministic confidently-wrong / ungrounded /
  fabricated output (advertised via a response header) to test that your guardrails
  catch it — something a real model won't do on demand.
- **Semantic error modes** — well-formed responses that break agents *after* the
  200: `finish_reason: length` truncation, assistant refusals, and tool calls with
  malformed JSON arguments ([cookbook example](examples/semantic-errors-agent.yaml)).
- **Chaos & fault injection** — inject latency, errors, and rate limits per agent
  to test the unhappy paths.
- **Strict tools mode** (`strict_tools`) — opt into failing like
  production: round-trip tool id validation (orphan/mismatched
  `tool_call_id`/`tool_use_id`/`call_id` → the provider's real 400),
  `tool_choice: required`/named forcing with forced-call synthesis and
  `finish_reason: "stop"`, `parallel_tool_calls: false` capping, and
  `strict: true` schema-subset validation — per agent or fleet-wide via
  `MOCKAGENTS_STRICT_TOOLS=off|warn|strict` (`warn` = header-only).

### Agent protocols, not just chat endpoints

- **Mocks MCP servers too** — test agents that call Model Context Protocol
  servers, deterministically (JSON-RPC 2.0 + bidirectional SSE).
- **Manage agents over MCP** — `mockagents mcp --manage` exposes the agent write
  API as MCP tools (`list_agents`, `get_agent`, `validate_agent`, `create_agent`,
  `put_agent`, `delete_agent`), so an MCP client can create, inspect, and edit
  your mock fixtures conversationally.
- **Mocks A2A servers too** (`kind: A2AServer`, `mockagents a2a`) — test
  multi-agent systems that speak Google's Agent2Agent protocol: serves the Agent
  Card at `/.well-known/agent-card.json` and answers `message/send` / `tasks/get`
  / `tasks/cancel` with canned, match-based task responses.
- **Multi-agent pipelines** (`kind: Pipeline`) — sequential, parallel, and graph
  topologies with conditional edges.
- **Agent-trajectory assertions** — assert the *shape* of an agent's behavior,
  not just its text: `tool_call` (name + partial args), `tool_call_count`,
  `tool_call_sequence` (ordered tool names), and `node_sequence` (the ordered
  pipeline nodes that ran) — deterministic checks for the wrong-tool /
  wrong-count / wrong-order bugs that belong on every PR. Available both in
  `kind: TestSuite` YAML via `mockagents test` **and natively in your own
  pytest / Vitest / Jest suite** through the SDKs, with matching semantics.
  (`node_sequence` is YAML-only — pipelines have no HTTP execution surface yet,
  [#33](https://github.com/mockagents/mockagents/issues/33).)
- **Tool-call simulation** — return canned tool calls on every protocol surface;
  test your agent's routing and argument handling without a live model. (Tool
  `responses:` tables resolve results for the test runner and MCP servers — on
  the HTTP protocol endpoints your client executes the tools, as with real APIs.)
- **Scenario matching** — route by message content, regex, or turn number;
  assert *which* path fired, not just the text.

### Provider API coverage

- **OpenAI + Anthropic + Gemini parity** — real response shapes, `tool_calls` /
  `tool_use` / `functionCall`, `usage` token counts, and SSE streaming.
- **OpenAI Responses API** (`/v1/responses`) — the default OpenAI Agents SDK
  transport: typed output items, the full `response.*` streaming-event ladder,
  and stateful `previous_response_id` multi-turn loops.
- **OpenAI Conversations API** (`/v1/conversations` + `/items`) — the stateful
  companion to Responses and the Assistants-Threads replacement: create a
  conversation, then drive a multi-turn loop by passing its id on each
  `/v1/responses` call (prior items replay as context; each turn appends back).
- **OpenAI Realtime API over WebSocket** (`/v1/realtime`) — test **voice agents**
  offline: connect over a WebSocket, send the Realtime events, and get back the
  full streamed response ladder with a deterministic transcript (from your
  scenarios) and synthesized audio deltas — no TTS, no audio model, fully
  reproducible.
- **OpenAI Embeddings** (`/v1/embeddings`) — deterministic, unit-normalized
  vectors (stable across runs), configurable `dimensions`, `float`/`base64`
  encoding, and usage tokens — zero-config, no agent definition needed.
- **Structured outputs** (`response_format`) — `json_schema` strict mode returns
  schema-conforming JSON your SDK `.parse()` (Pydantic/Zod) round-trips,
  synthesized from the request schema; `json_object` mode + a refusal path too.
- **Moderations** (`/v1/moderations`) — deterministic omni-moderation responses
  (`flagged` + 13 category scores) for testing guardrail pipelines offline:
  known-harmful phrases flag the right category, benign text stays clean.
- **Files + Batch API** (`/v1/files`, `/v1/batches`) — run the full OpenAI batch
  flow offline: upload a request JSONL, create a batch over
  `/v1/chat/completions`, `/v1/embeddings`, or `/v1/responses`, poll it to
  `completed`, and download the `output_file`. Each line is replayed through the
  live endpoint (so a batched response matches the synchronous one); a
  configurable processing delay lets a poll loop observe `in_progress`.
- **Anthropic Message Batches** (`/v1/messages/batches`) — the inline sibling of
  the OpenAI Batch API: submit requests inline (no Files upload), poll
  `processing_status` to `ended`, then stream the per-request `results` JSONL.
  Each request's `params` is replayed through the live `/v1/messages` handler;
  cancel/delete and the same `X-Mockagents-Batch-Delay-Ms` poll-observation knob
  are supported.
- **Azure OpenAI URLs** — point an `AzureOpenAI()` client at the mock unchanged:
  the `/openai/deployments/{deployment}/…` and `/openai/v1/…` surfaces route to
  the OpenAI handlers (deployment name → model; `api-version` ignored).
- **Anthropic depth** — `/v1/messages/count_tokens`, prompt-caching usage
  (`cache_creation`/`cache_read`, driven by `cache_control`), and
  extended-thinking blocks — to test cost-cache and thinking-trace handling
  offline.
- **Vision input** — OpenAI `image_url` (incl. `data:` URLs) and Anthropic
  base64/url image parts are parsed; match on image presence via the `has_image`
  scenario rule and read the count from the `X-Mockagents-Image-Count` header.
### Fits your CI

- **One static binary, no runtime** — no Node, JVM, Python, or GPU. Drop it into
  any CI in seconds (pure-Go, no cgo).
- **Record & replay** — capture real upstream traffic once, replay it offline
  forever (SSE streams included).
- **Contract testing** — extract an agent contract as JSON; diff breaking changes
  in CI.
- **Three SDKs** (Python / TypeScript / Go) with streaming helpers; the Go SDK
  runs an engine **in-process** with no subprocess.
- **Batteries included** — CLI, web console, Docker image, Helm chart, and
  GitHub Actions / GitLab CI templates.

## How it compares

> Best-effort comparison as of mid-2026 — these projects move fast. A cell that's
> wrong or stale? [Open a PR](CONTRIBUTING.md) and we'll fix it.

| | MockAgents | [CopilotKit/aimock](https://github.com/CopilotKit/aimock) | [WireMock](https://wiremock.org) (OSS) | [mockllm](https://github.com/StacklokLabs/mockllm) | [ai-mocks](https://github.com/mokksy/ai-mocks) |
|---|---|---|---|---|---|
| **Language / requires** | Go binary, no runtime | Node.js | JVM 17+ | Python 3.10+ | JVM 17+ (Kotlin/Ktor) |
| **LLM-API-specific** | Yes | Yes | No — general HTTP mock | Yes | Yes |
| **Providers** | OpenAI, Anthropic, Gemini (+ Azure, Responses, Embeddings, Moderations) | 14 incl. OpenAI, Anthropic, Gemini, Bedrock, Vertex, Ollama | Any HTTP API via stubs (LLM templates exist) | OpenAI, Anthropic | OpenAI, Anthropic, Gemini, Ollama |
| **SSE streaming** | Per-token pacing + TTFT/ITL physics + stream faults | Yes (approx. timing) | Limited¹ | Yes (char-level) | Yes — headline feature |
| **Tool-call simulation** | Canned `tool_calls`/`tool_use`/`functionCall` | Replayed tool rounds (fixtures) + MCP | No | No | No |
| **Scenario matching** | Declarative YAML: content, regex, turn, `has_image` | JSON fixtures | Handlebars templating | YAML prompt→response | Kotlin DSL matchers (content/param) |
| **MCP mocking** | JSON-RPC 2.0 + bidirectional SSE | Yes (+ A2A) | No | No | A2A protocol |
| **Chaos / fault injection** | Latency dists, HTTP errors, rate-limit, connection-layer faults, stream truncation | Errors, disconnects, malformed | Delays + bad-response faults (OSS); more in Cloud | Latency only | Delays, errors, malformed, timeouts |
| **Record & replay** | SSE cassettes, redaction, record-on-miss, importers (vcrpy / OpenAI stored) | Timing-aware fixtures, drift detection | No | No | No |
| **Contract testing** | Extract + diff in CI | No | No | No | No |
| **Multi-agent pipelines** | Sequential / parallel / graph | No | No | No | No |
| **Run model** | Standalone server + Go in-process SDK | Standalone (npx / Docker) | Standalone or embedded in JUnit | Standalone server | Embedded in Gradle tests |
| **License** | Apache-2.0 | MIT | Apache-2.0 | Apache-2.0 | Apache-2.0 |
| **Open-core / paid tier** | **No** — fully OSS, no account | No | Yes — WireMock Cloud (paid SaaS) | No | No |

¹ WireMock's genuine strength is being the most battle-tested *general-purpose* HTTP
mock available — if your stack isn't LLM-specific, it's an excellent choice.

**MockAgents isn't always the right tool.** Need a general HTTP mock for non-LLM
APIs? Use **WireMock**. Mocking inside a **JVM/Kotlin** test suite? **ai-mocks**
fits cleanly in-process. Want the **widest provider count** (Bedrock, Ollama,
ElevenLabs, vector DBs)? **aimock** covers more surface. What MockAgents commits to:
a single static Go binary, real LLM wire-protocol fidelity, and **Apache-2.0 with no
account, no open-core, no paid tier** — ever. When
[LocalStack moved its free tier behind an account and non-commercial-only terms in 2026](https://blog.localstack.cloud/the-road-ahead-for-localstack/),
teams that relied on it had to migrate or pay. MockAgents is committed to never
putting you in that position.

## Define an agent (YAML)

```yaml
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support-agent
spec:
  protocol: openai-chat-completions   # or anthropic-messages | google-gemini
  model: gpt-4o
  tools:
    - name: lookup_order
      parameters:
        type: object
        properties:
          order_id: { type: string }
        required: [order_id]
      responses:
        - match: { order_id: "ORD-123" }
          response: { status: shipped, tracking: "1Z999" }
        - default: true
          response: { status: processing }
  behavior:
    scenarios:
      - name: order-query
        match: { content_contains: "order" }
        response:
          content: "Let me look that up."
          tool_calls:
            - name: lookup_order
              arguments: { order_id: "ORD-123" }
      - name: default
        response:
          content: "How can I help you today?"
    streaming: { enabled: true, chunk_size: 4 }
```

## Run it in CI

The [`setup-mockagents` GitHub Action](deploy/actions/setup-mockagents) starts
the server and exports the base URLs for the rest of the job, so the tests from
[Then write the test](#then-write-the-test) run in CI with no extra wiring:

```yaml
- uses: mockagents/mockagents/deploy/actions/setup-mockagents@main
  with:
    agents-dir: ./agents
- run: pytest              # OPENAI_BASE_URL etc. already point at the mock
```

→ **[Testing AI Agents guide](site/docs/guides/testing-agents.md)** — runnable
cookbooks for asserting agent **tool-calls** (right tool, right arguments) and
**mocking an MCP server**, deterministically and offline.

## A complete example: RAG with a guardrail that fires

[`demo/rag-agent/`](demo/rag-agent) is a small retrieval-augmented-generation
app plus a ten-test suite. Clone, one command, green in seconds — no network,
no tokens, no compose file:

```bash
make build && cd demo/rag-agent
pip install -r requirements.txt && pip install -e ../../sdk/python
MOCKAGENTS_BIN=../../mockagents pytest        # 10 passed in 5.53s
```

The happy path is the boring part. The suite also pins an **empty index**, a
**low-confidence-only result set**, and a model that answers the refund question
with a confident, plausible, completely ungrounded claim — citing `doc-7`, a
document retrieval never returned. The app's citation check catches it. In a
suite running against a live model that branch is dead code nobody has ever
executed, because the model has to misbehave first and it won't on request.

Retrieval is mocked as an MCP tool server, not a vector store — [VectorMock is
planned but does not exist yet](docs/ADOPTION_REQUIREMENTS.md) — and the demo
says so rather than implying otherwise. CI runs it on every push.

## What it is *not*

MockAgents mocks the **wire protocol, not the model**. It won't tell you whether
your prompt is *good* — only that your code handles the API correctly. Pair it
with an eval tool (promptfoo, Braintrust, LangSmith, DeepEval) for output
quality.

Those are two different questions, and most teams have one of them and think
they have coverage: **evals measure whether the model is good enough; tests
measure whether your code is correct.** You cannot ask a real model to emit
malformed tool arguments on cue, and you cannot ask a mock whether an answer is
any good. → **[Evals vs. tests](docs/EVALS_VS_TESTS.md)**, the short opinionated
version.

---

## CLI Commands

```bash
mockagents init [name] [--template <pack>|--list-templates]   # Scaffold a new project from a starter pack
mockagents start [--watch]     # Start the mock server (-w = fsnotify auto-reload)
mockagents validate [path]     # Validate agent definitions
mockagents add <file> [--replace] [--server URL] [--api-key KEY]   # Hot-add/replace an agent on a running server
mockagents rm <name> [--server URL] [--api-key KEY]               # Delete an agent from a running server
mockagents logs                # Query interaction logs
mockagents test [path] [--format text|json|junit]   # Run TestSuite YAML
mockagents record              # Proxy a real upstream LLM API and record to a cassette
mockagents replay              # Serve a recorded cassette over the mock endpoints
mockagents mcp                 # Serve a kind:MCPServer definition over HTTP or stdio
mockagents contract            # extract or diff agent contracts (CI-friendly)
```

`mockagents start` binds to `127.0.0.1` by default. Container and remote
deployments should opt in explicitly with `--host 0.0.0.0` or
`MOCKAGENTS_HOST=0.0.0.0`. `mockagents start --watch` adds an fsnotify hot-reload
loop: saving any agent YAML re-parses, validates, and re-registers it without
restarting; validation failures are logged and the previous known-good
definition is preserved.

On-disk state (the `.mockagents.db` interaction log, `.mockagents-audit.db`
audit trail, and `.mockagents-tenancy.db`) is written to the working directory
by default; set `MOCKAGENTS_DATA_DIR=/path/to/dir` to relocate it — required
when the working directory isn't writable (read-only containers). The official
Docker image runs from the `/data` volume, so state persists across container
restarts out of the box.

## CI integration

`mockagents test --format junit > report.xml` produces a Jenkins-compatible
JUnit XML file that drops straight into any test-reporter that speaks JUnit. The
project ships ready-to-use wrappers for the two most common CI hosts.

**GitHub Actions** — two composite actions under `deploy/actions/`:

- [`setup-mockagents`](deploy/actions/setup-mockagents/README.md) installs the
  CLI, starts the mock as a background service, and exports `OPENAI_BASE_URL` /
  `ANTHROPIC_BASE_URL` for the rest of the job — point your existing test suite
  at it with no code changes. Pass `source-path: ${{ github.workspace }}` to
  build the CLI from a checkout instead of a published release.
- [`mockagents-test`](deploy/actions/mockagents-test) installs the CLI, validates
  agents, runs a TestSuite, and exposes the JUnit path as a step output:

```yaml
- uses: mockagents/mockagents/deploy/actions/mockagents-test@main
  id: mockagents
  with:
    agents-dir: ./agents
    suites: ./tests
- uses: mikepenz/action-junit-report@v5
  if: always()
  with:
    report_paths: ${{ steps.mockagents.outputs.junit-report }}
```

**GitLab CI** — include the template under `deploy/ci/gitlab-ci.yml`; the
`mockagents:test` job installs the binary, validates agents, writes JUnit XML,
and attaches it as a GitLab `artifacts.reports.junit` so results show up in the
Merge Request UI automatically.

## Mock MCP Server

MockAgents ships a JSON-RPC 2.0 Model Context Protocol mock with two transports
— the current **Streamable HTTP** transport (a single `/mcp` endpoint answering
POST/GET/DELETE) and stdio (line-delimited JSON) — so you can develop and test
MCP clients without standing up a real server. A `kind: MCPServer` definition
declaratively lists tools, resources, and prompts; tool calls resolve via the
same match/default pattern used by LLM agents.

```bash
# HTTP transport
mockagents mcp --transport http --port 8081 --agents-dir examples

# stdio transport (for clients that spawn the server as a subprocess)
mockagents mcp --transport stdio --agents-dir examples --server weather-mcp
```

Over HTTP the `/mcp` endpoint implements the MCP **Streamable HTTP** transport
(protocol revision `2025-11-25`): `POST` a JSON-RPC message (the server replies
with `application/json`, or an SSE stream when the client sends
`Accept: …text/event-stream`), open a `GET` for the resumable server→client SSE
stream (`Last-Event-ID` replays missed events), and `DELETE` to end the session.
The `initialize` response mints an `Mcp-Session-Id` the client must echo on
later requests; the `Origin` and `MCP-Protocol-Version` headers are validated. A
plain POST-JSON transport (no sessions) remains at `/mcp/rpc`.

Supported methods (v0.3): `initialize`, `tools/list`, `tools/call`,
`resources/list`, `resources/read`, `resources/subscribe`,
`resources/unsubscribe`, `prompts/list`, `prompts/get`, `completion/complete`,
`logging/setLevel`, `ping`, and `notifications/initialized`. Tool and prompt
content blocks may be `text`, `image`, `audio`, or an embedded `resource`
(emitted as the spec's `{type:"resource", resource:{…}}` shape). Server-initiated
calls (`sampling/createMessage`, `roots/list`) flow through a bidirectional SSE
transport: clients subscribe to `GET /mcp/events`, read incoming JSON-RPC
requests, and POST their responses to `POST /mcp/response`. Test harnesses can
drive the outbound side directly via `POST /mcp/sample` or `POST /mcp/roots`. See
`examples/weather-mcp.yaml`.

### Conformance-validated

The Streamable-HTTP server is exercised in CI by the official
[MCP conformance suite](https://www.npmjs.com/package/@modelcontextprotocol/conformance)
(`mcp-conformance` workflow): it serves the `conformance/server/` fixture and
runs `@modelcontextprotocol/conformance server` against `/mcp`, gated by
`conformance/expected-failures.yml`. All static-content scenarios pass —
initialize, ping, `tools/list` + `tools/call` (text / image / audio / embedded
resource / mixed / error), `resources/{list,read,subscribe,unsubscribe}`,
`prompts/{list,get}`, `completion/complete`, multi-stream SSE, and DNS-rebind
protection. The baseline lists the scenarios a *static* declarative mock can't
model (server-initiated sampling / elicitation / progress / log notifications
mid-call, and stateful URI templates); a new regression — or a baselined
scenario that starts passing — fails the build. Run it locally:

```bash
mockagents mcp --transport http --port 8081 --agents-dir conformance/server &
npx @modelcontextprotocol/conformance server \
  --url http://127.0.0.1:8081/mcp \
  --expected-failures conformance/expected-failures.yml
```

## Framework Adapters (Python)

`mockagents.adapters` exposes zero-boilerplate factories that point LangChain /
LangGraph / CrewAI at a running MockAgents server. The framework packages are
optional dependencies — install only the one you need:

```bash
pip install 'mockagents[langchain]'   # LangChain + LangGraph
pip install 'mockagents[crewai]'      # CrewAI
```

```python
from mockagents import MockAgentServer
from mockagents.adapters import chat_openai, crewai_mock_llm, patched_env

with MockAgentServer(agents_dir="./agents") as server:
    llm = chat_openai(server, model="gpt-4o")          # LangChain
    llm.invoke("hello")

    with patched_env(server):                          # any framework reading OPENAI_BASE_URL
        ...

    crewai_llm = crewai_mock_llm(server, model="gpt-4o")  # CrewAI
```

Each factory forwards extra kwargs to the underlying framework class, so
temperature, max_tokens, custom headers, etc. still work.

## Record and Playback

**The fastest on-ramp** — don't hand-write YAML, record your real provider
traffic once and replay it forever. Reach for hand-authored agents only for the
cases you can't record (synthetic edge cases, faults). Cassettes are JSON-lines
files — safe to diff, check in, and grep.

```bash
# 1. Record against a real upstream (keys stay on your machine, never in the cassette)
mockagents record --upstream https://api.openai.com --cassette fixtures/gpt4o.jsonl --api-key "$OPENAI_API_KEY"

# ...point your SDK at http://localhost:8080 and run your flow...

# 2. Replay offline, deterministically
mockagents replay --cassette fixtures/gpt4o.jsonl
```

Request matching canonicalizes JSON (sorted keys) so SDK reorderings still hit
the cassette. Streaming (SSE) responses are captured and replayed faithfully
(v0.2+). Unknown requests during replay return 404 with the SHA-256 prefix of
the miss so you can diff.

→ **[Record & Replay guide](site/docs/guides/record-replay.md)** — the full
record → replay → graduate-to-YAML workflow.

## Chaos Engineering

Every agent can inject faults via a `spec.behavior.chaos` block. Three
independent knobs:

- **`latency`** — `fixed`, `uniform` (`min_ms`/`max_ms`), or `normal`
  (`mean_ms`/`stddev_ms`) distributions sleep the response before returning.
- **`errors`** — probability-gated injection of HTTP errors (`status_code` or a
  list of `status_codes`), plus an optional `timeout` mode that sleeps for
  `timeout_ms` and returns a synthetic 504.
- **`rate_limit`** — rolling-window token bucket (`requests` per `window_ms`)
  that returns `429 Too Many Requests` with `Retry-After` when exceeded.

Chaos is evaluated inside the engine before tool resolution, so it works
identically across the OpenAI, Anthropic, and Gemini endpoints. See
`examples/chaos-agent.yaml`.

**Streaming faults & timing** — the `streaming` block adds SSE-level physics and
fault injection: `ttft_ms` (time to first token), `tokens_per_sec` (paced
output), `jitter_ms`, plus `truncate_after_chunks` (cut the stream mid-flight,
no `[DONE]`) and `malformed` (emit an invalid-JSON frame) to test client
robustness. See `examples/stream-faults-agent.yaml`.

## Multi-Agent Pipelines

MockAgents supports multi-agent topologies via `kind: Pipeline`. A pipeline
references agents by name and wires them in `sequential`, `parallel`, or `graph`
topologies (with substring-matched conditional edges). TestSuite files (`kind:
TestSuite`) declare cases with assertions (`tool_call`, `response_contains`,
`scenario_matched`, `latency_ms_lt`) targeting either an agent or a pipeline and
execute under `mockagents test`. See `examples/research-pipeline.yaml` and
`examples/research-suite.yaml`.

## Contract Testing

Agent definitions double as public contracts. `mockagents contract extract`
writes the canonical consumer-visible surface (protocol, tools with input
schemas, scenarios, streaming) as JSON so it can be checked into git;
`mockagents contract diff` compares two contracts and exits non-zero when
breaking changes are detected — safe to drop into a CI pipeline.

```bash
mockagents contract extract agents/support.yaml -o contracts/support.json
mockagents contract diff contracts/support.json agents/support.yaml   # fails on drift
```

Severity rules: removing a tool/scenario, tightening `required`, changing a
property's schema, or disabling streaming are **breaking**. Adding a tool,
relaxing `required`, or adding a scenario are **additive**. Description and
model-name changes are **info**.

## Observability

`GET /metrics` serves Prometheus text exposition with no flag to turn on:
`mockagents_requests_total{protocol,agent,status}`,
`mockagents_request_duration_seconds{protocol}` (histogram),
`mockagents_scenario_matches_total{agent,scenario,kind}` — where
`kind=fallback` means a request hit no fixture at all — and
`mockagents_chaos_injections_total{agent,kind}`.

```console
$ curl -s localhost:8080/metrics | grep chaos_injections
mockagents_chaos_injections_total{agent="flaky-agent",kind="error"} 3
mockagents_chaos_injections_total{agent="flaky-agent",kind="latency"} 17
mockagents_chaos_injections_total{agent="flaky-agent",kind="rate_limit"} 5
```

Liveness and readiness are separate signals: `/api/v1/health` returns 200 while
the process is up, and `/api/v1/ready` returns 503 naming the failing
dependency when fixtures are missing or the interaction-log store stops
answering. The Helm chart points its two probes at the two paths.

Tracing is opt-in — the tracer provider is a no-op until you set
`OTEL_EXPORTER_OTLP_ENDPOINT` (or `MOCKAGENTS_OTEL_STDOUT=1` for local
development). Each request then produces an outer `http.request` span and an
inner `engine.process_request` span carrying `agent.name`, `agent.model`,
`agent.protocol`, `agent.scenario`, and `agent.tool_calls`.

**See the [Observability guide](site/docs/guides/observability.md).**

## Kubernetes (Helm chart)

A production-shaped Helm chart lives under `deploy/helm/mockagents`. It runs the
Docker image as a non-root Deployment with a Service, ConfigMap-backed agents
directory, optional Ingress, a `helm test` health probe, and sensible defaults
(read-only rootfs, dropped caps, resource requests/limits).

```bash
helm install demo ./deploy/helm/mockagents \
  --set agents.inline."echo.yaml"="$(cat examples/minimal-agent.yaml)"
helm test demo
```

See `deploy/helm/mockagents/README.md` for all values.

## Web Console

A Next.js 15 web console lives under `gui/` (the "MockAgents Console" design
system, light/dark): agent catalog + detail, pipeline DAG viewer, interaction
logs with a real SSE live feed (`/logs?live=1`), a schema-validating `/editor`,
cost estimates, audit log, and multi-tenant admin pages.

```bash
mockagents start --agents-dir ./agents        # terminal 1
cd gui && npm install && npm run dev          # terminal 2 → :3001
```

Set `MOCKAGENTS_API_URL` to point the GUI at a non-local server. See
`gui/README.md` for the full feature list.

## Multi-tenant & control plane

For platform/DevEx teams: MockAgents has an optional SaaS-style control plane —
API-key auth, tenants, RBAC (`viewer < editor < admin < platform`), key
rotation, per-tenant quotas, OIDC SSO, and an always-on audit log. It is opt-in
(`MOCKAGENTS_MULTI_TENANT=1`); single-tenant mode is the default and needs none
of it. **See the [Multi-Tenant & Control-Plane guide](docs/guides/multi-tenant.md).**

## Documentation

- [Quickstart Guide](site/docs/getting-started/quickstart.md)
- [Drop-in Recipes (OpenAI/Anthropic/Gemini SDKs, Vercel AI, LangChain, LlamaIndex)](site/docs/guides/drop-in-recipes.md)
- [Testing AI Agents (tool-calls + MCP)](site/docs/guides/testing-agents.md)
- [Testing with Agent Frameworks (OpenAI Agents/Claude SDK/Google ADK/CrewAI/LangChain)](site/docs/guides/framework-testing.md)
- [Scenario Packs](site/docs/guides/scenario-packs.md) · [Hallucination Testing](site/docs/guides/hallucination-testing.md)
- [Record & Replay](site/docs/guides/record-replay.md)
- [CLI Reference](site/docs/guides/cli-reference.md)
- [YAML Schema](site/docs/guides/yaml-schema.md)
- [Python SDK](site/docs/sdk/python-sdk.md)
- [Management API](site/docs/guides/management-api.md)
- [Observability & Metrics](site/docs/guides/observability.md)
- [Evals vs. tests](docs/EVALS_VS_TESTS.md) — why you need both, and which one catches what
- [RAG demo](demo/rag-agent/README.md) — a complete runnable app + test suite, CI-verified
- [Multi-Tenant & Control Plane](docs/guides/multi-tenant.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

Apache 2.0 — see [LICENSE](LICENSE) for details.
