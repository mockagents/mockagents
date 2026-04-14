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
| `--port` | `-p` | `8080` | HTTP server port |
| `--json-logs` | | `false` | Output logs in JSON format |

Environment variable: `MOCKAGENTS_PORT`.

**Examples:**

```bash
# Start with defaults
mockagents start

# Custom port and debug logging
mockagents start --port 9090 --log-level debug

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
