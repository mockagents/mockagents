# MockAgents — MVP Deployment Cut

**Version:** 0.1.0-alpha  
**Date:** April 7, 2026  
**Author:** MockAgents Core Team  
**Status:** Draft  
**Applies to:** Phase 1 (Months 1-3) of the MockAgents Product Plan  

---

## 1. Document Info

| Field | Value |
|---|---|
| Document type | MVP Deployment Cut |
| Product | MockAgents |
| Target release | v0.1.0-alpha |
| Target date | End of Month 3 (July 2026) |
| Core language | Go |
| Repository structure | Monorepo |
| MVP storage | SQLite |
| MVP interface | CLI only |
| Primary audience | AI Engineers (Builder Persona) |

---

## 2. MVP Vision

The MockAgents MVP proves a single thesis: **an AI engineer can go from zero to a fully functional local mock of any LLM agent integration — complete with tool-call simulation, streaming responses, and deterministic test assertions — in under five minutes, without any API keys, network access, or paid service dependencies.** The MVP delivers a Go-based mock server with protocol-accurate OpenAI and Anthropic adapters, a Python SDK for writing tests, and a CLI that scaffolds, starts, and validates mock agent definitions from YAML files. If this works, developers will adopt MockAgents as the default way to test agent integrations locally and in CI, and we will have validated the core value proposition before investing in multi-agent orchestration, GUI tooling, or cloud infrastructure.

---

## 3. Target Persona

### AI Engineers (Builder Persona)

**Who they are:** Developers building applications that integrate with LLM agents, tool-calling APIs, or multi-agent pipelines.

**Key needs:**
- Mock an LLM agent's tool-call sequences to test application logic without burning API tokens
- Simulate deterministic agent responses for unit and integration tests
- Reproduce specific failure modes (hallucinations, tool errors, timeouts)
- Run tests locally and in CI/CD without external dependencies

**Success metric:** "I can write a test for my agent integration in under 5 minutes, and it runs in CI without any API keys."

**Why MVP-first for this persona:** AI Engineers represent the largest segment of early adopters. They have immediate, daily pain (burning tokens in tests, flaky integration suites, no offline testing). They are comfortable with CLI tools and YAML configuration. Winning this persona first creates organic word-of-mouth and community contributions that accelerate subsequent phases.

---

## 4. IN Scope

### 4.1 Mock Engine

- [ ] **Agent definitions in YAML** — declarative `mockagents/v1` schema with `kind: Agent`, metadata, spec, tools, and behavior sections
- [ ] **Static responses** — fixed content mapping for deterministic, reproducible testing
- [ ] **Template-based responses** — Handlebars-style templating with access to request context, variables, and built-in helpers (`{{ random_int }}`, `{{ date_offset }}`, `{{ uuid }}`)
- [ ] **Scenario matching** — route responses based on `content_contains`, `content_regex`, `role`, and `metadata` matchers
- [ ] **Default scenario fallback** — a catch-all response when no scenario matches
- [ ] **Conversation state tracking** — maintain per-session conversation history and turn count across multi-turn interactions
- [ ] **Agent persona metadata** — `systemPrompt`, `model` name, and `description` fields in agent definitions (for context, not executed)

### 4.2 Tool Call Simulation

- [ ] **Tool registry** — define tools with `name`, `description`, and JSON Schema `parameters` in agent YAML
- [ ] **Match-based tool responses** — return specific responses when tool input matches defined patterns (exact match on parameter values)
- [ ] **Default tool responses** — fallback response when no match pattern applies
- [ ] **Error responses** — return tool errors with `code` and `message` fields for specific match patterns (e.g., `NOT_FOUND`, `RATE_LIMITED`, `TIMEOUT`)
- [ ] **Tool call validation** — validate incoming tool-call parameters against the defined JSON Schema before matching

### 4.3 Protocol Adapters

