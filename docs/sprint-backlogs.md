# MockAgents -- Sprint Backlogs

**Version:** 1.2
**Date:** April 15, 2026
**Status:** Sprints 1–6 complete; Phase 4 v0.2 + v0.3 work tracked in PROGRESS.md
**Author:** Engineering Team
**Related:** [Implementation Plan](./implementation-plan.md) | [PRD](./PRD.md) | [PROGRESS.md](./PROGRESS.md)

---

## Active Sprint (post-MVP, 2026-04-15)

> **Read PROGRESS.md §1A "Resume Notes" first.** That section is
> the authoritative handoff for any future work. The original
> Sprint 1–6 tables below are kept for historical reference but
> every story in them is closed.

The original 6-sprint MVP plan is **complete**. Phase 4 v0.2 and
v0.3 slices all landed since the v0.1 freeze and are ledgered as
PROGRESS.md §§2.17–2.37:

- **v0.2** — perf workstream (§§2.18–2.24), Python + TS SDK
  streaming, GUI v0.2 (costs/audit/log-detail/live-feed-polling),
  Helm v0.2 (HPA/PDB/NetworkPolicy/ServiceMonitor), MCP v0.2
  (completion/logging/notifications), tenant-scoped agent isolation.
- **v0.3** — Go SDK streaming + in-process engine mode (§2.31),
  GUI admin auth with cookie-backed sessions (§2.32), MCP v0.3
  bidirectional transport for sampling/roots (§2.33), GUI live
  feed via real server-sent events (§2.34), schema-aware config
  editor (§2.35), pipeline DAG viewer + mgmt API (§2.36), API
  key rotation (§2.37).

**Recommended next slices** (per PROGRESS.md §1A), in declining
ROI order:

1. **GUI v0.3 workflow editor** — drag-to-rewire for
   `kind: Pipeline`. The read-only DAG viewer shipped in §2.36;
   the editor needs a DAG widget (React Flow or similar) and a
   YAML round-trip layer.
2. **SaaS-tier multi-tenancy** — Postgres tenancy store, SSO/OAuth,
   per-tenant agent name collisions, billing/quotas. Needs design
   discussion before implementation.

## Active Hardening Sprint (architecture review, 2026-05-30)

The 2026-05-30 architect review found several issues that should be
handled before adding more SaaS-tier surface area. The work is ordered
to close externally visible security and isolation gaps first, then fix
runtime correctness, observability fidelity, and release hygiene.

### Sprint Goal

Make the current local-first + experimental multi-tenant runtime safe
enough to extend: localhost binding by default, tenant-derived access
control everywhere, tenant-scoped logs/costs/models, concurrency-safe
session state, trustworthy interaction logs, and clean package metadata.

### Workstream Plan

| Workstream | Scope | Priority | Owner | Sequencing |
| ---------- | ----- | -------- | ----- | ---------- |
| AHR-01 Runtime exposure control | Add server host config and `--host`; default bind to `127.0.0.1`; update Docker/Helm to opt into `0.0.0.0`; add tests. | P0 | BE-1 | First |
| AHR-02 Tenant isolation closure | Remove untrusted tenant header semantics; derive tenant only from authenticated principal; scope `/v1/models` and LLM endpoint resolution. | P0 | BE-2 | First |
| AHR-03 Tenant-scoped observability data | Add tenant id to interaction logs; filter logs, costs, live streams, stream metrics, and deletes by tenant; add migrations/tests. | P0 | BE-2 | After AHR-02 |
| AHR-04 Session state concurrency | Make per-session mutation atomic via store update closure or per-session mutex; add concurrent same-session tests and CI race guidance. | P1 | BE-3 | Parallel after AHR-01 |
| AHR-05 Interaction log fidelity | Capture sanitized request body, real `X-Session-Id`, protocol, scenario, tool count, response error, and truncation status. | P1 | BE-1 | After AHR-03 schema decision |
| AHR-06 CORS and GUI cookie hardening | Make CORS origins/headers configurable; include `X-Api-Key`; set `secure` cookies when configured for HTTPS. | P1 | DX-1 | Parallel with AHR-02 |
| AHR-07 Adapter registration boundary | Introduce adapter registration/mounting interface so server does not directly hardwire OpenAI and Anthropic handlers. | P2 | BE-3 | After P0/P1 stabilization |
| AHR-08 Release hygiene and contract checks | Align license metadata to Apache 2.0, expand `.gitignore`, remove generated artifacts from tracking, add schema/API/SDK drift checks. | P2 | DX-1 | Parallel, low blast radius |

### Task Board

| Task ID | Task | Story | Owner | Est (days) | Priority | Status | Dependencies | Acceptance Criteria |
| ------- | ---- | ----- | ----- | ---------- | -------- | ------ | ------------ | ------------------- |
| AHR-01a | Add `Host` to server config and `--host` CLI flag | AHR-01 | BE-1 | 0.5 | P0 | DONE | -- | Default server listen address is `127.0.0.1:8080`; tests cover configured host. |
| AHR-01b | Update Docker, Compose, Helm, and docs for explicit `0.0.0.0` bind | AHR-01 | BE-1 | 0.5 | P0 | DONE | AHR-01a | Container deployments remain reachable; local binary remains localhost-only. |
| AHR-02a | Replace `X-Mockagents-Tenant` trust with principal-derived tenant context | AHR-02 | BE-2 | 0.75 | P0 | DONE | -- | Unauthenticated requests cannot select tenant-scoped agents by header. |
| AHR-02b | Scope `/v1/models` to the authenticated tenant or global-only anonymous view | AHR-02 | BE-2 | 0.5 | P0 | DONE | AHR-02a | Model listing never exposes foreign tenant agents. |
| AHR-03a | Add tenant id to interaction log model, SQLite schema, and insert path | AHR-03 | BE-2 | 0.75 | P0 | DONE | AHR-02a | New log rows include tenant id; existing DBs migrate cleanly. |
| AHR-03b | Filter log list/detail/delete, cost aggregates, and live streams by tenant | AHR-03 | BE-2 | 1.0 | P0 | DONE | AHR-03a | Tenant callers see only their rows; admin/global behavior is documented. |
| AHR-04a | Make session state updates atomic for same-session concurrent requests | AHR-04 | BE-3 | 1.0 | P1 | DONE | -- | Concurrent same-session tests do not lose turns or race under `go test -race`. |
| AHR-04b | Add CI/race documentation for Windows CGO caveat | AHR-04 | BE-3 | 0.25 | P1 | DONE | AHR-04a | REF-03: CI runs `-race` on Linux/macOS only; CONTRIBUTING "Race detection" + Makefile note. |
| AHR-05a | Capture sanitized request body and real protocol/session metadata | AHR-05 | BE-1 | 0.75 | P1 | DONE | AHR-03a | REF-04: request-body tee under `MOCKAGENTS_LOG_BODIES`; real `X-Session-Id`. |
| AHR-05b | Record scenario name, tool count, error, and truncation state | AHR-05 | BE-1 | 0.75 | P1 | DONE | AHR-05a | REF-04: scenario/tool-count/error stamped via RequestMeta; persisted `truncated` column. |
| AHR-06a | Add configurable CORS origins and allowed headers | AHR-06 | DX-1 | 0.5 | P1 | DONE | -- | `Config.CORSAllowedOrigins` reflects an explicit allow-list with `Vary: Origin` (empty/`*` keeps wildcard); `internal/server/middleware.go` (F-MW-001). |
| AHR-06b | Set secure GUI auth cookies when deployment URL is HTTPS | AHR-06 | DX-1 | 0.5 | P1 | DONE | -- | `gui/lib/auth.ts` sets `httpOnly`, `sameSite: "strict"`, and `secure` (gated on `NODE_ENV === "production"`). |
| AHR-07a | Define adapter route registration interface and migrate OpenAI/Anthropic mounting | AHR-07 | BE-3 | 1.0 | P2 | DONE | AHR-01a | REF-05: `adapter.Adapter`/`Registry`/`DefaultRegistry`; server mounts via one loop. |
| AHR-08a | Align package/license metadata and OpenAPI license to Apache 2.0 | AHR-08 | DX-1 | 0.25 | P2 | DONE | -- | Both SDK manifests now read `Apache-2.0` (see REF-01, 2026-06-05). |
| AHR-08b | Expand `.gitignore` and remove generated artifacts from tracking | AHR-08 | DX-1 | 0.5 | P2 | DONE | -- | `.gitignore` expanded; 56 tracked artifacts (egg-info + `*.pyc`) untracked (see REF-02, 2026-06-05). |
| AHR-08c | Add lightweight schema/API/SDK drift check to CI | AHR-08 | DX-1 | 0.75 | P2 | DONE | AHR-08a | REF-06: `tools/driftcheck` (api-spec `$ref` resolution + license agreement), wired into CI `lint` + `make drift`. |

---

## Refinement Pass (2026-06-05)

Backlog grooming after the docs/homelab session. Every open item from
the Active Hardening Sprint above was re-verified against the current
tree (the code moved since 2026-05-30, so some rows were stale). AHR-06a
and AHR-06b are reclassified **DONE**. The remaining open items are
restated below as ready-to-pull tickets with verified current state and
sharper acceptance criteria. Ordered by ROI (low-risk hygiene first,
then correctness, then larger slices).

### Verification summary (what's actually open)

