# MockAgents Test Plan

| Field            | Value                          |
|------------------|--------------------------------|
| Document         | Test Plan                      |
| Project          | MockAgents                     |
| Version          | 1.0                            |
| Date             | 2026-04-07                     |
| Status           | Draft                          |
| Scope            | MVP (Phase 1)                  |
| Maintainers      | MockAgents Core Team           |
| Related          | [Testing Strategy](testing-strategy.md), [PRD](PRD.md), [Implementation Plan](implementation-plan.md) |

---

## 1. Document Info

### 1.1 Revision History

| Version | Date       | Author               | Changes               |
|---------|------------|-----------------------|------------------------|
| 1.0     | 2026-04-07 | MockAgents Core Team  | Initial draft          |

### 1.2 Scope

This document is the **actionable test plan** for MockAgents MVP (Phase 1). It defines specific test cases, testing types, execution schedules, and traceability to functional and non-functional requirements from the PRD. The companion [Testing Strategy](testing-strategy.md) covers philosophy, CI/CD pipeline design, and infrastructure; this document covers **what to test, how to verify it, and when**.

### 1.3 References

| Document               | Purpose                                              |
|------------------------|------------------------------------------------------|
| PRD                    | Functional requirements (FR-001 through FR-049), NFRs (NFR-001 through NFR-030) |
| Testing Strategy       | Test philosophy, CI/CD pipeline, flaky test policy    |
| Implementation Plan    | Sprint schedule and task breakdown                    |
| Definition of Done     | Per-story completion criteria                         |
| Low-Level Design       | Module decomposition, interfaces, data flow           |

---

## 2. Test Plan Overview

### 2.1 Objectives

1. Verify that every P0 and P1 functional requirement in the PRD is implemented correctly.
2. Validate protocol conformance against real OpenAI and Anthropic API schemas.
3. Confirm non-functional requirements (performance, compatibility, security) are met.
4. Establish regression safety so that future changes do not break existing behavior.
5. Provide traceability from every requirement to at least one test case.

### 2.2 Scope

**In scope:**
- Go core engine (config, matcher, response generators, state, tools)
- OpenAI Chat Completions adapter (non-streaming, streaming, function calling)
- Anthropic Messages adapter (non-streaming, streaming, tool use)
- Management API (health, agents, logs)
- CLI commands (init, start, validate, version, help)
- Python SDK (server lifecycle, client, scenarios, assertions, pytest integration)
- SQLite interaction logging
- Docker image build and smoke test
- Cross-platform binary builds (Linux, macOS, Windows)

**Out of scope:**
- Phase 2+ features (multi-agent orchestration, record-and-playback, GUI dashboard)
- MCP protocol adapter
- TypeScript/Go SDKs
- Cloud deployment and SaaS infrastructure

### 2.3 Approach

Testing follows the **test pyramid** defined in the Testing Strategy:

- **Unit tests** form the base: fast, isolated, run on every save and every PR.
- **Integration tests** validate the HTTP request/response pipeline end-to-end through real HTTP calls.
- **E2E tests** exercise user-facing workflows (CLI commands, SDK lifecycle, Docker).
- **Conformance tests** compare mock responses against golden fixtures recorded from real APIs.
- **Performance tests** validate throughput, latency, and memory stability.

### 2.4 Test Environments

| Environment       | Purpose                                    | Configuration                                    |
|-------------------|--------------------------------------------|--------------------------------------------------|
| Local Development | Developer workstation testing               | `go test`, `pytest`, manual CLI invocation        |
| CI (GitHub Actions)| Automated testing on every PR and merge    | Matrix: Linux/macOS/Windows, Go 1.22, Python 3.10-3.12 |
| Docker            | Container-based testing                     | Multi-stage build, Alpine-based, smoke tests      |

### 2.5 Entry Criteria

Testing begins when:
- [ ] Code compiles without errors (`go build ./cmd/mockagents`)
- [ ] Linting passes (`golangci-lint run`, `ruff check sdk/python/`)
- [ ] All dependencies resolve (`go mod tidy`, `poetry install`)
- [ ] Feature branch is pushed and PR is opened

### 2.6 Exit Criteria

A sprint is considered test-complete when:
- [ ] All P0 test cases for the sprint pass (100%)
- [ ] All P1 test cases for the sprint pass (100%)
- [ ] No Critical or High severity defects remain open
- [ ] Code coverage >= 80% for all changed packages
- [ ] Integration tests pass on all CI matrix configurations