#### OpenAI Chat Completions Adapter
- [ ] **`POST /v1/chat/completions`** — full request/response cycle matching OpenAI wire format
- [ ] **Function calling** — `tools` and `tool_choice` in requests, `tool_calls` in assistant responses
- [ ] **Streaming** — `stream: true` support with SSE (`data: [DONE]` termination), configurable chunk size and delay
- [ ] **Model field passthrough** — echo back the requested model name in responses
- [ ] **Usage statistics** — return realistic `prompt_tokens`, `completion_tokens`, `total_tokens` counts
- [ ] **Stop reasons** — correct `finish_reason` values (`stop`, `tool_calls`, `length`)

#### Anthropic Messages Adapter
- [ ] **`POST /v1/messages`** — full request/response cycle matching Anthropic wire format
- [ ] **Tool use** — `tool_use` content blocks in assistant responses, `tool_result` in user follow-ups
- [ ] **Streaming** — `stream: true` with SSE events (`message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`)
- [ ] **System prompt handling** — accept `system` field in requests
- [ ] **Usage statistics** — return `input_tokens` and `output_tokens` counts
- [ ] **Stop reasons** — correct `stop_reason` values (`end_turn`, `tool_use`, `max_tokens`)

### 4.4 CLI (`mockagents`)

- [ ] **`mockagents init`** — scaffold a new mock agent project with directory structure, example agent YAML, example test file, and `.mockagents.yaml` config
- [ ] **`mockagents start`** — start the mock server on a configurable port (default `8080`), load all agent definitions from the project directory, hot-reload on file changes
- [ ] **`mockagents start --port <N>`** — override the default port
- [ ] **`mockagents start --agents <dir>`** — specify agent definitions directory
- [ ] **`mockagents validate`** — validate all agent definition YAML files against the schema, report errors with file path and line number
- [ ] **`mockagents version`** — print version and build info
- [ ] **`--help` for all commands** — comprehensive usage information

### 4.5 Python SDK

- [ ] **`MockAgentServer` context manager** — start/stop a mock server in a `with` block, auto-select available port, return base URL
- [ ] **`MockAgentServer.from_config(path)`** — load agent definitions from a YAML file or directory
- [ ] **`Scenario` class** — define a sequence of interaction steps (`role`, `content`, `tool_calls`)
- [ ] **`server.run_scenario(scenario)`** — execute a scenario against the mock server and return a `Result` object
- [ ] **`expect()` assertion API:**
  - [ ] `expect(result).to_have_tool_call(name, params)` — assert a specific tool was called with expected parameters
  - [ ] `expect(result).to_have_response_containing(text)` — assert response content contains a substring
  - [ ] `expect(result).to_have_tool_error(code)` — assert a tool error was returned
  - [ ] `expect(result.latency_ms).to_be_less_than(threshold)` — assert response time
  - [ ] `expect(result.latency_ms).to_be_greater_than(threshold)` — assert minimum response time
- [ ] **Synchronous client** — blocking API for simple test scripts
- [ ] **Async client** — `asyncio`-compatible API for async test frameworks (pytest-asyncio)
- [ ] **No network dependency** — SDK starts a local server process; all communication is localhost-only
- [ ] **Compatible with pytest** — works as standard pytest tests with no special plugins required

### 4.6 Storage

- [ ] **SQLite interaction logging** — persist all request/response pairs to a local SQLite database
- [ ] **Queryable log** — store timestamp, agent name, request body, response body, latency, matched scenario, and tool calls
- [ ] **Auto-create database** — create the SQLite file on first server start if it does not exist
- [ ] **Configurable log path** — set via `.mockagents.yaml` or `--log-db` flag

### 4.7 Distribution

- [ ] **Single Go binary** — statically compiled, zero runtime dependencies
  - [ ] Linux (amd64, arm64)
  - [ ] macOS (amd64, arm64)
  - [ ] Windows (amd64)
- [ ] **Docker image** — minimal image (distroless or Alpine-based) with the Go binary
- [ ] **PyPI package** — `pip install mockagents`, bundles or downloads the correct Go binary for the platform
- [ ] **GitHub Releases** — pre-built binaries attached to tagged releases

---

## 5. OUT of Scope

The following features are explicitly **deferred** to post-MVP phases. They are not forgotten — they are sequenced intentionally to keep the MVP focused.