> **Update 2026-06-05:** REF-01 through REF-06 all landed on `main` the same
> day (PRs #18–#20 + the drift-check PR). The table below is the original
> as-found snapshot; every row except the two big-bet slices (REF-07/08) is now
> closed — see each ticket's ✅ marker.

| ID | Was | Verified state (2026-06-05) | Evidence |
| -- | --- | --------------------------- | -------- |
| AHR-04b | TODO | **Open** — `.github/workflows/ci.yml:35` runs `go test -race` across the OS matrix incl. Windows, but `-race` is a no-op without cgo on this pure-Go build; no doc explains the caveat or where real race coverage comes from. | `.github/workflows/ci.yml`, `Makefile` |
| AHR-05 | TODO | **Partial** — SEC-05 body sanitization shipped (`MOCKAGENTS_LOG_BODIES`, `SanitizeBody`). But `InteractionCapture` never populates `RequestBody`, `Protocol`, `ScenarioName`, `ToolCallsCount`, or `Error`, and there is no truncation-status column. `SessionID` is filled from `X-Request-Id`, not a real session id. | `internal/server/log_handlers.go`, `internal/storage/models.go` |
| AHR-06 | TODO | **Done** — see reclassified rows above. Only residual: `X-Api-Key` absent from the CORS allow-headers list, which is moot (auth is `Authorization: Bearer`). | `internal/server/middleware.go`, `gui/lib/auth.ts` |
| AHR-07 | TODO | **Open** — OpenAI/Anthropic handlers are still hardwired in `server.go:264-271`; no registration boundary. | `internal/server/server.go` |
| AHR-08a | TODO | **Partial** — root `LICENSE`, README, and `api-spec.yaml` are Apache-2.0, but `sdk/python/pyproject.toml` and `sdk/typescript/package.json` both declare **MIT**. | `sdk/python/pyproject.toml:10`, `sdk/typescript/package.json:42` |
| AHR-08b | TODO | **Open** — `.gitignore` lacks `*.pyc`, `__pycache__`, `*.egg-info`, coverage files, `*.db`, and local binaries. | `.gitignore` |
| AHR-08c | TODO | **Open** — no OpenAPI/SDK drift check in CI. | `.github/workflows/ci.yml` |

### Refined ticket: REF-01 — License metadata alignment (was AHR-08a) ✅ DONE 2026-06-05

**Priority:** P2 · **Points:** 1 · **Lane:** release hygiene (lowest blast radius — start here)

The SDKs ship the wrong license string. Bring them in line with the
Apache-2.0 root license.

**Acceptance criteria:**
- [x] `sdk/python/pyproject.toml` `license` reads `Apache-2.0` (SPDX expression form, dropping the deprecated `{text = "MIT"}` table; bumped `setuptools>=77` so the string form parses, dropped the MIT classifier).
- [x] `sdk/typescript/package.json` `"license"` reads `"Apache-2.0"`.
- [x] A CI assertion guards against future drift — delivered as REF-06 (`tools/driftcheck`).
- [x] Changes are metadata-only; no test logic touched.

### Refined ticket: REF-02 — `.gitignore` hygiene (was AHR-08b) ✅ DONE 2026-06-05

**Priority:** P2 · **Points:** 1 · **Lane:** release hygiene

**Acceptance criteria:**
- [x] `.gitignore` ignores `__pycache__/`, `*.pyc`, `*.egg-info/`, `.pytest_cache/`, `coverage.out`, `*.db` / `.mockagents.db`, and the built `mockagents` / `mockagents.exe` binary.
- [x] `git ls-files` shows none of the above still tracked — 56 artifacts untracked (the `mockagents.egg-info/` dir incl. a stale MIT `PKG-INFO`, plus ~50 `*.pyc` across `examples/` and the SDK).
- [x] No tracked `.db` fixtures existed, so no negation was needed.

### Refined ticket: REF-03 — Document the race-detector caveat (was AHR-04b) ✅ DONE 2026-06-05

**Priority:** P1 · **Points:** 1 · **Lane:** correctness/ops

`go test -race` requires `CGO_ENABLED=1` + a C compiler; this build is
otherwise pure-Go (`modernc.org/sqlite`), so a bare Windows dev box can't run
it. (Note: `-race` is a hard error without cgo, not a silent no-op — the
original framing was slightly off.)

**Acceptance criteria:**
- [x] Policy chosen: CI runs `-race` on the **Linux + macOS** legs (always have a C toolchain) and the **Windows** leg without `-race`, so the job never depends on the runner image's C compiler.
- [x] `CONTRIBUTING.md` gains a "Race detection" section; `Makefile` `test-race` carries a comment + explicit `CGO_ENABLED=1`.
- [x] `.github/workflows/ci.yml` matches the documented policy (split race / no-race steps gated on `runner.os`).

### Refined ticket: REF-04 — Interaction-log fidelity (was AHR-05) ✅ DONE 2026-06-05

**Priority:** P1 · **Points:** 5 · **Lane:** correctness/observability

The log schema promises fields the capture path never fills, so the
costs/audit/log-detail surfaces show blanks. SEC-05 sanitization already
exists — this wires the remaining metadata through.

**Acceptance criteria:**
- [x] `InteractionCapture` populates `Protocol`, `ScenarioName`, `ToolCallsCount`, and `Error` from `RequestMeta` (both adapters stamp them — protocol first so early validation errors still record the surface).
- [x] `RequestBody` is captured via a bounded, pooled request-body tee under the existing `MOCKAGENTS_LOG_BODIES` mode (sanitized/none honored), preserving `MaxBytesReader` semantics.
- [x] A persisted `truncated` flag records when a captured body was clipped.
- [x] `SessionID` reflects the real engine session id (`X-Session-Id` when supplied), falling back to request id only when the engine never ran.
- [x] Additive SQLite `truncated` column with a migration for existing DBs; storage round-trip + legacy-migration tests and three server capture tests cover the new behavior.

### Refined ticket: REF-05 — Adapter registration boundary (was AHR-07) ✅ DONE 2026-06-05

**Priority:** P2 · **Points:** 5 · **Lane:** architecture (do after REF-04 so route/log behavior is stable)

**Acceptance criteria:**
- [x] `adapter.Adapter` interface (`Name()` + `Routes()`) plus an ordered `Registry` replace the hardwired `mux.HandleFunc` block; the server mounts via one loop over `adapter.DefaultRegistry(engine).Adapters()`.
- [x] OpenAI and Anthropic register through that boundary; wire behavior is byte-identical (conformance suite green).
- [x] Adding a third provider requires no `server.go` edits — only `DefaultRegistry` + an `Adapter` impl; a `fakeAdapter` test proves it mounts and serves.
- [x] Tenant scope / `ProcessRequestContext` plumbing preserved (handlers unchanged).

### Refined ticket: REF-06 — API/SDK drift check in CI (was AHR-08c) ✅ DONE 2026-06-05

**Priority:** P2 · **Points:** 2 · **Lane:** release hygiene (depends on REF-01)

Implemented as a dependency-light Go tool, `tools/driftcheck` (matching the
`tools/benchreport` precedent), rather than an ad-hoc shell/Python sweep — so
it has unit tests and runs anywhere Go does.

**Acceptance criteria:**
- [x] `tools/driftcheck` walks `docs/api-spec.yaml` and asserts every internal `#/...` `$ref` resolves to an existing node (full JSON-pointer resolution, not just a regex).
- [x] It asserts the license string agrees (`Apache-2.0`) across root `LICENSE`, both SDK manifests, and the OpenAPI `info.license.name` (guards REF-01).
- [x] Wired into the CI `lint` job (`go run ./tools/driftcheck`) + `make drift`; runs in <1s and exits 1 with a per-problem list on drift. Tool unit tests run in the Go job and include live-repo integration guards.

### Larger slices

| ID | Title | Priority | Points | Notes |
| -- | ----- | -------- | ------ | ----- |
| REF-07 | GUI workflow editor (drag-to-rewire `kind: Pipeline`) | P1 | 13 | ✅ **DONE 2026-06-05** (PRs #22/#23/#24). Server write-back (`PUT /api/v1/pipelines/{name}`, ETag/If-Match) + React Flow canvas + live-validate/save. Design: `docs/REF-07-pipeline-editor-design.md`. |
| REF-08 | SaaS-tier multi-tenancy | P2 | 21 | **Scoped 2026-06-05** — `docs/REF-08-saas-multitenancy-design.md` covers all four sub-slices (per-tenant agent collisions, pluggable Postgres tenancy store, enforcement-only quotas, OIDC/SSO) with sequencing. Not yet built. |

### Recommended pull order

1. **One-sitting hygiene batch:** REF-01 → REF-02 (tiny, no-risk, unblock REF-06).
2. **Correctness lane:** REF-03 (doc) and REF-04 (log fidelity) — REF-04 is the highest-value functional fix.
3. **Architecture lane:** REF-05 then REF-06.
4. **Big bets:** REF-07 ✅ done; REF-08 scoped (design doc) — build its four sub-slices in the documented order (collisions → Postgres → quotas → SSO) when product wants to push surface area.

---

## Document Info

| Field             | Value                                                        |
| ----------------- | ------------------------------------------------------------ |
| Project           | MockAgents                                                   |
| Document type     | Sprint Backlogs (detailed)                                   |
| Sprints covered   | Sprint 1 through Sprint 6 (12 weeks)                        |
| Team              | BE-1, BE-2, BE-3 (Backend Engineers), DX-1 (SDK/DevEx)      |
| Sprint duration   | 2 weeks (10 working days)                                    |
| Productive days   | ~8 per engineer per sprint (meetings, reviews, slack)        |
| Capacity          | 4 engineers x 8 days = 32 person-days per sprint             |
| Velocity target   | ~40 story points per sprint                                  |
| Story point basis | 1 SP ~= 0.5 person-day of focused work                      |

### Status Legend

| Status       | Meaning                                      |
| ------------ | -------------------------------------------- |
| TODO         | Not started                                  |
| IN PROGRESS  | Work has begun                               |
| IN REVIEW    | PR submitted, awaiting code review           |
| DONE         | Merged to main, acceptance criteria met      |
| BLOCKED      | Cannot proceed, dependency or issue          |
| CARRY-OVER   | Not completed, moved to next sprint          |

---

## Sprint 1: Project Bootstrap & Core Types (Weeks 1-2)

**Goal:** Establish the monorepo, define all foundational Go types and interfaces, ship the `mockagents validate` command, and set up CI.

**Sprint dates:** April 14 -- April 25, 2026

**Capacity:** 4 engineers x 8 productive days = 32 person-days

**Velocity target:** ~40 story points

### Sprint Planning

| Story ID | Story                                       | Points | Owner(s)     |
| -------- | ------------------------------------------- | ------ | ------------ |
| S1-01    | Initialize Go module                        | 1      | BE-1         |
| S1-02    | Monorepo directory structure                | 1      | BE-1         |
| S1-03    | Makefile with build/test/lint targets       | 2      | BE-1         |
| S1-04    | GitHub Actions CI pipeline                  | 3      | DX-1         |
| S1-05    | Pre-commit hooks                            | 1      | DX-1         |
| S1-06    | Define `AgentDefinition` Go struct          | 3      | BE-2         |
| S1-07    | Define `Adapter` interface                  | 2      | BE-2         |
| S1-08    | Define `ResponseGenerator` interface        | 2      | BE-2         |
| S1-09    | Define `ScenarioMatcher` interface          | 2      | BE-3         |
| S1-10    | Define `ToolProcessor` interface            | 2      | BE-3         |
| S1-11    | Define `StateStore` interface               | 2      | BE-3         |
| S1-12    | JSON Schema for agent definition YAML       | 3      | BE-2         |
| S1-13    | YAML config loader with schema validation   | 5      | BE-3         |
| S1-14    | Unit tests for config parsing               | 3      | BE-3         |
| S1-15    | `mockagents validate` CLI command           | 3      | BE-1         |
| S1-16    | Cobra CLI scaffolding (root + validate)     | 2      | BE-1         |
| S1-17    | Python SDK project scaffold (Poetry)        | 3      | DX-1         |
| S1-18    | Example agent definition files              | 2      | DX-1         |

**Total committed:** 42 story points
**Risk buffer:** 3.2 person-days (10%) reserved -- S1-18 can slip if needed

### Task Board

| Task ID | Task | Story | Owner | Est (days) | Priority | Status | Dependencies | Acceptance Criteria |
| ------- | ---- | ----- | ----- | ---------- | -------- | ------ | ------------ | ------------------- |
| S1-01a | Run `go mod init github.com/mockagents/mockagents` | S1-01 | BE-1 | 0.25 | P0 | TODO | -- | `go.mod` exists with correct module path |
| S1-01b | Add initial `go.sum` and verify `go build ./...` | S1-01 | BE-1 | 0.25 | P0 | TODO | S1-01a | Clean build with no errors |
| S1-02a | Create full directory tree (cmd, internal, pkg, schema, sdk, examples, docker, docs) | S1-02 | BE-1 | 0.25 | P0 | TODO | S1-01a | All directories exist with `.gitkeep` where empty |
| S1-02b | Add `cmd/mockagents/main.go` entry point stub | S1-02 | BE-1 | 0.25 | P0 | TODO | S1-02a | `go run ./cmd/mockagents` prints version |
| S1-03a | Create Makefile with `build` target | S1-03 | BE-1 | 0.15 | P0 | TODO | S1-01b | `make build` produces `bin/mockagents` binary |
| S1-03b | Add `test`, `lint`, `fmt` targets | S1-03 | BE-1 | 0.2 | P0 | TODO | S1-03a | `make test` runs all Go tests, `make lint` runs golangci-lint |
| S1-03c | Add `generate` target for JSON Schema codegen | S1-03 | BE-1 | 0.15 | P1 | TODO | S1-03a | `make generate` is a no-op placeholder for now |
| S1-04a | Create `.github/workflows/ci.yml` | S1-04 | DX-1 | 0.5 | P0 | TODO | S1-01b | Workflow triggers on push and PR to main |
| S1-04b | Add Go test and lint steps | S1-04 | DX-1 | 0.25 | P0 | TODO | S1-04a | `go test ./...` and `golangci-lint run` pass in CI |
| S1-04c | Add Python lint step (ruff) | S1-04 | DX-1 | 0.25 | P1 | TODO | S1-04a, S1-17 | Python linting runs in CI |
| S1-05a | Configure pre-commit hooks (gofmt, golangci-lint, trailing whitespace) | S1-05 | DX-1 | 0.5 | P1 | TODO | S1-04a | `.pre-commit-config.yaml` exists; hooks pass locally |
| S1-06a | Define `AgentDefinition` struct with metadata fields | S1-06 | BE-2 | 0.25 | P0 | TODO | S1-02a | Struct has apiVersion, kind, metadata (name, version, description) |
| S1-06b | Define `AgentSpec` with model, system prompt, tools, scenarios | S1-06 | BE-2 | 0.5 | P0 | TODO | S1-06a | All nested structs: ToolDef, Scenario, Behavior, StreamingConfig, ChaosConfig |
| S1-06c | Add JSON/YAML struct tags and GoDoc comments | S1-06 | BE-2 | 0.25 | P0 | TODO | S1-06b | All exported fields have `json` and `yaml` tags |
| S1-07a | Define `Adapter` interface with `HandleRequest` and `Protocol` | S1-07 | BE-2 | 0.25 | P0 | TODO | S1-06c | Interface compiles, GoDoc describes contract |
| S1-07b | Define `AdapterRequest` and `AdapterResponse` wrapper types | S1-07 | BE-2 | 0.25 | P0 | TODO | S1-07a | Wrapper types decouple wire format from engine types |
| S1-08a | Define `ResponseGenerator` interface with `Generate` method | S1-08 | BE-2 | 0.25 | P0 | TODO | S1-06c | Method signature: `Generate(ctx, ScenarioMatch, StateStore) (AgentResponse, error)` |
| S1-08b | Define `AgentResponse` type with content, tool calls, stop reason | S1-08 | BE-2 | 0.25 | P0 | TODO | S1-08a | Type covers text content, tool calls array, usage metadata |
| S1-09a | Define `ScenarioMatcher` interface with `Match` method | S1-09 | BE-3 | 0.25 | P0 | TODO | S1-06c | Returns `(ScenarioMatch, bool)` |
| S1-09b | Define `ScenarioMatch` result type | S1-09 | BE-3 | 0.25 | P0 | TODO | S1-09a | Contains matched scenario, confidence score, match reason |
| S1-10a | Define `ToolProcessor` interface with `ProcessToolCall` method | S1-10 | BE-3 | 0.25 | P0 | TODO | S1-06c | Accepts tool call + tool definition, returns ToolResult |
| S1-10b | Define `ToolResult` type | S1-10 | BE-3 | 0.25 | P0 | TODO | S1-10a | Contains content string, is_error flag, error details |
| S1-11a | Define `StateStore` interface (Get, Set, Delete, Clear) | S1-11 | BE-3 | 0.25 | P0 | TODO | -- | Keyed by conversation ID, values are `any` |
| S1-11b | Define `ConversationState` type | S1-11 | BE-3 | 0.25 | P0 | TODO | S1-11a | Holds message history, turn count, metadata map |
| S1-12a | Draft JSON Schema covering metadata and spec sections | S1-12 | BE-2 | 0.5 | P0 | TODO | S1-06c | Schema validates apiVersion, kind, metadata fields |
| S1-12b | Add schema rules for tools, scenarios, behavior, chaos | S1-12 | BE-2 | 0.5 | P0 | TODO | S1-12a | All AgentDefinition fields covered with types, required, defaults |
| S1-13a | Implement YAML file reader with multi-document support | S1-13 | BE-3 | 0.5 | P0 | TODO | S1-06c | Reads single-file and multi-file YAML |
| S1-13b | Integrate JSON Schema validation (gojsonschema) | S1-13 | BE-3 | 0.5 | P0 | TODO | S1-12b, S1-13a | Invalid YAML reports schema violations with JSON paths |
| S1-13c | Unmarshal validated YAML into typed `AgentDefinition` struct | S1-13 | BE-3 | 0.5 | P0 | TODO | S1-13b | Returns `[]AgentDefinition` with all fields populated |
| S1-14a | Write tests for valid agent definitions (happy path) | S1-14 | BE-3 | 0.25 | P0 | TODO | S1-13c | At least 3 valid YAML fixtures parse without error |
| S1-14b | Write tests for missing required fields | S1-14 | BE-3 | 0.25 | P0 | TODO | S1-13c | Missing `name`, `model`, `scenarios` produce clear errors |
| S1-14c | Write tests for invalid types and edge cases | S1-14 | BE-3 | 0.25 | P0 | TODO | S1-13c | Wrong types, empty files, malformed YAML all handled |
| S1-14d | Write tests for multi-document YAML | S1-14 | BE-3 | 0.25 | P1 | TODO | S1-13c | File with `---` separators produces multiple definitions |
| S1-15a | Implement `validate` command: load files from args or glob | S1-15 | BE-1 | 0.5 | P0 | TODO | S1-13c, S1-16 | `mockagents validate agents/*.yaml` loads all matched files |
| S1-15b | Format and print validation errors with file path and line numbers | S1-15 | BE-1 | 0.5 | P0 | TODO | S1-15a | Errors show `file.yaml:12: metadata.name is required` |
| S1-16a | Set up Cobra root command with version and help | S1-16 | BE-1 | 0.25 | P0 | TODO | S1-02b | `mockagents --version` prints version; `mockagents --help` shows usage |
| S1-16b | Add `--verbose` and `--config` global flags | S1-16 | BE-1 | 0.25 | P1 | TODO | S1-16a | Verbose flag increases log level; config flag overrides default path |
| S1-17a | Initialize Poetry project with `pyproject.toml` | S1-17 | DX-1 | 0.25 | P0 | TODO | S1-02a | `cd sdk/python && poetry install` works |
| S1-17b | Create package structure (`mockagents/`, `tests/`) | S1-17 | DX-1 | 0.25 | P0 | TODO | S1-17a | `__init__.py`, `client.py`, `server.py` stubs exist |
| S1-17c | Add dev dependencies (pytest, ruff, mypy) | S1-17 | DX-1 | 0.25 | P0 | TODO | S1-17b | `poetry run pytest` and `poetry run ruff check .` pass |
| S1-17d | Write initial `__init__.py` with version export | S1-17 | DX-1 | 0.25 | P1 | TODO | S1-17c | `from mockagents import __version__` works |
| S1-18a | Write customer-support agent example YAML | S1-18 | DX-1 | 0.25 | P1 | TODO | S1-12b | Valid against JSON Schema, demonstrates all key features |
| S1-18b | Write simple echo agent example YAML | S1-18 | DX-1 | 0.25 | P2 | TODO | S1-12b | Minimal agent definition for quick testing |

### Day-by-Day Schedule

| Day | BE-1 | BE-2 | BE-3 | DX-1 |
| --- | ---- | ---- | ---- | ---- |
| D1 (Apr 14) | S1-01a, S1-01b, S1-02a | S1-06a, S1-06b | S1-11a, S1-11b, S1-09a | S1-04a |
| D2 (Apr 15) | S1-02b, S1-03a, S1-03b | S1-06b, S1-06c | S1-09b, S1-10a, S1-10b | S1-04b, S1-04c |
| D3 (Apr 16) | S1-03c, S1-16a, S1-16b | S1-07a, S1-07b, S1-08a | S1-13a | S1-05a |
| D4 (Apr 17) | -- (reviews/meetings) | S1-08b, S1-12a | S1-13b | S1-17a, S1-17b |
| D5 (Apr 18) | -- (reviews/meetings) | S1-12b | S1-13c | S1-17c, S1-17d |
| D6 (Apr 21) | S1-15a | S1-12b (finish) | S1-14a, S1-14b | S1-18a |
| D7 (Apr 22) | S1-15a (finish), S1-15b | -- (reviews/meetings) | S1-14c, S1-14d | S1-18b |
| D8 (Apr 23) | S1-15b (finish) | -- (reviews/meetings) | -- (reviews/meetings) | -- (reviews/meetings) |
| D9 (Apr 24) | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes |
| D10 (Apr 25) | Sprint review & demo | Sprint review & demo | Sprint review & demo | Sprint review & demo |

### Sprint Risks

| # | Risk | Likelihood | Impact | Mitigation |
| - | ---- | ---------- | ------ | ---------- |
| 1 | JSON Schema library (`gojsonschema`) may not support all schema features needed for complex agent definitions | Medium | High | Spike on D1; if gaps found, fall back to manual validation for unsupported features |
| 2 | Team unfamiliar with Cobra CLI patterns; may take longer than estimated | Low | Medium | BE-1 has prior Cobra experience; share Cobra quick-start doc on D1 |
| 3 | Agreement on interface contracts takes longer than expected due to design discussions | Medium | Medium | Timebox design discussions to 30 min; BE-2 drafts first, others review async |

### Sprint Exit Criteria

- [ ] `go build ./cmd/mockagents` produces a working binary on all 3 dev machines
- [ ] `mockagents validate examples/customer-support.yaml` succeeds for valid YAML
- [ ] `mockagents validate` with an invalid YAML prints clear errors with field paths
- [ ] All 6 interfaces (`Adapter`, `ResponseGenerator`, `ScenarioMatcher`, `ToolProcessor`, `StateStore` + `AgentDefinition` struct) are defined with GoDoc
- [ ] JSON Schema file exists and validates all example YAML files
- [ ] CI pipeline passes: Go test, Go lint, Python lint all green
- [ ] Config parser unit tests: 10+ test cases, all passing
- [ ] Python SDK scaffold is installable via `poetry install`

### Demo Script

- **Show 1:** Run `mockagents validate examples/customer-support.yaml` -- demonstrates successful validation of a well-formed agent definition.
- **Show 2:** Run `mockagents validate` against a deliberately broken YAML -- demonstrate clear error messages with file paths and field-level detail.
- **Show 3:** Show the CI pipeline running on a PR -- green checks for Go build, Go test, Go lint, and Python lint.

---

## Sprint 2: Mock Engine Core (Weeks 3-4)

**Goal:** Implement the response generation pipeline so the engine can process a request and return a matched response using static or template generators.

**Sprint dates:** April 28 -- May 9, 2026

**Capacity:** 4 engineers x 8 productive days = 32 person-days

**Velocity target:** ~40 story points

### Sprint Planning

| Story ID | Story                                        | Points | Owner(s)     |
| -------- | -------------------------------------------- | ------ | ------------ |
| S2-01    | Static response generator                    | 3      | BE-1         |
| S2-02    | Template response generator                  | 5      | BE-1         |
| S2-03    | Template function registry                   | 2      | BE-1         |
| S2-04    | Scenario matcher: `content_contains`         | 2      | BE-2         |
| S2-05    | Scenario matcher: `content_regex`            | 2      | BE-2         |
| S2-06    | Scenario matcher: `default` fallback         | 1      | BE-2         |
| S2-07    | Scenario matcher: priority/ordering          | 2      | BE-2         |
| S2-08    | Scenario matcher: composite match (and/or)   | 3      | BE-2         |
| S2-09    | In-memory conversation state store           | 3      | BE-3         |
| S2-10    | Engine `ProcessRequest` pipeline             | 5      | BE-3         |
| S2-11    | Engine request/response internal types       | 2      | BE-3         |
| S2-12    | Unit tests: static generator                 | 2      | BE-1         |
| S2-13    | Unit tests: template generator               | 3      | BE-1         |
| S2-14    | Unit tests: scenario matcher                 | 3      | BE-2         |
| S2-15    | Unit tests: engine pipeline                  | 3      | BE-3         |
| S2-16    | Unit tests: state store                      | 2      | BE-3         |

**Total committed:** 43 story points
**Risk buffer:** 3.2 person-days (10%) reserved -- S2-08 composite matcher is stretch if velocity is low

**Carry-over from Sprint 1:** None expected

### Task Board

| Task ID | Task | Story | Owner | Est (days) | Priority | Status | Dependencies | Acceptance Criteria |
| ------- | ---- | ----- | ----- | ---------- | -------- | ------ | ------------ | ------------------- |
| S2-01a | Implement `StaticGenerator` struct with `Generate` method | S2-01 | BE-1 | 0.5 | P0 | TODO | S1-08 | Returns pre-configured response body verbatim |
| S2-01b | Handle missing/empty static response gracefully | S2-01 | BE-1 | 0.25 | P0 | TODO | S2-01a | Returns descriptive error, not panic |
| S2-01c | Support static responses with tool_calls array | S2-01 | BE-1 | 0.25 | P1 | TODO | S2-01a | Static response can include pre-defined tool_calls |
| S2-02a | Implement `TemplateGenerator` struct with `Generate` method | S2-02 | BE-1 | 0.5 | P0 | TODO | S1-08 | Evaluates Go text/template against scenario config |
| S2-02b | Implement template context (request data, state, metadata) | S2-02 | BE-1 | 0.5 | P0 | TODO | S2-02a | Templates can access `.Request.Messages`, `.State`, `.Agent.Name` |
| S2-02c | Implement template error handling (malformed templates) | S2-02 | BE-1 | 0.5 | P0 | TODO | S2-02a | Malformed template returns error with template name and parse error |
| S2-02d | Add template caching (parse once, execute many) | S2-02 | BE-1 | 0.5 | P1 | TODO | S2-02a | Templates parsed at agent load time, not per-request |
| S2-03a | Create `FuncMap` with `random_int`, `random_string` | S2-03 | BE-1 | 0.25 | P0 | TODO | S2-02a | `{{ random_int 1 100 }}` produces int in range |
| S2-03b | Add `date_offset`, `uuid`, `now` functions | S2-03 | BE-1 | 0.25 | P0 | TODO | S2-03a | `{{ uuid }}` returns valid UUID v4; `{{ now }}` returns ISO 8601 |
| S2-04a | Implement `ContentContainsMatcher` | S2-04 | BE-2 | 0.25 | P0 | TODO | S1-09 | Case-insensitive substring match against last user message |
| S2-04b | Handle multi-message matching (search all user messages vs last only) | S2-04 | BE-2 | 0.25 | P0 | TODO | S2-04a | Configurable: match last message (default) or any user message |
| S2-05a | Implement `RegexMatcher` with compiled regex | S2-05 | BE-2 | 0.25 | P0 | TODO | S1-09 | Regex compiled at load time; match against message content |
| S2-05b | Handle invalid regex gracefully at load time | S2-05 | BE-2 | 0.25 | P0 | TODO | S2-05a | Invalid regex in YAML produces config validation error, not runtime panic |
| S2-06a | Implement `DefaultMatcher` (always matches) | S2-06 | BE-2 | 0.25 | P0 | TODO | S1-09 | Returns match with lowest priority score |
| S2-07a | Implement priority ordering: iterate scenarios in definition order | S2-07 | BE-2 | 0.25 | P0 | TODO | S2-04a, S2-05a, S2-06a | First matching scenario wins; default always sorted last |
| S2-07b | Support explicit `priority` field override in scenario | S2-07 | BE-2 | 0.25 | P1 | TODO | S2-07a | Scenario with `priority: 10` beats scenario with `priority: 5` |
| S2-08a | Implement `CompositeMatcher` with `and` combinator | S2-08 | BE-2 | 0.5 | P1 | TODO | S2-04a, S2-05a | `match: { and: [content_contains: "help", content_regex: "order.*\\d+"] }` |
| S2-08b | Implement `or` combinator | S2-08 | BE-2 | 0.25 | P1 | TODO | S2-08a | `match: { or: [content_contains: "help", content_contains: "support"] }` |
| S2-08c | Support nested composites (and inside or) | S2-08 | BE-2 | 0.25 | P2 | TODO | S2-08a, S2-08b | Recursive evaluation of composite matchers |
| S2-09a | Implement `InMemoryStateStore` with sync.RWMutex | S2-09 | BE-3 | 0.5 | P0 | TODO | S1-11 | Thread-safe Get/Set/Delete/Clear |
| S2-09b | Implement TTL-based expiry with background cleanup goroutine | S2-09 | BE-3 | 0.5 | P1 | TODO | S2-09a | Expired entries cleaned up every 60s; configurable TTL |
| S2-10a | Define `Engine` struct with injected dependencies | S2-10 | BE-3 | 0.25 | P0 | TODO | S2-01a, S2-04a, S2-09a | Engine holds matcher, generators, state store, agent registry |
| S2-10b | Implement `ProcessRequest`: load agent by name | S2-10 | BE-3 | 0.5 | P0 | TODO | S2-10a | Looks up agent definition from in-memory registry |
| S2-10c | Implement `ProcessRequest`: match scenario | S2-10 | BE-3 | 0.5 | P0 | TODO | S2-10b, S2-07a | Passes request through matcher, selects best scenario |
| S2-10d | Implement `ProcessRequest`: generate response | S2-10 | BE-3 | 0.5 | P0 | TODO | S2-10c | Routes to correct generator (static vs template) based on scenario config |
| S2-10e | Implement `ProcessRequest`: error handling and no-match fallback | S2-10 | BE-3 | 0.25 | P0 | TODO | S2-10d | No match returns 400 with descriptive error; generator errors return 500 |
| S2-11a | Define `EngineRequest` (agent name, messages, metadata) | S2-11 | BE-3 | 0.25 | P0 | TODO | S1-06 | Decoupled from OpenAI/Anthropic wire formats |
| S2-11b | Define `EngineResponse` (content, tool calls, usage, stop reason) | S2-11 | BE-3 | 0.25 | P0 | TODO | S2-11a | Protocol-agnostic response structure |
| S2-12a | Write tests for static generator (happy path, empty, tool_calls) | S2-12 | BE-1 | 0.5 | P0 | TODO | S2-01c | 5+ test cases covering all static response variants |
| S2-13a | Write tests for template generator (basic substitution) | S2-13 | BE-1 | 0.25 | P0 | TODO | S2-02c | Template with `{{ .Agent.Name }}` produces correct output |
| S2-13b | Write tests for all custom template functions | S2-13 | BE-1 | 0.5 | P0 | TODO | S2-03b | Each function tested independently; edge cases for random ranges |
| S2-13c | Write tests for malformed templates and missing data | S2-13 | BE-1 | 0.25 | P0 | TODO | S2-02c | Invalid templates return error, not panic |
| S2-14a | Write tests for each matcher type independently | S2-14 | BE-2 | 0.5 | P0 | TODO | S2-08c | content_contains, regex, default each tested with match/no-match |
| S2-14b | Write tests for priority ordering and composite matchers | S2-14 | BE-2 | 0.5 | P0 | TODO | S2-08c | Verify first-match wins, default is last, and/or combinators work |
| S2-15a | Write pipeline integration tests with mock dependencies | S2-15 | BE-3 | 0.5 | P0 | TODO | S2-10e | End-to-end through ProcessRequest; verify correct scenario selected |
| S2-15b | Write pipeline tests for error paths (no match, generator error, missing agent) | S2-15 | BE-3 | 0.5 | P0 | TODO | S2-10e | Each error path returns appropriate error type |
| S2-16a | Write concurrency tests for state store | S2-16 | BE-3 | 0.25 | P0 | TODO | S2-09b | 100 goroutines reading/writing simultaneously; no data races |
| S2-16b | Write TTL expiry tests | S2-16 | BE-3 | 0.25 | P1 | TODO | S2-09b | Expired entries not returned by Get; cleanup goroutine removes them |

### Day-by-Day Schedule

| Day | BE-1 | BE-2 | BE-3 | DX-1 |
| --- | ---- | ---- | ---- | ---- |
| D1 (Apr 28) | S2-01a, S2-01b | S2-04a, S2-04b | S2-11a, S2-11b, S2-09a | SDK scaffold refinements from S1 feedback |
| D2 (Apr 29) | S2-01c, S2-02a | S2-05a, S2-05b | S2-09b | SDK scaffold refinements |
| D3 (Apr 30) | S2-02b, S2-02c | S2-06a, S2-07a | S2-10a, S2-10b | CI pipeline improvements; review PRs |
| D4 (May 1) | S2-02d, S2-03a | S2-07b, S2-08a | S2-10c | Review PRs; documentation prep |
| D5 (May 2) | S2-03b | S2-08b, S2-08c | S2-10d, S2-10e | Review PRs; documentation prep |
| D6 (May 5) | S2-12a, S2-13a | S2-14a | S2-15a | -- (reviews/meetings) |
| D7 (May 6) | S2-13b, S2-13c | S2-14b | S2-15b | -- (reviews/meetings) |
| D8 (May 7) | -- (reviews/meetings) | -- (reviews/meetings) | S2-16a, S2-16b | Review all test PRs |
| D9 (May 8) | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes |
| D10 (May 9) | Sprint review & demo | Sprint review & demo | Sprint review & demo | Sprint review & demo |

### Sprint Risks

| # | Risk | Likelihood | Impact | Mitigation |
| - | ---- | ---------- | ------ | ---------- |
| 1 | Go `text/template` lacks features needed for complex response generation (e.g., JSON output formatting) | Medium | High | Add `toJSON` and `toPrettyJSON` helper functions to FuncMap early; test with real-world agent YAML on D3 |
| 2 | Composite matcher (and/or) design may require refactoring the matcher interface | Low | Medium | S2-08 is P1; can be deferred to Sprint 3 if interface changes are extensive |
| 3 | State store TTL cleanup goroutine may introduce subtle concurrency bugs | Medium | Medium | Use race detector (`go test -race`) on all state store tests; BE-3 has concurrency experience |

### Sprint Exit Criteria

- [ ] `StaticGenerator.Generate()` returns exact configured response for 5+ test cases
- [ ] `TemplateGenerator.Generate()` evaluates all 5 custom functions correctly
- [ ] All 5 matcher types work: content_contains, content_regex, default, priority ordering, composite (and/or)
- [ ] `InMemoryStateStore` passes race detector tests with 100 concurrent goroutines
- [ ] `Engine.ProcessRequest()` wires matcher -> generator -> response correctly end-to-end
- [ ] Test coverage for `internal/engine/` package is 80% or higher
- [ ] Test coverage for `internal/matcher/` package is 80% or higher
- [ ] All PRs reviewed and merged; CI green on main

### Demo Script

- **Show 1:** Walk through a unit test that sends an `EngineRequest` with "I need help with my order" message, show it matching the `content_contains: "order"` scenario and returning a static response.
- **Show 2:** Demonstrate template generator by showing a response that includes `{{ uuid }}`, `{{ now }}`, and `{{ .Request.Messages | len }}` -- each producing dynamic output.
- **Show 3:** Show test coverage report -- highlight 80%+ coverage across engine, generator, and matcher packages.

---

## Sprint 3: OpenAI Adapter & HTTP Server (Weeks 5-6)

**Goal:** Serve OpenAI-compatible mock responses over HTTP, including streaming SSE and function calling, with management API endpoints.

**Sprint dates:** May 12 -- May 23, 2026

**Capacity:** 4 engineers x 8 productive days = 32 person-days

**Velocity target:** ~40 story points

### Sprint Planning

| Story ID | Story                                         | Points | Owner(s)     |
| -------- | --------------------------------------------- | ------ | ------------ |
| S3-01    | HTTP server with Chi router                   | 3      | BE-1         |
| S3-02    | Middleware: request logging                   | 2      | BE-1         |
| S3-03    | Middleware: CORS                              | 1      | BE-1         |
| S3-04    | Middleware: API key extraction                | 1      | BE-1         |
| S3-05    | OpenAI adapter: request parsing              | 3      | BE-2         |
| S3-06    | OpenAI adapter: non-streaming response       | 4      | BE-2         |
| S3-07    | OpenAI adapter: SSE streaming                | 5      | BE-2         |
| S3-08    | OpenAI adapter: function/tool_calls          | 4      | BE-3         |
| S3-09    | OpenAI adapter: usage token estimation       | 2      | BE-3         |
| S3-10    | Management API: health                       | 1      | BE-1         |
| S3-11    | Management API: agents list                  | 2      | BE-1         |
| S3-12    | Management API: agent detail                 | 2      | BE-1         |
| S3-13    | Management API: agent reload                 | 2      | BE-1         |
| S3-14    | Agent routing by model name or path          | 3      | BE-3         |
| S3-15    | `mockagents start` CLI command               | 3      | BE-3         |
| S3-16    | Integration tests: non-streaming             | 3      | DX-1         |
| S3-17    | Integration tests: streaming                 | 3      | DX-1         |
| S3-18    | Integration tests: tool calls                | 3      | DX-1         |

**Total committed:** 47 story points
**Risk buffer:** 3.2 person-days (10%) -- S3-13 (reload) and S3-17 (streaming tests) can slip to Sprint 4 if needed

### Task Board

| Task ID | Task | Story | Owner | Est (days) | Priority | Status | Dependencies | Acceptance Criteria |
| ------- | ---- | ----- | ----- | ---------- | -------- | ------ | ------------ | ------------------- |
| S3-01a | Create Chi router with graceful shutdown | S3-01 | BE-1 | 0.5 | P0 | TODO | S2-10 | Server starts, listens on configurable port, shuts down cleanly on SIGINT |
| S3-01b | Add request ID middleware (X-Request-Id header) | S3-01 | BE-1 | 0.25 | P0 | TODO | S3-01a | Every response includes unique X-Request-Id |
| S3-01c | Add configurable read/write timeouts | S3-01 | BE-1 | 0.25 | P1 | TODO | S3-01a | Default 30s read, 60s write; configurable via CLI flags |
| S3-02a | Implement structured request/response logging middleware | S3-02 | BE-1 | 0.5 | P0 | TODO | S3-01a | Logs: method, path, status, duration, request ID |
| S3-03a | Add CORS middleware with permissive defaults | S3-03 | BE-1 | 0.25 | P1 | TODO | S3-01a | `Access-Control-Allow-Origin: *` for local dev |
| S3-04a | Extract Bearer token from Authorization header into context | S3-04 | BE-1 | 0.25 | P1 | TODO | S3-01a | Token available via `ctx.Value(apiKeyContextKey)` |
| S3-05a | Define `ChatCompletionRequest` Go struct | S3-05 | BE-2 | 0.25 | P0 | TODO | -- | Covers messages, model, tools, stream, temperature, max_tokens, top_p |
| S3-05b | Implement request body parsing and validation | S3-05 | BE-2 | 0.5 | P0 | TODO | S3-05a | Invalid requests return 400 with OpenAI-style error JSON |
| S3-05c | Map `ChatCompletionRequest` to `EngineRequest` | S3-05 | BE-2 | 0.25 | P0 | TODO | S3-05b, S2-11 | Extracts agent name from model field, maps messages |
| S3-06a | Define `ChatCompletionResponse` Go struct | S3-06 | BE-2 | 0.25 | P0 | TODO | -- | Matches OpenAI API response schema exactly |
| S3-06b | Build response from `EngineResponse`: populate choices, usage, id, model | S3-06 | BE-2 | 0.5 | P0 | TODO | S3-06a, S3-05c | Response JSON is indistinguishable from real OpenAI response |
| S3-06c | Handle stop reasons (stop, length, tool_calls) | S3-06 | BE-2 | 0.25 | P0 | TODO | S3-06b | `finish_reason` set correctly based on engine response |
| S3-06d | Generate unique response ID (`chatcmpl-xxxx` format) | S3-06 | BE-2 | 0.25 | P1 | TODO | S3-06b | IDs match OpenAI format: `chatcmpl-` prefix + random alphanumeric |
| S3-07a | Implement SSE writer utility (flush-capable http.ResponseWriter) | S3-07 | BE-2 | 0.5 | P0 | TODO | S3-01a | Sets `Content-Type: text/event-stream`, `Cache-Control: no-cache` |
| S3-07b | Chunk text content into token-sized pieces (~4 chars each) | S3-07 | BE-2 | 0.5 | P0 | TODO | S3-07a | Content split into realistic-looking token chunks |
| S3-07c | Format each chunk as `ChatCompletionChunk` with delta object | S3-07 | BE-2 | 0.5 | P0 | TODO | S3-07b | JSON matches OpenAI chunk schema: `choices[0].delta.content` |
| S3-07d | Send `data: [DONE]` sentinel after all chunks | S3-07 | BE-2 | 0.25 | P0 | TODO | S3-07c | Stream ends with `data: [DONE]\n\n` |
| S3-07e | Add configurable delay between chunks (simulate latency) | S3-07 | BE-2 | 0.25 | P1 | TODO | S3-07c | Default 10ms per chunk; configurable in agent behavior.streaming |
| S3-08a | Map `EngineResponse` tool_calls to OpenAI `tool_calls` format | S3-08 | BE-3 | 0.5 | P0 | TODO | S3-06b | `choices[0].message.tool_calls` array with function name, arguments JSON |
| S3-08b | Handle `tool` role messages in request parsing | S3-08 | BE-3 | 0.5 | P0 | TODO | S3-05b | `tool` role messages with `tool_call_id` mapped correctly to engine |
| S3-08c | Support tool_calls in streaming mode (chunked function arguments) | S3-08 | BE-3 | 0.5 | P0 | TODO | S3-07c, S3-08a | Tool call arguments streamed as delta chunks |
| S3-09a | Implement token estimation: `len(text)/4` for prompt and completion | S3-09 | BE-3 | 0.25 | P1 | TODO | S3-06b | `usage.prompt_tokens`, `completion_tokens`, `total_tokens` populated |
| S3-09b | Count tokens across all messages for prompt_tokens | S3-09 | BE-3 | 0.25 | P1 | TODO | S3-09a | Sum of all message content lengths / 4 |
| S3-10a | Implement `GET /api/v1/health` endpoint | S3-10 | BE-1 | 0.25 | P0 | TODO | S3-01a | Returns `{"status": "ok", "version": "0.1.0", "uptime": "..."}` |
| S3-11a | Implement `GET /api/v1/agents` endpoint | S3-11 | BE-1 | 0.5 | P0 | TODO | S3-01a | Returns JSON array of loaded agent summaries (name, model, scenario count) |
| S3-12a | Implement `GET /api/v1/agents/:name` endpoint | S3-12 | BE-1 | 0.5 | P1 | TODO | S3-11a | Returns full agent definition as JSON; 404 for unknown agent |
| S3-13a | Implement `POST /api/v1/agents/:name/reload` endpoint | S3-13 | BE-1 | 0.5 | P1 | TODO | S3-12a | Re-reads YAML from disk, re-validates, replaces in-memory agent definition |
| S3-14a | Implement model-name-based routing (extract agent from `model` field) | S3-14 | BE-3 | 0.5 | P0 | TODO | S2-10, S3-05c | `model: "customer-support"` routes to that agent definition |
| S3-14b | Implement fallback routing (default agent if model not found) | S3-14 | BE-3 | 0.25 | P1 | TODO | S3-14a | Configurable default agent; 404 if no default and agent not found |
| S3-14c | Support path-based routing override (`/v1/agents/:name/chat/completions`) | S3-14 | BE-3 | 0.25 | P2 | TODO | S3-14a | Optional alternative to model-based routing |
| S3-15a | Implement `start` Cobra command: load agents from directory | S3-15 | BE-3 | 0.5 | P0 | TODO | S1-13, S3-01a | Loads all `*.yaml` files from `--agents` directory |
| S3-15b | Start HTTP server and print listening address | S3-15 | BE-3 | 0.25 | P0 | TODO | S3-15a | Prints `MockAgents listening on http://localhost:8080` |
| S3-15c | Handle SIGINT/SIGTERM for graceful shutdown | S3-15 | BE-3 | 0.25 | P0 | TODO | S3-15b | Ctrl+C stops server cleanly, prints shutdown message |
| S3-16a | Write test: basic non-streaming chat completion request | S3-16 | DX-1 | 0.25 | P0 | TODO | S3-06c | POST to `/v1/chat/completions`, verify JSON response structure |
| S3-16b | Write test: verify response fields (id, model, choices, usage) | S3-16 | DX-1 | 0.25 | P0 | TODO | S3-16a | All fields present and correctly typed |
| S3-16c | Write test: model routing (correct agent receives request) | S3-16 | DX-1 | 0.25 | P0 | TODO | S3-14a | Two agents loaded; model field routes to correct one |
| S3-16d | Write test: invalid request returns 400 | S3-16 | DX-1 | 0.25 | P0 | TODO | S3-05b | Missing `messages` returns OpenAI-style error |
| S3-17a | Write test: streaming response produces valid SSE events | S3-17 | DX-1 | 0.5 | P0 | TODO | S3-07d | Each line starts with `data: `, parseable as JSON |
| S3-17b | Write test: streaming chunks reassemble into full content | S3-17 | DX-1 | 0.25 | P0 | TODO | S3-17a | Concatenated `delta.content` equals expected full response |
| S3-17c | Write test: streaming ends with `[DONE]` sentinel | S3-17 | DX-1 | 0.25 | P0 | TODO | S3-17a | Last SSE event is `data: [DONE]` |
| S3-18a | Write test: non-streaming response with tool_calls | S3-18 | DX-1 | 0.5 | P0 | TODO | S3-08a | `choices[0].message.tool_calls` present with correct structure |
| S3-18b | Write test: tool role message round-trip | S3-18 | DX-1 | 0.25 | P0 | TODO | S3-08b | Send tool result, get assistant response referencing it |
| S3-18c | Write test: streaming with tool_calls | S3-18 | DX-1 | 0.25 | P0 | TODO | S3-08c | Tool call arguments streamed correctly in chunks |

### Day-by-Day Schedule

| Day | BE-1 | BE-2 | BE-3 | DX-1 |
| --- | ---- | ---- | ---- | ---- |
| D1 (May 12) | S3-01a, S3-01b | S3-05a, S3-05b | S3-14a | Test framework setup for integration tests |
| D2 (May 13) | S3-01c, S3-02a | S3-05c, S3-06a | S3-14b, S3-09a | Test framework setup (cont.) |
| D3 (May 14) | S3-03a, S3-04a, S3-10a | S3-06b, S3-06c | S3-09b, S3-08a | S3-16a, S3-16b |
| D4 (May 15) | S3-11a, S3-12a | S3-06d, S3-07a | S3-08b, S3-08c | S3-16c, S3-16d |
| D5 (May 16) | S3-13a | S3-07b, S3-07c | S3-15a, S3-15b | -- (reviews/meetings) |
| D6 (May 19) | -- (reviews/meetings) | S3-07d, S3-07e | S3-15c, S3-14c | S3-17a, S3-17b |
| D7 (May 20) | -- (reviews/meetings) | -- (reviews/meetings) | -- (reviews/meetings) | S3-17c, S3-18a |
| D8 (May 21) | Bug fixes from integration tests | Bug fixes from integration tests | Bug fixes from integration tests | S3-18b, S3-18c |
| D9 (May 22) | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes |
| D10 (May 23) | Sprint review & demo | Sprint review & demo | Sprint review & demo | Sprint review & demo |

### Sprint Risks

| # | Risk | Likelihood | Impact | Mitigation |
| - | ---- | ---------- | ------ | ---------- |
| 1 | SSE streaming implementation is tricky -- Go's `http.Flusher` interface may not work with all middleware combinations | High | High | Spike SSE on D1 with a minimal proof-of-concept; ensure Chi middleware is flush-compatible before building full implementation |
| 2 | OpenAI response format has many edge cases (null vs absent fields, integer vs float for tokens) that cause SDK compatibility issues | Medium | High | Test against real `openai` Python SDK on D6; fix format issues discovered in integration tests |
| 3 | Sprint is the most point-heavy (47 SP); may exceed velocity | Medium | Medium | S3-13 (reload) and S3-14c (path routing) are P1/P2 and can slip to Sprint 4 |

### Sprint Exit Criteria

- [ ] `mockagents start --agents ./examples/` starts server and responds on `http://localhost:8080`
- [ ] `curl -X POST http://localhost:8080/v1/chat/completions -d '{"model":"customer-support","messages":[...]}'` returns valid OpenAI JSON
- [ ] Streaming requests with `"stream": true` return valid SSE event stream ending with `[DONE]`
- [ ] Tool calls appear correctly in both streaming and non-streaming responses
- [ ] `GET /api/v1/health` returns OK; `GET /api/v1/agents` lists loaded agents
- [ ] All 12 integration test cases pass (4 non-streaming + 3 streaming + 3 tool calls + 2 error cases)
- [ ] Graceful shutdown works: Ctrl+C stops server, in-flight requests complete

### Demo Script

- **Show 1:** Start the mock server with `mockagents start --agents ./examples/`, then use `curl` to send a chat completion request and show the JSON response side-by-side with a real OpenAI response to highlight format compatibility.
- **Show 2:** Use the OpenAI Python SDK (`openai.ChatCompletion.create()`) pointing at the mock server -- demonstrate that the SDK works without modification, both streaming and non-streaming.
- **Show 3:** Hit the management API: `GET /api/v1/agents` to list agents, then `GET /api/v1/health` for status.

---

## Sprint 4: Tool Call Simulation & Anthropic Adapter (Weeks 7-8)

**Goal:** Deliver the tool call processor with match-based resolution and error injection, and a fully working Anthropic Messages adapter with streaming.

**Sprint dates:** May 26 -- June 6, 2026

**Capacity:** 4 engineers x 8 productive days = 32 person-days

**Velocity target:** ~40 story points

### Sprint Planning

| Story ID | Story                                        | Points | Owner(s)     |
| -------- | -------------------------------------------- | ------ | ------------ |
| S4-01    | Tool call processor: match-based resolution  | 4      | BE-1         |
| S4-02    | Tool call processor: default responses       | 2      | BE-1         |
| S4-03    | Tool call processor: error injection         | 3      | BE-1         |
| S4-04    | Tool call processor: parallel tool calls     | 2      | BE-1         |
| S4-05    | Anthropic adapter: request parsing           | 3      | BE-2         |
| S4-06    | Anthropic adapter: non-streaming response    | 4      | BE-2         |
| S4-07    | Anthropic adapter: SSE streaming             | 5      | BE-2         |
| S4-08    | Anthropic adapter: tool_use content blocks   | 3      | BE-3         |
| S4-09    | Anthropic adapter: tool_result handling      | 2      | BE-3         |
| S4-10    | Anthropic adapter: model routing             | 2      | BE-3         |
| S4-11    | Adapter registry: auto-detection by path     | 2      | BE-3         |
| S4-12    | Integration tests: Anthropic non-streaming   | 3      | DX-1         |
| S4-13    | Integration tests: Anthropic streaming       | 3      | DX-1         |
| S4-14    | Integration tests: Anthropic tool_use        | 3      | DX-1         |
| S4-15    | Integration tests: tool call processor       | 3      | BE-1         |
| S4-16    | Cross-adapter integration test               | 2      | DX-1         |

**Total committed:** 46 story points
**Risk buffer:** 3.2 person-days (10%) -- S4-04 (parallel tool calls) and S4-16 (cross-adapter) can slip

### Task Board

| Task ID | Task | Story | Owner | Est (days) | Priority | Status | Dependencies | Acceptance Criteria |
| ------- | ---- | ----- | ----- | ---------- | -------- | ------ | ------------ | ------------------- |
| S4-01a | Implement `ToolCallProcessor` struct with match registry | S4-01 | BE-1 | 0.5 | P0 | TODO | S1-10 | Accepts tool call, looks up match blocks from agent definition |
| S4-01b | Implement argument matching logic (exact match, partial match) | S4-01 | BE-1 | 0.5 | P0 | TODO | S4-01a | Exact JSON match and partial key-subset match supported |
| S4-01c | Implement match scoring and best-match selection | S4-01 | BE-1 | 0.5 | P0 | TODO | S4-01b | When multiple matches, most specific (most keys matched) wins |
| S4-02a | Implement default response when no match found | S4-02 | BE-1 | 0.25 | P0 | TODO | S4-01c | Returns configured `default_response` for the tool |
| S4-02b | Implement global default when no tool-specific default exists | S4-02 | BE-1 | 0.25 | P0 | TODO | S4-02a | Falls back to `{"status": "ok"}` if no default configured |
| S4-03a | Implement error injection with configurable error rate | S4-03 | BE-1 | 0.5 | P0 | TODO | S4-01c | `error_rate: 0.1` causes 10% of tool calls to return errors |
| S4-03b | Implement specific error responses (timeout, rate_limit, invalid_input) | S4-03 | BE-1 | 0.25 | P0 | TODO | S4-03a | Error type configurable per tool; returns structured error JSON |
| S4-03c | Implement deterministic error mode (seed-based) for reproducible tests | S4-03 | BE-1 | 0.25 | P1 | TODO | S4-03a | With same seed, same sequence of errors/successes |
| S4-04a | Process multiple tool calls from a single request concurrently | S4-04 | BE-1 | 0.5 | P1 | TODO | S4-01c | Uses goroutines with errgroup; results ordered by original index |
| S4-05a | Define `MessagesRequest` Go struct | S4-05 | BE-2 | 0.25 | P0 | TODO | -- | Covers messages, model, system, tools, max_tokens, stream, temperature |
| S4-05b | Implement request parsing and validation | S4-05 | BE-2 | 0.5 | P0 | TODO | S4-05a | Invalid requests return 400 with Anthropic-style error JSON |
| S4-05c | Map `MessagesRequest` to `EngineRequest` | S4-05 | BE-2 | 0.25 | P0 | TODO | S4-05b | System prompt, messages, tools all mapped correctly |
| S4-06a | Define `MessagesResponse` Go struct with content blocks | S4-06 | BE-2 | 0.25 | P0 | TODO | -- | Matches Anthropic API response schema: content array, stop_reason, usage |
| S4-06b | Build text content blocks from `EngineResponse` | S4-06 | BE-2 | 0.5 | P0 | TODO | S4-06a, S4-05c | `content: [{type: "text", text: "..."}]` |
| S4-06c | Handle stop reasons (end_turn, max_tokens, tool_use) | S4-06 | BE-2 | 0.25 | P0 | TODO | S4-06b | `stop_reason` field set correctly |
| S4-06d | Populate usage (input_tokens, output_tokens) | S4-06 | BE-2 | 0.25 | P1 | TODO | S4-06b | Token estimation reused from S3-09 logic |
| S4-06e | Generate unique message ID (`msg_xxxx` format) | S4-06 | BE-2 | 0.25 | P1 | TODO | S4-06b | Anthropic-style ID format |
| S4-07a | Implement SSE writer for Anthropic event format | S4-07 | BE-2 | 0.5 | P0 | TODO | S3-07a | `event: message_start\ndata: {...}\n\n` format |
| S4-07b | Emit `message_start` event with message metadata | S4-07 | BE-2 | 0.25 | P0 | TODO | S4-07a | Contains id, type, role, model, usage |
| S4-07c | Emit `content_block_start` and `content_block_delta` events for text | S4-07 | BE-2 | 0.5 | P0 | TODO | S4-07b | Text chunked into delta events with `text_delta` type |
| S4-07d | Emit `content_block_stop`, `message_delta`, `message_stop` events | S4-07 | BE-2 | 0.25 | P0 | TODO | S4-07c | Correct event sequence: start -> deltas -> stop per block, then message_delta + message_stop |
| S4-07e | Add configurable inter-event delay | S4-07 | BE-2 | 0.25 | P1 | TODO | S4-07c | Simulates real API latency between events |
| S4-07f | Support streaming tool_use content blocks | S4-07 | BE-2 | 0.25 | P0 | TODO | S4-07c, S4-08a | Tool use blocks streamed with `input_json_delta` events |
| S4-08a | Build `tool_use` content blocks from `EngineResponse` tool calls | S4-08 | BE-3 | 0.5 | P0 | TODO | S4-06b, S4-01c | `content: [{type: "tool_use", id: "toolu_xxx", name: "...", input: {...}}]` |
| S4-08b | Generate unique tool use IDs (`toolu_xxxx` format) | S4-08 | BE-3 | 0.25 | P0 | TODO | S4-08a | Anthropic-style tool_use ID |
| S4-08c | Handle mixed text + tool_use content blocks in single response | S4-08 | BE-3 | 0.25 | P0 | TODO | S4-08a | Response can contain both text and tool_use blocks |
| S4-09a | Parse `tool_result` content blocks from user messages | S4-09 | BE-3 | 0.25 | P0 | TODO | S4-05b | `{type: "tool_result", tool_use_id: "...", content: "..."}` mapped to engine |
| S4-09b | Map tool results to engine state for response generation | S4-09 | BE-3 | 0.25 | P0 | TODO | S4-09a | Engine can reference tool results in subsequent response generation |
| S4-10a | Route requests to agent based on `model` field in Anthropic request | S4-10 | BE-3 | 0.25 | P0 | TODO | S3-14a | Same routing logic as OpenAI, different request structure |
| S4-10b | Support `claude-*` model name patterns | S4-10 | BE-3 | 0.25 | P1 | TODO | S4-10a | `model: "claude-3-sonnet"` can map to agent via config |
| S4-11a | Implement adapter registry with path-based detection | S4-11 | BE-3 | 0.25 | P0 | TODO | S3-01a | `/v1/chat/completions` -> OpenAI adapter, `/v1/messages` -> Anthropic adapter |
| S4-11b | Register both adapters at server startup | S4-11 | BE-3 | 0.25 | P0 | TODO | S4-11a | Both paths active simultaneously on same port |
| S4-12a | Write test: basic non-streaming Anthropic messages request | S4-12 | DX-1 | 0.25 | P0 | TODO | S4-06c | POST to `/v1/messages`, verify response structure |
| S4-12b | Write test: verify Anthropic response fields (id, content, stop_reason, usage) | S4-12 | DX-1 | 0.25 | P0 | TODO | S4-12a | All fields present, correct types |
| S4-12c | Write test: system prompt handling | S4-12 | DX-1 | 0.25 | P0 | TODO | S4-05c | System prompt passed correctly to engine |
| S4-12d | Write test: invalid request returns 400 with Anthropic error format | S4-12 | DX-1 | 0.25 | P0 | TODO | S4-05b | Error JSON matches Anthropic API error schema |
| S4-13a | Write test: streaming produces correct event sequence | S4-13 | DX-1 | 0.5 | P0 | TODO | S4-07d | message_start -> content_block_start -> deltas -> content_block_stop -> message_delta -> message_stop |
| S4-13b | Write test: streaming text reassembles correctly | S4-13 | DX-1 | 0.25 | P0 | TODO | S4-13a | Concatenated text_delta values equal expected response |
| S4-13c | Write test: streaming with multiple content blocks | S4-13 | DX-1 | 0.25 | P0 | TODO | S4-13a | Multiple content blocks each get start/delta/stop events |
| S4-14a | Write test: tool_use blocks in non-streaming response | S4-14 | DX-1 | 0.25 | P0 | TODO | S4-08c | Verify tool_use content block structure |
| S4-14b | Write test: tool_result round-trip (send tool result, get response) | S4-14 | DX-1 | 0.5 | P0 | TODO | S4-09b | Multi-turn: assistant sends tool_use, user sends tool_result, assistant responds |
| S4-14c | Write test: streaming with tool_use blocks | S4-14 | DX-1 | 0.25 | P0 | TODO | S4-07f | Tool use blocks streamed correctly |
| S4-15a | Write test: tool call match resolution (exact match, partial, no match) | S4-15 | BE-1 | 0.5 | P0 | TODO | S4-02b | Each match type returns expected result |
| S4-15b | Write test: error injection at configured rate | S4-15 | BE-1 | 0.25 | P0 | TODO | S4-03c | With seed, verify error distribution matches configured rate |
| S4-15c | Write test: parallel tool call processing | S4-15 | BE-1 | 0.25 | P0 | TODO | S4-04a | Multiple tool calls processed and results returned in order |
| S4-16a | Write cross-adapter test: same agent YAML serves both OpenAI and Anthropic | S4-16 | DX-1 | 0.5 | P1 | TODO | S4-11b | Same agent, same scenario, verify equivalent responses from both endpoints |

### Day-by-Day Schedule

| Day | BE-1 | BE-2 | BE-3 | DX-1 |
| --- | ---- | ---- | ---- | ---- |
| D1 (May 26) | S4-01a, S4-01b | S4-05a, S4-05b | S4-11a, S4-11b | Test framework setup for Anthropic tests |
| D2 (May 27) | S4-01c, S4-02a | S4-05c, S4-06a | S4-10a, S4-10b | S4-12a, S4-12b |
| D3 (May 28) | S4-02b, S4-03a | S4-06b, S4-06c | S4-08a, S4-08b | S4-12c, S4-12d |
| D4 (May 29) | S4-03b, S4-03c | S4-06d, S4-06e, S4-07a | S4-08c, S4-09a | S4-13a |
| D5 (May 30) | S4-04a | S4-07b, S4-07c | S4-09b | S4-13b, S4-13c |
| D6 (Jun 2) | S4-15a | S4-07d, S4-07e | -- (reviews/meetings) | S4-14a, S4-14b |
| D7 (Jun 3) | S4-15b, S4-15c | S4-07f | -- (reviews/meetings) | S4-14c, S4-16a |
| D8 (Jun 4) | -- (reviews/meetings) | -- (reviews/meetings) | Bug fixes from integration tests | Bug fixes from integration tests |
| D9 (Jun 5) | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes |
| D10 (Jun 6) | Sprint review & demo | Sprint review & demo | Sprint review & demo | Sprint review & demo |

### Sprint Risks

| # | Risk | Likelihood | Impact | Mitigation |
| - | ---- | ---------- | ------ | ---------- |
| 1 | Anthropic streaming event format is complex (6 event types with specific sequencing); implementation may have subtle ordering bugs | High | High | DX-1 writes Anthropic streaming tests early (D4-D5) so BE-2 can validate against them; reference Anthropic SDK source for expected event sequences |
| 2 | Tool call processor match-based resolution has ambiguous edge cases (what if two matches tie?) | Medium | Medium | Define clear tie-breaking rules in design doc on D1: most specific match wins, then first-defined wins |
| 3 | High story point count (46 SP) risks burnout; Sprint 3 was also high | Medium | Medium | DX-1 can absorb some BE bug-fix work; S4-04 (parallel) and S4-16 (cross-adapter) are explicitly deferrable |

### Sprint Exit Criteria

- [ ] Tool call processor resolves matches correctly: exact, partial, default, and no-match all tested
- [ ] Error injection works at configured rates; deterministic mode produces reproducible results
- [ ] `POST /v1/messages` returns valid Anthropic-format response (non-streaming)
- [ ] Anthropic streaming produces correct 6-event sequence: message_start through message_stop
- [ ] `tool_use` content blocks appear correctly in both streaming and non-streaming
- [ ] `tool_result` round-trip works: send tool result, receive assistant response
- [ ] Same agent definition serves both `/v1/chat/completions` and `/v1/messages` correctly
- [ ] All 16 integration test cases pass (4 Anthropic non-streaming + 3 streaming + 3 tool_use + 3 tool processor + 1 cross-adapter + 2 error cases)

### Demo Script

- **Show 1:** Use the Anthropic Python SDK (`anthropic.Anthropic().messages.create()`) pointing at the mock server -- send a message, receive a response, demonstrate streaming in a terminal.
- **Show 2:** Demonstrate tool call simulation: send a request that triggers a tool call, show the tool_use block, then send a tool_result and receive the final response.
- **Show 3:** Side-by-side: same agent definition queried via OpenAI SDK and Anthropic SDK, showing equivalent results from both adapters.

---

## Sprint 5: Python SDK & CLI Polish (Weeks 9-10)

**Goal:** Ship the Python SDK with MockAgentServer, scenario assertions, and pytest integration. Polish CLI with init command, SQLite logging, Docker image.

**Sprint dates:** June 9 -- June 20, 2026

**Capacity:** 4 engineers x 8 productive days = 32 person-days

**Velocity target:** ~40 story points

### Sprint Planning

| Story ID | Story                                        | Points | Owner(s)     |
| -------- | -------------------------------------------- | ------ | ------------ |
| S5-01    | Python SDK: `MockAgentClient`                | 5      | DX-1         |
| S5-02    | Python SDK: `MockAgentServer`                | 5      | DX-1         |
| S5-03    | Python SDK: `Scenario` class                 | 3      | DX-1         |
| S5-04    | Python SDK: `expect()` assertion helpers     | 4      | DX-1         |
| S5-05    | Python SDK: `run_scenario()` method          | 3      | DX-1         |
| S5-06    | Python SDK: pytest integration               | 2      | DX-1         |
| S5-07    | `mockagents init` CLI command                | 4      | BE-1         |
| S5-08    | `mockagents init` templates                  | 2      | BE-1         |
| S5-09    | SQLite interaction logging                   | 5      | BE-2         |
| S5-10    | SQLite schema and migrations                 | 2      | BE-2         |
| S5-11    | `mockagents logs` CLI command                | 3      | BE-2         |
| S5-12    | CLI: `--port`, `--host`, `--log-level` flags | 2      | BE-3         |
| S5-13    | CLI: colored output and progress indicators  | 2      | BE-3         |
| S5-14    | Docker multi-stage build                     | 3      | BE-3         |
| S5-15    | `docker-compose.yml` for local dev           | 2      | BE-3         |
| S5-16    | Python SDK tests                             | 3      | DX-1         |
| S5-17    | Publish Python SDK to TestPyPI               | 2      | DX-1         |

**Total committed:** 52 story points (DX-1 is heavily loaded at ~27 SP; see risk mitigation)
**Risk buffer:** 3.2 person-days (10%) -- S5-17 (TestPyPI) and S5-15 (docker-compose) can slip

**Note:** DX-1 has the heaviest load this sprint. BE-3 finishes Docker work by D6 and picks up SDK testing support.

### Task Board

| Task ID | Task | Story | Owner | Est (days) | Priority | Status | Dependencies | Acceptance Criteria |
| ------- | ---- | ----- | ----- | ---------- | -------- | ------ | ------------ | ------------------- |
| S5-01a | Implement `MockAgentClient.__init__` with base URL and protocol selection | S5-01 | DX-1 | 0.25 | P0 | TODO | -- | `client = MockAgentClient(base_url="http://localhost:8080", protocol="openai")` |
| S5-01b | Implement `client.chat()` for OpenAI-mode requests | S5-01 | DX-1 | 0.5 | P0 | TODO | S5-01a | Sends POST to `/v1/chat/completions`, returns parsed response |
| S5-01c | Implement `client.messages()` for Anthropic-mode requests | S5-01 | DX-1 | 0.5 | P0 | TODO | S5-01a | Sends POST to `/v1/messages`, returns parsed response |
| S5-01d | Implement streaming support (yields chunks) | S5-01 | DX-1 | 0.5 | P0 | TODO | S5-01b, S5-01c | `for chunk in client.chat(stream=True): ...` |
| S5-01e | Add type hints and docstrings | S5-01 | DX-1 | 0.25 | P1 | TODO | S5-01d | Full type annotations; mypy passes |
| S5-02a | Implement `MockAgentServer.__init__` with binary path and agent dir | S5-02 | DX-1 | 0.25 | P0 | TODO | -- | Locates `mockagents` binary (PATH or explicit) |
| S5-02b | Implement `start()` / `stop()` subprocess management | S5-02 | DX-1 | 0.5 | P0 | TODO | S5-02a | Starts process, waits for health check, stops cleanly |
| S5-02c | Implement context manager (`__enter__` / `__exit__`) | S5-02 | DX-1 | 0.25 | P0 | TODO | S5-02b | `with MockAgentServer(...) as server: ...` starts/stops automatically |
| S5-02d | Implement health check polling (wait for server ready) | S5-02 | DX-1 | 0.25 | P0 | TODO | S5-02b | Polls `/api/v1/health` with backoff; raises TimeoutError after 10s |
| S5-02e | Capture and expose server stdout/stderr for debugging | S5-02 | DX-1 | 0.25 | P0 | TODO | S5-02b | `server.logs` property returns captured output |
| S5-02f | Support `from_config()` class method | S5-02 | DX-1 | 0.25 | P1 | TODO | S5-02b | `MockAgentServer.from_config("agent.yaml")` is shorthand |
| S5-03a | Implement `Scenario` class with step definitions | S5-03 | DX-1 | 0.5 | P0 | TODO | S5-01d | `scenario.add_step(role="user", content="...")` |
| S5-03b | Support multi-turn conversation scenarios | S5-03 | DX-1 | 0.25 | P0 | TODO | S5-03a | Steps maintain conversation history |
| S5-03c | Support tool call / tool result steps | S5-03 | DX-1 | 0.25 | P0 | TODO | S5-03a | Scenario can include expected tool calls and provide tool results |
| S5-04a | Implement `expect(response)` wrapper returning `Expectation` object | S5-04 | DX-1 | 0.25 | P0 | TODO | S5-01b | `expect(result)` returns chainable assertion object |
| S5-04b | Implement `to_have_response_containing(text)` | S5-04 | DX-1 | 0.25 | P0 | TODO | S5-04a | Asserts response content contains substring |
| S5-04c | Implement `to_have_tool_call(name, args)` | S5-04 | DX-1 | 0.25 | P0 | TODO | S5-04a | Asserts response contains tool call with matching name and args |
| S5-04d | Implement `to_have_tool_error()` | S5-04 | DX-1 | 0.25 | P1 | TODO | S5-04a | Asserts tool call resulted in error |
| S5-04e | Implement `to_be_less_than(duration_ms)` for latency checks | S5-04 | DX-1 | 0.25 | P1 | TODO | S5-04a | Asserts response time under threshold |
| S5-04f | Produce clear assertion failure messages | S5-04 | DX-1 | 0.25 | P0 | TODO | S5-04b | `AssertionError: Expected response to contain "order" but got "Hello..."` |
| S5-05a | Implement `run_scenario()` method on `MockAgentClient` | S5-05 | DX-1 | 0.5 | P0 | TODO | S5-03c, S5-01d | Executes steps sequentially, collects results |
| S5-05b | Return `ScenarioResult` with all step results and timing | S5-05 | DX-1 | 0.25 | P0 | TODO | S5-05a | Result object has `.steps`, `.total_duration`, `.passed` |
| S5-05c | Handle step failures gracefully (continue vs stop modes) | S5-05 | DX-1 | 0.25 | P1 | TODO | S5-05a | Default: stop on first failure; `continue_on_failure=True` runs all |
| S5-06a | Create `@pytest.fixture` for `MockAgentServer` lifecycle | S5-06 | DX-1 | 0.25 | P0 | TODO | S5-02c | `def test_foo(mock_server): ...` auto-starts/stops server |
| S5-06b | Create `conftest.py` example with fixture configuration | S5-06 | DX-1 | 0.25 | P1 | TODO | S5-06a | Example fixture with custom port and agent directory |
| S5-07a | Implement `init` Cobra command: create project directory | S5-07 | BE-1 | 0.5 | P0 | TODO | -- | `mockagents init my-project` creates `my-project/` directory |
| S5-07b | Generate example agent YAML in project directory | S5-07 | BE-1 | 0.5 | P0 | TODO | S5-07a, S5-08a | `my-project/agents/example.yaml` with annotated example |
| S5-07c | Generate example test file (Python or Go) | S5-07 | BE-1 | 0.25 | P1 | TODO | S5-07b | `my-project/tests/test_example.py` with basic test |
| S5-07d | Generate project Makefile or README | S5-07 | BE-1 | 0.25 | P1 | TODO | S5-07b | Makefile with `start`, `test`, `validate` targets |
| S5-08a | Create embedded Go templates for scaffolded files | S5-08 | BE-1 | 0.25 | P0 | TODO | -- | Templates use `embed` package; parameterized with project name |
| S5-08b | Add template for `requirements.txt` / `pyproject.toml` | S5-08 | BE-1 | 0.25 | P1 | TODO | S5-08a | Generated project includes Python dependency file |
| S5-09a | Design SQLite schema for interactions table | S5-09 | BE-2 | 0.25 | P0 | TODO | -- | Columns: id, timestamp, agent_name, protocol, request_body, response_body, latency_ms, scenario_matched, status_code |
| S5-09b | Implement SQLite database initialization and connection management | S5-09 | BE-2 | 0.5 | P0 | TODO | S5-09a | Auto-creates DB file on first start; connection pooling |
| S5-09c | Implement logging middleware that writes to SQLite | S5-09 | BE-2 | 0.5 | P0 | TODO | S5-09b | Every request/response pair logged asynchronously |
| S5-09d | Add batch write buffer (don't block request on DB write) | S5-09 | BE-2 | 0.5 | P0 | TODO | S5-09c | Buffer up to 100 entries; flush every 1s or when full |
| S5-09e | Add log rotation / max DB size limit | S5-09 | BE-2 | 0.25 | P2 | TODO | S5-09b | Configurable max size; oldest entries pruned |
| S5-10a | Implement schema migration on startup | S5-10 | BE-2 | 0.25 | P0 | TODO | S5-09a | Checks schema version, applies migrations if needed |
| S5-10b | Add migration for v1 schema | S5-10 | BE-2 | 0.25 | P0 | TODO | S5-10a | Creates interactions table if not exists |
| S5-11a | Implement `logs` Cobra command with default output (last 20 entries) | S5-11 | BE-2 | 0.5 | P0 | TODO | S5-09b | `mockagents logs` prints recent interactions in table format |
| S5-11b | Add `--agent` filter flag | S5-11 | BE-2 | 0.25 | P0 | TODO | S5-11a | `mockagents logs --agent customer-support` filters by agent |
| S5-11c | Add `--since` and `--limit` flags | S5-11 | BE-2 | 0.25 | P1 | TODO | S5-11a | `mockagents logs --since 1h --limit 50` |
| S5-12a | Add `--port` and `--host` flags to `start` command | S5-12 | BE-3 | 0.25 | P0 | TODO | S3-15 | `mockagents start --port 9090 --host 0.0.0.0` |
| S5-12b | Add `--log-level` flag (debug, info, warn, error) | S5-12 | BE-3 | 0.25 | P0 | TODO | S3-15 | `mockagents start --log-level debug` increases verbosity |
| S5-13a | Add colored output for CLI (success=green, error=red, warn=yellow) | S5-13 | BE-3 | 0.25 | P1 | TODO | -- | Terminal output uses ANSI colors; respects NO_COLOR env var |
| S5-13b | Add startup banner with ASCII art and listening info | S5-13 | BE-3 | 0.25 | P2 | TODO | S5-13a | Banner shows version, port, loaded agents count |
| S5-14a | Create multi-stage Dockerfile (builder + runtime) | S5-14 | BE-3 | 0.5 | P0 | TODO | -- | Stage 1: Go build. Stage 2: Alpine + binary. Image < 30MB |
| S5-14b | Add health check to Dockerfile | S5-14 | BE-3 | 0.25 | P0 | TODO | S5-14a | `HEALTHCHECK` instruction hitting `/api/v1/health` |
| S5-14c | Test Docker image: build and run with example agents | S5-14 | BE-3 | 0.25 | P0 | TODO | S5-14b | `docker run -v ./examples:/agents mockagents/mockagents` serves responses |
| S5-15a | Create `docker-compose.yml` with mockagents service | S5-15 | BE-3 | 0.25 | P1 | TODO | S5-14c | Volume mount for agents directory, port mapping |
| S5-15b | Add example Python test service to docker-compose | S5-15 | BE-3 | 0.25 | P2 | TODO | S5-15a | Shows how to run tests against mock server in Docker |
| S5-16a | Write unit tests for `MockAgentClient` (mock HTTP responses) | S5-16 | DX-1 | 0.25 | P0 | TODO | S5-01d | Client methods tested with httpx mock |
| S5-16b | Write unit tests for `MockAgentServer` lifecycle | S5-16 | DX-1 | 0.25 | P0 | TODO | S5-02e | Start/stop/context manager tested |
| S5-16c | Write unit tests for `expect()` assertion helpers | S5-16 | DX-1 | 0.25 | P0 | TODO | S5-04f | Each assertion tested for pass and fail cases |
| S5-16d | Write integration test for full scenario execution | S5-16 | DX-1 | 0.25 | P0 | TODO | S5-05c | End-to-end: start server, run scenario, verify assertions |
| S5-17a | Configure Poetry for PyPI publishing | S5-17 | DX-1 | 0.25 | P1 | TODO | S5-16d | `pyproject.toml` has correct metadata for PyPI |
| S5-17b | Create CI job for TestPyPI publish on tag | S5-17 | DX-1 | 0.25 | P1 | TODO | S5-17a | GitHub Actions workflow publishes to TestPyPI on `v*-rc*` tags |

### Day-by-Day Schedule

| Day | BE-1 | BE-2 | BE-3 | DX-1 |
| --- | ---- | ---- | ---- | ---- |
| D1 (Jun 9) | S5-08a, S5-07a | S5-09a, S5-10a, S5-10b | S5-12a, S5-12b | S5-01a, S5-01b |
| D2 (Jun 10) | S5-07b | S5-09b | S5-13a, S5-13b | S5-01c, S5-01d |
| D3 (Jun 11) | S5-07c, S5-07d | S5-09c | S5-14a | S5-01e, S5-02a |
| D4 (Jun 12) | S5-08b | S5-09d, S5-09e | S5-14b, S5-14c | S5-02b, S5-02c |
| D5 (Jun 13) | -- (reviews/meetings) | S5-11a | S5-15a, S5-15b | S5-02d, S5-02e, S5-02f |
| D6 (Jun 16) | -- (reviews/meetings) | S5-11b, S5-11c | -- (reviews/meetings) | S5-03a, S5-03b, S5-03c |
| D7 (Jun 17) | Review PRs; help DX-1 | -- (reviews/meetings) | Review PRs; help DX-1 | S5-04a, S5-04b, S5-04c |
| D8 (Jun 18) | Review PRs; help DX-1 | Review PRs; help DX-1 | Review PRs; help DX-1 | S5-04d, S5-04e, S5-04f, S5-05a |
| D9 (Jun 19) | Buffer / bug fixes | Buffer / bug fixes | Buffer / bug fixes | S5-05b, S5-05c, S5-06a, S5-06b, S5-16a-d, S5-17a-b |
| D10 (Jun 20) | Sprint review & demo | Sprint review & demo | Sprint review & demo | Sprint review & demo |

### Sprint Risks

| # | Risk | Likelihood | Impact | Mitigation |
| - | ---- | ---------- | ------ | ---------- |
| 1 | DX-1 is overloaded (27 SP of 52 total) and may not complete all SDK features | High | High | BE-1 and BE-3 finish their work by D6 and shift to supporting DX-1 with PR reviews and bug fixes. S5-17 (TestPyPI) is explicitly deferrable to Sprint 6 |
| 2 | SQLite async write buffer may lose data on crash or SIGKILL | Medium | Medium | Implement flush-on-shutdown in `mockagents start` graceful shutdown path; document that SIGKILL may lose last ~1s of logs |
| 3 | Docker build may fail on ARM64 (M-series Mac) if CGO is required for SQLite | Medium | High | Use `modernc.org/sqlite` (pure Go SQLite) instead of `mattn/go-sqlite3` to avoid CGO requirement |

### Sprint Exit Criteria

- [ ] `MockAgentServer.from_config("agent.yaml")` starts the Go binary as a subprocess and passes health check
- [ ] `MockAgentClient.chat()` and `.messages()` return parsed responses
- [ ] `expect(result).to_have_tool_call("lookup_order", {"order_id": "ORD-12345"})` passes and fails correctly
- [ ] `run_scenario()` executes multi-turn conversation and collects results
- [ ] `@pytest.fixture` starts/stops server for test lifecycle
- [ ] `mockagents init my-project` scaffolds a working project with agent YAML and test file
- [ ] SQLite logs written for every request; `mockagents logs` displays them
- [ ] Docker image builds and runs: `docker run` serves mock responses
- [ ] Python SDK tests pass: 20+ test cases

### Demo Script

- **Show 1:** Full pytest demo: `pytest tests/test_customer_support.py -v` -- show a test file that uses `@pytest.fixture` to start MockAgentServer, runs a 3-step scenario, and uses `expect()` assertions. All tests pass with green output.
- **Show 2:** Run `mockagents init demo-project && cd demo-project && mockagents start --agents ./agents/` -- show the scaffolded project and immediately run the mock server.
- **Show 3:** Run `mockagents logs --agent customer-support` after a few requests -- show the SQLite interaction log with timestamps, latency, and matched scenarios.

---

## Sprint 6: Hardening & Release (Weeks 11-12)

**Goal:** Harden the system with E2E tests and performance benchmarks, write comprehensive documentation, and ship the public alpha release.

**Sprint dates:** June 23 -- July 4, 2026

**Capacity:** 4 engineers x 8 productive days = 32 person-days

**Velocity target:** ~40 story points

### Sprint Planning

| Story ID | Story                                        | Points | Owner(s)     |
| -------- | -------------------------------------------- | ------ | ------------ |
| S6-01    | End-to-end test suite                        | 5      | BE-1         |
| S6-02    | E2E: OpenAI SDK compatibility test           | 3      | BE-2         |
| S6-03    | E2E: Anthropic SDK compatibility test        | 3      | BE-2         |
| S6-04    | Performance benchmarking                     | 4      | BE-3         |
| S6-05    | Performance: identify and fix bottlenecks    | 4      | BE-3         |
| S6-06    | Security review                              | 2      | BE-1         |
| S6-07    | Error handling audit                         | 2      | BE-1         |
| S6-08    | Documentation site setup (MkDocs)            | 3      | DX-1         |
| S6-09    | Quickstart guide                             | 3      | DX-1         |
| S6-10    | API reference docs                           | 3      | DX-1         |
| S6-11    | Python SDK docs                              | 2      | DX-1         |
| S6-12    | Agent definition reference                   | 2      | DX-1         |
| S6-13    | README.md                                    | 2      | DX-1         |
| S6-14    | Example agents directory                     | 2      | DX-1         |
| S6-15    | CONTRIBUTING.md and LICENSE                  | 1      | DX-1         |
| S6-16    | PyPI publish pipeline                        | 2      | DX-1         |
| S6-17    | GitHub release pipeline                      | 2      | BE-3         |
| S6-18    | Docker Hub publish pipeline                  | 2      | BE-3         |
| S6-19    | CHANGELOG.md                                 | 1      | DX-1         |
| S6-20    | Final regression pass                        | 3      | All          |

**Total committed:** 51 story points (elevated; documentation tasks are smaller-grained)
**Risk buffer:** 3.2 person-days (10%) -- S6-14 (examples), S6-15 (CONTRIBUTING), S6-19 (CHANGELOG) can be done post-sprint

**Carry-over from Sprint 5:** S5-17 (TestPyPI publish) if not completed

### Task Board

| Task ID | Task | Story | Owner | Est (days) | Priority | Status | Dependencies | Acceptance Criteria |
| ------- | ---- | ----- | ----- | ---------- | -------- | ------ | ------------ | ------------------- |
| S6-01a | Write E2E test: `mockagents init` -> validate -> start -> test -> logs | S6-01 | BE-1 | 1.0 | P0 | TODO | S5-07 | Full lifecycle works end-to-end in CI |
| S6-01b | Write E2E test: multi-agent setup with routing | S6-01 | BE-1 | 0.5 | P0 | TODO | S6-01a | 3 agents loaded; model routing works correctly |
| S6-01c | Write E2E test: Python SDK scenario execution | S6-01 | BE-1 | 0.5 | P0 | TODO | S6-01a | Python test using MockAgentServer fixture passes in CI |
| S6-02a | Test with `openai` Python package v1.x: basic chat completion | S6-02 | BE-2 | 0.25 | P0 | TODO | S3-06 | `openai.ChatCompletion.create()` succeeds |
| S6-02b | Test with `openai` v1.x: streaming, tool calls, function calling | S6-02 | BE-2 | 0.5 | P0 | TODO | S6-02a | All major OpenAI SDK features work against mock |
| S6-02c | Test with `openai` v1.x: error handling (invalid model, bad request) | S6-02 | BE-2 | 0.25 | P1 | TODO | S6-02a | SDK raises expected exceptions for error responses |
| S6-03a | Test with `anthropic` Python package: basic messages | S6-03 | BE-2 | 0.25 | P0 | TODO | S4-06 | `anthropic.Anthropic().messages.create()` succeeds |
| S6-03b | Test with `anthropic`: streaming, tool_use, tool_result | S6-03 | BE-2 | 0.5 | P0 | TODO | S6-03a | All major Anthropic SDK features work against mock |
| S6-03c | Test with `anthropic`: error handling | S6-03 | BE-2 | 0.25 | P1 | TODO | S6-03a | SDK raises expected exceptions for error responses |
| S6-04a | Go `testing.B` harness wired at `internal/engine/benchmark_test.go` | S6-04 | BE-3 | 0.5 | P0 | DONE | -- | 12 benchmarks covering engine hot path |
| S6-04b | Non-streaming single-agent latency benchmarks | S6-04 | BE-3 | 0.5 | P0 | DONE | S6-04a | Static p50 ≈ 595 ns/op, template 1617 ns/op; see `docs/benchmarks/latest.md` |
| S6-04c | Per-component benchmarks (matcher/generator/tools/registry) | S6-04 | BE-3 | 0.25 | P0 | DONE | S6-04a | ContentContains 76 ns/op, registry 14 ns/op |
| S6-04d | Reproducible CI-friendly report | S6-04 | BE-3 | 0.25 | P1 | DONE | S6-04a | `make bench-report` emits `docs/benchmarks/latest.{json,md}` |
| S6-05a | pprof CPU profile of static-response pipeline | S6-05 | BE-3 | 0.5 | P0 | DONE | S6-04b | Top hotspots in `docs/benchmarks/README.md` Release 2026-04-14 notes |
| S6-05b | Investigate bottlenecks | S6-05 | BE-3 | 0.5 | P0 | DONE | S6-05a | GC-bound profile; all benchmarks inside target envelope, no regressions to fix |
| S6-05c | Allocation analysis | S6-05 | BE-3 | 0.25 | P1 | DONE | S6-05a | `B/op` + `allocs/op` columns in `latest.md`; Session.AppendUserMessage flagged as next lever |
| S6-05d | Archive results for future diffs | S6-05 | BE-3 | 0.25 | P0 | DONE | S6-05b | `docs/benchmarks/latest.json` schema v1 committed |
| S6-06a | Audit input validation on all HTTP endpoints | S6-06 | BE-1 | 0.25 | P0 | TODO | -- | All user input validated; no injection vectors |
| S6-06b | Check for path traversal in agent file loading | S6-06 | BE-1 | 0.25 | P0 | TODO | -- | Agent directory is sandboxed; `../` in paths rejected |
| S6-07a | Audit all error returns for context wrapping | S6-07 | BE-1 | 0.25 | P0 | TODO | -- | All errors wrapped with `fmt.Errorf("context: %w", err)` |
| S6-07b | Verify no panics in production code paths (use recover where needed) | S6-07 | BE-1 | 0.25 | P0 | TODO | -- | `go vet` clean; recovery middleware catches unexpected panics |
| S6-08a | Set up MkDocs with Material theme | S6-08 | DX-1 | 0.5 | P0 | TODO | -- | `mkdocs serve` runs locally; theme configured |
| S6-08b | Configure GitHub Pages deployment in CI | S6-08 | DX-1 | 0.25 | P0 | TODO | S6-08a | `mkdocs gh-deploy` runs on main branch push |
| S6-08c | Set up navigation structure (Getting Started, Guides, Reference, SDK) | S6-08 | DX-1 | 0.25 | P0 | TODO | S6-08a | `mkdocs.yml` nav section covers all planned pages |
| S6-09a | Write quickstart guide: installation methods (binary, go install, docker) | S6-09 | DX-1 | 0.25 | P0 | TODO | S6-08a | Three install methods documented with copy-paste commands |
| S6-09b | Write quickstart guide: create first agent and start server | S6-09 | DX-1 | 0.25 | P0 | TODO | S6-09a | 5-minute path from install to running mock server |
| S6-09c | Write quickstart guide: write first test with Python SDK | S6-09 | DX-1 | 0.25 | P0 | TODO | S6-09b | Complete example: install SDK, write test, run with pytest |
| S6-09d | Test quickstart guide end-to-end (fresh machine walkthrough) | S6-09 | DX-1 | 0.25 | P0 | TODO | S6-09c | All commands in guide work as documented |
| S6-10a | Document all CLI commands (validate, start, init, logs) | S6-10 | DX-1 | 0.25 | P0 | TODO | S6-08a | Each command documented with usage, flags, examples |
| S6-10b | Document management API endpoints | S6-10 | DX-1 | 0.25 | P0 | TODO | S6-08a | health, agents, agent detail, reload -- with curl examples |
| S6-10c | Document YAML schema with examples | S6-10 | DX-1 | 0.25 | P0 | TODO | S6-08a | Every YAML field documented with type, default, example |
| S6-10d | Add request/response examples for OpenAI and Anthropic endpoints | S6-10 | DX-1 | 0.25 | P1 | TODO | S6-10b | Full JSON request/response pairs |
| S6-11a | Write Python SDK usage guide with code examples | S6-11 | DX-1 | 0.25 | P0 | TODO | S6-08a | Client, Server, Scenario, expect() all documented |
| S6-11b | Generate Python SDK API reference (autodoc or manual) | S6-11 | DX-1 | 0.25 | P1 | TODO | S6-11a | All public classes and methods documented |
| S6-12a | Write full agent definition reference (every field, every option) | S6-12 | DX-1 | 0.25 | P0 | TODO | S6-08a | Reference covers metadata, spec, tools, scenarios, behavior, chaos, streaming |
| S6-12b | Add annotated examples for common patterns | S6-12 | DX-1 | 0.25 | P1 | TODO | S6-12a | 4-5 annotated YAML snippets showing common patterns |
| S6-13a | Write README.md with badges, overview, install, quickstart link | S6-13 | DX-1 | 0.25 | P0 | TODO | S6-09d | README is the landing page for the GitHub repo |
| S6-13b | Add feature list, architecture diagram link, contributing link | S6-13 | DX-1 | 0.25 | P1 | TODO | S6-13a | Comprehensive but concise README |
| S6-14a | Create customer-support agent example (multi-scenario, tool calls) | S6-14 | DX-1 | 0.15 | P1 | TODO | -- | Fully annotated, validates against schema |
| S6-14b | Create code-assistant agent example (template responses) | S6-14 | DX-1 | 0.1 | P1 | TODO | -- | Demonstrates template generator features |
| S6-14c | Create RAG agent example (multi-turn with state) | S6-14 | DX-1 | 0.15 | P2 | TODO | -- | Demonstrates state store usage |
| S6-14d | Create multi-tool agent example (parallel tool calls, error injection) | S6-14 | DX-1 | 0.1 | P2 | TODO | -- | Demonstrates advanced tool call features |
| S6-15a | Add Apache 2.0 LICENSE file | S6-15 | DX-1 | 0.1 | P0 | TODO | -- | Standard Apache 2.0 text |
| S6-15b | Write CONTRIBUTING.md with dev setup, PR process, code style | S6-15 | DX-1 | 0.15 | P1 | TODO | -- | Clear instructions for new contributors |
| S6-16a | Create GitHub Actions workflow for PyPI publish on release tag | S6-16 | DX-1 | 0.25 | P0 | TODO | S5-17 | `v*` tags trigger publish to PyPI |
| S6-16b | Test PyPI publish with `v0.1.0-rc1` tag | S6-16 | DX-1 | 0.25 | P0 | TODO | S6-16a | Package installable via `pip install mockagents` |
| S6-17a | Configure GoReleaser for cross-compilation | S6-17 | BE-3 | 0.25 | P0 | TODO | -- | Builds for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64 |
| S6-17b | Create GitHub Actions workflow for release on tag | S6-17 | BE-3 | 0.25 | P0 | TODO | S6-17a | `v*` tags trigger GoReleaser; binaries attached to GitHub release |
| S6-18a | Create GitHub Actions workflow for Docker Hub publish | S6-18 | BE-3 | 0.25 | P0 | TODO | S5-14 | Pushes `mockagents/mockagents:latest` and `mockagents/mockagents:v0.1.0` |
| S6-18b | Test Docker Hub image pull and run | S6-18 | BE-3 | 0.25 | P0 | TODO | S6-18a | `docker run mockagents/mockagents` starts and serves responses |
| S6-19a | Write CHANGELOG.md covering all MVP features | S6-19 | DX-1 | 0.25 | P1 | TODO | -- | Organized by category: features, CLI, SDK, adapters, infrastructure |
| S6-20a | Run full test suite on Linux | S6-20 | BE-1 | 0.25 | P0 | TODO | S6-01c | All tests pass on Ubuntu 22.04 |
| S6-20b | Run full test suite on macOS | S6-20 | BE-2 | 0.25 | P0 | TODO | S6-03c | All tests pass on macOS (ARM64) |
| S6-20c | Run full test suite on Windows | S6-20 | BE-3 | 0.25 | P0 | TODO | S6-05d | All tests pass on Windows 11 |
| S6-20d | Manual smoke test: full user journey on fresh machine | S6-20 | DX-1 | 0.5 | P0 | TODO | S6-09d | Install, init, start, Python test, logs -- all work as documented |

### Day-by-Day Schedule

| Day | BE-1 | BE-2 | BE-3 | DX-1 |
| --- | ---- | ---- | ---- | ---- |
| D1 (Jun 23) | S6-01a | S6-02a, S6-02b | S6-04a, S6-04b | S6-08a, S6-08b, S6-08c |
| D2 (Jun 24) | S6-01b, S6-01c | S6-02c, S6-03a | S6-04c, S6-04d | S6-09a, S6-09b |
| D3 (Jun 25) | S6-06a, S6-06b | S6-03b, S6-03c | S6-05a, S6-05b | S6-09c, S6-09d |
| D4 (Jun 26) | S6-07a, S6-07b | -- (reviews/meetings) | S6-05c, S6-05d | S6-10a, S6-10b |
| D5 (Jun 27) | -- (reviews/meetings) | -- (reviews/meetings) | S6-17a, S6-17b | S6-10c, S6-10d |
| D6 (Jun 30) | Bug fixes from E2E/audit | Bug fixes from SDK compat | S6-18a, S6-18b | S6-11a, S6-11b, S6-12a, S6-12b |
| D7 (Jul 1) | Bug fixes (cont.) | Bug fixes (cont.) | -- (reviews/meetings) | S6-13a, S6-13b, S6-14a-d |
| D8 (Jul 2) | S6-20a | S6-20b | S6-20c | S6-15a, S6-15b, S6-16a, S6-16b, S6-19a |
| D9 (Jul 3) | Fix regressions | Fix regressions | Fix regressions | S6-20d; fix regressions |
| D10 (Jul 4) | Sprint review & RELEASE | Sprint review & RELEASE | Sprint review & RELEASE | Sprint review & RELEASE |

### Sprint Risks

| # | Risk | Likelihood | Impact | Mitigation |
| - | ---- | ---------- | ------ | ---------- |
| 1 | OpenAI or Anthropic SDK compatibility tests reveal format bugs that require engine changes late in sprint | High | High | Run SDK compat tests on D1-D2 (not D8-D9); fix format bugs immediately; reserve D6-D7 for bug fixes |
| 2 | Performance target (1000 req/s) not met; optimization takes longer than expected | Medium | Medium | If target not met after D3 optimizations, document actual performance and file improvement ticket for Phase 2; do not block release |
| 3 | Documentation takes longer than expected; DX-1 cannot complete all docs | Medium | Medium | Prioritize quickstart guide and README (P0); API reference and SDK docs (P0); examples and CONTRIBUTING are P1/P2 and can follow post-release |

### Sprint Exit Criteria

- [ ] All E2E tests pass on Linux, macOS, and Windows (CI matrix)
- [ ] OpenAI Python SDK v1.x works against mock server: chat, streaming, tool calls
- [ ] Anthropic Python SDK works against mock server: messages, streaming, tool_use
- [ ] Performance benchmark result: >1000 req/s single-agent non-streaming (or documented actual with improvement plan)
- [ ] Security review: no path traversal, all inputs validated
- [ ] Error handling audit: no unwrapped errors, no panics in production paths
- [ ] Documentation site live on GitHub Pages with quickstart, API reference, and agent definition reference
- [ ] `pip install mockagents` installs from PyPI (or TestPyPI for RC)
- [ ] `docker run mockagents/mockagents` starts and serves mock responses
- [ ] GitHub release exists with binaries for 5 OS/arch combinations
- [ ] Docker Hub image published as `mockagents/mockagents:v0.1.0`
- [ ] Zero known critical or high-severity bugs
- [ ] CHANGELOG.md documents all MVP features
- [ ] README.md provides clear entry point for new users

### Demo Script

- **Show 1:** Live demo of the full user journey: `pip install mockagents` -> `mockagents init demo` -> `cd demo` -> `mockagents start --agents ./agents/` -> run Python test with pytest -> `mockagents logs` to see interaction history.
- **Show 2:** Show the documentation site: quickstart guide walkthrough, link to API reference, show agent definition reference with examples.
- **Show 3:** Performance numbers: show benchmark results (req/s, p99 latency) and Docker Hub image size. Pull Docker image live and run it.

---

## Appendix A: Story Point Reference

| Points | Effort         | Example                                             |
| ------ | -------------- | --------------------------------------------------- |
| 1      | ~0.5 day       | Add a CLI flag, write a LICENSE file                |
| 2      | ~1 day         | Implement a simple interface, add middleware         |
| 3      | ~1.5 days      | Config parser with validation, integration test set  |
| 5      | ~2.5 days      | SSE streaming implementation, engine pipeline        |
| 8      | ~4 days        | Full adapter implementation (not used in this plan) |

## Appendix B: Cumulative Velocity Tracking

| Sprint | Planned SP | Actual SP | Cumulative Planned | Cumulative Actual | Notes |
| ------ | ---------- | --------- | ------------------ | ----------------- | ----- |
| 1      | 42         |           | 42                 |                   |       |
| 2      | 43         |           | 85                 |                   |       |
| 3      | 47         |           | 132                |                   |       |
| 4      | 46         |           | 178                |                   |       |
| 5      | 52         |           | 230                |                   |       |
| 6      | 51         |           | 281                |                   |       |

## Appendix C: Cross-Sprint Dependencies

```
Sprint 1 (Core Types) ──────────────────────────────────────────────────────┐
  S1-06 AgentDefinition ──> S2-01 Static Generator                          │
  S1-08 ResponseGenerator interface ──> S2-01, S2-02                        │
  S1-09 ScenarioMatcher interface ──> S2-04 through S2-08                   │
  S1-11 StateStore interface ──> S2-09 InMemoryStateStore                   │
  S1-13 Config Loader ──> S3-15 mockagents start                            │
                                                                             │
Sprint 2 (Engine) ──────────────────────────────────────────────────────────┤
  S2-10 Engine ProcessRequest ──> S3-05 OpenAI request parsing              │
  S2-10 Engine ProcessRequest ──> S4-05 Anthropic request parsing           │
  S2-11 Engine types ──> S3-05, S4-05                                       │
                                                                             │
Sprint 3 (OpenAI + HTTP) ──────────────────────────────────────────────────┤
  S3-01 Chi server ──> S4-07 Anthropic SSE (reuses SSE writer)             │
  S3-07 SSE streaming ──> S4-07 Anthropic SSE                               │
  S3-14 Agent routing ──> S4-10 Anthropic model routing                     │
  S3-15 mockagents start ──> S5-02 MockAgentServer (subprocess)             │
                                                                             │
Sprint 4 (Anthropic + Tools) ──────────────────────────────────────────────┤
  S4-01 Tool processor ──> S5-04 expect(to_have_tool_call)                  │
  S4-11 Adapter registry ──> S5-01 MockAgentClient (protocol selection)     │
                                                                             │
Sprint 5 (SDK + CLI) ──────────────────────────────────────────────────────┤
  S5-01 MockAgentClient ──> S6-01 E2E tests                                │
  S5-02 MockAgentServer ──> S6-01 E2E tests                                │
  S5-07 mockagents init ──> S6-01 E2E tests                                │
  S5-14 Docker image ──> S6-18 Docker Hub publish                           │
  S5-17 TestPyPI ──> S6-16 PyPI publish                                    │
                                                                             │
Sprint 6 (Hardening + Release) ────────────────────────────────────────────┘
  All prior work ──> S6-20 Final regression
```

## Appendix D: Definition of Done (Global)

Every task is considered DONE when:

1. Code is written and compiles without warnings
2. Unit tests are written and pass (coverage >= 80% for new code)
3. Code is formatted (`gofmt` / `ruff format`)
4. Linter passes (`golangci-lint` / `ruff check`)
5. PR is submitted with description and linked story ID
6. PR is reviewed and approved by at least 1 team member
7. PR is merged to main
8. CI pipeline passes on main after merge
9. Acceptance criteria in the task board are met
