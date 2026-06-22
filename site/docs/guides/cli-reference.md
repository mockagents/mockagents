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

| Flag | Default | Description |
|------|---------|-------------|
| `--force` | `false` | Overwrite existing files |

**Examples:**

```bash
# Create new project directory
mockagents init my-project

# Scaffold in current directory
mockagents init .

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

Environment variables: `MOCKAGENTS_HOST`, `MOCKAGENTS_PORT`.

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

**Endpoints registered:**

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | OpenAI Chat Completions |
| `POST` | `/v1/messages` | Anthropic Messages |
| `GET` | `/v1/models` | List models |
| `GET` | `/api/v1/health` | Health check |
| `GET` | `/api/v1/agents` | List agents |
| `GET` | `/api/v1/agents/{name}` | Agent detail |
| `POST` | `/api/v1/agents/{name}/reload` | Hot reload agent |
| `GET` | `/api/v1/logs` | Query logs |

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

Manage agents on a **running** server over its write API (FB-04) — no restart.
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
mockagents test [path...] [--format text|json|junit]
```

Assertions include `tool_call`, `tool_call_args` (nested dotted-path argument
match), `no_tool_call`, `tool_error` (a simulated tool returned an error),
`handles_tool_error` (the agent recovered from one), `response_contains`,
`response_matches` (regex), `scenario_matched`, `refusal`, `latency_ms_lt`,
`tool_call_count`, `tool_call_sequence`, and `node_sequence` (pipeline targets).
`--format junit` writes a Jenkins-compatible report for CI.

```bash
mockagents test tests/ --format junit > report.xml
```

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

Serve a `kind: MCPServer` definition over HTTP or stdio.

```bash
mockagents mcp --transport http --port 8081 --agents-dir examples
mockagents mcp --transport stdio --agents-dir examples --server weather-mcp
```

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
