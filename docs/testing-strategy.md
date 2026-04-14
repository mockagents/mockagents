# MockAgents Testing Strategy

| Field          | Value                        |
|----------------|------------------------------|
| Document       | Testing Strategy             |
| Project        | MockAgents                   |
| Version        | 1.0                          |
| Last Updated   | 2026-04-07                   |
| Status         | Active                       |
| Maintainers    | MockAgents Core Team         |

## 1. Purpose

This document defines how **MockAgents itself** is tested -- its Go core engine, Python SDK, CLI tooling, and protocol adapters. It does not describe how end-users use MockAgents to test their own AI agent integrations.

---

## 2. Testing Philosophy

MockAgents is, at its core, a **mock server**. Correctness of protocol emulation matters more than almost anything else. A subtle deviation in a response field can cause downstream SDK clients to break in ways that are difficult to diagnose. This shapes our testing priorities.

### Test Pyramid

```
           /  E2E  \            Few, slow, high confidence
          /----------\
         / Integration \        Many, moderate speed, high value
        /----------------\
       /    Unit Tests    \     Many, fast, foundational
      /--------------------\
```

**Unit tests** form the base. Every parser, matcher, generator, and store function has targeted unit tests that run in milliseconds. These catch regressions early and run on every save during local development.

**Integration tests** are where MockAgents gets the most value. Because the product is a server that speaks HTTP and SSE, testing the full request-response pipeline through real HTTP calls is essential. Integration tests outnumber unit tests in importance (though not necessarily in count).

**End-to-end tests** exercise the CLI, Python SDK, and Docker packaging against a running server. These are slower and run primarily in CI on merge to main.

### What to Test at Each Level

| Level       | What it validates                                              |
|-------------|----------------------------------------------------------------|
| Unit        | Individual functions in isolation: parsing, matching, generation, state management |
| Integration | HTTP request through adapter through engine to HTTP response; database writes; management API |
| E2E         | User-facing workflows: CLI commands, SDK lifecycle, Docker container behavior |
| Conformance | Protocol-level correctness against OpenAI and Anthropic schemas |
| Performance | Throughput, memory stability, and concurrency under load       |

---

## 3. Test Levels

### 3.1 Unit Tests (Go)

Unit tests live alongside the code they test in `*_test.go` files within each package under `internal/`.

#### Config Parser (`internal/config/`)

| Test Case                  | Description                                                        |
|----------------------------|--------------------------------------------------------------------|
| Valid YAML                 | Parse a well-formed agent definition; verify all fields populated  |
| Invalid YAML               | Malformed YAML returns a clear parse error                         |
| Missing required fields    | Omitting `name`, `provider`, or `scenarios` returns validation error with field name |
| Unknown fields             | Extra fields in YAML are either ignored or produce a warning, depending on strict mode |
| Environment variable refs  | `${ENV_VAR}` placeholders resolve correctly                        |

#### Scenario Matcher (`internal/engine/matcher/`)

| Test Case          | Description                                                              |
|--------------------|--------------------------------------------------------------------------|
| Exact match        | Request content exactly equals scenario trigger string                   |
| Regex match        | Request content matches scenario regex pattern                           |
| `content_contains` | Request content includes the specified substring                         |
| Default fallback   | When no scenario matches, the default scenario is selected               |
| No match           | When no scenario matches and no default exists, engine returns a structured error |
| Priority ordering  | When multiple scenarios match, the first defined wins                     |

#### Static Response Generator (`internal/engine/response/`)

| Test Case              | Description                                          |
|------------------------|------------------------------------------------------|
| Returns configured body | Output matches the literal response defined in YAML  |
| Correct content type   | Response content type matches the provider adapter    |
| Status code override   | Custom HTTP status codes are applied when configured  |

#### Template Response Generator (`internal/engine/response/`)

| Test Case                         | Description                                                  |
|-----------------------------------|--------------------------------------------------------------|
| Variable substitution             | `{{.input}}` and `{{.session.key}}` resolve correctly        |
| `date_offset` function            | `{{date_offset "2d"}}` returns a date two days from now      |
| `random_int` function             | `{{random_int 1 100}}` returns an integer in range           |
| `uuid` function                   | `{{uuid}}` returns a valid v4 UUID string                    |
| Missing variable                  | Undefined variable produces empty string or clear error      |