A release is considered test-complete when:
- [ ] All test cases (P0, P1, P2) pass
- [ ] Protocol conformance tests pass at 100%
- [ ] Performance targets met (NFR-001 through NFR-008)
- [ ] Cross-platform smoke tests pass on Linux, macOS, and Windows
- [ ] Coverage report published with overall `internal/` coverage > 80%

### 2.7 Suspension Criteria

Testing is suspended when:
- The build is broken (code does not compile)
- CI infrastructure is unavailable (GitHub Actions outage)
- A Critical defect blocks test execution (e.g., server crashes on startup)

### 2.8 Resumption Criteria

Testing resumes when:
- The blocking issue is resolved and verified
- A new build is available and compiles cleanly
- CI infrastructure is restored

---

## 3. Testing Types

### 3.1 Unit Testing

**Purpose:** Verify individual functions and methods in isolation. Catch regressions early with tests that run in milliseconds.

**Tools:**
- Go: `testing` package + `testify/assert` and `testify/require`
- Python: `pytest` with standard assertions

**Naming convention:**
```
Go:     Test{Function}_{Scenario}_{Expected}
Python: test_{function}_{scenario}_{expected}
```

**Examples:**
```
TestParseAgentYAML_MissingName_ReturnsValidationError
TestContentContainsMatcher_CaseInsensitive_MatchesSubstring
test_expect_to_have_tool_call_matching_call_passes
```

**Coverage target:** >= 80% line coverage on all packages under `internal/` and `sdk/python/mockagents/`.

**Location:** `*_test.go` files alongside source code (Go); `sdk/python/tests/unit/` (Python).

---

### 3.2 Integration Testing

**Purpose:** Validate the HTTP request/response pipeline end-to-end. Because MockAgents is a mock server, integration tests that exercise the full stack through real HTTP calls are the highest-value tests.

**Tools:**
- Go: `net/http/httptest` for in-process HTTP server; `net/http` client for real HTTP calls
- Python: `httpx` for HTTP client calls against a running server

**Scope:**
- Each adapter endpoint (OpenAI `/v1/chat/completions`, Anthropic `/v1/messages`)
- Management API endpoints (`/api/v1/health`, `/api/v1/agents`, `/api/v1/logs`)
- Streaming (SSE event validation)
- Tool call round-trips
- SQLite logging verification
- Session state continuity across multi-turn conversations

**Location:** `tests/integration/` (Go); `sdk/python/tests/integration/` (Python).

---

### 3.3 End-to-End Testing

**Purpose:** Validate user-facing workflows from the outside, the way a user would interact with MockAgents.

**Tools:**
- Shell scripts and Go test binaries that invoke the `mockagents` CLI as a subprocess
- Python `subprocess` for SDK lifecycle tests
- Docker CLI for container tests

**Scope:**
- `mockagents init` creates the correct project scaffold
- `mockagents start` launches a server that responds to requests
- `mockagents validate` accepts valid YAML and rejects invalid YAML
- Python SDK starts the Go binary, sends requests, and asserts on responses
- Docker container starts and serves mock responses

**Location:** `tests/e2e/` (Go/shell); `sdk/python/tests/e2e/` (Python).

---

### 3.4 Protocol Conformance Testing

**Purpose:** Ensure that MockAgents responses are schema-identical to real provider responses. A subtle deviation in a field name, type, or structure will break downstream SDK clients silently.

**Approach:**
1. **Golden fixture comparison:** Record real responses from OpenAI and Anthropic APIs, store them in `testdata/golden/`. Compare MockAgents output field-by-field against these fixtures.
2. **JSON Schema validation:** Validate every MockAgents response against the OpenAI and Anthropic JSON Schemas.

**Fixtures:** Recorded from real OpenAI and Anthropic APIs, stored in:
```
tests/conformance/testdata/
    golden/
        openai/
            chat_completion.json
            chat_completion_streaming.jsonl
            chat_completion_tool_calls.json
        anthropic/
            messages.json
            messages_streaming.jsonl
            messages_tool_use.json
    schemas/
        openai_chat_completion.schema.json
        anthropic_messages.schema.json
```

**Update process:** Run `go test ./tests/conformance/... -update-golden` to regenerate golden files after an intentional change.

**Location:** `tests/conformance/`.

---

### 3.5 Performance Testing

**Purpose:** Validate that MockAgents meets throughput, latency, and resource-usage targets defined in NFR-001 through NFR-008.

**Tools:**
- `wrk` or `hey` for HTTP load testing
- Go `testing.B` benchmarks for micro-benchmarks
- `pprof` for profiling hot paths

**Targets:**

