# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Project Overview

**MockAgents** is a Go-based mock server that acts as a drop-in replacement for the OpenAI (`/v1/chat/completions`) and Anthropic (`/v1/messages`) APIs. Developers define agents declaratively in YAML; the server matches incoming requests against scenarios and returns canned responses, simulated tool calls, and optional SSE streams — no real LLM calls.

Note: `mock-agents-product-plan.md` describes a broader multi-language vision (Rust core, React GUI, NATS, chaos engine, etc.). The current codebase is the Phase 1 Go implementation only — ignore the product plan when making code decisions unless explicitly asked.

## Common Commands

All workflows go through the `Makefile`:

```bash
make build           # Compile ./cmd/mockagents -> ./mockagents
make test            # go test ./... -count=1 -timeout 5m
make test-race       # same, with -race
make test-coverage   # writes coverage.out and prints total
make test-python     # cd sdk/python && pytest tests/ -v
make test-typescript # cd sdk/typescript && npm test
make test-all        # Go + Python + TypeScript
make lint            # go vet ./...
make fmt             # gofmt -w .
make validate        # build, then `./mockagents validate examples/`
make run             # build, then start server with examples/ and debug logs
make bench           # go test -run=^$ -bench=. -benchmem -benchtime=1s ./internal/engine/...
make bench-report    # tools/benchreport: refresh docs/benchmarks/latest.{json,md}
make docker          # build Docker image
make docker-up       # docker compose up -d
make docker-down     # docker compose down
make helm-lint       # helm lint deploy/helm/mockagents (with ci/test-values.yaml)
make helm-template   # helm template demo deploy/helm/mockagents
make gui-dev         # cd gui && npm run dev (Next.js 15 console)
make gui-build       # cd gui && npm run build (tsc --strict gate)
make release         # goreleaser release --snapshot --clean (dry run)
make setup           # install goimports + editable Python SDK
```

Run a single Go test:
```bash
go test ./internal/engine -run TestScenarioMatcher -v
go test ./internal/adapter -run TestOpenAI/streaming -v
```

Python SDK single test (from `sdk/python/`):
```bash
python -m pytest tests/test_client.py::test_assertion -v
```

## Architecture

Request flow: **HTTP handler → protocol adapter → engine → response generator → adapter → HTTP response (optionally SSE)**. Interaction logs are written to SQLite via `internal/storage`.

Key packages under `internal/`:

