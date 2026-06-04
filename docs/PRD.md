# MockAgents -- Product Requirements Document

> **Implementation status (updated 2026-06-04):** this document captures
> the original **Phase-1 MVP** requirements (single-agent mocking, CLI,
> Python SDK). It is preserved as the historical product spec; its
> "Out of Scope for MVP" items (MCP, chaos, multi-agent, cloud/SaaS) have
> since shipped in later phases. The product now spans Phases 1–4: multi-
> agent pipelines, chaos, MCP (incl. bidirectional), record/replay,
> contracts, OpenTelemetry, Python/TypeScript/Go SDKs, a Next.js web
> console, Helm packaging, and a multi-tenant control plane (RBAC with a
> `platform` super-admin role, audit, costs, in-place key rotation) — plus
> dedicated performance and security-hardening passes. For the
> authoritative, current list of what ships today see
> [PROGRESS.md](./PROGRESS.md) (slice ledger through §2.55) and
> [CHANGELOG.md](../CHANGELOG.md); for the live API surface see
> [api-spec.yaml](./api-spec.yaml). When this PRD and PROGRESS.md
> disagree, PROGRESS.md wins.

## 1. Document Info

| Field          | Value                                                      |
| -------------- | ---------------------------------------------------------- |
| **Document**   | Product Requirements Document (PRD) -- MockAgents MVP      |
| **Version**    | 1.0                                                        |
| **Date**       | 2026-04-07                                                 |
| **Author**     | MockAgents Product Team                                    |
| **Status**     | Draft                                                      |
| **Approvers**  | Engineering Lead, Product Lead, Architecture Lead          |
| **Target**     | Phase 1 MVP -- 3-month delivery (Months 1--3)              |
| **Repository** | `mock-agents` monorepo                                     |

### Revision History

| Version | Date       | Author              | Changes              |
| ------- | ---------- | -------------------- | -------------------- |
| 1.0     | 2026-04-07 | MockAgents Product Team | Initial draft      |

---

## 2. Product Overview

### 2.1 Problem Statement

AI agents are becoming the backbone of modern software systems -- from LLM-powered assistants with tool use to multi-agent orchestration frameworks like CrewAI, AutoGen, and LangGraph. Yet the tooling to **test** these agent integrations lags far behind what is available for traditional API and microservice testing.

Key pain points from industry research:

- **46% of teams** cite integration with existing systems as their primary challenge when deploying AI agents.
- Fewer than **1 in 4 organizations** have successfully scaled agents from prototype to production, with quality being the top barrier for 32%.
- Setup complexity, data pipeline failures, and legacy system compatibility extend implementation timelines significantly.
- **No widely-adopted, agent-specific mock/virtualization platform** exists analogous to WireMock or Mockoon for traditional APIs.

Teams today resort to ad-hoc mocking, burn real API tokens during development, and rely on unpredictable third-party services for testing -- leading to slow feedback loops, flaky tests, and costly iteration cycles.

### 2.2 Vision

MockAgents is an open-source platform that lets AI Engineers spin up realistic mock agents -- complete with configurable behaviors, tool responses, and streaming simulation -- so teams can test their agent integrations without calling real LLMs, burning tokens, or relying on unpredictable third-party services.

MockAgents provides deterministic, repeatable, cost-free simulation of AI agents, enabling teams to build, test, and ship reliable agent integrations with confidence.

### 2.3 Target User -- MVP

The MVP exclusively targets the **AI Engineer (Builder Persona)**:

**Who they are:** Developers building applications that integrate with LLM agents, tool-calling APIs, or agent pipelines.

**Key needs:**

- Mock an LLM agent's tool-call sequences to test application logic without burning API tokens.
- Simulate deterministic agent responses for unit and integration tests.
- Reproduce specific failure modes (tool errors, timeouts).
- Run tests locally and in CI/CD without external dependencies.

**Success signal:** "I can write a test for my agent integration in under 5 minutes, and it runs in CI without any API keys."

### 2.4 Key Architectural Decisions (Pre-Made)

| Decision                | Choice                                   | Rationale                                                   |
| ----------------------- | ---------------------------------------- | ----------------------------------------------------------- |
| Core engine language    | **Go**                                   | Faster MVP iteration than Rust, single binary, great concurrency primitives |
| Repository structure    | **Monorepo**                             | Unified versioning, simpler cross-package changes            |
| Database (MVP)          | **SQLite only**                          | Zero-config, embedded, no external dependencies              |
| Database (post-MVP)     | PostgreSQL added later                   | Scalable cloud deployment                                    |
| MVP interface           | **CLI only**                             | Matches AI Engineer workflow; GUI deferred                   |
| MVP SDK                 | **Python SDK only**                      | Largest AI engineering community                             |

---

## 3. Goals and Success Metrics

### 3.1 Product Goals

| # | Goal                                                                 |
| - | -------------------------------------------------------------------- |
| G1 | Deliver a working CLI tool that serves mock agent responses compatible with OpenAI and Anthropic APIs |
| G2 | Support tool-call simulation with configurable static and pattern-matched responses |
| G3 | Enable streaming response simulation with configurable chunking and timing |
| G4 | Provide a Python SDK for programmatic mock server control and assertions |
| G5 | Achieve zero external runtime dependencies -- single binary + SQLite |
| G6 | Ship a public alpha release on PyPI and GitHub within 3 months |

