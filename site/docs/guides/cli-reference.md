# CLI Reference

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--agents-dir` | `./agents` | Directory containing agent definition files |
| `--log-level` | `info` | Log level: debug, info, warn, error |
| `--no-color` | `false` | Disable colored output |

Environment variables: `MOCKAGENTS_AGENTS_DIR`, `MOCKAGENTS_LOG_LEVEL`.

---

## `mockagents init`

Scaffold a new MockAgents project.

```bash
mockagents init [project-name] [flags]
```

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--force` | | `false` | Overwrite existing files |
| `--template` | `-t` | `basic` | Starter pack to scaffold (see `--list-templates`) |
| `--list-templates` | | `false` | List available starter packs and exit |

Available templates: `basic`, `customer-support`, `rag`, `coding-agent`,
`planner` — each ships an agent plus a passing TestSuite
(see [Scenario Packs](scenario-packs.md#scaffold-a-starter-pack)).

**Examples:**

```bash
# Create new project directory
mockagents init my-project

# Scaffold in current directory
mockagents init .

# Start from a curated pack
mockagents init my-bot --template customer-support

# Overwrite existing files
mockagents init my-project --force
```

**Created Files:**

| File | Description |
|------|-------------|
| `.mockagents.yaml` | Project configuration |
| `agents/example-agent.yaml` | Sample agent definition |
| `tests/example-test.yaml` | Sample test scenario |
| `README.md` | Project readme with next steps |

---

## `mockagents start`

Start the mock agent HTTP server.

```bash
mockagents start [flags]
```

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--host` | | `127.0.0.1` | HTTP server bind address |
| `--port` | `-p` | `8080` | HTTP server port |
| `--json-logs` | | `false` | Output logs in JSON format |
| `--watch` | `-w` | `false` | Auto-reload agent YAML files on change (fsnotify) |

Environment variables: `MOCKAGENTS_HOST`, `MOCKAGENTS_PORT`.

State & logging knobs:

| Variable | Default | What it does |
|----------|---------|--------------|
| `MOCKAGENTS_DATA_DIR` | working directory | Relocates the on-disk state (`.mockagents.db` interaction log, `.mockagents-audit.db` audit trail, `.mockagents-tenancy.db`) to any writable directory (auto-created). Use when the working directory is read-only. An unwritable path degrades to a WARN — logging off, server still serving. The Docker image runs from the `/data` volume, so state persists across container restarts without this. |
| `MOCKAGENTS_LOG_BODIES` | `full` | Interaction-log body capture: `full` \| `sanitized` \| `none`. |
| `MOCKAGENTS_LOG_MAX_ROWS` | `0` (unlimited) | Bounds the interaction-log table via a background pruner. |

Strictness knobs:

| Variable | Values | Default | What it gates |
|----------|--------|---------|---------------|
| `MOCKAGENTS_STRICT_TOOLS` | `off` / `warn` / `strict` | `off` | Fleet default for the strict-tools family: round-trip tool id validation, `tool_choice` required/named forcing (+ parallel cap), and `strict:true` schema validation. Per-agent `spec.behavior.strict_tools` overrides it. `warn` sets the `X-Mockagents-Strict-Violation` header instead of failing. |
| `MOCKAGENTS_REALTIME_STRICT` | `1` / `true` | off | GA-strict `session.update` field validation on the Realtime WebSocket surface (`unknown_parameter` errors with param paths). |

**Examples:**

```bash
# Start with defaults
mockagents start

# Custom port and debug logging
mockagents start --port 9090 --log-level debug

# Listen on all interfaces for a container or remote test host
mockagents start --host 0.0.0.0

# JSON logs for CI
mockagents start --json-logs --log-level warn
```

**Endpoints registered** (protocol surface):

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | OpenAI Chat Completions |
| `POST` | `/v1/responses` | OpenAI Responses API |
| `POST`/`GET`/`DELETE` | `/v1/conversations[/{id}[/items…]]` | OpenAI Conversations API |
| `POST` | `/v1/embeddings` | OpenAI Embeddings |
| `POST` | `/v1/moderations` | OpenAI Moderations |
| `POST`/`GET` | `/v1/files…`, `/v1/batches…` | OpenAI Files + Batch API |
| `GET` | `/v1/realtime` | OpenAI Realtime (WebSocket) — see the [Realtime guide](realtime.md) |
| `POST` | `/v1/realtime/client_secrets`, `/v1/realtime/sessions` | Realtime ephemeral keys |
| `POST` | `/openai/deployments/{deployment}/…`, `/openai/v1/…` | Azure OpenAI URL shapes |
| `POST` | `/v1/messages` | Anthropic Messages |
| `POST` | `/v1/messages/count_tokens` | Anthropic token counting |
| `POST`/`GET` | `/v1/messages/batches…` | Anthropic Message Batches |
| `POST` | `/v1beta/models/{model}:generateContent` | Google Gemini |
| `GET` | `/v1/models` | List models |

Plus the management API under `/api/v1/` (health, agents CRUD, logs + SSE
stream, costs, audit, pipelines, config validation, tenants/keys/quota) — see
the [Management API guide](management-api.md).

---

## `mockagents validate`

Validate agent definition files.

```bash
mockagents validate [file|directory...] [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Output format: text or json |
| `--strict` | `false` | Treat warnings as errors |

**Exit Codes:**

| Code | Meaning |
|------|---------|
| 0 | All definitions valid |
| 1 | Validation errors found |
| 2 | Unexpected error |

**Examples:**

```bash
# Validate default agents directory
mockagents validate

# Validate specific files
mockagents validate agents/my-agent.yaml

# JSON output for CI
mockagents validate --format json
```

---

## `mockagents add` / `mockagents rm`

Manage agents on a **running** server over its write API — no restart.
`add` sends an agent definition file (`POST /api/v1/agents`); `--replace` upserts
it instead (`PUT /api/v1/agents/{name}`). `rm` deletes by name.

```
mockagents add <file> [--replace] [--server URL] [--api-key KEY]
mockagents rm  <name>  [--server URL] [--api-key KEY]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--server`  | `$MOCKAGENTS_SERVER` or `http://localhost:8080` | Base URL of the running server |
| `--api-key` | `$MOCKAGENTS_API_KEY` | API key (editor+) for multi-tenant servers |
| `--replace` | `false` | Upsert via PUT instead of failing on a name conflict (`add` only) |

```bash
# Hot-add a new agent (fails if it already exists)
mockagents add agents/my-agent.yaml

# Create-or-replace
mockagents add agents/my-agent.yaml --replace

# Against a remote, authenticated server
mockagents add agents/my-agent.yaml --server https://mock.example.com --api-key mas_...

# Delete it again
mockagents rm my-agent
```

The file is validated server-side with the same rules as `mockagents validate`;
a rejected write prints the server's error and exits non-zero. The agent is owned
by the API key's tenant.

---

## `mockagents logs`

Query interaction logs.

```bash
mockagents logs [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--agent` | | Filter by agent name |
| `--session` | | Filter by session ID |
| `--since` | | Show logs from duration ago (e.g., `1h`, `30m`) |
| `--limit` | `50` | Maximum results |
| `--output` | `table` | Output format: table or json |
| `--db` | `.mockagents.db` | SQLite database path |

**Examples:**

```bash
# Show recent logs
mockagents logs

# Filter by agent
mockagents logs --agent customer-support --limit 20

# JSON output
mockagents logs --output json --since 1h
```

---

## `mockagents test`

Run `kind: TestSuite` files against agents or pipelines.

```bash
mockagents test [file|directory...] [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Output format: `text`, `json`, or `junit` (JUnit XML on stdout) |
| `--suites-dir` | | Directory containing TestSuite files (defaults to `--agents-dir`) |

Assertions include `tool_call`, `tool_call_args` (nested dotted-path argument
match), `no_tool_call`, `tool_error` (a simulated tool returned an error),
`handles_tool_error` (the agent recovered from one), `response_contains`,
`response_matches` (regex), `scenario_matched`, `refusal`, `latency_ms_lt`,
`tool_call_count`, `tool_call_sequence`, and `node_sequence` (pipeline targets).
`--format junit` writes a Jenkins-compatible report for CI.

```bash
mockagents test tests/ --format junit > report.xml
```

Glob patterns are expanded by the command itself, so they behave identically
in shells that don't expand them (Windows, `docker run` args). In zsh, quote
the pattern so the shell doesn't fail on it before the command runs:

```bash
mockagents test 'tests/*.yaml'     # quoted → mockagents expands it
```

Matches run in sorted filename order; an unmatched pattern is an error
(exit 2), never a silent zero-suite run.

---

## `mockagents record` / `mockagents replay`

Capture real upstream traffic once, then serve it offline.

```bash
# Proxy a real upstream and capture to a JSON-lines cassette
mockagents record --upstream https://api.openai.com \
  --cassette fixtures/gpt4o.jsonl --api-key "$OPENAI_API_KEY"

# Replay the cassette over the mock endpoints (no upstream, no keys)
mockagents replay --cassette fixtures/gpt4o.jsonl

# Ignore replay-time sampling fields when matching (repeatable)
mockagents replay --cassette fixtures/gpt4o.jsonl \
  --match-ignore temperature --match-ignore seed

# Record on miss: replay what's recorded, fetch+record anything new
mockagents replay --cassette fixtures/gpt4o.jsonl \
  --record-mode new_episodes --upstream https://api.openai.com \
  --api-key "$OPENAI_API_KEY" --redact
```

`--record-mode` is one of `none` (default, replay only), `new_episodes` (record
on miss), `once` (record only if the cassette is empty/new, else replay), or
`all` (always forward + record). The recording modes require `--upstream` and
reuse `--api-key` / `--redact` / `--redact-pattern` from `mockagents record`.
Record-on-miss never caches a 4xx/5xx or a broken stream.

`--match-ignore <field>` makes matching ignore the named top-level request-body
fields (replay-time only; the cassette is unchanged). A replay miss returns a
JSON `404` with the request hash and a `nearest` block — the closest recorded
interaction on the same method+path plus a field-level diff — so a drifted
prompt names what changed instead of returning an opaque hash.

Provider keys stay on your machine — they are never written to the cassette.
SSE streams are captured and replayed.

Add `--redact` to mask common secret formats (`sk-*`, `key-*`, `Bearer`, AWS,
GitHub, Slack, Google keys, JWTs) inside recorded **bodies** before they are
written, and `--redact-pattern <regexp>` (repeatable; implies `--redact`) for
your own formats. Redaction rewrites JSON string values only, so it never breaks
the cassette or replay matching — see the [record/replay guide](record-replay.md).

---

## `mockagents import`

Convert recordings from other tools into a MockAgents cassette.

```bash
# vcrpy YAML cassette → MockAgents cassette
mockagents import vcr fixtures/openai.yaml -o cassette.jsonl
mockagents import vcr fixtures/openai.yaml --all   # include non-LLM interactions

# OpenAI stored-completions JSONL → MockAgents cassette
mockagents import openai-stored-completions export.jsonl -o cassette.jsonl
```

| Flag | Default | Applies to | Description |
|------|---------|-----------|-------------|
| `--cassette` / `-o` | `cassette.jsonl` | both | Output cassette path |
| `--all` | `false` | `vcr` | Import every interaction, not just POSTs to known LLM paths |

`import vcr` decodes base64/gzip bodies and parsed-JSON request bodies, drops
credential-bearing headers, and skips anything it can't import with a printed
reason. Secrets embedded in request/response **bodies** are not redacted — review
before committing.

---

## `mockagents mcp`

Serve a `kind: MCPServer` definition over HTTP or stdio — see the
[MCP guide](mcp.md).

```bash
mockagents mcp --transport http --port 8081 --agents-dir examples
mockagents mcp --transport stdio --agents-dir examples --server weather-mcp
mockagents mcp --manage --agents-dir ./agents   # agent-management tools over MCP
```

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--transport` | | `http` | Transport: `http` or `stdio` |
| `--port` | `-p` | `8081` | HTTP port (`--transport=http`) |
| `--bind` | | `127.0.0.1` | Interface to bind (`0.0.0.0` to expose) |
| `--server` | | | MCPServer to serve (required when multiple are loaded) |
| `--manage` | | `false` | Also expose agent-management tools (`list_agents`, `get_agent`, `validate_agent`, `create_agent`, `put_agent`, `delete_agent`) backed by the write API |

The HTTP transport binds `127.0.0.1` by default, per the MCP spec's guidance
for local servers. Pass `--bind 0.0.0.0` to expose it beyond the host — e.g.
when running inside a container whose port is mapped out.

---

## `mockagents a2a`

Serve a `kind: A2AServer` definition (Agent2Agent protocol) — see the
[A2A guide](a2a.md).

```bash
mockagents a2a --agents-dir examples --server weather-a2a
```

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8083` | HTTP port |
| `--server` | | | A2AServer to serve (required when multiple are loaded) |

Serves the Agent Card at `/.well-known/agent-card.json` (and the legacy
`/.well-known/agent.json` alias) and JSON-RPC (`message/send`,
`message/stream`, `tasks/get`, `tasks/cancel`) on `POST /`.

---

## `mockagents contract`

Extract an agent's consumer-visible contract or diff two contracts in CI.

```bash
mockagents contract extract agents/support.yaml -o contracts/support.json
mockagents contract diff contracts/support.json agents/support.yaml
```

`diff` exits non-zero on breaking changes (removed tool/scenario, tightened
`required`, changed schema, disabled streaming).

---

## Multi-tenant mode

Set `MOCKAGENTS_MULTI_TENANT=1` before `mockagents start` to enable API-key auth
+ tenants + RBAC on the `/api/v1/*` management routes. On first boot a `default`
tenant and a bootstrap `platform`/admin key are created and the plaintext is
printed once to stderr. See the [Management API](management-api.md) guide for the
control-plane routes and role floors.