| Metric                          | Target           | NFR     |
|---------------------------------|------------------|---------|
| Non-streaming throughput        | >= 1000 req/s    | NFR-002 |
| Non-streaming p99 latency      | < 10ms           | NFR-003 |
| Streaming first-byte latency   | < 10ms           | NFR-004 |
| Server startup (10 agents)     | < 500ms          | NFR-001 |
| Concurrent connections          | 1000+            | NFR-005 |
| Memory after 100k requests     | < 2x baseline    | NFR-008 |

**Location:** `tests/benchmark/`.

---

### 3.6 Security Testing

**Purpose:** Validate that MockAgents correctly handles malicious or malformed input without exposing vulnerabilities.

**Approach:**
- Manual code review of input handling paths
- Automated fuzzing with Go's built-in fuzz testing (`testing.F`)
- Dependency scanning with `govulncheck` (Go) and `pip-audit` (Python)

**Scope:**
- **YAML loading:** Reject YAML bombs, oversized files, and recursive anchors
- **Template engine:** Verify sandboxing -- no file system access, no network calls, no shell execution (NFR-019)
- **File paths:** Prevent path traversal in agent definition loading (e.g., `../../etc/passwd`)
- **Request body parsing:** Handle oversized request bodies, deeply nested JSON, invalid UTF-8
- **Management API:** Validate all input parameters; reject injection attempts

---

### 3.7 Compatibility Testing

**Purpose:** Verify that MockAgents works with official SDKs and across supported platforms.

**Matrix:**

| Dimension            | Values                                          | NFR     |
|----------------------|-------------------------------------------------|---------|
| OpenAI Python SDK    | v1.x (latest minor)                             | NFR-026 |
| Anthropic Python SDK | v0.40+                                           | NFR-027 |
| Python version       | 3.10, 3.11, 3.12, 3.13                          | NFR-029 |
| Platform             | Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64) | NFR-028 |
| Go version           | 1.22+                                            | NFR-030 |

**Approach:** CI matrix runs the full test suite across platform and language version combinations. SDK compatibility tests use the real `openai` and `anthropic` Python packages with `base_url` overridden to point at the mock server.

---

### 3.8 Regression Testing

**Purpose:** Ensure that bug fixes and new features do not reintroduce previously resolved defects.

**Approach:**
- Every bug fix must include a regression test that reproduces the original failure before the fix and passes after it.
- Regression tests are added to the appropriate test suite (unit, integration, or E2E) and run in CI on every PR.
- The test name or comment must reference the issue number (e.g., `TestStreamingChunk_EmptyContent_NoNilPointer // Regression: #42`).

---

### 3.9 Smoke Testing

**Purpose:** Quick sanity check that the most critical paths work after a build, deployment, or release.

**Scope (5 checks, < 30 seconds total):**
1. `GET /api/v1/health` returns 200 with `{"status": "ok"}`
2. `POST /v1/chat/completions` with a minimal OpenAI request returns a valid response
3. `POST /v1/messages` with a minimal Anthropic request returns a valid response
4. `POST /v1/chat/completions` with `stream: true` returns SSE events ending with `data: [DONE]`
5. `mockagents validate examples/` exits with code 0

**When run:** After every Docker image build, after every release binary build, as the first step in E2E test suites.

---

## 4. Test Cases -- Traceability Matrix

### 4.1 Config and Validation (TC-100 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-101 | Unit | FR-001 | Load a valid agent YAML file with all required fields | AgentDefinition struct populated correctly; no error | S1 | P0 |
| TC-102 | Unit | FR-001 | Load YAML with missing required field (`name`) | Validation error returned with field path `metadata.name` | S1 | P0 |
| TC-103 | Unit | FR-003 | Load YAML with `spec.protocol: "unsupported-protocol"` | Error message: "unsupported protocol: unsupported-protocol" | S1 | P0 |
| TC-104 | Unit | FR-006 | Load YAML with two tools having the same `name` | Validation error: duplicate tool name detected | S1 | P0 |
| TC-105 | Unit | FR-006 | Load YAML with invalid JSON Schema in tool `parameters` | Validation error referencing the invalid schema | S1 | P1 |
| TC-106 | Unit | FR-033 | Validate multiple YAML files, some invalid | All errors reported (not just the first); exit code 1 | S1 | P0 |
| TC-107 | Unit | FR-047 | Load YAML with template expressions in response content | No validation error (templates are validated at runtime, not load time) | S1 | P1 |