### 3.2 Success Metrics (KPIs)

| Metric                                | Target (Phase 1, 3 months) | Measurement Method                        |
| ------------------------------------- | -------------------------- | ----------------------------------------- |
| Time to first mock agent response     | Under 5 minutes            | Timed user testing with quickstart guide  |
| GitHub stars                          | 500                        | GitHub analytics                          |
| PyPI downloads (Python SDK)           | 1,000                      | PyPI download stats                       |
| Community contributors                | 10                         | GitHub contributor count                  |
| Mock server startup time              | Under 500ms                | Automated benchmark suite                 |
| Mock response latency (p99)           | Under 10ms (non-streaming) | Automated benchmark suite                 |
| Test suite pass rate in CI            | 100% on main branch        | CI/CD pipeline metrics                    |
| Agent definition validation errors    | Zero false positives       | User-reported issues                      |
| Protocol adapter compatibility score  | 95%+ with official client SDKs | Compatibility test suite               |
| Documentation coverage                | 100% of CLI commands and SDK public API | Documentation audit              |

---

## 4. User Stories

### Epic 1: Agent Definition

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-1.1 | As an AI Engineer, I want to define a mock agent in a YAML file so that my mock configuration is declarative, version-controllable, and human-readable. | 1. YAML schema supports `apiVersion`, `kind`, `metadata`, and `spec` fields. 2. Agent definition is loadable by the mock engine. 3. Invalid YAML produces a clear validation error with line number. |
| US-1.2 | As an AI Engineer, I want to specify which protocol an agent speaks (OpenAI or Anthropic) so that client SDKs can connect without modification. | 1. `spec.protocol` accepts `openai-chat-completions` and `anthropic-messages`. 2. Protocol determines the wire format of responses. |
| US-1.3 | As an AI Engineer, I want to define the tools an agent can call, with input/output schemas, so that tool-call simulation is realistic. | 1. `spec.tools` accepts an array of tool definitions with `name`, `description`, `parameters` (JSON Schema). 2. Each tool can have multiple response mappings. |
| US-1.4 | As an AI Engineer, I want to configure an agent's system prompt and reported model name so that my tests match production configuration. | 1. `spec.systemPrompt` is included in responses where the protocol supports it. 2. `spec.model` is returned in response metadata. |
| US-1.5 | As an AI Engineer, I want to validate my agent definition files before starting the server so that configuration errors are caught early. | 1. `mockagents validate` checks all YAML files in a directory. 2. Reports all errors (does not stop at first). 3. Exit code is non-zero when errors exist. |

### Epic 2: Mock Server

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-2.1 | As an AI Engineer, I want to start a local mock server with a single command so that I can begin testing immediately. | 1. `mockagents start` launches an HTTP server on a configurable port (default 8080). 2. Server loads all agent definitions from the project directory. 3. Server is ready within 500ms. |
| US-2.2 | As an AI Engineer, I want the mock server to serve multiple agents simultaneously so that I can test multi-agent integrations from a single server. | 1. Each agent is routable by a path prefix or model name. 2. All agents share the same server process. |
| US-2.3 | As an AI Engineer, I want to hot-reload agent definitions when files change so that I do not have to restart the server during development. | 1. File changes are detected within 2 seconds. 2. Only changed agents are reloaded. 3. In-flight requests are not interrupted. |
| US-2.4 | As an AI Engineer, I want structured request/response logging so that I can debug failing tests. | 1. Logs include timestamp, request path, matched agent, matched scenario, and response summary. 2. Log level is configurable (debug, info, warn, error). 3. Logs can be output to stdout or a file. |

