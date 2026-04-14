# Changelog

## v0.1.0 (Unreleased)

Initial alpha release of MockAgents.

### Features

- **Agent Definitions** — Declarative YAML configuration with schema validation
- **Mock Engine** — Scenario matching (content_contains, content_regex, turn_number), template expressions with 15+ built-in functions, conversation state management
- **Tool Call Simulation** — Match-based tool responses, error injection, parameter validation, parallel processing
- **Protocol Adapters** — OpenAI Chat Completions (`/v1/chat/completions`) and Anthropic Messages (`/v1/messages`) wire-compatible endpoints
- **SSE Streaming** — Configurable chunk size and delay for both OpenAI and Anthropic formats
- **HTTP Server** — Multi-agent routing, graceful shutdown, hot reload, management API
- **CLI** — `init`, `start`, `validate`, `logs` commands with colored output and environment variable support
- **Python SDK** — `MockAgentServer`, `MockAgentClient`, `Scenario`, `expect()` assertions, pytest integration
- **Interaction Logging** — SQLite-backed request/response logging with query API
- **Docker** — Multi-stage Alpine image, docker-compose for local development
- **CI/CD** — GitHub Actions for testing, releasing, and Docker publishing

### Protocol Support

- OpenAI Chat Completions API (non-streaming + SSE streaming)
- Anthropic Messages API (non-streaming + SSE streaming)

### CLI Commands

- `mockagents init [project-name]` — Scaffold new project
- `mockagents start` — Start mock server
- `mockagents validate [paths]` — Validate agent definitions
- `mockagents logs` — Query interaction logs