### 4.2 Engine Core (TC-200 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-201 | Unit | FR-044 | Static response generator returns exact configured content | Response body matches the literal string in the YAML scenario | S2 | P0 |
| TC-202 | Unit | FR-047 | Template response with `random_int(1, 100)` | Response contains an integer between 1 and 100 inclusive | S2 | P0 |
| TC-203 | Unit | FR-047 | Template response with `date_offset("2d")` | Response contains a date exactly 2 days from now | S2 | P0 |
| TC-204 | Unit | FR-047 | Template response with `uuid()` | Response contains a valid v4 UUID (regex: `[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`) | S2 | P0 |
| TC-205 | Unit | FR-047 | Template response with `request.content` | Response echoes the last user message content | S2 | P0 |
| TC-206 | Unit | FR-045 | Scenario matcher: `content_contains` matches substring | Case-insensitive match on user message content selects the correct scenario | S2 | P0 |
| TC-207 | Unit | FR-046 | Scenario matcher: `content_regex` matches pattern | Regex match against user message selects the correct scenario | S2 | P1 |
| TC-208 | Unit | FR-044 | Scenario matcher: first match wins when multiple scenarios match | First scenario in definition order is selected | S2 | P0 |
| TC-209 | Unit | FR-048 | Scenario matcher: default fallback when no match | Scenario with no `match` field is selected as fallback | S2 | P0 |
| TC-210 | Unit | FR-048 | Scenario matcher: no default defined, no match | Built-in fallback returns "Mock response from {agent_name}" | S2 | P0 |
| TC-211 | Unit | FR-044 | Conversation state: session created on first request | New session ID generated; state store contains the session | S2 | P0 |
| TC-212 | Unit | FR-044 | Conversation state: session retrieved on subsequent request | Same session ID returns the previously stored state | S2 | P0 |
| TC-213 | Unit | FR-044 | Conversation state: session expires after TTL | After TTL elapses, session is no longer retrievable | S2 | P1 |

### 4.3 OpenAI Adapter (TC-300 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-301 | Integration | FR-024 | `POST /v1/chat/completions` with non-streaming request | Valid OpenAI JSON response with `Content-Type: application/json` | S3 | P0 |
| TC-302 | Integration | FR-024, FR-028, FR-029 | Response includes all required OpenAI fields | Response contains `id` (chatcmpl- prefix), `object` ("chat.completion"), `created` (unix timestamp), `model`, `choices` (array), `usage` (object) | S3 | P0 |
| TC-303 | Integration | FR-020 | Streaming request (`stream: true`) returns SSE events | Response `Content-Type: text/event-stream`; each event prefixed with `data: `; each data line is valid JSON | S3 | P0 |
| TC-304 | Integration | FR-020 | Streaming final event is `data: [DONE]` | Last non-empty SSE line is exactly `data: [DONE]` | S3 | P0 |
| TC-305 | Integration | FR-026 | Function calling: `tools` in request triggers `tool_calls` in response | Response `choices[0].message.tool_calls` is a non-empty array with `id` (call_ prefix), `type` ("function"), `function.name`, `function.arguments` | S3 | P0 |
| TC-306 | Integration | FR-026 | `tool_choice: "none"` suppresses tool calls | Response `choices[0].message.tool_calls` is null or absent; `finish_reason` is "stop" | S3 | P0 |
| TC-307 | Integration | FR-026 | `tool_choice: "required"` forces tool calls | Response always includes `tool_calls` regardless of scenario match; `finish_reason` is "tool_calls" | S3 | P1 |
| TC-308 | Integration | FR-026 | `tool_choice: {"type": "function", "function": {"name": "X"}}` | Response `tool_calls` includes a call to function "X" specifically | S3 | P1 |
| TC-309 | Integration | FR-028 | Token usage estimation is reasonable | `usage.prompt_tokens` and `usage.completion_tokens` are positive integers; `total_tokens` equals their sum | S3 | P1 |
| TC-310 | Integration | FR-010 | Agent routing by model name | Request with `model: "agent-a"` routes to agent-a; `model: "agent-b"` routes to agent-b | S3 | P0 |
| TC-311 | E2E | NFR-026 | OpenAI Python SDK compatibility (`base_url` override) | `openai.OpenAI(base_url="http://localhost:PORT/v1").chat.completions.create()` returns a valid response object | S6 | P0 |
| TC-312 | Integration | FR-014 | Invalid request body (malformed JSON) | 400 response with JSON body containing `error.message` | S3 | P0 |
| TC-313 | Integration | FR-010 | Unknown model name in request | 404 response or fallback agent response (if a default agent is configured) | S3 | P1 |