### Epic 3: Tool Call Simulation

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-3.1 | As an AI Engineer, I want the mock agent to return tool-call requests in responses so that my application's tool-handling logic is exercised. | 1. Response includes `tool_calls` array (OpenAI format) or `tool_use` blocks (Anthropic format). 2. Tool calls reference tools defined in the agent spec. |
| US-3.2 | As an AI Engineer, I want to define tool response stubs matched by input parameters so that different inputs produce different outputs. | 1. Tool responses can be matched by exact parameter values. 2. A `default` response is returned when no match is found. 3. Match evaluation is deterministic. |
| US-3.3 | As an AI Engineer, I want to simulate tool errors (not-found, timeout, malformed response) so that I can test my application's error handling. | 1. Tool response mapping supports an `error` field with `code` and `message`. 2. The error is returned in the protocol-appropriate format. |
| US-3.4 | As an AI Engineer, I want to simulate multi-step tool chains (agent calls tool A, then tool B based on A's result) so that I can test sequential tool use. | 1. Agent scenarios can define an ordered sequence of tool calls. 2. Each step's response is available to subsequent steps via template variables. |
| US-3.5 | As an AI Engineer, I want tool-call parameter validation against the defined JSON Schema so that I can verify my application sends correct parameters. | 1. When validation is enabled, requests with invalid parameters return a schema validation error. 2. Validation can be toggled per-tool. |

### Epic 4: Streaming

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-4.1 | As an AI Engineer, I want the mock agent to stream responses token-by-token via SSE so that I can test my streaming UI/handler code. | 1. When the client sets `stream: true`, the server responds with `text/event-stream`. 2. Each SSE event contains a chunk matching the protocol format (OpenAI `delta` / Anthropic `content_block_delta`). |
| US-4.2 | As an AI Engineer, I want to configure chunk size and inter-chunk delay so that I can simulate realistic streaming timing. | 1. `spec.behavior.streaming.chunk_size` controls tokens per event. 2. `spec.behavior.streaming.chunk_delay_ms` controls the pause between events. 3. Defaults are sensible (4 tokens, 50ms). |
| US-4.3 | As an AI Engineer, I want to stream tool-call chunks so that I can test incremental tool-call parsing. | 1. Tool calls are delivered incrementally across multiple SSE events. 2. The chunking matches the real provider's behavior (e.g., OpenAI sends function name first, then arguments in pieces). |

### Epic 5: Protocol Adapters (OpenAI + Anthropic)

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-5.1 | As an AI Engineer, I want the mock server to expose an OpenAI-compatible `/v1/chat/completions` endpoint so that I can point the official OpenAI Python/Node SDK at it without code changes. | 1. Endpoint accepts the full OpenAI Chat Completions request schema. 2. Responses conform to the OpenAI response schema including `id`, `object`, `created`, `model`, `choices`, and `usage`. 3. The official `openai` Python SDK works with `base_url` pointed at the mock. |
| US-5.2 | As an AI Engineer, I want the mock server to expose an Anthropic-compatible `/v1/messages` endpoint so that I can point the official Anthropic Python SDK at it. | 1. Endpoint accepts the Anthropic Messages API request schema. 2. Responses conform to the Anthropic response schema including `id`, `type`, `role`, `content`, `model`, `stop_reason`, and `usage`. 3. The official `anthropic` Python SDK works with `base_url` pointed at the mock. |
| US-5.3 | As an AI Engineer, I want OpenAI function-calling fields (`tools`, `tool_choice`) to be correctly handled so that my tool-calling tests work end-to-end. | 1. Request `tools` array is parsed and validated. 2. `tool_choice` (`auto`, `none`, `required`, specific function) is respected. 3. Response `tool_calls` array uses OpenAI's schema (`id`, `type`, `function.name`, `function.arguments`). |
| US-5.4 | As an AI Engineer, I want Anthropic tool-use fields (`tools`, `tool_choice`) to be correctly handled. | 1. Request `tools` array is parsed. 2. Response `tool_use` content blocks use Anthropic's schema (`type`, `id`, `name`, `input`). 3. `tool_result` content blocks are accepted in follow-up requests. |
| US-5.5 | As an AI Engineer, I want the mock to return realistic `usage` fields (prompt tokens, completion tokens) so that my token-tracking code can be tested. | 1. `usage` object is included in every non-streaming response. 2. Token counts are approximated from input/output text length. |

### Epic 6: CLI

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-6.1 | As an AI Engineer, I want to run `mockagents init` to scaffold a new project so that I have a working starting point. | 1. Creates a project directory with a sample agent YAML, a sample test, and a config file. 2. Includes a README with next steps. 3. Works in an empty directory. |
| US-6.2 | As an AI Engineer, I want to run `mockagents start` with flags for port, log level, and config directory so that I can customize server behavior. | 1. `--port` (default 8080), `--log-level` (default info), `--dir` (default `.`). 2. Flags can also be set via environment variables (`MOCKAGENTS_PORT`, etc.). 3. `--help` documents all flags. |
| US-6.3 | As an AI Engineer, I want to run `mockagents validate` to check all agent definitions so that I catch errors before starting the server. | 1. Validates YAML syntax and schema. 2. Checks that tool parameter schemas are valid JSON Schema. 3. Reports all errors with file path and line number. 4. Exits with code 0 on success, 1 on failure. |
| US-6.4 | As an AI Engineer, I want clear, colored terminal output with progress indicators so that the CLI feels polished and professional. | 1. Errors are red, warnings are yellow, success is green. 2. Server startup shows a clear "ready" message with the URL. 3. `--no-color` flag disables colored output. |
| US-6.5 | As an AI Engineer, I want the CLI to be distributed as a single binary with no runtime dependencies so that installation is trivial. | 1. Pre-built binaries are available for Linux (amd64, arm64), macOS (amd64, arm64), and Windows (amd64). 2. Installation is a single download or `go install`. 3. No Go runtime, Python, or other dependencies required. |

### Epic 7: Python SDK

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-7.1 | As an AI Engineer, I want to start and stop a mock server from Python so that I can use it in pytest fixtures. | 1. `MockAgentServer.start()` / `.stop()` lifecycle methods. 2. Context manager support (`with MockAgentServer(...) as server`). 3. Server runs in a background thread or subprocess. |
| US-7.2 | As an AI Engineer, I want to load agent definitions from YAML files via the SDK so that my test setup is minimal. | 1. `MockAgentServer.from_config("path/to/agent.yaml")` loads and validates the definition. 2. Multiple agent files can be loaded. |
| US-7.3 | As an AI Engineer, I want an assertion library to verify tool calls, response content, and latency so that my tests are expressive. | 1. `expect(result).to_have_tool_call(name, params)` checks tool call presence. 2. `expect(result).to_have_response_containing(text)` checks response content. 3. `expect(result.latency_ms).to_be_less_than(n)` checks latency. |
| US-7.4 | As an AI Engineer, I want the SDK to be installable via pip so that it integrates with standard Python tooling. | 1. Published to PyPI as `mockagents`. 2. Supports Python 3.10+. 3. Minimal dependencies (requests, pyyaml). |
| US-7.5 | As an AI Engineer, I want to define scenarios programmatically so that I can generate test cases dynamically. | 1. `Scenario` class accepts a name and a list of message steps. 2. `server.run_scenario(scenario)` executes the scenario and returns a result object. |

### Epic 8: Response Generation

| ID    | User Story                                                                                                    | Acceptance Criteria                                                                                                           |
| ----- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| US-8.1 | As an AI Engineer, I want to define static responses mapped to input patterns so that my tests are fully deterministic. | 1. `behavior.scenarios[].match.content_contains` matches substring in user message. 2. Matched scenario's `response.content` is returned verbatim. 3. First matching scenario wins. |
| US-8.2 | As an AI Engineer, I want to use template expressions in responses so that I can generate dynamic but predictable output. | 1. Template syntax (e.g., `{{ random_int min max }}`, `{{ date_offset N unit }}`, `{{ request.content }}`) is supported. 2. Templates are evaluated at response time. 3. Custom template functions can be registered. |
| US-8.3 | As an AI Engineer, I want a default/fallback response when no scenario matches so that the server never returns an unexpected error. | 1. A scenario with no `match` field acts as the default. 2. If no default is defined, a sensible built-in fallback is used. 3. The fallback response is valid for the agent's protocol. |
| US-8.4 | As an AI Engineer, I want to use regex patterns for matching so that I can write flexible input matchers. | 1. `match.content_regex` accepts a regular expression. 2. Capture groups are available as template variables. |

---

## 5. Functional Requirements

### 5.1 Agent Definition and Configuration

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-001 | P0       | The system shall load agent definitions from YAML files conforming to the `mockagents/v1` schema. | YAML files with `apiVersion: mockagents/v1` and `kind: Agent` are parsed and validated. Invalid files produce actionable errors.     |
| FR-002 | P0       | The system shall support `metadata` fields: `name` (required, unique), `description`, and `tags`. | Agent name uniqueness is enforced within a project. Description and tags are optional.                                               |
| FR-003 | P0       | The system shall support `spec.protocol` with values `openai-chat-completions` and `anthropic-messages`. | Protocol value determines response wire format. Unsupported values produce a validation error.                                       |
| FR-004 | P1       | The system shall support `spec.model` to set the reported model name in responses.                | Model name appears in the `model` field of API responses. Defaults to `mock-agent` if unset.                                         |
| FR-005 | P1       | The system shall support `spec.systemPrompt` as a string field.                                   | System prompt is stored and available for template rendering. It is included in OpenAI response context where applicable.             |
| FR-006 | P0       | The system shall support `spec.tools` as an array of tool definitions with JSON Schema parameters.| Each tool has `name` (required), `description`, `parameters` (valid JSON Schema). Duplicate tool names produce a validation error.   |
| FR-007 | P0       | The system shall support tool response mappings with `match` (parameter matching) and `default`.  | Exact-match on parameter values selects the response. `default` is used when no match hits. At least one response mapping is required.|
| FR-008 | P1       | The system shall support tool error responses with `code` and `message` fields.                   | Error responses are returned in the protocol-appropriate error format.                                                                |

### 5.2 Mock Server

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-009 | P0       | The system shall start an HTTP server on a configurable port (default 8080).                      | Server accepts `--port` flag and `MOCKAGENTS_PORT` env var. Startup completes in under 500ms.                                        |
| FR-010 | P0       | The system shall serve multiple agents from a single server instance.                             | Agents are routable by model name in the request body. All loaded agents respond on the same port.                                   |
| FR-011 | P1       | The system shall watch agent definition files and hot-reload on changes.                          | Changes are detected within 2 seconds. Reload does not drop in-flight requests. Reload errors are logged without crashing the server.|
| FR-012 | P1       | The system shall log all requests and responses in structured JSON format.                        | Logs include timestamp, method, path, agent name, matched scenario, status code, and latency. Log level is configurable.             |
| FR-013 | P0       | The system shall store agent definitions and request logs in an embedded SQLite database.         | SQLite DB is created automatically in the project directory. No external database setup required.                                     |
| FR-014 | P1       | The system shall return appropriate HTTP error codes for malformed requests.                      | 400 for invalid request body, 404 for unknown routes, 422 for schema validation failures. Error bodies include a message field.      |

### 5.3 Tool Call Simulation

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-015 | P0       | The system shall include tool calls in responses when the matched scenario specifies them.        | Tool calls follow the wire format of the agent's protocol (OpenAI `tool_calls` / Anthropic `tool_use` content blocks).               |
| FR-016 | P0       | The system shall resolve tool response stubs by matching request parameters against defined matchers. | Exact match on specified parameter values. Unspecified parameters are ignored during matching. First match wins.                     |
| FR-017 | P1       | The system shall support multi-step tool-call sequences within a single scenario.                 | Scenario can define an ordered list of tool calls. Each step executes after the client submits the previous tool result.              |
| FR-018 | P1       | The system shall validate tool-call parameters against the tool's JSON Schema when validation is enabled. | Validation is togglable per tool via a `validate` boolean. Validation failures return a 422 response with details.                  |
| FR-019 | P1       | The system shall support template expressions inside tool response values.                        | Template functions (`random_int`, `date_offset`, etc.) are evaluated at response time within tool response payloads.                 |

### 5.4 Streaming

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-020 | P0       | The system shall support SSE streaming when `stream: true` is set in the request.                 | Response `Content-Type` is `text/event-stream`. Events are formatted as `data: {json}\n\n`. Stream ends with `data: [DONE]` (OpenAI) or `event: message_stop` (Anthropic). |
| FR-021 | P0       | The system shall chunk text responses into configurable token-sized pieces.                       | `chunk_size` (default 4 tokens) and `chunk_delay_ms` (default 50ms) are configurable per agent.                                     |
| FR-022 | P1       | The system shall stream tool-call chunks incrementally.                                           | Function name is sent in the first chunk, arguments are split across subsequent chunks. Format matches real provider behavior.        |
| FR-023 | P2       | The system shall support configurable streaming backpressure behavior.                            | When the client reads slowly, the server can be configured to buffer, drop, or pause. Default is buffer.                             |

### 5.5 Protocol Adapters

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-024 | P0       | The system shall expose a `/v1/chat/completions` endpoint fully compatible with the OpenAI Chat Completions API. | Accepts OpenAI request schema. Returns OpenAI response schema. Official `openai` Python SDK connects successfully with `base_url` override. |
| FR-025 | P0       | The system shall expose a `/v1/messages` endpoint fully compatible with the Anthropic Messages API. | Accepts Anthropic request schema. Returns Anthropic response schema. Official `anthropic` Python SDK connects successfully with `base_url` override. |
| FR-026 | P0       | The system shall handle OpenAI function-calling fields: `tools`, `tool_choice`, and response `tool_calls`. | `tool_choice` values (`auto`, `none`, `required`, specific function) influence whether tool calls appear in the response.           |
| FR-027 | P0       | The system shall handle Anthropic tool-use fields: request `tools`, response `tool_use` blocks, and follow-up `tool_result` blocks. | Tool use blocks conform to Anthropic schema. `tool_result` in follow-up messages is parsed and available for scenario matching.      |
| FR-028 | P1       | The system shall return a realistic `usage` object with `prompt_tokens` and `completion_tokens`.  | Token counts are approximated (word count * 1.3 as a heuristic). Counts are consistent between streaming and non-streaming modes.    |
| FR-029 | P1       | The system shall generate unique, realistic response IDs.                                         | OpenAI: `chatcmpl-{uuid}`. Anthropic: `msg_{uuid}`. IDs are unique per response.                                                   |
| FR-030 | P2       | The system shall support OpenAI structured output (`response_format: { type: "json_object" }`).   | When `response_format` is set, the response content is valid JSON. Non-JSON responses produce a simulated refusal.                   |

### 5.6 CLI

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-031 | P0       | The CLI shall provide an `init` command that scaffolds a new MockAgents project.                  | Creates directory structure with sample agent YAML, sample config, and README. Works in empty or existing directories.               |
| FR-032 | P0       | The CLI shall provide a `start` command that launches the mock server.                            | Accepts `--port`, `--log-level`, `--dir` flags. Prints server URL when ready. Handles SIGINT/SIGTERM for graceful shutdown.          |
| FR-033 | P0       | The CLI shall provide a `validate` command that checks all agent definitions.                     | Validates YAML syntax, schema conformance, JSON Schema validity of tool parameters. Reports all errors. Exit code 1 on failure.      |
| FR-034 | P1       | The CLI shall support `--output json` for machine-readable output.                                | All commands support `--output json` producing structured JSON to stdout. Default is human-readable text with colors.                |
| FR-035 | P1       | The CLI shall display a `--version` flag showing the current version.                             | Outputs the semantic version (e.g., `mockagents v0.1.0`).                                                                           |
| FR-036 | P1       | The CLI shall support `--no-color` to disable colored terminal output.                            | All ANSI color codes are stripped. Also respects `NO_COLOR` environment variable.                                                    |
| FR-037 | P0       | The CLI shall be distributed as a statically-linked single binary for Linux, macOS, and Windows.  | Pre-built binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64. Also installable via `go install`.       |

### 5.7 Python SDK

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-038 | P0       | The SDK shall provide a `MockAgentServer` class with `start()`, `stop()`, and context manager support. | Server launches the Go binary as a subprocess. Context manager ensures cleanup. Port is auto-assigned if not specified.              |
| FR-039 | P0       | The SDK shall provide `MockAgentServer.from_config(path)` to load agent definitions from YAML.    | Accepts a single file path or a list of paths. Validates definitions before starting. Raises `ConfigError` on invalid YAML.          |
| FR-040 | P0       | The SDK shall provide a `Scenario` class for defining interaction sequences.                      | Scenario has `name` (str) and `steps` (list of message dicts with `role` and `content`). Scenarios are passed to `server.run_scenario()`. |
| FR-041 | P0       | The SDK shall provide an assertion module with `expect()` for fluent test assertions.             | `to_have_tool_call(name, params)`, `to_have_response_containing(text)`, `to_have_tool_error(code)`, `to_be_less_than(n)`, `to_be_greater_than(n)`. Failed assertions raise `AssertionError` with descriptive messages. |
| FR-042 | P0       | The SDK shall be published to PyPI as `mockagents` supporting Python 3.10+.                      | `pip install mockagents` works. Dependencies are minimal (requests, pyyaml). Wheels include the Go binary for supported platforms.   |
| FR-043 | P1       | The SDK shall support pytest fixtures for common test patterns.                                   | `mockagents.pytest` module provides a `mock_agent_server` fixture. Fixture is session-scoped by default, configurable per test.      |

### 5.8 Response Generation

| ID     | Priority | Requirement                                                                                       | Acceptance Criteria                                                                                                                  |
| ------ | -------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| FR-044 | P0       | The system shall support static response mapping via `behavior.scenarios`.                        | Each scenario has an optional `match` and a required `response`. Scenarios are evaluated in order; first match wins.                 |
| FR-045 | P0       | The system shall support `content_contains` matching on the last user message.                    | Substring match is case-insensitive. Matches against the `content` field of the last `user` role message.                            |
| FR-046 | P1       | The system shall support `content_regex` matching with capture group extraction.                  | Regex is evaluated against the last user message. Named capture groups are available as `{{ match.group_name }}` in templates.        |
| FR-047 | P0       | The system shall support template expressions in response content and tool response values.       | Built-in functions: `random_int(min, max)`, `random_string(length)`, `date_offset(n, unit)`, `uuid()`, `request.content`. Template errors produce a 500 with details. |
| FR-048 | P0       | The system shall provide a default fallback response when no scenario matches.                    | Default scenario (no `match` field) is used. If none defined, returns a protocol-valid response with content "Mock response from {agent_name}". |
| FR-049 | P2       | The system shall support response metadata for custom key-value pairs.                            | `response.metadata` is a free-form map. Metadata is accessible to the SDK but not included in the protocol response body.           |

---

## 6. Non-Functional Requirements

### 6.1 Performance

| ID      | Requirement                                                                          | Target                        |
| ------- | ------------------------------------------------------------------------------------ | ----------------------------- |
| NFR-001 | Mock server startup time (cold start, 10 agent definitions)                          | Under 500ms                   |
| NFR-002 | Non-streaming response latency (p50)                                                 | Under 5ms                     |
| NFR-003 | Non-streaming response latency (p99)                                                 | Under 10ms                    |
| NFR-004 | Streaming first-byte latency                                                         | Under 10ms                    |
| NFR-005 | Concurrent connection handling                                                       | 1,000+ simultaneous connections |
| NFR-006 | Agent definition hot-reload time                                                     | Under 2 seconds               |
| NFR-007 | CLI binary size                                                                      | Under 30 MB                   |
| NFR-008 | Memory usage (idle, 10 agents loaded)                                                | Under 50 MB                   |

### 6.2 Scalability

| ID      | Requirement                                                                          | Target                        |
| ------- | ------------------------------------------------------------------------------------ | ----------------------------- |
| NFR-009 | Number of agent definitions per project                                              | 100+ without degradation      |
| NFR-010 | Number of scenarios per agent                                                        | 500+ without degradation      |
| NFR-011 | Number of tools per agent                                                            | 50+ without degradation       |
| NFR-012 | SQLite database size for request logs                                                | Automatic rotation at 100 MB  |

### 6.3 Reliability

| ID      | Requirement                                                                          | Target                        |
| ------- | ------------------------------------------------------------------------------------ | ----------------------------- |
| NFR-013 | Server uptime during development sessions                                            | No crashes from valid configs |
| NFR-014 | Graceful handling of invalid agent definitions                                       | Server continues with valid agents; invalid agents are skipped with errors logged |
| NFR-015 | Graceful shutdown on SIGINT/SIGTERM                                                  | In-flight requests complete (up to 5s timeout), then server exits |
| NFR-016 | Hot-reload failure handling                                                           | Previous valid configuration is retained; error is logged |

### 6.4 Security

| ID      | Requirement                                                                          | Target                        |
| ------- | ------------------------------------------------------------------------------------ | ----------------------------- |
| NFR-017 | The mock server shall bind to localhost by default.                                   | Binding to `0.0.0.0` requires explicit `--host` flag |
| NFR-018 | No authentication is required for MVP (local development tool).                      | Documented as a known limitation; auth planned for cloud deployment |
| NFR-019 | The server shall not execute arbitrary code from agent definitions.                  | Template expressions are sandboxed; no file system access, no network calls, no shell execution |
| NFR-020 | Dependencies shall be audited for known vulnerabilities before release.              | Go module dependencies checked via `govulncheck`; Python dependencies checked via `pip-audit` |

### 6.5 Usability

| ID      | Requirement                                                                          | Target                        |
| ------- | ------------------------------------------------------------------------------------ | ----------------------------- |
| NFR-021 | Time from install to first working mock                                              | Under 5 minutes using quickstart guide |
| NFR-022 | Error messages shall include actionable remediation steps.                            | Every validation error suggests how to fix it |
| NFR-023 | CLI help text shall be comprehensive and include examples.                            | Every command and flag has a description; key commands have inline examples |
| NFR-024 | Documentation shall cover all CLI commands, SDK public API, and YAML schema.          | 100% coverage of public surface area |
| NFR-025 | The YAML schema shall be published as a JSON Schema for IDE autocompletion.           | JSON Schema file included in the repository and referenced via `$schema` in generated YAML |

### 6.6 Compatibility

| ID      | Requirement                                                                          | Target                        |
| ------- | ------------------------------------------------------------------------------------ | ----------------------------- |
| NFR-026 | OpenAI Python SDK compatibility                                                      | Tested against `openai` v1.x |
| NFR-027 | Anthropic Python SDK compatibility                                                   | Tested against `anthropic` v0.40+ |
| NFR-028 | Platform support                                                                     | Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64) |
| NFR-029 | Python SDK compatibility                                                             | Python 3.10, 3.11, 3.12, 3.13 |
| NFR-030 | Go version                                                                           | Go 1.26+ for building from source |

