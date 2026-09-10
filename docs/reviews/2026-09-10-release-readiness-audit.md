# MockAgents release-readiness audit

**Verdict: Not ready.** The reviewed candidate contains a reproduced privilege escalation, known vulnerable compiler floor, false-success test/contract gates, unsafe state/error paths, broken SDK process lifecycle behavior and an incompatible npm peer contract. Successful builds and substantial existing tests do not compensate for those release blockers.

| Audit identity | Value |
|---|---|
| Repository | https://github.com/mockagents/mockagents |
| Reviewed branch / commit | `main` / `6ddb03e54a14484e5929a19673f0cfd8a1975f07` |
| Commit subject | `release: prepare v0.5.0 (#141)` |
| Audit date / environment | 2026-09-10; Windows amd64, PowerShell; Go 1.26.4; CGO disabled |
| Initial checkout | Clean; origin matches requested repository |
| Inventory | 991 tracked files: 471 Go, 98 TypeScript/TSX, 72 Python, 108 Markdown, 97 YAML/YML, plus schemas/manifests/fixtures/assets |
| Changes made | Audit reports and diagnostic reproduction sources only; no production fixes, commits, publishing or deployment |

The audit is anchored to this SHA, not to an unverified later remote main or a published binary. The public repository was consulted; local source is the evidence for code findings. Registry credentials, live infrastructure and repository branch/environment protection settings were not inspected.

## Report set and method

This is a repository-wide, risk-directed audit using architecture/API, security/runtime, delivery/SDK/GUI, and CLI/documentation/testing personas. Each did per-file inspection followed by cross-component tracing. It is not a claim that every line in all 991 files was manually verified or that every provider feature is conformant. Detailed inspection inventories and remaining uncertainty are retained rather than converted into a blanket certification.

- [Architecture, protocols and multi-agent report](2026-09-10-architecture-api.md): AR-01–08, code fixes and retained regression probes.
- [Security, privacy and dependency report](2026-09-10-security-runtime.md): SR-01–05, exact conditions, containment and permanent fixes.
- [Delivery, SDK and GUI report](2026-09-10-delivery-sdk.md): DS-01–12, workflow/Helm/package/process patches.
- [CLI, contracts and documentation report](2026-09-10-cli-documentation.md): CL-01–05, false-success reproductions and agent onboarding changes.
- [Cross-file integration report](2026-09-10-integration-checks.md).
- [Engineering implementation plan and release gates](2026-09-10-action-plan.md).

Severity: **Critical** is a demonstrated privilege-boundary failure with broad impact; **High** is a release-significant security, reliability, installation or correctness failure; **Medium** is a narrower compatibility, isolation, operability or deterministic-execution defect; **Low** is documentation/packaging/developer friction. Severity and release blocking are separate: a Medium issue blocks a release that advertises the affected contract. No CVSS values are invented.

## A. Repository map and surface analysis

