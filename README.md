# MockAgents

**Mock the OpenAI, Anthropic & Gemini APIs. Test your AI agents with zero LLM calls — fast, free, deterministic, offline.**

[![CI](https://github.com/mockagents/mockagents/actions/workflows/ci.yml/badge.svg)](https://github.com/mockagents/mockagents/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mockagents/mockagents)](https://goreportcard.com/report/github.com/mockagents/mockagents)
[![PyPI](https://img.shields.io/pypi/v/mockagents)](https://pypi.org/project/mockagents/)
[![Docker](https://img.shields.io/docker/v/mockagents/mockagents?label=docker)](https://hub.docker.com/r/mockagents/mockagents)
[![MCP Conformance](https://github.com/mockagents/mockagents/actions/workflows/mcp-conformance.yml/badge.svg)](https://github.com/mockagents/mockagents/actions/workflows/mcp-conformance.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

MockAgents is a single pure-Go binary that's a **drop-in replacement** for the
OpenAI, Anthropic, and Google Gemini HTTP APIs — and for MCP servers. Point your
app's base URL at it and your existing SDK code just works, returning
**deterministic** canned responses, simulated tool calls, and real SSE streams,
with **no real LLM calls**. So your agent tests stop costing a dollar a run,
stop flaking on model updates, and run fully offline.

<!-- TODO(RR-03): embed a 12-second GIF of `mockagents start` + the web console streaming SSE. -->

## 60-second start

```bash
docker run -p 8080:8080 mockagents/mockagents          # 1. run the mock (no prereqs)
export OPENAI_BASE_URL=http://localhost:8080/v1        # 2. the ONLY change to your app
export OPENAI_API_KEY=mock
python my_existing_app.py                              # 3. it works — free, offline, deterministic
```

That's the whole idea: **swap the base URL, change nothing else.** Works with
the official OpenAI / Anthropic / Google SDKs, LangChain, LlamaIndex, the Vercel
AI SDK — anything that talks these APIs over HTTP.

### Or install the CLI

| Method | Command |
|---|---|
| npx (no install) | `npx mockagents start` |
| pipx (no install) | `pipx run mockagents start` |
| Homebrew | `brew install mockagents/tap/mockagents` |
| Docker | `docker run -p 8080:8080 mockagents/mockagents` |
| Go | `go install github.com/mockagents/mockagents/cmd/mockagents@latest` |
| Binary | [GitHub Releases](https://github.com/mockagents/mockagents/releases) |

**SDKs** (client libraries, not the server): `pip install mockagents` (Python),
`npm install @mockagents/sdk` (TypeScript), `go get github.com/mockagents/mockagents/sdk/go/mockagents` (Go).

**Test-runner helpers** (auto-spawn the server + redirect the provider SDKs):
`@mockagents/vitest` for [Vitest/Jest](sdk/vitest/README.md), and the bundled
`pytest` plugin in the Python SDK.

```bash
mockagents init my-project && cd my-project   # scaffold an example agent
# ...or start from a curated pack:
mockagents init my-bot --template customer-support   # see `--list-templates`
mockagents start                              # prints your base URL + a ready-to-paste snippet
```

## Why MockAgents

- **One static binary, no runtime** — no Node, JVM, Python, or GPU. Drop it into
  any CI in seconds (pure-Go, no cgo).
- **OpenAI + Anthropic + Gemini parity** — real response shapes, `tool_calls` /
  `tool_use` / `functionCall`, `usage` token counts, and SSE streaming.
- **OpenAI Responses API** (`/v1/responses`) — the default OpenAI Agents SDK
  transport: typed output items, the full `response.*` streaming-event ladder,
  and stateful `previous_response_id` multi-turn loops.
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
- **Mocks MCP servers too** — test agents that call Model Context Protocol
  servers, deterministically (JSON-RPC 2.0 + bidirectional SSE).
- **Scenario matching** — route by message content, regex, or turn number;
  assert *which* path fired, not just the text.
- **Tool-call simulation** — return canned tool calls and tool results; test your
  agent's routing and argument handling without a live model.
- **Multi-agent pipelines** (`kind: Pipeline`) — sequential, parallel, and graph
  topologies with conditional edges.
- **Chaos & fault injection** — inject latency, errors, and rate limits per agent
  to test the unhappy paths.
- **Hallucination fixtures** — return a deterministic confidently-wrong / ungrounded /
  fabricated output (advertised via a response header) to test that your guardrails
  catch it — something a real model won't do on demand.
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

## Test it (pytest)

```python
# `pip install mockagents` ships a pytest plugin — the `mockagents` fixture
# points the OpenAI/Anthropic/Gemini SDKs at the mock with zero code changes.
def test_greeting(mockagents):
    from openai import OpenAI
    out = OpenAI().chat.completions.create(
        model="gpt-4o",
        messages=[{"role": "user", "content": "hello"}],
    )
    assert "How can I help" in out.choices[0].message.content
```

In CI, the [`setup-mockagents` GitHub Action](deploy/actions/setup-mockagents)
starts the server and exports the base URLs for the rest of the job.

→ **[Testing AI Agents guide](site/docs/guides/testing-agents.md)** — runnable
cookbooks for asserting agent **tool-calls** (right tool, right arguments) and
**mocking an MCP server**, deterministically and offline.

## What it is *not*

MockAgents mocks the **wire protocol, not the model**. It won't tell you whether
your prompt is *good* — only that your code handles the API correctly. Pair it
with an eval tool (e.g. promptfoo) for output quality.

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

## Observability (OpenTelemetry)

The tracer provider defaults to a no-op, so there is zero runtime overhead until
you opt in via environment variables:

| Env var                                 | Effect                                           |
| --------------------------------------- | ------------------------------------------------ |
| `OTEL_EXPORTER_OTLP_ENDPOINT=https://…` | Send spans to an OTLP/HTTP collector             |
| `MOCKAGENTS_OTEL_STDOUT=1`              | Pretty-print spans to stdout (local development) |

Each request produces an outer `http.request` span and an inner
`engine.process_request` span carrying `agent.name`, `agent.model`,
`agent.protocol`, `agent.scenario`, and `agent.tool_calls`.

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
- [Multi-Tenant & Control Plane](docs/guides/multi-tenant.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

Apache 2.0 — see [LICENSE](LICENSE) for details.