---

## 7. Out of Scope for MVP

The following capabilities are explicitly deferred to Phase 2 and beyond. They are documented here to set expectations and prevent scope creep.

| Item                                  | Deferred To | Rationale                                                          |
| ------------------------------------- | ----------- | ------------------------------------------------------------------ |
| GUI dashboard                         | Phase 2     | CLI-first approach; GUI adds significant frontend complexity       |
| Multi-agent orchestration simulation  | Phase 2     | Requires topology modeling, inter-agent messaging, and graph execution engine |
| Model Context Protocol (MCP) support  | Phase 3     | MCP spec is still maturing; MVP focuses on established protocols   |
| Chaos engine (latency/error injection)| Phase 3     | Valuable but not essential for initial adoption                    |
| Go SDK                                | Phase 4     | AI engineering community primarily uses Python; Go SDK has smaller audience |
| TypeScript/Node.js SDK                | Phase 2     | Python-first; TypeScript SDK follows after patterns are proven     |
| Cloud/SaaS deployment                 | Phase 4     | MVP is a local development tool; cloud adds auth, multi-tenancy, billing |
| Record-and-playback mode              | Phase 2     | Useful but not required for initial mock-based testing             |
| LLM-backed responses                  | Phase 2+    | Defeats the "no API keys needed" principle of the MVP              |
| Test DSL and test runner              | Phase 2     | MVP provides assertion helpers in the Python SDK; dedicated runner comes later |
| CI/CD plugins (GitHub Actions, etc.)  | Phase 2     | Users can call the CLI directly in CI for now                      |
| Visual workflow editor                | Phase 4     | Requires GUI dashboard                                             |
| IDE extensions (VS Code, JetBrains)   | Phase 3+    | Nice-to-have; JSON Schema provides basic IDE support in the interim |
| Contract testing                      | Phase 4     | Enterprise feature; requires shared registry infrastructure        |
| OpenTelemetry integration             | Phase 4     | Observability is important but not critical for local development  |
| LLM-as-judge evaluation               | Phase 3     | Requires LLM integration which is antithetical to MVP's zero-dependency goal |
| Kubernetes/Helm deployment            | Phase 4     | Docker image is sufficient for CI/CD; Kubernetes adds operational complexity |
| Google Gemini API adapter             | Phase 2+    | OpenAI and Anthropic cover the vast majority of current usage      |
| Ollama / local LLM API adapter       | Phase 2+    | Lower priority than cloud LLM API adapters                         |
| Agent framework adapters (CrewAI, LangGraph, etc.) | Phase 2 | Protocol-level mocking (OpenAI/Anthropic APIs) covers most framework usage indirectly |