| Surface | Entrypoints / technology | Verification / risk |
|---|---|---|
| Server binary | `cmd/mockagents/main.go`, `start.go`; Go/Cobra/net/http | `go build ./cmd/mockagents`; HTTP management/provider routing, startup/shutdown, environment config |
| Engine | `internal/engine`, `internal/engine/state` | Named/model routing, tenant-scoped sessions, templates, strict tools and chaos; state transitions on failures are critical |
| Multi-agent orchestration | `internal/engine/pipeline.go`, registry; `internal/server/pipeline_handlers.go` | Sequential/parallel/graph execution, request context, per-node sessions, partial failure, pipeline persistence |
| Provider APIs | `internal/adapter`, `internal/streaming` | OpenAI Chat/Responses/Conversations/files/batches/embeddings/moderation, Anthropic, Gemini, Azure, Bedrock, Ollama; SSE and protocol-specific error shapes |
| Peer protocols | `internal/mcp`, `internal/a2a`, `internal/realtime`; CLI `mcp`, `a2a` | JSON-RPC, stdio/HTTP/SSE, A2A task lifecycle, realtime WebSocket state and auth-origin rules |
| RAG/search | `internal/vector`, `internal/adapter/{chroma,qdrant,pinecone,cohere_rerank,tavily_search}.go` | Tenant keys, numeric domain, collection configuration, filtering, document/score fidelity |
| Authentication/control plane | `internal/tenancy`, `internal/oidcauth`, `internal/server/{route_authz,tenancy_handlers,oidc_handlers}.go` | API keys, bcrypt, session cookies, platform vs tenant roles, OIDC identity provisioning, credential rotation |
| Persistence | `internal/storage`, `internal/tenancy`, `internal/audit`; agent/pipeline writes | SQLite logs/audit/default tenancy; optional PostgreSQL tenancy; migrations, retention, revocation, disk errors, durable file updates |
| External traffic | `internal/recording/proxy.go`, OIDC, OTLP exporter, SDK downloaders, drift collectors | Real upstream credentials, response redaction, download checksums, egress timeout/cancellation; ordinary mock responses stay local |
| Observability | `internal/metrics`, `internal/observability`, log worker/broadcaster/pruner, readiness | Global metrics labels vs tenant isolation, async drops, drain/shutdown, data retention, authenticated monitoring |
| CLI gates | `cmd/mockagents/{test,validate,contract,drift}.go`, `internal/{runner,contract,drift}` | Declarative cases, JUnit/JSON, exit status, semantic validation, comparison scope |
| Python SDK | `sdk/python/pyproject.toml`, `mockagents/*`; setuptools/requests/PyYAML | Python >=3.10, HTTP/SSE/MCP, auto-spawn/pytest plugin, cached binary bootstrap |
| JS SDK/helper/launcher | `sdk/typescript`, `sdk/vitest`, `sdk/npx` | TypeScript/native fetch, Vitest/Jest lifecycle hooks, npm peers, archive verification, process cleanup |
| Go SDK | `sdk/go/mockagents` in root module | HTTP, subprocess and in-process modes; synchronized subprocess logging and teardown |
| GUI | `gui/app`, `gui/lib`; Next.js 16.3.4/React 19, TS | Separate Node deployment on 3001; server actions, HttpOnly credential cookies, SSR fetch, SSE proxy; not embedded in Go release binary |
| Build/release | `Makefile`, `.goreleaser.yml`, `.github/workflows/{ci,release}.yml` | Cross-platform archives/checksums, npm/PyPI, Docker Hub/GHCR, optional Homebrew; exact-candidate gate and immutable package versions |
| Deployment | `Dockerfile`, `docker-compose.yml`, `deploy/helm/mockagents` | Non-root Go container, `/data` storage, ConfigMap/PVC, ingress, HPA/PDB/NetworkPolicy/ServiceMonitor; shared tenancy is not shared runtime state |
| Documentation/contracts | `README.md`, `ARCHITECTURE.md`, `CONTRIBUTING.md`, `SECURITY.md`, `docs/RELEASING.md`, `docs/api-spec.yaml`, `schema/`, `site/docs` | Extensive docs and examples exist; API schema check covers 35 schemas, not all behavioral contracts; agent instruction file is intentionally gitignored |
| Conformance/drift/performance | `conformance/`, `testdata/drift`, `tools`, separate workflow files | Offline baselines, conformance exceptions, perf comparison; these must be tied to exact versions and not confused with universal upstream parity |

### Commands and development contract

```sh
# Core (use mockagents.exe on Windows)
go build -o mockagents ./cmd/mockagents
./mockagents start --agents-dir examples
./mockagents validate examples/
./mockagents test --agents-dir agents tests/
go test ./... -count=1 -timeout 5m
go vet ./...
go run ./tools/driftcheck
go run ./tools/liquidcheck
# Linux with C compiler
CGO_ENABLED=1 go test -race ./... -count=1 -timeout 5m

# Run each in the named directory
# sdk/python: install -e '.[dev]' into an isolated environment first
python -m pytest tests/ -v
# sdk/typescript and sdk/vitest (build local SDK before helper)
npm ci
npm test
npm run build
# sdk/npx
npm test
# gui
npm ci
npm run typecheck
npm run build
npm test
npx playwright install --with-deps chromium
npm run test:e2e

# Release/deployment; no publish
helm lint ./deploy/helm/mockagents --values ./deploy/helm/mockagents/ci/test-values.yaml
helm template audit ./deploy/helm/mockagents --values ./deploy/helm/mockagents/ci/test-values.yaml
docker build -t mockagents:audit .
goreleaser check
goreleaser release --snapshot --clean
```

`make test-all` is not a full release gate: its dependency list omits GUI, helper/npx, Helm, container, vuln and package-consumer verification. Shell-oriented Makefile recipes also need a compatible shell on Windows. Main CI currently runs Go Linux/Windows, not macOS; race tests require a C compiler even though the product builds without CGO.

### High-risk and agent-readiness conclusions

Keep auth target authorization, shared-cache revocation, record/replay credentials, tenant-boundary persistence, generated response encoding, binary downloads and publication gates under explicit ownership. Existing `ARCHITECTURE.md` gives a useful map/import direction and relevant regression tests. CL-04 adds a small tracked `AGENTS.md` linking that map and declaring required checks; it should not duplicate architecture prose. Configuration/types changes must be reviewed against schema, API, SDK, fixtures and docs together. Parallel agent patches need non-overlapping file ownership and a final integration review; local per-package green results are insufficient.