### 4.4 Anthropic Adapter (TC-400 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-401 | Integration | FR-025 | `POST /v1/messages` with non-streaming request | Valid Anthropic JSON response with `Content-Type: application/json` | S4 | P0 |
| TC-402 | Integration | FR-025, FR-028, FR-029 | Response includes all required Anthropic fields | Response contains `id` (msg_ prefix), `type` ("message"), `role` ("assistant"), `content` (array), `model`, `stop_reason`, `usage` | S4 | P0 |
| TC-403 | Integration | FR-020, FR-025 | Streaming returns correct event sequence | SSE events in order: `message_start`, `content_block_start`, one or more `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop` | S4 | P0 |
| TC-404 | Integration | FR-027 | Tool use: scenario triggers `tool_use` content blocks | Response `content` array includes object with `type: "tool_use"`, `id`, `name`, `input` | S4 | P0 |
| TC-405 | Integration | FR-027 | Tool result: `tool_result` content block in follow-up request parsed correctly | Server accepts user message with `tool_result` content block; uses result for scenario matching | S4 | P0 |
| TC-406 | E2E | NFR-027 | Anthropic Python SDK compatibility (`base_url` override) | `anthropic.Anthropic(base_url="http://localhost:PORT").messages.create()` returns a valid response object | S6 | P0 |
| TC-407 | Integration | FR-025 | System prompt handling | Request with `system` field is accepted; system prompt is available for template rendering | S4 | P1 |
| TC-408 | Integration | FR-025 | Multiple content blocks in response | Response `content` array contains multiple objects (e.g., text + tool_use) | S4 | P1 |

### 4.5 Tool Call Simulation (TC-500 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-501 | Unit | FR-016 | Tool call with matching parameters returns configured response | Tool response matches the `match` block where parameter values match | S4 | P0 |
| TC-502 | Unit | FR-007 | Tool call with no parameter match returns default response | `default` tool response is returned when no `match` block matches | S4 | P0 |
| TC-503 | Unit | FR-008 | Tool call error injection returns error response | Tool configured with error response returns `code` and `message` fields | S4 | P1 |
| TC-504 | Integration | FR-015 | Parallel tool calls are all resolved | Request with multiple tool calls returns a response for each one | S4 | P0 |
| TC-505 | Unit | FR-018 | Tool parameter validation enabled: invalid params rejected | When tool `validate: true`, invalid parameters return 422 with details | S4 | P1 |
| TC-506 | Unit | FR-019 | Template expressions in tool responses are resolved | Tool response containing `{{ uuid() }}` returns an actual UUID | S4 | P1 |
| TC-507 | Integration | FR-017 | Multi-step tool chain: sequential resolution | Scenario with ordered tool call steps executes sequentially; each step depends on prior tool result | S4 | P1 |

### 4.6 Management API (TC-600 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-601 | Integration | FR-009 | `GET /api/v1/health` | 200 response with `status`, `version`, and `uptime` fields | S3 | P0 |
| TC-602 | Integration | FR-010 | `GET /api/v1/agents` | 200 response with array of loaded agent summaries (name, protocol, model) | S3 | P0 |
| TC-603 | Integration | FR-010 | `GET /api/v1/agents/{name}` for existing agent | 200 response with full agent details (metadata, spec, scenarios, tools) | S3 | P0 |
| TC-604 | Integration | FR-014 | `GET /api/v1/agents/{unknown}` for non-existent agent | 404 response with error message | S3 | P0 |
| TC-605 | Integration | FR-011 | `POST /api/v1/agents/reload` triggers hot-reload | Agents are reloaded from disk; response confirms reload; updated agent serves new responses | S3 | P1 |
| TC-606 | Integration | FR-013 | `GET /api/v1/logs` returns interaction logs | 200 response with paginated array of log entries (timestamp, agent, request, response, latency) | S5 | P1 |
| TC-607 | Integration | FR-013 | `GET /api/v1/logs?agent=X` filters logs by agent | Only logs for agent X are returned | S5 | P1 |
| TC-608 | Integration | FR-013 | `DELETE /api/v1/logs` clears all logs | 200 response; subsequent `GET /api/v1/logs` returns empty array | S5 | P2 |

### 4.7 CLI (TC-700 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-701 | E2E | FR-031 | `mockagents init` in an empty directory | Creates directory structure with example agent YAML, example test file, and `.mockagents.yaml` config | S5 | P0 |
| TC-702 | E2E | FR-031 | `mockagents init` in a non-empty directory | Creates scaffold files without overwriting existing files | S5 | P0 |
| TC-703 | E2E | FR-032 | `mockagents start` launches server | Server starts, prints URL to stdout, responds to `GET /api/v1/health` | S3 | P0 |
| TC-704 | E2E | FR-032 | `mockagents start --port 9090` | Server binds to port 9090; health check on port 9090 succeeds | S3 | P0 |
| TC-705 | E2E | FR-032, NFR-015 | `mockagents start` then Ctrl+C (SIGINT) | Server shuts down gracefully; process exits with code 0; in-flight requests complete | S3 | P0 |
| TC-706 | E2E | FR-033 | `mockagents validate valid.yaml` | Exit code 0; no error output | S1 | P0 |
| TC-707 | E2E | FR-033 | `mockagents validate invalid.yaml` | Exit code 1; stderr includes validation errors with field paths | S1 | P0 |
| TC-708 | E2E | FR-035 | `mockagents --version` | Prints version string (e.g., `mockagents v0.1.0`) to stdout | S5 | P1 |
| TC-709 | E2E | FR-035, NFR-023 | `mockagents --help` | Prints help text listing all commands (init, start, validate, version) with descriptions | S5 | P1 |