---

## 8. Dependencies and Assumptions

### 8.1 Dependencies

| ID   | Dependency                                                 | Type       | Risk    | Notes                                                       |
| ---- | ---------------------------------------------------------- | ---------- | ------- | ----------------------------------------------------------- |
| D-01 | Go 1.26+ toolchain                                        | Build      | Low     | Stable, well-supported language with predictable releases   |
| D-02 | SQLite (via `modernc.org/sqlite` or `mattn/go-sqlite3`)   | Runtime    | Low     | Pure Go or CGo binding; embedded, no external dependency    |
| D-03 | OpenAI Chat Completions API specification                  | External   | Medium  | API evolves; adapter must track changes. Version-pin to a known spec snapshot. |
| D-04 | Anthropic Messages API specification                       | External   | Medium  | API evolves; same mitigation as OpenAI.                     |
| D-05 | Python 3.10+ ecosystem (pip, PyPI)                         | Build/Dist | Low     | Mature and stable                                           |
| D-06 | GitHub Actions (for CI/CD)                                 | Build      | Low     | Standard; alternative CI systems are straightforward        |
| D-07 | Cross-compilation targets (GOOS/GOARCH)                    | Build      | Low     | Go's cross-compilation is first-class                       |

### 8.2 Assumptions

| ID   | Assumption                                                                                        | Impact if Wrong                                                         |
| ---- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| A-01 | AI Engineers are the primary early adopters and are comfortable with CLI tools and YAML.           | Lower adoption; may need to accelerate GUI development.                 |
| A-02 | OpenAI and Anthropic APIs cover 80%+ of agent integration testing needs for the MVP audience.     | Need to add more protocol adapters sooner than planned.                 |
| A-03 | Single-agent mocking is sufficient for MVP; most users do not need multi-agent simulation immediately. | Lower perceived value; may need to pull multi-agent forward.           |
| A-04 | Python is the dominant SDK language for AI engineering workflows.                                  | If TypeScript demand is high, need to accelerate TS SDK.               |
| A-05 | SQLite performance is adequate for local development and CI workloads.                             | Need to accelerate PostgreSQL support.                                 |
| A-06 | Users will point official OpenAI/Anthropic SDKs at the mock server via `base_url` override.       | If SDKs make this difficult, may need to provide wrapper clients.      |
| A-07 | A monorepo structure will not impede contribution velocity.                                       | May need to split into separate repositories.                          |
| A-08 | The `mockagents/v1` YAML schema will be stable enough to avoid breaking changes during Phase 1.   | Breaking changes require migration tooling and erode trust.            |

