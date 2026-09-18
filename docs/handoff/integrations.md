# SDK, GUI, and integration map

Provider SDKs connect to a wire-compatible mock endpoint; MockAgents SDKs manage mock lifecycles, make calls, and provide assertions. These are different roles. A framework can often use a local provider base URL without a dedicated MockAgents adapter.

| Consumer | Integration point | Ownership / limits |
| --- | --- | --- |
| OpenAI / Anthropic / Gemini SDK | Matching HTTP path and base URL, model in fixture catalog | Provider parser and wire contract must be tested; field acceptance is not full upstream parity |
| Azure, Ollama, Bedrock client | Dedicated adapter routes | Preserve provider framing/error semantics rather than assuming Chat Completions everywhere |
| MCP client | Separate HTTP `/mcp` or stdio process | Initialize/session lifecycle and RPC capability subset |
| A2A client | Agent Card plus JSON-RPC POST `/` | Separate fixture-driven task lifecycle |
| Vector clients | Qdrant, Pinecone, Chroma profile routes | Common mock store with profile-specific translation and supported filters |
| Search/rerank/moderation client | Tavily, Cohere, OpenAI moderation routes | Synthetic/local behavior; no live retrieval/classification model |
| Python SDK | [client](../../sdk/python/mockagents/client.py), [server](../../sdk/python/mockagents/server.py), pytest plugin, assertions and scenario builders | requests/PyYAML runtime dependencies; Python floor declared in pyproject |
| TypeScript SDK | [client](../../sdk/typescript/src/client.ts), [server](../../sdk/typescript/src/server.ts), assertions/builders/MCP | Package manifests and lockfiles govern build tooling |
| Go SDK | [client](../../sdk/go/mockagents/client.go), server manager, in-process client, streaming, MCP and expectations | Go module version follows repository |
| npx launcher | [binary installer](../../sdk/npx/lib/binary.js) | Resolve platform/archive/version, verify download, execute Go binary |
| Vitest/Jest helpers | [core](../../sdk/vitest/src/core.ts), [fixture](../../sdk/vitest/src/index.ts), [Jest](../../sdk/vitest/src/jest.ts) | Test-scoped process/fixture management; teardown matters |
| LangChain/CrewAI/Vercel AI | SDK adapter modules and [framework recipes](../../site/docs/guides/framework-testing.md) | Framework-facing helpers or base-URL routing, not new server schedulers |

Python has LangChain/CrewAI adapters; TypeScript has LangChain/AI SDK adapters. Other documented framework recipes can use protocol routing without implying there is a native adapter module for every named framework.

## SDK lifecycle

Process managers start the binary, wait for readiness/health as implemented, expose connection details, and stop the child. Tests should use context managers/fixtures or explicit close/stop in teardown, including startup failure paths. Installers are a supply-chain boundary: preserve pinned version selection, checksum/archive validation, bounded download behavior, and subprocess argument separation.

The Go `NewInProcessClient` creates an `httptest.Server` with Chat Completions, model listing, Anthropic Messages, and a minimal health route. It does **not** mount the full management API, tenancy chain, or every provider adapter. Use the actual binary/server for those integration tests. This is a useful example of why an embedded client's available methods do not prove its test server exposes every route.

Versions of the Python/npm wrappers and launcher must match the release binary tag because they download that binary version. Inspect [RELEASING.md](../RELEASING.md) before changing package versions.

## Web console

[gui/package.json](../../gui/package.json) declares Next.js 16 and React 19 at the baseline; exact resolved dependencies are in its lockfile. Pages are server components with interactive client islands for catalog filtering, log feeds, editors, pipeline execution and reports. It is a separate application listening on port 3001 in development, with `MOCKAGENTS_API_URL` selecting the backend.

[gui/lib/api.ts](../../gui/lib/api.ts) owns typed HTTP calls, forwarding authentication, timeouts and wire models. Normal management calls use a 10-second timeout, probes 5 seconds and pipeline runs 60 seconds. Server-side cookies carry the API key or SSO session token, which is forwarded as Bearer; [auth.ts](../../gui/lib/auth.ts) handles lifecycle and secure cookie behavior. The GUI asks `/api/v1/identity` for actual role/capabilities instead of fabricating permissions from a successful privileged probe.

| Surface | Backend contract | Important UX invariant |
| --- | --- | --- |
| Overview | Health, readiness, identity, catalog revisions | Unknown/unreachable is distinct from healthy, empty or unauthenticated |
| Agent editor | Definition GET, validation, conditional PUT | Review complete draft; preserve unsupported fields; keep draft after 412 |
| Pipeline view/run/editor | Pipeline GET/PUT/run, nanosecond durations | Display actual node trajectory/partial failure; no invented fan-in |
| Logs | Query/detail and same-origin SSE proxy | Respect truncation/source labels and reconnect bounds |
| Costs/reports | Bounded aggregates / locally loaded rows | Estimates and snapshots, not attested savings or complete server evidence |
| Admin/account | Role/tenant gates and key lifecycle | Browser controls are advisory; backend authorizes each mutation |

Report exports exclude raw bodies by default; enabling them exposes whatever the backend captured. GUI rendering does not provide a substitute for server log policy or recording redaction. SSO needs correct same-origin/proxy cookie deployment; a separate development port alone does not define a complete production login topology.

## Dependencies and external traffic

Go dependency roles include Cobra (CLI), yaml.v3 (authoring), fsnotify (watch), modernc SQLite (no-cgo storage), pgx (optional Postgres), coder/websocket (Realtime), OIDC/OAuth libraries, cryptography and OTel. [go.mod](../../go.mod) pins the Go dependency versions. The [dependency map](reference/dependencies.md) shows internal import directions.

Ordinary fixture responses require no upstream model API. Explicit recording modes, OIDC login/discovery, optional Postgres/OTLP, and installer downloads can contact external services. Provider-drift workflows may deliberately use credentials/network; [testing](testing.md) distinguishes them from offline fixture checks.