#### Tool Call Processor (`internal/engine/tools/`)

| Test Case             | Description                                                 |
|-----------------------|-------------------------------------------------------------|
| Matched tool response | Tool call name matches a configured tool; returns its response |
| Default response      | Unmatched tool name falls back to default tool response     |
| Error response        | Tool configured to return an error produces correct error structure |
| Unknown tool          | Tool name not in config and no default returns a tool-not-found error |

#### State Store (`internal/state/`)

| Test Case        | Description                                             |
|------------------|---------------------------------------------------------|
| Create session   | New session ID is generated and stored                  |
| Get session      | Existing session data is retrieved by ID                |
| Update session   | Session data is mutated and persisted                   |
| TTL expiry       | Sessions are evicted after their TTL elapses            |
| Concurrent access| Simultaneous reads/writes do not race (tested with `-race`) |

#### Coverage Target

> **>80% line coverage** on all packages under `internal/`.

---

### 3.2 Integration Tests (Go)

Integration tests live in `tests/integration/` and use `httptest.Server` or a real server subprocess to make actual HTTP requests.

#### OpenAI Adapter

| Test Case               | Description                                                        |
|-------------------------|--------------------------------------------------------------------|
| Non-streaming request   | `POST /v1/chat/completions` returns well-formed JSON response      |
| Streaming SSE           | `stream: true` returns chunked SSE with correct `data:` prefixes   |
| Function calling        | `functions` parameter triggers function_call in response           |
| `tool_calls` response   | Response includes tool_calls array with correct structure          |
| Error responses         | Invalid model name returns 404; missing API key returns 401        |

#### Anthropic Adapter

| Test Case               | Description                                                        |
|-------------------------|--------------------------------------------------------------------|
| Non-streaming request   | `POST /v1/messages` returns well-formed JSON with content blocks   |
| Streaming events        | `stream: true` returns SSE with `message_start`, `content_block_delta`, `message_stop` |
| `tool_use` blocks       | Response includes tool_use content blocks when scenarios configure them |
| Error responses         | Malformed request returns structured Anthropic error object        |

#### Full Request Pipeline

| Test Case                        | Description                                                |
|----------------------------------|------------------------------------------------------------|
| HTTP request to adapter to engine | End-to-end through the server: request in, response out    |
| Scenario selection               | Correct scenario is matched and its response is returned   |
| Session continuity               | Multi-turn conversation maintains session state            |

#### SQLite Logging

| Test Case                   | Description                                              |
|-----------------------------|----------------------------------------------------------|
| Interaction recorded        | After a request, the interactions table has a new row    |
| Fields populated correctly  | Timestamp, request body, response body, latency are set  |
| Concurrent writes           | Multiple simultaneous requests do not corrupt the database |

#### Management API

| Test Case               | Description                                                    |
|-------------------------|----------------------------------------------------------------|
| List agents             | `GET /api/agents` returns all loaded agents                    |
| Get agent               | `GET /api/agents/:id` returns a single agent                   |
| Create agent            | `POST /api/agents` with valid YAML creates a new agent         |
| Update agent            | `PUT /api/agents/:id` modifies an existing agent               |
| Delete agent            | `DELETE /api/agents/:id` removes the agent                     |
| List logs               | `GET /api/logs` returns interaction history                    |
| Filter logs by agent    | `GET /api/logs?agent=foo` filters correctly                    |
| Get session             | `GET /api/sessions/:id` returns session state                  |
| Delete session          | `DELETE /api/sessions/:id` clears session                      |
| Invalid input           | Malformed requests return 400 with descriptive error           |

#### Coverage Target

> Every API endpoint has **at least 2 test cases**: one happy path and one error path.

---

### 3.3 End-to-End Tests

E2E tests exercise MockAgents from the outside, the way a user would.

#### CLI Commands

| Test Case                       | Description                                                   |
|---------------------------------|---------------------------------------------------------------|
| `mockagents init`               | Creates correct directory scaffold with example agent YAML    |
| `mockagents start`              | Server starts and responds to health check on expected port   |
| `mockagents start --port 9999`  | Server binds to custom port                                   |
| `mockagents validate`           | Valid config exits 0; invalid config exits non-zero with error message |
| `mockagents validate` (errors)  | Reports specific validation errors (missing field, bad regex) |