| Feature | Deferred to | Rationale |
|---|---|---|
| GUI dashboard (agent catalog, interaction explorer, workflow editor) | Phase 2 | CLI-first validates core value; GUI is a productivity layer on top |
| Multi-agent orchestration (pipelines, topologies, inter-agent messaging) | Phase 2 | Requires stable single-agent foundation first |
| MCP server mocking (stdio, HTTP/SSE transports) | Phase 3 | MCP adoption still early; single-protocol mocking proves the pattern |
| Chaos engineering (latency injection, error injection, rate limiting) | Phase 3 | MVP focuses on correctness testing, not resilience testing |
| Record-and-playback mode | Phase 2 | Requires proxy infrastructure; static/template mocks cover MVP use cases |
| LLM-backed responses (route to real LLM for semi-realistic responses) | Phase 3 | Contradicts MVP's zero-API-key promise; useful but not foundational |
| CrewAI adapter | Phase 2 | Framework-specific adapter; OpenAI/Anthropic protocols cover most cases |
| LangGraph adapter | Phase 2 | Framework-specific adapter; prioritize protocol-level compatibility first |
| AutoGen adapter | Phase 3 | Lower adoption priority |
| TypeScript/Node.js SDK | Phase 2 | Python community is larger in AI engineering; TS follows |
| Go SDK | Phase 4 | Smallest user segment for agent testing |
| Test runner CLI (`mockagents test`) | Phase 2 | pytest integration in Python SDK covers MVP testing needs |
| CI/CD plugins (GitHub Actions, GitLab CI, Jenkins) | Phase 2 | CLI + Docker image enables CI/CD without dedicated plugins |
| Kubernetes Helm chart | Phase 4 | Docker image is sufficient for MVP deployment scenarios |
| Cloud SaaS (multi-tenant hosted service) | Phase 4 | Requires significant infrastructure; local-first MVP proves value |
| RBAC (role-based access control) | Phase 4 | Single-user/team local tool does not need access control |
| Contract testing (agent contracts, breaking change detection) | Phase 4 | Requires stable agent definition schema and ecosystem maturity |
| VS Code extension | Phase 3 | Nice-to-have; YAML schema validation via JSON Schema covers basics |
| JetBrains plugin | Phase 4 | Even lower priority than VS Code |
| OpenTelemetry integration | Phase 4 | SQLite logging is sufficient for MVP observability |
| Cost estimation engine | Phase 4 | Usage statistics in responses provide raw data; estimation is a layer on top |
| Google Gemini adapter | Phase 3 | Third protocol priority after OpenAI and Anthropic |
| Ollama / local LLM adapter | Phase 3 | Niche use case for MVP |
| Test DSL (domain-specific language for scenarios) | Phase 2 | Python SDK scenario API is sufficient for MVP |
| Visual test builder | Phase 2+ | Requires GUI dashboard |
| Snapshot testing | Phase 2 | Useful but not foundational |
| LLM-as-judge evaluation | Phase 3 | Advanced evaluation; deterministic assertions cover MVP |
| Shared mock registry | Phase 4 | Collaboration feature; not needed for single-developer use |
| Agent marketplace / public registry | Phase 4+ | Community scale feature |

---

## 6. Success Criteria

Each criterion must be met before the MVP is declared ready for public alpha release.

| # | Criterion | Measurement | Target |
|---|---|---|---|
| 1 | **Time to first mock** | Wall-clock time from `mockagents init` to a running mock server returning a valid response | **< 5 minutes** |
| 2 | **Throughput** | Requests per second on a single CPU core (static response, no streaming, measured with `wrk` or `hey`) | **>= 1,000 req/s** |
| 3 | **Offline operation** | Python SDK test suite passes with network interfaces disabled (no API keys, no DNS, no HTTP egress) | **100% pass rate** |
| 4 | **OpenAI protocol conformance** | Adapter passes a conformance test suite covering chat completions, function calling, streaming, usage stats, and stop reasons | **100% pass rate** |
| 5 | **Anthropic protocol conformance** | Adapter passes a conformance test suite covering messages, tool use, streaming events, usage stats, and stop reasons | **100% pass rate** |
| 6 | **Docker image size** | Compressed image size on Docker Hub | **< 50 MB** |
| 7 | **Binary size** | Compressed binary size for Linux amd64 | **< 30 MB** |
| 8 | **Agent definition validation** | `mockagents validate` catches 100% of schema violations in a test corpus of 20+ intentionally broken YAML files | **100% detection rate** |
| 9 | **Cross-platform builds** | CI produces working binaries for Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64) | **All 5 targets build and pass smoke tests** |
| 10 | **Python SDK compatibility** | SDK works with Python 3.9, 3.10, 3.11, 3.12 on Linux, macOS, and Windows | **All matrix combinations pass** |