### 4.8 Python SDK (TC-800 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-801 | Integration | FR-039 | `MockAgentServer.from_config("agent.yaml")` starts Go binary | Server process is running; health check on auto-assigned port succeeds | S5 | P0 |
| TC-802 | Integration | FR-038 | `MockAgentServer` context manager lifecycle | `with MockAgentServer(...) as server:` starts server on enter; server process terminates on exit | S5 | P0 |
| TC-803 | Integration | FR-038 | `MockAgentClient` sends request and receives response | Client sends chat completion request; receives a valid response with content | S5 | P0 |
| TC-804 | Integration | FR-040 | `Scenario.run()` executes steps sequentially | Multi-step scenario sends messages in order; each step receives the expected response | S5 | P0 |
| TC-805 | Unit | FR-041 | `expect(result).to_have_tool_call("func", {"key": "val"})` passes on matching tool call | No exception raised when response contains matching tool call | S5 | P0 |
| TC-806 | Unit | FR-041 | `expect(result).to_have_tool_call("func", {"key": "val"})` fails on missing tool call | `AssertionError` raised with message describing expected vs. actual tool calls | S5 | P0 |
| TC-807 | Unit | FR-041 | `expect(result).to_have_response_containing("hello")` passes on substring match | No exception raised when response content contains "hello" | S5 | P0 |
| TC-808 | Unit | FR-041 | `expect(result.latency_ms).to_be_less_than(100)` passes when latency < 100ms | No exception raised when actual latency is below threshold | S5 | P0 |
| TC-809 | Integration | FR-038 | Async client works with asyncio | `async with MockAgentServer(...) as server:` works; async client sends request and receives response | S5 | P1 |
| TC-810 | Integration | FR-043 | pytest fixture integration | `@pytest.fixture` using `MockAgentServer` works in a standard pytest test file | S5 | P1 |

### 4.9 Performance (TC-900 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-901 | Performance | NFR-002 | Non-streaming throughput | >= 1000 requests/second on a single core with static response | S6 | P0 |
| TC-902 | Performance | NFR-003 | Non-streaming p99 latency | < 10ms under sustained load (1000 req/s) | S6 | P0 |
| TC-903 | Performance | NFR-004 | Streaming first-byte latency | < 10ms from request sent to first SSE event received | S6 | P0 |
| TC-904 | Performance | NFR-001 | Server startup time with 10 agents loaded | Cold start completes in < 500ms (measured from process start to health check success) | S6 | P0 |
| TC-905 | Performance | NFR-008 | Memory stability under 100k requests | RSS after 100k requests is < 2x RSS at startup; no goroutine leaks (delta < 10) | S6 | P1 |
| TC-906 | Performance | NFR-005 | Concurrent connections (1000+) handled | Server handles 1000 simultaneous connections without errors or dropped requests | S6 | P1 |

### 4.10 Cross-Cutting (TC-1000 Series)

| Test ID | Type | FR/NFR | Description | Expected Result | Sprint | Priority |
|---------|------|--------|-------------|-----------------|--------|----------|
| TC-1001 | Integration | FR-024, FR-025 | Same agent YAML serves both OpenAI and Anthropic requests | Single agent with `protocol: openai-chat-completions` responds on `/v1/chat/completions`; same agent also serves Anthropic format on `/v1/messages` (or protocol routing selects the correct adapter) | S4 | P0 |
| TC-1002 | Integration | FR-011 | Hot reload updates agent without server restart | Modify agent YAML on disk; trigger reload; subsequent requests reflect the updated configuration | S3 | P1 |
| TC-1003 | Integration | FR-013 | SQLite logs record all interactions | After sending N requests, the interactions table contains N rows with correct fields | S5 | P0 |
| TC-1004 | E2E | FR-037 | Docker container starts and serves requests | `docker run` with mounted agent config; health check succeeds; chat completion request returns valid response | S5 | P0 |
| TC-1005 | E2E | NFR-028 | Binary works on Linux, macOS, and Windows | CI matrix builds and runs smoke tests on all three platforms; all pass | S6 | P0 |