#### Python SDK

| Test Case                   | Description                                                    |
|-----------------------------|----------------------------------------------------------------|
| `MockAgentServer` lifecycle | `start()` launches server, `stop()` terminates it cleanly     |
| Scenario runner             | Scenarios execute in order; assertions are evaluated           |
| Assertion pass              | Correct response passes assertion without error                |
| Assertion fail              | Incorrect response raises `AssertionError` with clear message  |
| Async support               | `async with MockAgentServer()` works with asyncio              |

#### Docker

| Test Case                   | Description                                             |
|-----------------------------|---------------------------------------------------------|
| Container starts            | `docker run` with mounted config starts the server      |
| Serves requests             | Container responds to OpenAI-format requests correctly  |
| Graceful shutdown           | `docker stop` terminates without error                  |

#### Cross-Platform

Tests run on all three platforms via CI matrix:

- **Linux** (Ubuntu latest) -- primary CI platform
- **macOS** (latest) -- verifies Darwin build
- **Windows** (latest) -- verifies Windows build and path handling

---

### 3.4 Protocol Conformance Tests

These tests validate that MockAgents responses are indistinguishable from real provider responses at the schema level. They live in `tests/conformance/`.

#### OpenAI Conformance

| Validation                  | Description                                                     |
|-----------------------------|-----------------------------------------------------------------|
| Response `id` format        | Matches `chatcmpl-` prefix followed by alphanumeric string      |
| `object` field              | Equals `"chat.completion"` (non-streaming) or `"chat.completion.chunk"` (streaming) |
| `usage` fields              | `prompt_tokens`, `completion_tokens`, `total_tokens` are present and are integers |
| `choices` structure         | Array with `index`, `message` (or `delta`), `finish_reason`     |
| `finish_reason` values      | One of `"stop"`, `"length"`, `"function_call"`, `"tool_calls"`, `null` |
| Tool calls structure        | `tool_calls[].id` starts with `call_`; `type` is `"function"`   |

#### Anthropic Conformance

| Validation                  | Description                                                     |
|-----------------------------|-----------------------------------------------------------------|
| `content` blocks            | Array of objects with `type` (`"text"` or `"tool_use"`) and corresponding fields |
| `stop_reason`               | One of `"end_turn"`, `"max_tokens"`, `"stop_sequence"`, `"tool_use"` |
| `usage` fields              | `input_tokens` and `output_tokens` are present and are integers |
| `model` field               | Matches the requested model string                              |
| `role` field                | Always `"assistant"`                                            |

#### Streaming Conformance

| Validation                  | Description                                                     |
|-----------------------------|-----------------------------------------------------------------|
| SSE format                  | Every line is either `data: <json>`, `event: <type>`, or empty  |
| OpenAI terminator           | Stream ends with `data: [DONE]`                                 |
| Anthropic event types       | Events include `message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop` |
| Partial JSON validity       | Each `data:` line contains valid JSON                           |

---

### 3.5 Performance Tests

Performance tests live in `tests/benchmark/` and use Go's built-in benchmarking (`testing.B`) plus custom load scripts.

| Test                        | Description                                                    | Target           |
|-----------------------------|----------------------------------------------------------------|------------------|
| Non-streaming throughput    | Requests per second for simple chat completion                 | >= 1000 req/s on a single core |
| Streaming throughput        | Requests per second for streaming chat completion              | >= 500 req/s     |
| Memory under sustained load | RSS after 100k requests compared to baseline at startup        | < 2x baseline    |
| No goroutine leaks          | Goroutine count after 100k requests returns to baseline        | Delta < 10       |
| Concurrent agents           | Multiple agents serving simultaneously without interference    | No errors at 10 concurrent agents |
| Latency percentiles         | p50, p95, p99 latency for non-streaming responses              | p99 < 10ms       |

#### Running Performance Tests Locally

```bash
# Go benchmarks
go test -bench=. -benchmem ./tests/benchmark/...

# Load test with custom script
go run ./tests/benchmark/loadtest/main.go --requests 100000 --concurrency 50
```

---

### 3.6 Python SDK Tests

Python SDK tests live in `sdk/python/tests/` and use pytest.

#### Unit Tests (`sdk/python/tests/unit/`)