---

## 9. Risks and Mitigations

| ID   | Risk                                                                    | Impact | Likelihood | Mitigation                                                                                 |
| ---- | ----------------------------------------------------------------------- | ------ | ---------- | ------------------------------------------------------------------------------------------ |
| R-01 | **Protocol churn** -- OpenAI/Anthropic APIs change frequently, breaking adapter compatibility. | High   | High       | Abstract protocol layer behind a versioned `Adapter` interface. Version adapters independently. Maintain a compatibility matrix tested in CI against pinned SDK versions. |
| R-02 | **Adoption friction** -- developers already use ad-hoc mocking (hand-rolled stubs, httptest). | Medium | Medium     | Provide a 5-minute quickstart, zero-config defaults, and clear documentation showing the value over ad-hoc approaches. Publish comparison guides. |
| R-03 | **Scope creep** -- pressure to add multi-agent, chaos, evaluation, or GUI features before the core is solid. | High   | High       | This PRD is the scope contract. All additions require PRD amendment and approval. Phase 1 scope is locked. |
| R-04 | **SDK/binary distribution complexity** -- bundling a Go binary inside a Python wheel is non-trivial. | Medium | Medium     | Use platform-specific wheels with pre-built binaries. Fallback to subprocess-based server start with system-installed binary. Test on all target platforms in CI. |
| R-05 | **Streaming fidelity** -- subtle differences between real and mock streaming behavior cause false test results. | Medium | Medium     | Record real streaming sessions from OpenAI/Anthropic and use them as golden fixtures. Test mock streaming against the same assertions. |
| R-06 | **Template engine security** -- user-provided templates could enable injection or resource exhaustion. | Medium | Low        | Sandbox template execution: no file I/O, no network, no recursion, execution timeout. Whitelist available functions. |
| R-07 | **Go ecosystem maturity for AI tooling** -- Go is less common in AI/ML; community contributions may be harder to attract. | Low    | Medium     | Python SDK is the primary developer interface. Go is an implementation detail. Contributor docs emphasize that most contributions are YAML/Python, not Go. |
| R-08 | **Single-maintainer risk** -- early-stage project depends on a small team. | High   | Medium     | Write comprehensive contributor docs, maintain high test coverage (>80%), use conventional commit messages, and automate releases. Actively recruit co-maintainers. |