- **`adapter/`** — Wire-format translators. `openai.go` and `anthropic.go` convert between provider request/response JSON and the engine's internal `types`. `token.go` handles token counting. `decode.go` pools `bytes.Buffer` instances for request-body decoding (-39 % B/op vs `json.NewDecoder`). Adapter handlers honor an opt-in `X-Mockagents-Tenant` header and call `Engine.ProcessRequestContext` so engine resolution sees the caller's tenant. When adding a new provider, implement the same translate-in / translate-out pair and wire it up in `server/handlers.go`.
- **`engine/`** — The core. `engine.go` is the entry point; it uses `agent_registry.go` (with parallel `byModel` index + `*ForTenant` visibility methods) to look up an agent, `scenario_matcher.go` to pick a matching scenario, and `response_generator.go` to produce content (template expansion via a pooled `bytes.Buffer`). `tool_processor.go` + `tool_validator.go` handle simulated tool calls. `reqmeta.go` carries `RequestMeta` and `tenant id` on the context (so the package stays cycle-free of `tenancy`). State is in the `state/` subpackage; sessions pre-size their history slice (cap=16) to avoid `growslice` on the hot path.
- **`server/`** — `net/http` server, middleware (auth, logging, CORS), and route handlers for the OpenAI/Anthropic endpoints plus the management API. `log_worker.go` owns a bounded async writer pool that replaced unbounded goroutine fan-out; `log_broadcaster.go` fans writes out to SSE subscribers via `log_handlers.go` `StreamLogs` (GET /api/v1/logs/stream). `costs_handler.go` serves `GET /api/v1/costs`; `audit_handlers.go` serves `GET /api/v1/audit`; `validate_handler.go` serves `POST /api/v1/config/validate` and `pipeline_handlers.go` serves `GET /api/v1/pipelines[/{name}]`. `conformance_test.go` is the cross-adapter integration test suite.
- **`tenancy/`** — Multi-tenant store + bcrypt API keys + RBAC middleware. `Store` is an interface with two impls: `SQLiteStore` (default, pure-Go) and `PostgresStore` (`postgres_store.go`, pgx/database-sql, REF-08 slice B) selected at startup by `MOCKAGENTS_TENANCY_DSN` (unset = SQLite). Postgres uses `SELECT … FOR UPDATE` for read-modify-write instead of SQLite's single-connection serialization; both are exercised by the `Store` conformance suite (`conformance_test.go`, which runs against Postgres when `MOCKAGENTS_TEST_PG_DSN` is set). The store also holds SSO **users** + **sessions** (REF-08 slice D): `identity_{sqlite,postgres}.go` provision a user (email→tenant+role) and mint/resolve/revoke opaque session tokens (stored as a SHA-256 hash). `middleware.go`'s `AuthMiddleware` is **dual-auth** — it resolves an API key (`Authorization`/`X-Api-Key`) OR a session cookie (`mockagents_session`) to the same `Principal`. `auth_cache.go` is a hash-keyed bounded TTL cache that skips bcrypt on repeat resolutions (~36 ms cold → sub-µs hot). `middleware.go` exposes a package-level `DenialHook` so the server can route 401/403 events into the audit log without `tenancy` importing `audit`. `RotateAPIKey` (§2.37) regenerates a key's secret in place inside a transaction, preserving id/name/role/tenant and flushing the auth cache. RBAC roles are ordered `viewer < editor < admin < platform`; **platform** is the cross-tenant operator role and the only one allowed to manage the tenant collection (`GET/POST /api/v1/tenants`, `DELETE /tenants/{id}`). Platform keys are minted **only** by the bootstrap path (`cmd/mockagents`); the management API refuses to assign the platform role (`Role.IsAssignableViaAPI`), so a per-tenant admin cannot self-escalate (X-TN-001). Per-tenant admins manage only their own tenant's keys (handlers pass `principal.TenantID` to tenant-scoped store methods + `ensureOwnTenant` on `{id}` key routes).
- **`audit/`** — Append-only audit log. Nine event kinds (`tenant.created`, `tenant.deleted`, `api_key.created`, `api_key.deleted`, `api_key.role_changed`, `api_key.rotated`, `agent.reloaded`, `pipeline.saved`, `auth.denied`). SQLite-backed with WAL + multi-conn pool. `Recorder` takes a principal-extraction function so it never imports `tenancy`.
- **`pricing/`** — Per-model cost table + usage extractor. `MOCKAGENTS_PRICING` env var loads YAML overrides. Used by `costs_handler.go` and `log_handlers.go` to annotate logs with `cost_usd`.
- **`oidcauth/`** — OIDC relying-party seam for SSO login (REF-08 slice D). Wraps `coreos/go-oidc` + `x/oauth2` behind an `Authenticator` interface (`AuthCodeURL` + PKCE, `Exchange`→verified `Claims`) so `server/oidc_handlers.go` (`/auth/login`, `/auth/callback`, `/auth/logout`) is unit-testable with a fake provider. Callback maps the verified email domain → tenant (`MOCKAGENTS_OIDC_DOMAIN_MAP`), JIT-provisions a user at `MOCKAGENTS_OIDC_DEFAULT_ROLE`, and mints a session. Wired in `cmd/mockagents` from `MOCKAGENTS_OIDC_ISSUER`/`_CLIENT_ID`/`_CLIENT_SECRET`/`_REDIRECT_URL`; GUI "Sign in with SSO" is a follow-on.
- **`quota/`** — Per-tenant rate + monthly-spend enforcement (REF-08 slice C). `Enforcer` holds a token bucket + spend counter per tenant; `server/quota_middleware.go` rejects over-rate (429+Retry-After) / over-spend (402) on the LLM endpoints, the `InteractionCapture` spend hook accrues each response's cost, and `server/quota_handlers.go` serves `GET /api/v1/quota` + `PUT /api/v1/tenants/{id}/quota`. Defaults from `MOCKAGENTS_DEFAULT_RATE_PER_SEC`/`_RATE_BURST`/`_MONTHLY_SPEND_USD`. Per-tenant overrides PERSIST in the tenancy store (`tenant_quotas`, both backends) and reload at startup; spend counters remain in-memory/single-process (reset on restart) — multi-replica counters via Postgres atomic increments are the remaining follow-on. Empty tenant ("" = single-tenant/anonymous) is never limited.
- **`mcp/`** — JSON-RPC 2.0 dispatch + HTTP/stdio transports for `kind: MCPServer` documents. v0.2 added `completion/complete`, `logging/setLevel`, server-emitted notification queue + `NotifyHandler` admin endpoint. v0.3 adds a bidirectional transport (`bidirectional.go` + `sse.go`): `GET /mcp/events` streams server-initiated requests + notifications as SSE, `POST /mcp/response` routes client replies back through `DeliverResponse`, and `Server.SendRequest` / `Sample` / `ListRoots` are the in-process primitives. `POST /mcp/sample` + `/mcp/roots` are admin triggers for black-box tests.
- **`recording/`** — Cassette format + record/replay handlers. v0.2 captures and replays SSE streams via `Proxy.serveStreaming` and `Replay.serveStreaming`.
- **`streaming/`** — SSE chunking with configurable chunk size and delay, used when a request sets `stream: true`.
- **`storage/`** — SQLite interaction logging (via `modernc.org/sqlite`, pure-Go, no cgo). WAL + `synchronous=NORMAL` + `MaxOpenConns=8` for parallel readers/writers. `Log` now captures `LastInsertId` into `entry.ID` so SSE subscribers receive rows with a clickable primary key. Default DB file is `.mockagents.db`. Composite `(tenant_id, id DESC)` index serves the tenant dashboard query (PERF-13). **Privacy/retention (SEC-05):** `MOCKAGENTS_LOG_BODIES`=`full`|`sanitized`|`none` controls response-body capture (default `full`; `sanitized` wires `SanitizeBody`, `none` drops bodies but keeps by-agent grouping via the raw-body model probe), and `MOCKAGENTS_LOG_MAX_ROWS`=N bounds the table via a background `logPruner` calling `PruneToMaxRows` (keeps newest N; 0 = unlimited).
- **`config/`** — YAML loader + validator for agent definition files. `validate_bytes.go` (§2.35) exposes `ValidateBytes(data)` so callers can run the same validator on in-memory input (powers the GUI `/editor` page). The schema lives at `schema/mockagents-v1-agent.json`; examples are in `examples/*.yaml`.
- **`cli/`** — Shared CLI helpers: `scaffold.go` powers `mockagents init`, `color.go` handles terminal output.
- **`observability/`** — OpenTelemetry tracer wiring. Exposes `IsEnabled()` so hot-path callers (engine `ProcessRequestContext`) can skip span attribute construction when no exporter is configured.
- **`runner/`** — TestSuite executor for `mockagents test`. `junit.go` emits Jenkins-compatible JUnit XML.
- **`contract/`** — Contract extraction + diffing for `mockagents contract`. Classifies changes as breaking / additive / info.
- **`types/`** — Domain types shared across packages (Agent, Scenario, Tool, Message, MCP*, etc.). `Metadata.TenantID` is the multi-tenancy ownership marker. Changes here ripple widely.