---

## 7. Launch Checklist

Everything below must be completed before the public alpha announcement.

### Repository & Licensing
- [ ] GitHub repository is public under the `mock-agents` organization
- [ ] `LICENSE` file present (Apache 2.0)
- [ ] `CONTRIBUTING.md` with contribution guidelines, code of conduct reference, and DCO sign-off instructions
- [ ] `CODE_OF_CONDUCT.md` (Contributor Covenant)
- [ ] `.github/ISSUE_TEMPLATE/` with bug report and feature request templates
- [ ] `.github/PULL_REQUEST_TEMPLATE.md`

### Documentation
- [ ] `README.md` with project overview, quickstart (< 2 minutes to read), badges, and architecture diagram
- [ ] Documentation site live (Docusaurus or similar) with:
  - [ ] Installation guide (binary, Docker, pip)
  - [ ] Quickstart tutorial (end-to-end walkthrough)
  - [ ] Agent definition reference (full YAML schema documentation)
  - [ ] CLI reference (all commands and flags)
  - [ ] Python SDK reference (API docs)
  - [ ] Protocol adapter documentation (OpenAI, Anthropic)
  - [ ] Example gallery

### Distribution
- [ ] PyPI package published (`pip install mockagents` works)
- [ ] Docker image published to GitHub Container Registry (`ghcr.io/mock-agents/mockagents`)
- [ ] Docker image also mirrored to Docker Hub (`mockagents/mockagents`)
- [ ] GitHub Release with pre-built binaries for all 5 platform targets
- [ ] Homebrew formula submitted (optional but nice-to-have)

### CI/CD
- [ ] GitHub Actions CI pipeline green on `main`:
  - [ ] Go build and unit tests
  - [ ] Go linting (`golangci-lint`)
  - [ ] Python SDK tests (matrix: Python 3.9-3.12, Linux/macOS/Windows)
  - [ ] Protocol conformance tests
  - [ ] Docker image build and smoke test
  - [ ] Binary cross-compilation
- [ ] GitHub Actions release pipeline triggered on tag push (automated binary and image publishing)

### Content & Examples
- [ ] 3+ example agent definitions in `examples/` directory:
  - [ ] `customer-support-agent.yaml` — tool-calling agent with order lookup and ticket creation
  - [ ] `code-review-agent.yaml` — simple Q&A agent with scenario-based responses
  - [ ] `data-analyst-agent.yaml` — agent with multiple tools (query database, generate chart, export CSV)
- [ ] Example Python test files corresponding to each agent definition
- [ ] `CHANGELOG.md` with release notes for v0.1.0-alpha

### Quality Gates
- [ ] All success criteria in Section 6 are met and documented with evidence
- [ ] Security scan (Go dependencies via `govulncheck`, Python dependencies via `safety` or `pip-audit`)
- [ ] No critical or high-severity vulnerabilities in dependencies
- [ ] Manual smoke test on all 3 operating systems (Linux, macOS, Windows)

---

## 8. Post-MVP Priorities

Ordered by expected impact and dependency chain. Each item maps to a product plan phase.

