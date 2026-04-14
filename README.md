# MockAgents

**Simulate, test, and validate AI agent integrations — without calling real LLMs or burning tokens.**

[![CI](https://github.com/mockagents/mockagents/actions/workflows/ci.yml/badge.svg)](https://github.com/mockagents/mockagents/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mockagents/mockagents)](https://goreportcard.com/report/github.com/mockagents/mockagents)
[![PyPI](https://img.shields.io/pypi/v/mockagents)](https://pypi.org/project/mockagents/)
[![Docker](https://img.shields.io/docker/v/mockagents/mockagents?label=docker)](https://hub.docker.com/r/mockagents/mockagents)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

MockAgents lets you define mock AI agents in YAML and test your integrations against them. Point your OpenAI or Anthropic SDK at MockAgents — zero code changes required.

## Features

- **Drop-in replacement** for OpenAI (`/v1/chat/completions`) and Anthropic (`/v1/messages`) APIs
- **Declarative YAML** agent definitions with scenarios, tools, and match rules
- **Tool call simulation** with match-based responses and error injection
- **SSE streaming** with configurable chunk size and delay
- **Template expressions** — `{{ uuid }}`, `{{ random_int 1 100 }}`, `{{ fake_name }}`, and more
- **Python SDK** with fluent assertions and pytest integration
- **Single binary** — no runtime dependencies
- **Docker image** for CI/CD pipelines

## Quick Start

```bash
# Install
go install github.com/mockagents/mockagents/cmd/mockagents@latest

# Create a project
mockagents init my-project && cd my-project

# Start the mock server
mockagents start

# Test with OpenAI SDK
python3 -c "
import openai
client = openai.OpenAI(base_url='http://localhost:8080/v1', api_key='mock')
r = client.chat.completions.create(model='gpt-4o', messages=[{'role':'user','content':'hello'}])
print(r.choices[0].message.content)
"
```

## Installation

| Method | Command |
|--------|---------|
| Go | `go install github.com/mockagents/mockagents/cmd/mockagents@latest` |
| Docker | `docker run -p 8080:8080 -v ./agents:/agents mockagents/mockagents` |
| Python SDK | `pip install mockagents` |
| TypeScript SDK | `npm install mockagents` |
| Go SDK | `go get github.com/mockagents/mockagents/sdk/go/mockagents` |
| Binary | [GitHub Releases](https://github.com/mockagents/mockagents/releases) |

## Agent Definition Example

```yaml
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support-agent
spec:
  protocol: openai-chat-completions
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
```

## CLI Commands

```bash
mockagents init [name]         # Scaffold a new project
mockagents start [--watch]     # Start the mock server (-w = fsnotify auto-reload)
mockagents validate [path]     # Validate agent definitions
mockagents logs                # Query interaction logs
mockagents test [path] [--format text|json|junit]   # Run TestSuite YAML
mockagents record              # Proxy a real upstream LLM API and record to a cassette
mockagents replay              # Serve a recorded cassette over the mock endpoints
mockagents mcp                 # Serve a kind:MCPServer definition over HTTP or stdio
mockagents contract            # extract or diff agent contracts (CI-friendly)
```

### CI integration

`mockagents test --format junit > report.xml` produces a
Jenkins-compatible JUnit XML file that drops straight into any
test-reporter that speaks JUnit. The project ships ready-to-use
wrappers for the two most common CI hosts so you don't have to
write the boilerplate yourself.

**GitHub Actions** — single-step composite action at
`deploy/actions/mockagents-test`:

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

The composite action installs the CLI via `go install`, validates
agent definitions, runs the suite, and exposes the JUnit path as a
step output. See `deploy/actions/mockagents-test/README.md` for all
inputs.

**GitLab CI** — include the template under `deploy/ci/gitlab-ci.yml`:

```yaml
include:
  - project: mockagents/mockagents
    file: /deploy/ci/gitlab-ci.yml
    ref: main
```

The `mockagents:test` job installs the binary, validates the agents,
writes JUnit XML, and attaches it as a GitLab `artifacts.reports.junit`
so the results show up in the Merge Request UI automatically.

For local development, `mockagents start --watch` adds an fsnotify
hot-reload loop: saving any agent YAML file re-parses, validates, and
re-registers it without restarting the server. Validation failures
are logged; the previous known-good definition is preserved.

## Audit logging

Every control-plane mutation (tenant create/delete, API key
create/delete, agent reload) is appended to a dedicated SQLite file
(`.mockagents-audit.db`) and exposed for query at `GET /api/v1/audit`.
Audit is always on — there's no flag to enable it because the cost is
a handful of SQLite writes per admin action.

```bash
# Fetch all recent events
curl -H "Authorization: Bearer $ADMIN_KEY" http://localhost:8080/api/v1/audit

# Filter by kind + time window
curl -H "Authorization: Bearer $ADMIN_KEY" \
  "http://localhost:8080/api/v1/audit?kind=api_key.created&since=2026-04-13T00:00:00Z&limit=50"
```

Supported `kind` values: `tenant.created`, `tenant.deleted`,
`api_key.created`, `api_key.deleted`, `agent.reloaded`. Additional
filters: `actor` (exact-match actor name), `since` (RFC3339 lower
bound), `limit` (default 100, max 1000).

Each event records the authenticated principal's tenant id, key id,
role, and remote IP. In single-tenant mode the actor is
`"anonymous"`. Plaintext API keys are never written to the audit log
— the `api_key.created` event carries only the key's opaque id, its
public prefix, its name, and its role.

When multi-tenant mode is enabled, `GET /api/v1/audit` requires the
admin role; in single-tenant mode it is open (matching the rest of
the management API).

## Multi-tenant mode (experimental)

MockAgents ships a first SaaS slice: **API-key auth + tenants + RBAC**
applied to the management control plane (`/api/v1/*`). It is opt-in —
set `MOCKAGENTS_MULTI_TENANT=1` before `mockagents start` to enable it.
When the flag is off everything behaves exactly as today.

On first boot with the flag set, MockAgents creates a `default` tenant
and a `bootstrap-admin` API key, then prints the plaintext **exactly
once** to stderr so you can capture it:

```
================================================================
MockAgents multi-tenant mode enabled.
Bootstrap admin key (shown once): mak_1c3a9e0f_MXh6A2ci8RaWGpQBxLHFhRRacKvKnovL
Store this in your password manager. Use it via:
  Authorization: Bearer <key>   or   X-Api-Key: <key>
================================================================
```

The key is bcrypt-hashed immediately; there is no recovery path if you
lose it. Three roles, ordered by privilege: `viewer` < `editor` <
`admin`. Roles gate the control-plane routes:

| Route                                     | Min role |
| ----------------------------------------- | -------- |
| `GET  /api/v1/health`                     | open     |
| `GET  /api/v1/agents`, `/api/v1/logs`     | viewer   |
| `POST /api/v1/agents/{name}/reload`       | viewer   |
| `GET  /api/v1/tenants/{id}/keys`          | editor   |
| `GET  /api/v1/tenants`                    | admin    |
| `POST /api/v1/tenants`, `DELETE ...`      | admin    |
| `POST /api/v1/tenants/{id}/keys`          | admin    |
| `DELETE /api/v1/keys/{id}`                | admin    |

**The LLM endpoints (`/v1/chat/completions`, `/v1/messages`,
`/v1/models`, `/v1/engines/*`) deliberately remain unauthenticated** —
clients send their own provider API keys which MockAgents ignores, and
forcing a second layer of credentials would break every existing SDK.

```bash
# Mint a viewer key for a read-only CI bot:
curl -H "Authorization: Bearer $ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"ci-bot","role":"viewer"}' \
  http://localhost:8080/api/v1/tenants/$TENANT_ID/keys
```

### What's deliberately not in v0.1

- **Tenant-scoped agent data isolation.** Agents are still loaded from
  disk and shared across tenants; the current slice protects only the
  control plane. Per-tenant agent stores rewire the engine and deserve
  their own slice.
- **Postgres backend.** The tenancy store is pure-Go SQLite
  (`.mockagents-tenancy.db`); the `Store` interface makes a Postgres
  implementation straightforward once it's needed.
- **Billing, quotas, usage metering.** SaaS primitives — separate slice.
- **SSO / OAuth / key rotation workflows.** API keys only for now.

## Kubernetes (Helm chart)

A production-shaped Helm chart lives under `deploy/helm/mockagents`. It
runs the existing Docker image as a non-root Deployment with a Service,
ConfigMap-backed agents directory, optional Ingress, a `helm test`
health probe, and sensible defaults (read-only rootfs, dropped caps,
resource requests/limits).

```bash
helm install demo ./deploy/helm/mockagents \
  --set agents.inline."echo.yaml"="$(cat examples/minimal-agent.yaml)"

helm test demo
```

See `deploy/helm/mockagents/README.md` for the full list of values and
the two ways to provide agent definitions (inline vs. existing ConfigMap).

## Web Console (GUI v0.1)

A minimal Next.js 15 web console lives under `gui/`. It's a read-only
dashboard that talks to the MockAgents management API — agent catalog,
agent detail with raw definition, recent interaction logs, and a live
health pill. Run it alongside the mock server:

```bash
mockagents start --agents-dir ./agents        # terminal 1
cd gui && npm install && npm run dev           # terminal 2 → :3001
```

Set `MOCKAGENTS_API_URL` to point the GUI at a non-local server. The
workflow editor, config editor, and WebSocket live-update feed in the
product plan are deliberately out of v0.1 scope.

## Multi-Agent Pipelines

MockAgents also supports multi-agent topologies via `kind: Pipeline`. A pipeline
references agents by name and wires them in `sequential`, `parallel`, or `graph`
topologies (with substring-matched conditional edges). TestSuite files (`kind:
TestSuite`) declare cases with assertions (`tool_call`, `response_contains`,
`scenario_matched`, `latency_ms_lt`) targeting either an agent or a pipeline and
execute under `mockagents test`. See `examples/research-pipeline.yaml` and
`examples/research-suite.yaml` for working examples.

## Contract Testing

Agent definitions double as public contracts. `mockagents contract
extract` writes the canonical consumer-visible surface (protocol, tools
with input schemas, scenarios, streaming) as JSON so it can be checked
into git; `mockagents contract diff` compares two contracts (either
agent YAML or extracted JSON) and exits non-zero when breaking changes
are detected — safe to drop straight into a CI pipeline.

```bash
# Snapshot today's contract
mockagents contract extract agents/support.yaml -o contracts/support.json

# Later, in CI, fail the build if the agent has drifted
mockagents contract diff contracts/support.json agents/support.yaml
```

Severity rules: removing a tool, removing a scenario, tightening
`required`, changing a property's schema, or disabling streaming are
**breaking**. Adding a tool, relaxing `required`, or adding a scenario
are **additive**. Description and model-name changes are **info**.

## Observability (OpenTelemetry)

MockAgents is instrumented with OpenTelemetry tracing. The tracer
provider defaults to a no-op, so there is zero runtime overhead until
you explicitly opt in via environment variables:

| Env var                                | Effect                                           |
| -------------------------------------- | ------------------------------------------------ |
| `OTEL_EXPORTER_OTLP_ENDPOINT=https://…` | Send spans to an OTLP/HTTP collector             |
| `MOCKAGENTS_OTEL_STDOUT=1`             | Pretty-print spans to stdout (local development) |

Each request produces two spans: an outer `http.request` span with
method, route, and status-code attributes, and an inner
`engine.process_request` span carrying `agent.name`, `agent.model`,
`agent.protocol`, `agent.scenario`, and `agent.tool_calls`. Chaos
errors, validation failures, and generation errors mark their span
with status `Error`.

## Mock MCP Server

MockAgents ships a JSON-RPC 2.0 Model Context Protocol mock with two
transports — HTTP (`POST /mcp`) and stdio (line-delimited JSON) — so
you can develop and test MCP clients without standing up a real server.
A `kind: MCPServer` definition declaratively lists tools, resources,
and prompts; tool calls resolve via the same match/default pattern used
by LLM agents.

```bash
# HTTP transport
mockagents mcp --transport http --port 8081 --agents-dir examples

# stdio transport (for clients that spawn the server as a subprocess)
mockagents mcp --transport stdio --agents-dir examples --server weather-mcp
```

Supported methods: `initialize`, `tools/list`, `tools/call`,
`resources/list`, `resources/read`, `prompts/list`, `prompts/get`,
`ping`, and `notifications/initialized`. Streaming notifications are
not supported in v1. See `examples/weather-mcp.yaml` for a working
definition.

## Framework Adapters (Python)

`mockagents.adapters` exposes zero-boilerplate factories that point
LangChain / LangGraph / CrewAI at a running MockAgents server. The
framework packages are optional dependencies — install only the one
you need:

```bash
pip install 'mockagents[langchain]'   # LangChain + LangGraph
pip install 'mockagents[crewai]'      # CrewAI
```

```python
from mockagents import MockAgentServer
from mockagents.adapters import chat_openai, crewai_mock_llm, patched_env

with MockAgentServer(agents_dir="./agents") as server:
    # LangChain
    llm = chat_openai(server, model="gpt-4o")
    llm.invoke("hello")

    # LangGraph / any framework that reads OPENAI_BASE_URL from the env
    with patched_env(server):
        # prebuilt_agent(...) now calls the mock under the hood
        ...

    # CrewAI
    crewai_llm = crewai_mock_llm(server, model="gpt-4o")
    # pass crewai_llm as llm= to your crewai.Agent definitions
```

Each factory forwards extra kwargs to the underlying framework class, so
anything LangChain or CrewAI support (temperature, max_tokens, custom
headers, etc.) still works.

## Record and Playback

Capture real OpenAI/Anthropic traffic once and replay it offline forever.
Cassettes are JSON-lines files — safe to diff, check in, and grep.

```bash
# 1. Record against a real upstream (keys stay on your machine, never in the cassette)
mockagents record --upstream https://api.openai.com --cassette fixtures/gpt4o.jsonl --api-key "$OPENAI_API_KEY"

# ...point your SDK at http://localhost:8080 and run your flow...

# 2. Replay offline, deterministically
mockagents replay --cassette fixtures/gpt4o.jsonl
```

Request matching canonicalizes JSON (sorted keys) so SDK reorderings still
hit the cassette. Streaming responses are not captured in v1 — the proxy
buffers the response body and stores it as JSON. Unknown requests during
replay return 404 with the SHA-256 prefix of the miss so you can diff.

## Chaos Engineering

Every agent can inject faults via a `spec.behavior.chaos` block. Three
independent knobs:

- **`latency`** — `fixed`, `uniform` (`min_ms`/`max_ms`), or `normal`
  (`mean_ms`/`stddev_ms`) distributions sleep the response before returning.
- **`errors`** — probability-gated injection of HTTP errors (`status_code` or a
  list of `status_codes`), plus an optional `timeout` mode that sleeps for
  `timeout_ms` and returns a synthetic 504.
- **`rate_limit`** — rolling-window token bucket (`requests` per `window_ms`)
  that returns `429 Too Many Requests` with `Retry-After` when the budget is
  exceeded.

Chaos is evaluated inside the engine before tool resolution, so it works
identically for the OpenAI and Anthropic endpoints. See
`examples/chaos-agent.yaml` for a combined example.

## Documentation

- [Quickstart Guide](site/docs/getting-started/quickstart.md)
- [CLI Reference](site/docs/guides/cli-reference.md)
- [YAML Schema](site/docs/guides/yaml-schema.md)
- [Python SDK](site/docs/sdk/python-sdk.md)
- [Management API](site/docs/guides/management-api.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

Apache 2.0 — see [LICENSE](LICENSE) for details.