Outside `internal/`:

- **`cmd/mockagents/`** — Cobra CLI entry points: `init`, `start`, `validate`, `logs`, `test`, `record`, `replay`, `mcp`, `contract`. `start.go` uses `config.LoadAllDocuments` so pipelines register alongside agents, enables the auth cache, and bootstraps multi-tenant mode when `MOCKAGENTS_MULTI_TENANT=1`.
- **`tools/benchreport/`** — Benchmark runner that emits `docs/benchmarks/latest.{json,md}`.
- **`gui/`** — Next.js 15 web console (v0.3). 15 routes covering agent catalog, pipeline DAG viewer, live log feed (real SSE), cost dashboard, audit, schema-validating `/editor`, and multi-tenant admin pages (`/login`, `/admin/tenants`, `/admin/tenants/[id]` with inline role change and Rotate button). Auth is cookie-based (HttpOnly `mockagents_api_key`) read via `next/headers` and injected as `Authorization: Bearer` into every server-side fetch. `gui/app/api/logs/stream/route.ts` is the same-origin SSE proxy for the live feed.
- **`sdk/python/`**, **`sdk/typescript/`**, **`sdk/go/mockagents/`** — Three language SDKs with full parity. All three expose `iter_stream` / `iterStream` / `IterStream` for protocol-agnostic streaming plus `StreamChunk`. The Go SDK additionally ships `NewInProcessClient` which spins up an engine + `httptest.Server` inline so downstream Go users can skip the subprocess in tests.
- **`deploy/helm/mockagents/`** — Chart v0.2 with opt-in HPA, PDB, NetworkPolicy, ServiceMonitor.
- **`deploy/actions/`** + **`deploy/ci/`** — GitHub Actions composite + GitLab CI templates.