| Priority | Feature | Phase | Rationale |
|---|---|---|---|
| 1 | **Test runner CLI (`mockagents test`)** | Phase 2 | Most-requested capability after basic mocking; enables native CI/CD integration without pytest dependency |
| 2 | **Record-and-playback mode (`mockagents record`)** | Phase 2 | Dramatically lowers mock creation effort; proxy captures real agent traffic and generates YAML definitions |
| 3 | **Multi-agent topology modeling** | Phase 2 | Unlocks the architect persona; sequential and parallel pipelines configurable via YAML |
| 4 | **TypeScript/Node.js SDK** | Phase 2 | Expands addressable market to full-stack developers; second-largest AI engineering community |
| 5 | **GUI dashboard v0.1** | Phase 2 | Agent catalog and interaction explorer; unlocks the application developer persona |
| 6 | **CrewAI and LangGraph adapters** | Phase 2 | Framework-specific adapters for the two most popular orchestration frameworks |
| 7 | **CI/CD plugins (GitHub Actions, GitLab CI)** | Phase 2 | First-class integration reduces friction in CI pipelines |
| 8 | **Chaos engineering (latency, errors, rate limits)** | Phase 3 | Unlocks the QA persona; resilience testing for agent integrations |
| 9 | **MCP server mocking** | Phase 3 | MCP adoption growing; mock MCP servers for tool and resource testing |
| 10 | **Google Gemini adapter** | Phase 3 | Third major LLM provider; broadens protocol coverage |
| 11 | **VS Code extension** | Phase 3 | IntelliSense for YAML definitions, inline test running |
| 12 | **Contract testing** | Phase 4 | Agent interface contracts with breaking change detection |
| 13 | **Kubernetes Helm chart** | Phase 4 | Enterprise deployment for load testing and staging environments |
| 14 | **OpenTelemetry integration** | Phase 4 | Observability for platform teams |
| 15 | **Cloud SaaS** | Phase 4 | Hosted multi-tenant service for teams without infrastructure capacity |

---

## 9. Rollback Plan

If the MVP launch encounters critical issues, the following escalation levels apply.

### Level 1 — Hotfix (severity: broken feature, not a showstopper)

**Trigger:** A specific feature (e.g., streaming, one protocol adapter) fails in production but the core mock engine works.

**Action:**
- Disable the broken feature via a feature flag or configuration toggle
- Publish a patch release (v0.1.1) within 48 hours
- Communicate the known issue and workaround in GitHub Discussions and the changelog

### Level 2 — Partial Rollback (severity: one distribution channel is broken)

**Trigger:** One distribution method fails (e.g., PyPI package is broken but Docker and binary work).

**Action:**
- Yank the broken package from the affected registry
- Direct users to an alternative installation method in the README
- Fix and republish within 72 hours
- Post-mortem to prevent recurrence

### Level 3 — Full Rollback (severity: core engine has a critical bug)

**Trigger:** The mock engine produces incorrect protocol responses, data loss in SQLite logging, or a security vulnerability is discovered.

**Action:**
- Mark the GitHub Release as pre-release and add a warning banner
- Yank the PyPI package
- Remove the `latest` tag from the Docker image
- Post a public incident notice in the repository's README and Discussions
- Root-cause analysis within 24 hours, fix within 1 week
- Re-launch as v0.1.1-alpha with the fix and a post-mortem published

### Level 4 — Launch Abort (severity: fundamental design flaw)

**Trigger:** The MVP thesis is invalidated (e.g., Go binary cannot achieve protocol conformance, Python SDK architecture is fundamentally incompatible with the mock server).

**Action:**
- Do not proceed with public announcement
- Archive the release as an internal milestone
- Conduct a design review with the team within 1 week
- Revise the technical architecture and restart the affected component
- Communicate transparently with any early testers or design partners

### General Rollback Principles

- **Every release is tagged in Git.** We can always check out and rebuild any prior version.
- **Docker images are immutable.** Specific version tags (e.g., `v0.1.0-alpha`) are never overwritten; only `latest` is updated.
- **PyPI supports yanking.** A broken package can be removed from installation without deleting the version permanently.
- **No database migrations in MVP.** SQLite schema is append-only in v0.1; no risk of irreversible migration failures.

---

*This document defines the boundary of the MockAgents MVP. Features not listed in Section 4 are not in scope, regardless of their presence in the product plan. Any scope changes require a revision of this document with updated success criteria and launch checklist.*