| Test Case                | Description                                           |
|--------------------------|-------------------------------------------------------|
| Client construction      | `MockAgentClient(base_url=...)` sets correct defaults |
| Assertion helpers        | `assert_content_contains`, `assert_tool_called`, etc. |
| Scenario building        | Fluent builder produces correct scenario dict         |
| Config serialization     | Python scenario objects serialize to valid YAML       |

#### Integration Tests (`sdk/python/tests/integration/`)

| Test Case                      | Description                                                |
|--------------------------------|------------------------------------------------------------|
| Full lifecycle with Go server  | SDK starts Go binary as subprocess, sends requests, stops  |
| Multi-turn conversation        | Session state is maintained across sequential requests     |
| Error scenarios                | SDK raises appropriate exceptions for server errors        |

#### Test Dependencies

```
pytest
pytest-asyncio
httpx
pyyaml
```

---

## 4. CI/CD Pipeline

All CI runs on **GitHub Actions**. The pipeline has three tiers triggered by different events.

### On Pull Request

```
lint (parallel)                 build (parallel)
  - golangci-lint                 - go build ./...
  - ruff check sdk/python/        
         |                              |
         +----------+------------------+
                    |
             unit tests (parallel)
               - go test ./internal/...
               - pytest sdk/python/tests/unit/
                    |
             integration tests
               - go test ./tests/integration/...
               - pytest sdk/python/tests/integration/
```

**Gate:** PR cannot merge if any step fails. All checks are required.

### On Merge to Main

Everything from the PR pipeline, plus:

```
e2e tests
  - CLI tests
  - Python SDK lifecycle tests
  - Cross-platform matrix (Linux, macOS, Windows)
         |
performance benchmarks
  - Go benchmarks with comparison to baseline
  - Results posted as PR comment (if triggered by merge)
         |
Docker build
  - Build and tag image as `mockagents:main`
  - Smoke test the container
         |
Publish artifacts
  - Upload Go binaries (Linux, macOS, Windows) as workflow artifacts
```

### On Tag (Release)

Everything from the main pipeline, plus:

```
PyPI publish
  - Build and upload sdk/python/ to PyPI
         |
Docker Hub push
  - Tag and push image as `mockagents:<version>` and `mockagents:latest`
         |
GitHub Release
  - Create release with changelog and attached binaries
```

### CI Matrix

```yaml
strategy:
  matrix:
    os: [ubuntu-latest, macos-latest, windows-latest]
    go-version: ["1.26"]
    python-version: ["3.10", "3.11", "3.12"]
```

---

## 5. Test Infrastructure

### Go

| Tool               | Purpose                                |
|--------------------|----------------------------------------|
| `testing`          | Standard Go test runner                |
| `testify/assert`   | Readable assertions and test helpers   |
| `testify/require`  | Fatal assertions that stop the test    |
| `net/http/httptest`| In-process HTTP server for integration tests |
| `testing.B`        | Built-in benchmarking                  |
| `-race` flag       | Race condition detection               |

### Python

| Tool              | Purpose                                 |
|-------------------|-----------------------------------------|
| `pytest`          | Test runner and fixture management      |
| `pytest-asyncio`  | Async test support                      |
| `httpx`           | HTTP client for integration tests       |
| `pyyaml`          | YAML fixture loading                    |

### Docker

| Tool                  | Purpose                             |
|-----------------------|-------------------------------------|
| `testcontainers` (Go) | Spin up Docker containers in tests  |
| Direct `docker` CLI   | Fallback in CI for simpler cases    |

### Fixtures

Test fixtures (example agent YAML files, expected responses, sample requests) are stored in `testdata/` directories adjacent to the tests that use them:

```
internal/config/testdata/
    valid_agent.yaml
    invalid_missing_name.yaml
    invalid_bad_yaml.yaml
    agent_with_tools.yaml

tests/integration/testdata/
    openai_chat_request.json
    openai_chat_response.json
    anthropic_messages_request.json
    anthropic_messages_response.json

tests/conformance/testdata/
    openai_schema.json
    anthropic_schema.json
```

---

## 6. Test Data Management

### Adding a New Test Fixture