## Things to Know

- **Go 1.26.1** per `go.mod`. Module path is `github.com/mockagents/mockagents`.
- **No cgo**: SQLite is `modernc.org/sqlite`. Keep it that way so cross-compilation and `goreleaser` stay simple. (Side effect: `go test -race` is unavailable on this codebase.)
- **Agent YAML schema** is authoritative in `schema/mockagents-v1-agent.json`. `make validate` runs the binary against `examples/` — run it after touching config/types.
- **Template expressions** in responses (`{{ uuid }}`, `{{ fake_name }}`, etc.) are expanded in `engine/response_generator.go`. Add new ones there.
- **Scenario matching** precedence and the `default: true` fallback are exercised by `engine/scenario_matcher_test.go` and the `_e008_` test files — consult those before changing match semantics.
- **Python SDK** in `sdk/python/` is a separate package with its own `pyproject.toml`; it talks to the server over HTTP and has no direct Go coupling.
- **Docs site** in `site/` is MkDocs — not part of the Go build.
- **Resume notes** for picking up unfinished work live in `docs/PROGRESS.md` §1A. Read it before starting any new slice — it has the latest test counts, recommended next task, and the import-direction conventions worth preserving.
- **Performance baseline**: `docs/benchmarks/latest.{json,md}` is checked in. Every perf-affecting change should rerun `make bench-report` and confirm the hot path stays inside the target envelope (see `docs/benchmarks/README.md`). Hot path moved -10 to -24 % during the v0.2 perf workstream — don't regress it.
- **Tenancy import direction**: `tenancy` may import `engine` but not vice versa. The engine uses `engine.WithTenantID` / `engine.TenantIDFromContext` (in `engine/reqmeta.go`) instead of importing `tenancy`. The audit recorder receives a principal-extraction function from the server package for the same reason.
- **Page files in `gui/app/`** can only export `default` plus a small set of config keys. Helper components must live in sibling `.tsx` files (e.g. `LogsConsole.tsx` next to `logs/page.tsx`, `AgentCatalog.tsx` next to `page.tsx`, `DAGViewer.tsx` next to `pipelines/[name]/page.tsx`, `YamlEditor.tsx` next to `editor/page.tsx`).
- **GUI auth**: `gui/lib/api.ts` `getAuthKey` reads `mockagents_api_key` (or, as a fallback, the SSO `mockagents_session` cookie) from `next/headers` and `fetchJSON` injects it as `Authorization: Bearer` on every upstream fetch — the backend accepts an API key or a session token (prefix `mas_`) as a bearer. Single-tenant mode sets no cookie and calls go through anonymously. `/login` accepts a raw key and, when `MOCKAGENTS_SSO_ENABLED=1`, shows a **"Sign in with SSO"** button linking to `/auth/login` (REF-08 slice D). SSO requires the API + GUI to share an origin (a reverse proxy routing `/auth` + `/api` to the backend) so the backend's session cookie is readable by the GUI; the OIDC redirect target should be that shared origin.
- **GUI test story**: `npm run build` under `tsc --strict` is the current coverage surface (component-level unit tests still TBD). Every GUI slice must keep the strict build green.
- **`CLAUDE.md` mirrors this file** (it targets Claude Code). The two share most content verbatim — when you change project overview, commands, or architecture here, apply the same edit to `CLAUDE.md` so they stay in sync.