---

## 5. Test Execution Schedule

### 5.1 Sprint-Level Test Plan

| Test Category | First Written | Description |
|---------------|--------------|-------------|
| Unit: Config/Validation | Sprint 1 | TC-101 through TC-107 |
| Unit: Engine Core | Sprint 2 | TC-201 through TC-213 |
| Integration: OpenAI Adapter | Sprint 3 | TC-301 through TC-313 |
| Integration: Management API | Sprint 3 | TC-601 through TC-605 |
| E2E: CLI (validate, start) | Sprint 3 | TC-703 through TC-707 |
| Integration: Anthropic Adapter | Sprint 4 | TC-401 through TC-408 |
| Unit/Integration: Tool Calls | Sprint 4 | TC-501 through TC-507 |
| Cross-Cutting: Dual Protocol | Sprint 4 | TC-1001 |
| Integration: Python SDK | Sprint 5 | TC-801 through TC-810 |
| Unit: Python Assertions | Sprint 5 | TC-805 through TC-808 |
| Integration: SQLite Logging | Sprint 5 | TC-606 through TC-608, TC-1003 |
| E2E: CLI (init, version, help) | Sprint 5 | TC-701, TC-702, TC-708, TC-709 |
| E2E: Docker | Sprint 5 | TC-1004 |
| Performance | Sprint 6 | TC-901 through TC-906 |
| Conformance: Protocol | Sprint 6 | Golden fixture comparison |
| Compatibility: SDK | Sprint 6 | TC-311, TC-406 |
| E2E: Cross-Platform | Sprint 6 | TC-1005 |

### 5.2 CI Execution Matrix

| Test Category        | Runs on PR | Runs on Merge to Main | Runs on Release Tag |
|----------------------|------------|------------------------|---------------------|
| Unit Tests (Go)      | Yes        | Yes                    | Yes                 |
| Unit Tests (Python)  | Yes        | Yes                    | Yes                 |
| Integration Tests    | Yes        | Yes                    | Yes                 |
| E2E Tests            | No         | Yes                    | Yes                 |
| Conformance Tests    | Yes        | Yes                    | Yes                 |
| Performance Tests    | No         | Yes (baseline compare) | Yes (full report)   |
| Security Scanning    | No         | Yes                    | Yes                 |
| Compatibility Matrix | No         | Yes                    | Yes                 |
| Smoke Tests          | No         | Yes                    | Yes                 |
| Docker Build + Smoke | No         | Yes                    | Yes                 |

---

## 6. Defect Management

### 6.1 Severity Levels

| Severity | Definition | Example |
|----------|-----------|---------|
| **Critical** | System is unusable; no workaround exists. Data loss or security vulnerability. | Server crashes on startup; responses contain wrong protocol format causing SDK exceptions |
| **High** | Major feature is broken; workaround exists but is impractical. | Streaming does not work; tool calls return wrong structure; hot reload crashes the server |
| **Medium** | Feature works but with noticeable issues; workaround is reasonable. | Token count estimation is significantly off; CLI help text is incomplete; minor schema deviation |
| **Low** | Cosmetic or minor usability issue; does not affect functionality. | CLI output formatting inconsistency; log message typo; unnecessary debug output |

### 6.2 Defect Lifecycle

```
Open --> In Progress --> Fixed --> Verified --> Closed
  |                        |
  +--> Won't Fix           +--> Reopened --> In Progress
  +--> Duplicate
```

- **Open:** Defect reported with reproduction steps and severity.
- **In Progress:** Assigned to an engineer; fix is underway.
- **Fixed:** Code change merged; regression test added.
- **Verified:** QA or author confirms the fix resolves the issue.
- **Closed:** Defect is resolved and verified.

### 6.3 SLA by Severity

| Severity | Response Time | Fix Deadline |
|----------|--------------|--------------|
| Critical | Within 4 hours | Within 24 hours |
| High     | Within 1 business day | Within the current sprint |
| Medium   | Within 2 business days | Within the next sprint |
| Low      | Within 1 sprint | Best effort; may be deferred |

### 6.4 Defect Reporting Template

Every defect must include:
1. **Title:** Concise description of the issue
2. **Severity:** Critical / High / Medium / Low
3. **Steps to reproduce:** Numbered steps from a clean state
4. **Expected result:** What should happen
5. **Actual result:** What actually happens
6. **Environment:** OS, Go version, Python version, MockAgents version
7. **Logs/Screenshots:** Relevant output, stack traces, or screenshots
8. **Test case reference:** Which test case (TC-XXX) exposed the defect, if applicable