---

## 10. Glossary

| Term                        | Definition                                                                                                                         |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| **Agent**                   | An AI system (typically LLM-based) that can take actions, use tools, and produce responses. In MockAgents, a simulated version of such a system. |
| **Agent Definition**        | A YAML file conforming to the `mockagents/v1` schema that declaratively specifies a mock agent's identity, tools, and behavior.    |
| **Adapter**                 | A component that translates between MockAgents' internal representation and a specific wire protocol (e.g., OpenAI, Anthropic).    |
| **Chat Completions API**    | OpenAI's API endpoint (`/v1/chat/completions`) for generating LLM responses from a sequence of messages.                           |
| **Messages API**            | Anthropic's API endpoint (`/v1/messages`) for generating Claude responses from a sequence of messages.                             |
| **Tool Call**               | A structured request from an LLM to execute a specific function/tool with given parameters. Also called "function calling."        |
| **Tool Use**                | Anthropic's term for tool calling. Represented as `tool_use` content blocks in responses.                                          |
| **SSE (Server-Sent Events)**| A standard for servers to push events to clients over HTTP. Used by OpenAI and Anthropic for streaming responses.                  |
| **Scenario**                | A named behavior rule within an agent definition that maps input conditions to a specific response.                                |
| **Scenario Matching**       | The process of evaluating incoming requests against scenario conditions to select the appropriate response.                         |
| **Template Expression**     | A placeholder in a response (e.g., `{{ random_int 1 100 }}`) that is evaluated at response time to produce dynamic content.        |
| **Mock Engine**             | The core runtime component that loads agent definitions, evaluates scenarios, and generates responses.                              |
| **Hot Reload**              | The ability to detect file changes and reload agent definitions without restarting the server.                                      |
| **Wire Format**             | The specific JSON structure used in HTTP requests and responses for a given protocol (OpenAI, Anthropic).                          |
| **Protocol**                | The API specification that a mock agent implements (e.g., `openai-chat-completions`, `anthropic-messages`).                        |
| **Monorepo**                | A single repository containing all project components (Go engine, Python SDK, docs, CI config).                                    |
| **MCP (Model Context Protocol)** | An open protocol for connecting LLMs to external tools and data sources. Out of scope for MVP.                                |
| **Chaos Engineering**       | The practice of injecting faults (latency, errors, failures) to test system resilience. Out of scope for MVP.                      |
| **P0 / P1 / P2**           | Priority levels. P0 = must-have for MVP launch. P1 = strongly desired for MVP, may slip to fast-follow. P2 = nice-to-have, planned for post-MVP. |
| **Golden Fixture**          | A recorded real-world API response used as a reference to validate mock fidelity.                                                  |

---

*End of document.*