## B. Risk register

**30 findings: 1 Critical, 15 High, 12 Medium, 2 Low.** Detailed reports contain exact line references, reproduction status, fix snippets, tests and documentation requirements for every row.

| ID | Severity | Concrete failure / evidence location | Gate |
|---|---|---|---|
| SR-01 | Critical | Tenant admin receives platform secret through key rotation; `server/tenancy_handlers.go:285-301` | Multi-tenant security |
| SR-05 | High | Go 1.26.4 returns eight reachable stdlib advisories; `go.mod:9` | Compiler/artifact security |
| SR-03 | High | Independent auth cache accepts deleted key; `tenancy/store.go:749`, `postgres_store.go:546` | Replicated revocation |
| SR-04 | High | Incomplete SSE JSON uses weaker redaction; `recording/redact.go:149-163` | Recording privacy |
| CL-01 | High | Empty/broken suites can exit 0; `cmd/mockagents/test.go:61-75,185-190` | Test gate correctness |
| CL-03 | High | Constraint change not detected; `internal/contract/contract.go:183-232` | Contract gate correctness |
| AR-01 | High | Unbounded successful A2A tasks/history; `internal/a2a/server.go:269-284` | A2A retention |
| AR-02 | High | Mutable A2A response aliases cancel target; `internal/a2a/server.go:377-402` | Concurrent A2A |
| AR-03 | High | Failed turn grows uncapped session history; `engine/state/session.go:74-79` | Core reliability |
| AR-04 | High | Chroma similarity query discards documents; `adapter/chroma.go:203-246` | RAG contract |
| DS-01 | High | Go SDK writers/readers use unrelated locks; `sdk/go/mockagents/server.go:73,111-112,228` | Go subprocess SDK |
| DS-02 | High | Python child pipes undrained/start failure leaks child; `sdk/python/mockagents/server.py:141-193` | Python subprocess SDK |
| DS-03 | High | TS considers signal sent to mean process exited; `sdk/typescript/src/server.ts:108-123` | TS/helper lifecycle |
| DS-04 | High | Helper 0.5.0 requires SDK ^0.4.0; `sdk/vitest/package.json:46-48` | Clean npm install |
| DS-05 | High | Tag publishing only gated by Go tests; `.github/workflows/release.yml:15-29` | All publication |
| DS-06 | High | Prerelease tags publish Docker latest; `.github/workflows/release.yml:79-85` | Stable channels |
| SR-02 | Medium | Viewer sees global tenant labels; `server/metrics_handlers.go:17-29` | Tenant isolation |
| CL-02 | Medium | Repeated suites reuse prior session; `internal/runner/runner.go:130` | Deterministic test runs |
| CL-04 | Medium | No tracked agent entrypoint, stale release/CI docs; `.gitignore:4`, `docs/RELEASING.md:22` | Engineering handoff |
| AR-05 | Medium | Chroma ignores distance config; `adapter/chroma.go:59-88` | Metric compatibility |
| AR-06 | Medium | Qdrant Euclid threshold uses wrong score domain; `adapter/qdrant.go:191-203` | Metric compatibility |
| AR-07 | Medium | A2A taskId silently ignored; `internal/a2a/server.go:242,269,333` | Multi-turn peer workflow |
| AR-08 | Medium | Finite vector overflow becomes empty 200; `adapter/encode.go:42-45` | Valid HTTP responses |
| DS-07 | Medium | ServiceMonitor never emits documented auth; `deploy/helm/mockagents/templates/servicemonitor.yaml:26-39` | Monitoring |
| DS-08 | Medium | Unversioned launcher cache defeats version pin; `sdk/npx/lib/binary.js:52-57` | Reproducible install |
| DS-09 | Medium | DSN key presence bypasses replica safety; `deploy/helm/mockagents/templates/deployment.yaml:15-16` | Deployment topology |
| DS-10 | Medium | Install verification permits stale/missing channel results; `.github/workflows/install-paths.yml`, `scripts/install-paths-report.sh` | Release verification |
| DS-11 | Medium | Failed suite suppresses JUnit output path; `deploy/actions/mockagents-test/action.yml:109-115` | CI action contract |
| CL-05 | Low | Docs scan traverses unrelated unreadable caches; `cmd/mockagents/config_docs_test.go:38-65` | Developer environment |
| DS-12 | Low | Python package names absent README; `sdk/python/pyproject.toml:9` | Package metadata |

Paths starting `server/`, `engine/`, `adapter/`, `tenancy/`, `recording/` in this compact table are beneath `internal/`; detailed reports use full repository-relative paths.

## C. Release blockers and concrete remediation