---

## 7. Test Reporting

### 7.1 Per-PR Report

Generated automatically by CI and posted as a PR comment:

- Total tests run / passed / failed / skipped
- Coverage percentage for changed packages
- Coverage delta from the base branch (increase or decrease)
- List of any newly failing tests
- Conformance test pass/fail status

### 7.2 Per-Sprint Report

Compiled at the end of each sprint:

- Total test count by type (unit, integration, E2E, conformance, performance)
- New tests added this sprint
- Overall coverage percentage for `internal/` and `sdk/python/mockagents/`
- Per-package coverage breakdown
- Open defect count by severity
- Defects opened, closed, and carried over this sprint
- Flaky test count and status

### 7.3 Per-Release Report

Compiled before each release:

- Full test suite pass/fail summary across all CI matrix configurations
- Coverage report (line coverage by package, overall percentage)
- Protocol conformance report (OpenAI and Anthropic fixture comparison results)
- Performance benchmark report (throughput, latency percentiles, memory)
- Compatibility matrix results (SDK versions, Python versions, platforms)
- Open defect inventory (must be zero Critical/High for release)
- Security scan results (`govulncheck`, `pip-audit`)

---

## 8. Test Data and Fixtures

### 8.1 Directory Structure

```
testdata/
    agents/
        valid_agent.yaml
        invalid_missing_name.yaml
        invalid_bad_yaml.yaml
        invalid_unsupported_protocol.yaml
        invalid_duplicate_tools.yaml
        invalid_tool_schema.yaml
        agent_with_tools.yaml
        agent_with_templates.yaml
        multi_scenario_agent.yaml
        stateful_agent.yaml
        error_agent.yaml

tests/
    integration/
        testdata/
            openai_chat_request.json
            openai_chat_response.json
            openai_streaming_request.json
            anthropic_messages_request.json
            anthropic_messages_response.json
            anthropic_streaming_request.json

    conformance/
        testdata/
            golden/
                openai/
                    chat_completion.json
                    chat_completion_streaming.jsonl
                    chat_completion_tool_calls.json
                    chat_completion_function_calling.json
                anthropic/
                    messages.json
                    messages_streaming.jsonl
                    messages_tool_use.json
                    messages_multi_content.json
            schemas/
                openai_chat_completion.schema.json
                openai_chat_completion_chunk.schema.json
                anthropic_messages.schema.json
                anthropic_streaming_event.schema.json

    benchmark/
        testdata/
            bench_agent_static.yaml
            bench_agent_template.yaml
```

### 8.2 Golden Fixtures

Golden fixtures are the authoritative reference for protocol conformance. They are recorded from real API calls to OpenAI and Anthropic.

**Recording process:**
1. Run the recording script: `go run ./tools/record-golden/main.go --provider openai --output tests/conformance/testdata/golden/openai/`
2. The script makes real API calls (requires valid API keys) and saves the raw responses.
3. Sensitive fields (API keys, organization IDs) are scrubbed automatically.
4. Commit the updated golden files to the repository.

**Updating golden fixtures:**
```bash
# Re-record from real APIs (requires API keys)
OPENAI_API_KEY=sk-... go run ./tools/record-golden/main.go --provider openai
ANTHROPIC_API_KEY=sk-... go run ./tools/record-golden/main.go --provider anthropic

# Or update in-place from current MockAgents output (after intentional change)
go test ./tests/conformance/... -update-golden
```

**When to update:**
- When a provider changes their API response format
- When MockAgents intentionally changes its response generation logic
- At the start of each sprint, check for provider API changes

### 8.3 Fixture Naming Convention

```
{what_it_tests}_{variant}.{ext}

Examples:
    valid_agent.yaml                  -- a well-formed agent definition
    invalid_missing_name.yaml         -- agent YAML missing the name field
    chat_completion_tool_calls.json   -- OpenAI response with tool calls
    messages_streaming.jsonl          -- Anthropic streaming events (one JSON per line)
    bench_agent_static.yaml           -- agent definition optimized for benchmark
```

### 8.4 Rules for Test Data

1. Never hard-code large JSON or YAML blobs inline in test code. Always use fixture files.
2. Every fixture file must have a comment at the top explaining what it exercises.
3. Reference fixtures using relative paths from the test file:
   - Go: `os.ReadFile(filepath.Join("testdata", "fixture_name.yaml"))`
   - Python: `Path(__file__).parent / "testdata" / "fixture_name.yaml"`
4. Fixtures must not contain real API keys, tokens, or personally identifiable information.
5. Golden files are checked into version control and reviewed in PRs like any other code change.
