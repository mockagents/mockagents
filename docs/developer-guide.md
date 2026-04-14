# MockAgents Developer Guide & Repository Structure

| Field         | Value                                      |
|---------------|--------------------------------------------|
| Document      | Developer Guide & Repository Structure     |
| Project       | MockAgents                                 |
| Version       | 0.1.0                                      |
| Last Updated  | 2026-04-07                                 |
| Status        | Living Document                            |

---

## Table of Contents

1. [Introduction](#introduction)
2. [Prerequisites](#prerequisites)
3. [Repository Structure](#repository-structure)
4. [Getting Started (Development Setup)](#getting-started-development-setup)
5. [Makefile Targets](#makefile-targets)
6. [Development Workflow](#development-workflow)
7. [Adding a New Protocol Adapter](#adding-a-new-protocol-adapter)
8. [Adding a New Response Generator](#adding-a-new-response-generator)
9. [Coding Standards](#coding-standards)
10. [Release Process](#release-process)

---

## Introduction

MockAgents is a platform for mocking AI agent integrations. The core engine is written in Go, a Python SDK is provided for test authoring, and the project is organized as a monorepo. This guide covers everything a contributor needs to clone, build, test, extend, and release MockAgents.

---

## Prerequisites

Before working on MockAgents, ensure the following tools are installed on your development machine.

| Tool              | Minimum Version | Purpose                                  |
|-------------------|-----------------|------------------------------------------|
| **Go**            | 1.22+           | Core engine, CLI, and server compilation |
| **Python**        | 3.11+           | SDK development and SDK tests            |
| **Docker**        | Latest stable   | Container builds and integration testing |
| **Make**          | GNU Make 4+     | Build automation (all platforms)         |
| **golangci-lint** | Latest stable   | Go linting (installed via `make setup`)  |
| **ruff**          | Latest stable   | Python linting and formatting            |

> **Note:** `golangci-lint` and `ruff` are installed automatically by `make setup`. You only need Go, Python, Docker, and Make pre-installed.

---

## Repository Structure

```
mockagents/
├── .github/
│   └── workflows/
│       ├── ci.yaml                 # PR checks (lint, test, build)
│       └── release.yaml            # Tag-triggered release pipeline
│
├── cmd/
│   └── mockagents/
│       └── main.go                 # CLI entry point
│
├── internal/
│   ├── adapter/
│   │   ├── adapter.go              # Adapter interface + registry
│   │   ├── openai/
│   │   │   ├── adapter.go          # OpenAI adapter implementation
│   │   │   ├── types.go            # OpenAI request/response types
│   │   │   ├── streaming.go        # SSE streaming support
│   │   │   └── adapter_test.go
│   │   └── anthropic/
│   │       ├── adapter.go          # Anthropic adapter implementation
│   │       ├── types.go            # Anthropic request/response types
│   │       ├── streaming.go        # SSE streaming support
│   │       └── adapter_test.go
│   │
│   ├── config/
│   │   ├── loader.go               # YAML loading + validation
│   │   ├── schema.go               # JSON Schema for agent definitions
│   │   └── loader_test.go
│   │
│   ├── engine/
│   │   ├── engine.go               # Core engine (request processing)
│   │   ├── scenario.go             # Scenario matching logic
│   │   ├── response/
│   │   │   ├── generator.go        # ResponseGenerator interface
│   │   │   ├── static.go           # Static response generator
│   │   │   ├── template.go         # Template response generator
│   │   │   └── functions.go        # Template custom functions
│   │   ├── state/
│   │   │   ├── store.go            # StateStore interface + in-memory impl
│   │   │   └── store_test.go
│   │   ├── tools/
│   │   │   ├── processor.go        # Tool call processing
│   │   │   └── processor_test.go
│   │   └── engine_test.go
│   │
│   ├── server/
│   │   ├── server.go               # HTTP server setup
│   │   ├── router.go               # Route registration
│   │   ├── middleware.go            # Logging, CORS, request ID middleware
│   │   └── handlers/
│   │       ├── health.go           # Health check endpoint
│   │       ├── agents.go           # Agent CRUD endpoints
│   │       ├── logs.go             # Request log endpoints
│   │       └── sessions.go         # Session management endpoints
│   │
│   ├── storage/
│   │   ├── sqlite.go               # SQLite storage implementation
│   │   ├── migrations/             # SQL migration files
│   │   └── sqlite_test.go
│   │
│   └── types/
│       ├── agent.go                # AgentDefinition, ToolDefinition, etc.
│       ├── request.go              # EngineRequest, EngineResponse
│       └── session.go              # ConversationSession, Message
│
├── sdk/
│   └── python/
│       ├── pyproject.toml          # Python package metadata and dependencies
│       ├── src/
│       │   └── mockagents/
│       │       ├── __init__.py
│       │       ├── client.py       # MockAgentClient
│       │       ├── server.py       # MockAgentServer (subprocess manager)
│       │       ├── scenario.py     # Scenario + expect() assertions
│       │       └── types.py        # Pydantic models
│       └── tests/                  # Python SDK test suite
│
├── testdata/                       # Shared test fixtures
│   └── agents/
│       ├── simple-agent.yaml       # Basic agent definition fixture
│       ├── tool-calling-agent.yaml # Agent with tool call scenarios
│       └── streaming-agent.yaml    # Agent with streaming responses
│
├── examples/                       # User-facing examples
│   ├── quickstart/                 # Minimal getting-started example
│   ├── openai-mock/                # OpenAI adapter example
│   └── anthropic-mock/             # Anthropic adapter example
│
├── docs/                           # Project documentation
├── Dockerfile                      # Multi-stage container build
├── Makefile                        # Build automation targets
├── go.mod                          # Go module definition
├── go.sum                          # Go dependency checksums
└── README.md                       # Project overview and quick start
```

### Key Directory Descriptions

| Directory           | Description |
|---------------------|-------------|
| `cmd/mockagents/`   | The CLI entry point. Parses flags, loads configuration, and starts the mock server. |
| `internal/adapter/` | Protocol adapters that translate between provider-specific wire formats (OpenAI, Anthropic) and the internal engine types. Each adapter lives in its own sub-package. |
| `internal/config/`  | YAML configuration loading, validation, and JSON Schema definitions for agent YAML files. |
| `internal/engine/`  | The core mock engine. Receives normalized requests, matches them against scenarios, generates responses, and manages conversation state. |
| `internal/server/`  | HTTP server, router, middleware, and handler functions. This is the network boundary of the application. |
| `internal/storage/` | Persistence layer. Currently backed by SQLite for request logs and session data. |
| `internal/types/`   | Shared domain types used across all internal packages. |
| `sdk/python/`       | The Python SDK that wraps the Go server for use in Python test suites. |
| `testdata/`         | YAML fixtures consumed by Go tests. Kept at the repo root so they are accessible to any package. |
| `examples/`         | Runnable examples for end users demonstrating common use cases. |

---

## Getting Started (Development Setup)

### 1. Clone the Repository

```bash
git clone https://github.com/your-org/mockagents.git
cd mockagents
```

### 2. Install Development Tools

```bash
make setup          # Installs golangci-lint, ruff, and other dev tools
```

### 3. Build the Go Binary

```bash
make build          # Compiles to ./bin/mockagents
```

### 4. Run Tests

```bash
make test           # Run all Go tests with race detection
make test-python    # Run Python SDK tests
```

### 5. Lint Everything

```bash
make lint           # Runs golangci-lint (Go) and ruff (Python)
```

### 6. Build the Docker Image

```bash
make docker         # Builds the mockagents Docker image
```

### Quick Verification

After setup, run the full check suite to confirm your environment is working:

```bash
make lint test test-python
```

---

## Makefile Targets

| Target           | Description                                                              |
|------------------|--------------------------------------------------------------------------|
| `make setup`     | Install development tools (`golangci-lint`, `ruff`, Python dev deps).    |
| `make build`     | Compile the Go binary to `./bin/mockagents`.                             |
| `make test`      | Run all Go tests with `-race` and `-count=1`.                            |
| `make test-python` | Run Python SDK tests via `pytest`.                                     |
| `make test-all`  | Run both Go and Python test suites.                                      |
| `make lint`      | Run `golangci-lint` on Go code and `ruff check` on Python code.         |
| `make lint-fix`  | Run linters with auto-fix enabled.                                       |
| `make fmt`       | Format Go code (`gofmt`) and Python code (`ruff format`).               |
| `make docker`    | Build the Docker image (`mockagents:latest`).                            |
| `make run`       | Build and run the server locally with default configuration.             |
| `make clean`     | Remove build artifacts (`./bin/`, `./dist/`).                            |
| `make generate`  | Run code generation (e.g., JSON Schema, mocks).                         |
| `make release`   | Build release binaries for all target platforms via `goreleaser`.        |
| `make help`      | Print all available targets with descriptions.                           |

---

## Development Workflow

### Branch Naming Convention

| Prefix      | Usage                        | Example                              |
|-------------|------------------------------|--------------------------------------|
| `feature/`  | New features                 | `feature/bedrock-adapter`            |
| `fix/`      | Bug fixes                    | `fix/streaming-timeout`              |
| `docs/`     | Documentation changes        | `docs/add-adapter-guide`             |
| `test/`     | Test additions or refactors  | `test/engine-edge-cases`             |
| `chore/`    | Tooling, CI, dependency bumps| `chore/upgrade-go-1.23`              |

### Pull Request Process

1. **Create a branch** from `main` using the naming convention above.
2. **Make your changes.** Keep PRs focused -- one logical change per PR.
3. **Run checks locally** before pushing:
   ```bash
   make lint test
   ```
4. **Push and open a PR.** The CI pipeline runs `ci.yaml` automatically.
5. **All checks must pass:** lint, unit tests, and build must succeed.
6. **One approval required** from a maintainer before merging.
7. **Squash-merge** into `main`. The PR title becomes the commit message.

### Commit Message Format

This project follows [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <description>

[optional body]

[optional footer]
```

**Types:**

| Type     | When to use                                |
|----------|--------------------------------------------|
| `feat`   | A new feature                              |
| `fix`    | A bug fix                                  |
| `docs`   | Documentation only changes                 |
| `test`   | Adding or updating tests                   |
| `chore`  | Build process, CI, tooling, dependencies   |
| `refactor` | Code restructuring with no behavior change |

**Examples:**

```
feat(adapter): add Anthropic streaming support
fix(engine): handle empty tool call array in scenario matching
docs: add developer guide and repo structure
test(config): add validation tests for malformed YAML
chore: upgrade golangci-lint to v1.58
```

---

## Adding a New Protocol Adapter

This section walks through adding support for a new AI provider (e.g., Bedrock, Gemini).

### Step 1: Create the Adapter Directory

```
internal/adapter/newprotocol/
├── adapter.go       # Adapter implementation
├── types.go         # Provider-specific request/response types
├── streaming.go     # SSE streaming (if supported)
└── adapter_test.go  # Unit tests
```

### Step 2: Implement the Adapter Interface

Every adapter must satisfy the `Adapter` interface defined in `internal/adapter/adapter.go`:

```go
// Adapter translates between a provider's wire format and internal engine types.
type Adapter interface {
    // Name returns the adapter identifier (e.g., "openai", "anthropic").
    Name() string

    // ParseRequest converts an HTTP request into an EngineRequest.
    ParseRequest(r *http.Request) (*types.EngineRequest, error)

    // FormatResponse converts an EngineResponse into an HTTP response body.
    FormatResponse(resp *types.EngineResponse) ([]byte, error)

    // FormatStreamChunk converts a single chunk for SSE streaming.
    FormatStreamChunk(chunk *types.StreamChunk) ([]byte, error)

    // Routes returns the HTTP routes this adapter handles.
    Routes() []Route
}
```

Implement each method in `adapter.go`. Define provider-specific structs in `types.go`. If the provider supports streaming, implement chunk formatting in `streaming.go`.

### Step 3: Register the Adapter

Open `internal/adapter/adapter.go` and add the new adapter to the default registry:

```go
func DefaultRegistry() *Registry {
    r := NewRegistry()
    r.Register(openai.New())
    r.Register(anthropic.New())
    r.Register(newprotocol.New())   // <-- add this line
    return r
}
```

### Step 4: Add Tests

Write unit tests in `adapter_test.go` covering:

- Request parsing (valid and malformed payloads).
- Response formatting (all response types the provider supports).
- Streaming chunk formatting.
- Route registration.

Add integration test fixtures under `testdata/agents/` if new YAML fields are needed.

### Step 5: Add an Example

Create `examples/newprotocol-mock/` with a minimal working example showing how to define an agent YAML and make requests against the mock.

### Step 6: Update Documentation

- Add the new adapter to the feature list in `README.md`.
- Add an example agent YAML in the relevant docs.

---

## Adding a New Response Generator

Response generators control how the engine produces mock responses. The built-in generators are `static` (fixed responses) and `template` (Go template-based responses).

### Step 1: Create the Generator File

Add a new file under `internal/engine/response/`:

```
internal/engine/response/
├── generator.go        # ResponseGenerator interface
├── static.go           # Existing: static responses
├── template.go         # Existing: template responses
├── newgenerator.go     # <-- your new generator
└── newgenerator_test.go
```

### Step 2: Implement the ResponseGenerator Interface

```go
// ResponseGenerator produces a response for a matched scenario.
type ResponseGenerator interface {
    // Type returns the generator identifier (e.g., "static", "template").
    Type() string

    // Generate produces an EngineResponse given a request and scenario config.
    Generate(req *types.EngineRequest, config map[string]any) (*types.EngineResponse, error)
}
```

### Step 3: Register the Generator

Add the generator to the factory function in `generator.go`:

```go
func NewGenerator(genType string) (ResponseGenerator, error) {
    switch genType {
    case "static":
        return &StaticGenerator{}, nil
    case "template":
        return &TemplateGenerator{}, nil
    case "newgenerator":                         // <-- add this case
        return &NewGenerator{}, nil
    default:
        return nil, fmt.Errorf("unknown generator type: %s", genType)
    }
}
```

### Step 4: Add Tests

Cover:

- Happy-path generation with valid config.
- Error handling for missing or invalid config keys.
- Edge cases specific to the generation strategy.

### Step 5: Add a Test Fixture

Create a YAML agent definition in `testdata/agents/` that exercises the new generator so integration tests can validate end-to-end behavior.

### Step 6: Document the Generator

Add usage examples to the docs showing the YAML syntax for configuring the new generator in an agent definition.

---

## Coding Standards

### Go

- **Style:** Follow [Effective Go](https://go.dev/doc/effective_go) and the [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).
- **Linting:** All code must pass `golangci-lint` using the project's `.golangci.yml` configuration.
- **Error handling:** Always wrap errors with context using `fmt.Errorf("operation: %w", err)`. Never call `panic` in library code -- panics are reserved for truly unrecoverable states during initialization.
- **Logging:** Use `log/slog` with structured fields:
  ```go
  slog.Info("request processed",
      "adapter", adapterName,
      "scenario", scenarioID,
      "duration_ms", elapsed.Milliseconds(),
  )
  ```
- **Naming:** Exported types and functions should have doc comments. Avoid stuttering (e.g., prefer `adapter.Registry` over `adapter.AdapterRegistry`).
- **Testing:** Use table-driven tests. Place test files alongside the code they test (`_test.go` suffix). Use `testdata/` for fixtures.

### Python

- **Linting and formatting:** All Python code must pass `ruff check` and `ruff format`.
- **Type hints:** Required on all public functions and methods. Use modern syntax (`str | None` instead of `Optional[str]`).
- **Models:** Use Pydantic `BaseModel` for all data structures that cross API boundaries.
- **Testing:** Use `pytest`. Place tests in `sdk/python/tests/`.
- **Docstrings:** Use Google-style docstrings on all public classes and functions.

### General

- **No secrets in code.** Never commit API keys, tokens, or credentials. Use environment variables.
- **Keep dependencies minimal.** Justify new dependencies in the PR description.
- **Write tests.** All new features and bug fixes should include corresponding tests.

---

## Release Process

### Versioning

MockAgents follows [Semantic Versioning](https://semver.org/):

- **MAJOR** (`X.0.0`): Breaking changes to the agent YAML schema, adapter interface, or SDK API.
- **MINOR** (`0.X.0`): New features, new adapters, new generators -- backward compatible.
- **PATCH** (`0.0.X`): Bug fixes and documentation improvements.

### Tag Format

```
v<MAJOR>.<MINOR>.<PATCH>
```

Examples: `v0.1.0`, `v0.2.1`, `v1.0.0`

### Release Steps

1. **Ensure `main` is clean.** All CI checks pass on the latest commit.

2. **Create and push a tag:**
   ```bash
   git tag -a v0.2.0 -m "Release v0.2.0"
   git push origin v0.2.0
   ```

3. **GitHub Actions takes over.** The `release.yaml` workflow is triggered by the tag push and performs the following:
   - Runs the full test suite.
   - Builds Go binaries for all target platforms via `goreleaser`.
   - Builds and pushes the Docker image.
   - Publishes the Python SDK to PyPI.
   - Creates a GitHub Release with changelogs and attached binaries.

### Release Artifacts

| Artifact               | Description                                    | Distribution     |
|------------------------|------------------------------------------------|------------------|
| Go binary (Linux)      | `mockagents-linux-amd64`, `mockagents-linux-arm64` | GitHub Release |
| Go binary (macOS)      | `mockagents-darwin-amd64`, `mockagents-darwin-arm64` | GitHub Release |
| Go binary (Windows)    | `mockagents-windows-amd64.exe`                 | GitHub Release   |
| Docker image           | `ghcr.io/your-org/mockagents:<version>`        | GitHub Container Registry |
| Python SDK             | `mockagents` package                           | PyPI             |

### Pre-release Versions

For release candidates, use the format `v0.2.0-rc.1`. These are published as pre-releases on GitHub and as pre-release versions on PyPI (`pip install mockagents==0.2.0rc1`).

---

*This is a living document. Update it when project structure, tooling, or processes change.*