Fix all Critical/High findings before releasing the complete advertised product. Additionally fix the Medium findings tied to included contracts (tenant metrics, vector behavior, A2A continuation, HTTP encoding, reproducible launchers, chart topology, monitoring and failure reports). An explicitly narrowed release may exclude a feature/channel only if it is disabled or rejected in code, documented as unsupported and removed from claims; a hidden caveat does not turn failing behavior into a passing gate.

Immediate patches include platform-only containment for sensitive key mutation, toolchain floor >=1.26.6, test/config errors exiting 2, complete root-schema constraint comparison, transactionally committed turns, bounded/snapshotted A2A task state, correct Chroma query projection, one shared Go log lock, continuous Python pipe draining, TS exit-based termination, and compatible npm peers. The [action plan](2026-09-10-action-plan.md) orders the changes and the four persona reports give code-level edits for each. Proposed code is not a claim that these changes are already implemented or tested.

## D. Validation evidence

| Check | Actual result |
|---|---|
| Fresh Go binary build | PASS; audit binary under `.gotmp`, no production database used |
| `go vet ./...` | PASS |
| Existing `go test ./... -count=1 -timeout 5m` | FAIL at configuration-reference walk due local `.pytest_cache` access denial; all other reported packages passed |
| CLI tests excluding that one environment-sensitive test | PASS; explicitly a narrowed rerun, not full green |
| Architecture scoped package rerun after removing temporary regression files | PASS across engine/state, config, adapters, MCP, A2A, realtime, streaming and vector |
| Schema/API drift | PASS: references, 35 Go/schema field sets and licenses |
| Examples validation | PASS: 29 files |
| Docs Liquid check | PASS, including new report files |
| TypeScript SDK | 72 tests PASS; build PASS |
| npx launcher | 3 tests PASS; downloader/cache gaps remain |
| GUI | 329 component tests PASS; typecheck and production build PASS |
| GUI production dependency audit | PASS, zero vulnerabilities from `npm audit --omit=dev --audit-level=high` |
| GUI browser suite | Incomplete: browser launch `spawn EPERM`; API-only cases ran; browser correctness unverified |
| Python SDK/helper tests | Not completed: bundled Python lacks pytest/requests; helper dependencies absent and npm cache access denied |
| Go dependency scan | FAIL: eight reachable stdlib advisories on compiler 1.26.4; completed using workspace-local cache |
| Coverage instrumentation | Failed with missing `runtime/coverage` testmain package; no percentage claimed |
| Race / real PostgreSQL / Helm / Docker / GoReleaser | Not executed locally; CGO/compiler/tools/service environment unavailable; required release gates |
| Registry package installs and branch protections | Not verified; no live account/settings assertion |

The security probes reproduced all SR-01–04 using temporary databases/synthetic tokens. CLI probes reproduced CL-01/02/03. Architecture probes reproduced AR-01–08, with the A2A concurrency issue established by response aliasing plus source flow rather than an executed race-detector run. TypeScript termination was reproduced with a deterministic fake child; Go/Python subprocess defects are source-verified and need Linux stress/race confirmation. Fixtures are retained under `security-probe/` and `2026-09-10-architecture-repro/`.

The Go scanner's fixed-version result is supported by the primary [URL advisory](https://pkg.go.dev/vuln/GO-2026-6218), [TLS advisory](https://pkg.go.dev/vuln/GO-2026-6090) and [HTTP timeout advisory](https://pkg.go.dev/vuln/GO-2026-6089). Reachability is not identical to exploitability. Floating CI/Docker Go 1.26 builds may select a later patch than the local compiler; inspect actual artifact metadata before making a statement about already published binaries.

## E. Remaining uncertainty and standards alignment

The main risks are lifecycle and integration invariants that unit tests currently miss, not an absence of tests. Required additional verification: two-instance revocation, role-target matrix, all SSE split positions, response snapshots under race, error-state rollback, numerical-domain edges, packed consumer installs and exact-version registry smoke. Enforce least privilege and build provenance at publish jobs while keeping vulnerability checks in the tag dependency graph.

MCP server-scoped subscriptions/notifications, graph fan-in/first-visit semantics, WebSocket authentication provenance and broader provider schemas warrant explicit follow-up design/contract tests. They are recorded as unresolved review areas, not counted as confirmed extra defects. Backup/restore and rolling updates need executable operator tests because shared PostgreSQL tenancy does not move session, registry, quota rate-bucket and interaction/audit state out of the individual process/pod.

**Final decision: Not ready at the reviewed SHA.** Re-audit the fixed candidate against the acceptance matrix; only then can the result become Ready for release. A successful scan, full test run and installed-artifact evidence must be attached to that exact candidate, including any explicitly unsupported surfaces.