1. Create the fixture file in the appropriate `testdata/` directory.
2. Name it descriptively: `<what_it_tests>_<variant>.yaml` (e.g., `agent_with_regex_scenarios.yaml`).
3. Add a comment at the top of the fixture explaining what it exercises.
4. Reference it in the test using `os.ReadFile(filepath.Join("testdata", "fixture_name.yaml"))` (Go) or `Path(__file__).parent / "testdata" / "fixture_name.yaml"` (Python).
5. Never hard-code large JSON/YAML blobs inline in test code. Always use fixture files.

### Example Agent Definitions for Tests

The following standard fixtures are used across multiple test suites:

| Fixture                       | Description                                          |
|-------------------------------|------------------------------------------------------|
| `simple_openai_agent.yaml`    | Minimal OpenAI-compatible agent with one scenario    |
| `simple_anthropic_agent.yaml` | Minimal Anthropic-compatible agent with one scenario |
| `multi_scenario_agent.yaml`   | Agent with exact, regex, and default scenarios       |
| `tool_calling_agent.yaml`     | Agent with tool definitions and tool responses       |
| `stateful_agent.yaml`         | Agent that uses session state across turns           |
| `template_agent.yaml`         | Agent using template responses with custom functions |
| `error_agent.yaml`            | Agent configured to return various error responses   |

### Golden Files

For conformance tests, golden files (expected outputs) are stored alongside fixtures. To update golden files after an intentional change:

```bash
go test ./tests/conformance/... -update-golden
```

---

## 7. Coverage Requirements

### Per-Package Targets

| Package                        | Minimum Coverage |
|--------------------------------|------------------|
| `internal/config`              | 85%              |
| `internal/engine/matcher`      | 90%              |
| `internal/engine/response`     | 85%              |
| `internal/engine/tools`        | 85%              |
| `internal/state`               | 80%              |
| `internal/adapter/openai`      | 80%              |
| `internal/adapter/anthropic`   | 80%              |
| `internal/server`              | 75%              |
| `sdk/python/mockagents`        | 80%              |
| **Overall `internal/`**        | **>80%**         |

### Running Coverage Locally

```bash
# Go -- all packages with HTML report
go test -coverprofile=coverage.out ./internal/...
go tool cover -html=coverage.out -o coverage.html

# Go -- single package with terminal summary
go test -cover ./internal/config/...

# Python
pytest --cov=mockagents --cov-report=html sdk/python/tests/
```

### CI Enforcement

- Coverage is computed on every PR.
- If overall `internal/` coverage drops below 80%, the CI check fails.
- Coverage diff is posted as a PR comment using a coverage bot (e.g., `codecov` or `go-coverage-report` action).
- New code in a PR should have >= 80% coverage (measured by diff coverage).

---

## 8. Flaky Test Policy

Flaky tests erode trust in the test suite. A test is considered **flaky** if it fails intermittently without code changes.

### Detection

- CI tracks test results over time. A test that fails on retry but passes on re-run is flagged.
- Any test that fails more than twice in a 7-day window without a corresponding code change is classified as flaky.

### Handling Procedure

1. **Identify.** When a flaky test is detected, open a GitHub issue with the label `flaky-test`. Include the test name, failure logs, and frequency.

2. **Quarantine.** If the test cannot be fixed within 48 hours, skip it with a clear annotation:
   ```go
   // Go
   t.Skip("FLAKY: #123 - intermittent timeout in SSE streaming test")
   ```
   ```python
   # Python
   @pytest.mark.skip(reason="FLAKY: #123 - intermittent timeout in SSE streaming test")
   ```

3. **Fix.** The linked issue is prioritized in the next sprint. Common causes and fixes:
   - **Timing dependencies:** Replace `time.Sleep` with polling/retry with timeout.
   - **Port conflicts:** Use dynamic port allocation (`":0"`) instead of fixed ports.
   - **File system race conditions:** Use `t.TempDir()` for isolated temporary directories.
   - **Order dependence:** Ensure tests do not depend on execution order; reset shared state in setup.

4. **Restore.** Once fixed, remove the skip annotation and monitor for 7 days to confirm stability.

5. **Escalate.** If a quarantined test remains unfixed for more than 2 weeks, it is escalated in the team standup and assigned an owner.

### Prevention

- Integration tests must use ephemeral ports and isolated temp directories.
- Tests must not depend on wall-clock time; use injectable clocks where needed.
- Tests must clean up all resources (servers, files, goroutines) in `t.Cleanup` or `defer`.
- Run the full suite 3 times in CI before merging a new test to verify stability.
