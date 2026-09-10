# Release remediation implementation plan

{% raw %}

Candidate baseline: `6ddb03e54a14484e5929a19673f0cfd8a1975f07`. This is an engineering execution plan, not a list of completed fixes. Owners are responsibility assignments, not named people. Every issue maps to the detailed patch and regression requirements in the [security](2026-09-10-security-runtime.md), [architecture](2026-09-10-architecture-api.md), [delivery](2026-09-10-delivery-sdk.md), or [CLI](2026-09-10-cli-documentation.md) report.

## Execution brief

**Objective:** resolve all 30 confirmed audit findings, validate the complete supported product, and produce a release candidate with reproducible signoff evidence. Current status: **planning complete; implementation not started**. No audit finding is closed by writing this plan.

The default scope includes the Go server, multi-tenant control plane, record/replay, A2A, vector adapters, all SDKs/launchers, GUI integration, Helm and advertised distribution channels. Do not silently defer a finding by treating its feature as optional. The release candidate is eligible for signoff only after Phases 1 and 2 close all 30 findings; Phase 3 contains continuing preventive improvements, not a place to move unresolved confirmed defects.

### Implementation decisions

| Area | Selected approach | Constraint |
|---|---|---|
| Platform-key protection | Add actor/target authorization inside SQLite and PostgreSQL key-mutation transactions; protect individual and bulk mutations. A small platform-only route patch may land first as containment. | Preserve ordinary tenant-admin management of non-platform own-tenant keys in the final design. Own-key endpoints remain scoped to the caller. |
| Revocation | Cache expensive credential verification, but confirm authoritative key existence/version/role before accepting cached identity. | A mutation committed before a new authorization lookup must take effect on every instance. Requests authorized before commit may finish; document this explicitly. |
| Failed engine turn | Generate against proposed state and commit only on success, including nested variable changes. | Failure does not advance turns or retain an uncapped user message; normal successful-turn semantics stay stable. |
| A2A state | Shared bounded task-store/transition layer with immutable snapshots, count/byte budgets, terminal-task TTL and bounded history. | Continue existing nonterminal task IDs; reject unsupported/invalid transitions; do not silently evict active tasks. |
| Vector contracts | Separate internal ranking from raw provider metric values and select output fields at adapter boundaries. | Honor supported Chroma/Qdrant semantics; computed non-finite values cannot produce successful empty HTTP responses. |
| Process ownership | Each SDK owns spawn, continuous bounded output capture, readiness, exit observation and cleanup. | Startup failure and shutdown must reap the child; signal delivery is not process exit. |
| Binary installation | Cache by requested version/OS/architecture; verify and publish installations atomically. | Retain explicit user binary override; do not trust an unversioned cache entry without checking version. |
| Monitoring/topology | Restrict global metrics to platform and render ServiceMonitor authorization; require explicit acknowledgment for multiple replicas. | A tenancy DSN does not make sessions, registry edits or logs shared. No transparent HA claim is introduced. |
| Publication | One reusable candidate-verification workflow; preflight all manifests/artifacts before publication; publish binary assets before dependent launchers. | Verify exact versions and immutable digests; prereleases never update stable channels. |

### Delivery sequence: 17 implementation PRs and one candidate-validation step

Each row is an independently reviewable change boundary. Dependencies describe what must be integrated before final validation of that row; unrelated rows can be authored concurrently by engineers with separate ownership. No extra tasks, branches or agents are created by this document.

| Work item / proposed PR title | Findings | Owner | Primary files / concrete output | Depends on | Completion test |
|---|---|---|---|---|---|
| R01 — Use a patched compiler and isolate the source documentation scan | SR-05, CL-05 | Release + developer tooling | `go.mod`, `Dockerfile`, Go setup in workflows; `cmd/mockagents/config_docs_test.go`. Capture compiler and scanner versions; repair cache-directory traversal. | None | Full Go tests/vet run without the audit's excluded-test workaround; source/artifact vulnerability scan using selected compiler passes. |
| R02 — Authorize key mutations against target privilege | SR-01 | Security | `internal/server/{route_authz,tenancy_handlers}.go`; tenancy Store interface and SQLite/Postgres implementations. Actor authority checked within mutation boundary. | R01 for validation | Admin cannot rotate/demote/delete a platform key, individually or in bulk; no forbidden mutation/secret; valid own-tenant operations pass. |
| R03 — Make cached authentication honor cross-instance revocation | SR-03 | Security + storage | `internal/tenancy/{store,postgres_store,auth_cache}.go`; credential-version migration and cache/authority lookup. | R02 | Two-store SQLite and actual PostgreSQL rotate/revoke/demote matrix; cache-race and DB-outage tests pass. |
| R04 — Redact complete recorded SSE frames | SR-04 | Recording | `internal/recording/{redact,proxy}.go`, affected importer/CLI callers; bounded frame assembler and propagated errors. | R01 | Synthetic tokens absent from persisted cassette at every split position; partial/oversize frames produce explicit errors; replay remains valid. |
| R05 — Fail closed on invalid suites and isolate each run | CL-01, CL-02 | CLI + engine testing | `cmd/mockagents/test.go`, `internal/runner/runner.go`, existing suite/pipeline/cross-document validators. | R01 | Exit-code 0/1/2 matrix; malformed mixed directory fails; repeated/concurrent same-named cases get independent sessions. |
| R06 — Detect all supported breaking schema constraints | CL-03 | API contracts | `internal/contract/contract.go`, `cmd/mockagents/contract.go`; input validation and conservative constraint comparison. | R01 | Additional-property/type/enum/combinator tightening returns breaking/nonzero; documented additive and annotation-only changes remain correct. |
| R07 — Commit conversation turns atomically | AR-03 | Engine | `internal/engine/state/session.go`, generator/engine regression tests and callback contract. | R01 | Failed build rolls back nested state and turn count; repeated failures remain bounded; successful retry uses intended scenario. |
| R08 — Implement bounded A2A task transitions and snapshots | AR-01, AR-02, AR-07 | A2A | `internal/a2a/server.go`, task-store abstraction/configuration, `cmd/mockagents/a2a.go`. | R01 | Send/stream continuation, terminal/missing IDs, retained-byte limits and expiry pass; concurrent get/cancel/marshal is race-clean. |
| R09 — Correct vector metric, projection and error contracts | AR-04, AR-05, AR-06, AR-08 | Vector + adapters | `internal/vector/store.go`; `internal/adapter/{chroma,qdrant,encode}.go`. | R01 | Actual source documents returned; analytic metric/threshold matrix passes; overflow and encoding errors are valid non-2xx responses. |
| R10 — Synchronize Go SDK output capture | DS-01 | Go SDK | `sdk/go/mockagents/server.go` and process fixture tests; one synchronized bounded log sink. | R01 | Concurrent stdout/stderr/read stress under race detector; no lost lines within budget; cleanup/restart correct. |
| R11 — Drain Python child output and clean up failed starts | DS-02, DS-12 | Python SDK | `sdk/python/mockagents/server.py`, `pytest_plugin.py`, new `sdk/python/README.md`; wheel metadata check. | R01 | Verbose child stays responsive; failed readiness/context entry leaves no child; wheel/sdist metadata and clean import pass. |
| R12 — Reap TypeScript children and align helper peers | DS-03, DS-04 | TypeScript SDK | `sdk/typescript/src/server.ts`; `sdk/vitest/package.json` and lockfile; packed-consumer fixture. | R01 | Real Linux stubborn child is killed/reaped; no referenced timers; SDK/helper tarballs install with strict peers and both entry points import. |
| R13 — Install exact binary versions atomically | DS-08 | Launchers | `sdk/python/mockagents/_binary.py`, `sdk/npx/lib/binary.js`; shared version/cache convention. | R11, R12 | A→B→A version selection, concurrent Python/npx installation and interrupted writes never expose a wrong/incomplete executable. |
| R14 — Secure monitoring and enforce deployment topology | SR-02, DS-07, DS-09 | Platform + security | Metrics route floor; Helm deployment/ServiceMonitor templates, values and values schema; monitoring/topology docs. | R02, R03 | Platform scrape200, absent credential401, tenant role403; default/empty-DSN/secret/HPA/replica renders and live scrape pass. |
| R15 — Require full candidate verification before publishing | DS-05, DS-06 | Release | Extract reusable verification; update `release.yml`; manifest preflight, publication dependencies, stable/prerelease policy and per-tag concurrency. | R01; integrate gates from R05–R14 before closure | Deliberately failing SDK/security/package/Helm gate blocks publishers; malformed/mismatched tags fail; rc never updates stable tags. |
| R16 — Verify exact install channels and retain failing JUnit reports | DS-10, DS-11 | CI integration | `install-paths.yml`, `scripts/install-paths-report.sh`, `deploy/actions/mockagents-test/action.yml`, action self-tests. | R05, R13, R15 | Missing/duplicate/stale channel results fail; intentional failing suite preserves nonzero status and usable JUnit output. |
| R17 — Publish accurate agent and operator instructions | CL-04 | Documentation + component owners | `.gitignore`, tracked `AGENTS.md`, `CONTRIBUTING.md`, `docs/RELEASING.md`, Makefile comments and component guides. | R02–R16 for final contract review | Repo map/risk surfaces/check commands agree with implementation; versions/platforms/rollback and limitations reviewed. |
| R18 — Validate and sign off the fixed candidate | All findings, evidence only | Release auditor + owners | Candidate evidence manifest, full acceptance matrix below, fresh architecture/security/integration review. | R01–R17 | Every finding has fix SHA, reproducer→regression proof and owner/reviewer; all required platform/package/deployment gates pass. |

R15 should be authored early to establish the missing gate infrastructure; its final closure waits for the new regressions and package checks. R17 documentation is updated within each component PR, then reconciled once in its own final pass. Do not postpone security/API documentation changes until after deployment.

### Pull-request completion contract

For each R item:

1. Re-read the affected implementation on the working branch and confirm the audit finding still applies. Retain the observed-before evidence; do not blindly apply old line numbers after refactors.
2. Convert the retained probe or source finding into a regression test that checks the required corrected behavior. Add subprocess, wire-level or concurrent tests at the boundary where the defect occurs, not a test that merely mirrors private implementation.
3. Implement the fix and update affected schema/API/SDK/config/docs together. Preserve intentional chaos behavior; distinguish injected invalid responses from accidental serialization failures.
4. Run focused tests, then the shared candidate gate. Security/storage/concurrency changes require Linux race and PostgreSQL evidence where applicable; a local Windows pass is insufficient.
5. Review the file-level invariants and caller/callee/deployment interactions. Attach change SHA, command/tool versions, results and remaining limitations to the PR.
6. Mark the associated audit IDs closed only after the fix is integrated and verified. A containment patch closes the exposure only if the final feature contract and tests support the intended release scope.

Suggested evidence record (one per finding):

```yaml
finding: SR-01
status: open
owner: security
work_item: R02
baseline_sha: 6ddb03e54a14484e5929a19673f0cfd8a1975f07
fix_sha: null
regression_tests: []
validation_artifacts: []
documentation_changes: []
reviewer: null
closed_at: null
```

### Migration, rollout and rollback checkpoints

- **Credential schema (R03):** inspect the actual migration mechanism first; add version column idempotently to both backends, backfill existing keys, and test upgrade of a populated database. Old instances cannot enforce the new cache contract: finish rollout/drain of old instances before asserting cross-instance revocation. Do not roll back to a vulnerable authorization implementation as routine recovery. Preserve database backups and favor forward repair.
- **SSE cassette representation (R04):** preserve the ability to replay old cassettes. If stored frame/delay semantics change, version or explicitly migrate the format and keep old-reader behavior documented; the redaction fix must not silently rewrite existing user recordings.
- **State semantics (R07/R08):** document that failed turns do not advance state and that terminal A2A tasks expire. Define what happens to in-flight work during restart; memory-backed task/session state is not a durable migration target.
- **Cache paths (R13):** publish a complete new versioned slot before use; leave legacy cache entries intact unless positively identified and safely migrated. Rollback selects the requested prior version's slot instead of overwriting a shared executable.
- **Monitoring/Helm (R14):** provision the new scrape credential/reference before switching the metrics floor; verify scrape health during rollout. Test values/schema compatibility with existing releases and explain topology guard failures.
- **Registry publishing (R15/R16):** prepare artifacts and preflight account/channel prerequisites before any mutation. Record every successfully published immutable version/digest; resume only missing operations. Never silently overwrite a failed release with different bytes under the same package version. A new version is required where registries enforce immutability.

### Environment prerequisites and handoff

The earlier audit left browser launch, Python/helper dependencies, Linux race, PostgreSQL, Helm/container and installed-channel evidence incomplete. Arrange a clean Linux CI runner with C compiler, Docker/PostgreSQL service and Helm, plus a clean Windows runner, isolated Python environments, Node/npm and Playwright Chromium. Keep local cache permissions separate from product failures. R01 owns restoring the complete baseline; R18 may not count an unavailable/skipped required job as passed.

Registry ownership/secrets, required status checks and protected publishing environments must be confirmed by their maintainers before release execution. This is a required readiness check, not an assertion that current settings are missing. No calendar commitment is made without assigning people and confirming this infrastructure; dependency ordering and acceptance criteria are defined above so estimates can be made per work item.

## Phase 1 — Immediate release blocker fixes

**Exit criterion:** all Critical/High findings closed with regression evidence; included public contracts work; no publication before the shared verification gate passes. Build all reviewable artifacts before requesting any eventual external release approval. This audit authorizes no publication.

### 1. Establish a trustworthy verification baseline

Release/security owner: update `go.mod` to an approved supported compiler >=1.26.6 (SR-05), retain Go language-version policy separately, remove the stale unconditional clean-scan assertion, align CI/release/container builder compiler versions, and record `go version -m` for built artifacts. Docker's current floating `golang:1.26-alpine` is not evidence of a vulnerable published binary. Pin a verified builder digest after selecting the patch; do not invent a digest. Run govulncheck on source and produced binaries, plus OS dependency scan of runtime image.

CLI owner: implement CL-01 fail-closed loader/semantic validation and CL-03 constraint-aware contract comparison first. False-green tools cannot serve as evidence for subsequent changes. Add subprocess exit-code tests using temporary fixtures, not only direct Go unit calls. Preserve 0=pass, 1=assertion failure, 2=config/load error for test command. Include load failures in machine-readable results where reports are emitted.

### 2. Close privilege and privacy boundaries

Security owner: apply SR-01 immediate platform-only containment to individual/bulk key mutations, then implement target-role checks atomically in both stores if tenant-admin key management must remain available. Test rotate/PATCH/DELETE/bulk and own-key exceptions together. Update role capabilities and GUI/admin docs. Restrict process-wide metrics to platform (SR-02) and land authenticated ServiceMonitor changes in the same release (DS-07).

For SR-03, either disable positive authentication caching on shared-backend configurations as containment or implement authoritative credential-version checks before cached authentication. Define revocation semantics for requests already in flight; do not claim that local invalidation reaches all pods. For SR-04, make cassette redaction operate on complete bounded SSE frames, propagate recording/redaction errors, and run every-byte-split tests using synthetic secrets.

### 3. Repair engine and peer state ownership

Engine owner: implement AR-03 atomic turn commit/rollback including nested variables; no error path may bypass history bounds. A2A owner: land AR-01 bounded count/byte/TTL task store and AR-02 snapshots as one coherent store change. Implement AR-07 continuation using the same transition/snapshot/retention APIs; reject unsupported continuations explicitly if omitted from scope. Cover send and stream together. Add race and soak tests with small deterministic budgets and injected clock.

Vector owner: repair AR-04 include/document/URI projection; coordinate AR-05/06 provider metric conversion around one documented internal ranking/raw-score contract. Implement AR-08 finite-result checks and encoder errors before exposing scores. Verify complete provider-shaped error responses, not just status codes. Use pinned supported Chroma/Qdrant client contracts and known analytical distances, with no real provider calls needed.

### 4. Make SDK startup, teardown and installation reliable

SDK owners: DS-01 shared synchronized Go log sink; DS-02 continuously drained/bounded Python output and startup-failure cleanup; DS-03 wait for process exit and clear timers before dropping handles. Execute Linux child-process tests including a verbose process and a process ignoring SIGTERM. Keep Windows checks where OS behavior differs.

Change `sdk/vitest/package.json` peer range to `^0.5.0` unless an explicitly tested older range is retained (DS-04), regenerate lockfile, and install packed SDK/helper together using strict peers. Implement DS-08 version/OS/arch cache paths and atomic verified publication; test concurrent Python/npx launchers. Add the missing Python README (DS-12) and inspect built wheel/sdist metadata.

### 5. Gate and stage release publication

Delivery owner: implement DS-05 reusable verification plus pre-publish manifest, DS-06 stable/prerelease policy, DS-10 exact-version complete install checks and DS-11 JUnit output handling. Artifact/version/peer checks must precede the first registry mutation. Launchers publish only after the referenced binary assets have been published and verified. Add per-tag concurrency. Preserve pinned action revisions and job-local permissions.

The following is the intended executable workflow structure. Keep existing concrete jobs/setup steps when extracting them into `verify.yml`; the named checks must actually run, not be empty placeholders:

```yaml
# .github/workflows/verify.yml
name: Verify candidate
on:
  workflow_call:
permissions:
  contents: read
jobs:
  # Move the existing Go Linux/Windows, Postgres, Python, GUI, Helm,
  # Docker, lint/schema/docs and vulnerability jobs here unchanged first.
  # Add JS SDK/helper/npx and packed/wheel consumer jobs with npm ci.
```

```yaml
# release.yml — replace the current Go-only test prerequisite.
jobs:
  verify:
    uses: ./.github/workflows/verify.yml
  # Existing release-binaries job: needs: verify
  # Existing release-docker job: needs: verify
  # Existing release-python and release-npm:
  #   needs: [verify, release-binaries]
  # All publication jobs must also depend on a concrete preflight job
  # that validates tag/package/peer versions and builds the artifact manifest.
```

A later artifact-preparation job should build once and publish the verified bytes, instead of rebuilding differently in each publisher. Consumer gate steps for the TypeScript SDK must run `npm ci`, `npm test`, and `npm run build` before building/installing the helper. Use temp-project tarball installs with `--strict-peer-deps`; local `file:` development links are not the acceptance test. Wheel gate uses a fresh virtual environment and `python -m twine check` or equivalent metadata validation. Keep optional channels explicitly identified; no missing required channel can silently pass.

Do not use the above skeleton as a completed workflow: implement the referenced existing jobs, run the nonpublishing negative tests, and review the rendered job dependency graph. The exact contained source patches are in the persona reports; state-store and workflow refactors require the named interfaces/tests as well as edits.

## Phase 2 — Architecture, reliability and agent readiness

**Exit criterion:** deterministic workflow behavior, bounded resources, declared deployment topology and usable operational guidance.

1. Finish CL-02 unique execution namespaces and cleanup for Runner and pipeline-node sessions. Verify concurrent and repeated suites preserve per-case turns. Define semantics for non-user steps and graph fan-in; either reject unsupported graphs or implement explicit aggregation with topological ordering. Do not silently change documented routing behavior.
2. Finish DS-09: require explicit multi-replica acknowledgment regardless of presence of a DSN key. Validate values types, empty/secret-supplied DSNs, HPA and persistence combinations. Test one-pod default, graceful drain, restart and rolling-update overlap. Distinguish shared tenant storage from per-pod session/registry/log/audit/rate state.
3. Complete monitoring and failure-mode work: authenticated platform scrape; counters for A2A retained bytes/evictions/rejections, generation/encode errors, recording redaction failures and subprocess termination escalation; bound labels; never log secrets or full failed payloads in diagnostics. Reuse the existing registry/logging/OTel seams instead of adding another telemetry stack.
4. Write a deploy/runbook section for data directories, log privacy/retention, SQLite backup consistency, PostgreSQL tenancy recovery, OIDC callback origin, key revocation, disk full, audit/log queue saturation, rollback and version migration. Execute restore from synthetic backups; define RPO/RTO from measured behavior before advertising durability. Clarify which data is intentionally ephemeral.
5. Land CL-04 tracked minimal `AGENTS.md`, corrected release/compiler/CI platform docs and CL-05 source-scoped env scanner. Link authoritative repo map, risky modules, behavior tests and verification commands. Keep local credentials/private configuration ignored. Avoid multiple copies of the architecture map.
6. Extend public API compatibility documentation: supported provider operations, transport versions, auth exemptions versus tenant principal resolution, streaming/error shapes, vector distances/field projection, A2A lifecycle and deliberate unsupported features. Add A2AServer/SearchService schema coverage if claiming editor/schema parity; verify serialized contracts rather than only field-name equality.

## Phase 3 — Maintainability, CI/CD and security standards

**Exit criterion:** newly introduced versions/features cannot bypass the repaired invariants.

| Workstream | Concrete change | Acceptance |
|---|---|---|
| Review ownership | Define owners for `internal/tenancy`, `internal/engine/state`, adapters/protocols, SDK release manifests and workflows. Require file-level plus cross-file review for changes to these areas. | PR template names affected contracts, negative test and release evidence; integration reviewer checks caller/callee/schema/deploy consistency. |
| Contract drift | Add executable consumer fixtures for request/response/stream/error sequences, pinned to supported SDK versions; keep drift exceptions owned and expiring. | Renamed fields, changed metric domain and dropped stream events deliberately fail fixtures. |
| Dependency hygiene | Expand Dependabot coverage to helper and Docker, pin release tools/action refs, use locks for npm/build inputs and separate dependency-upgrade PRs. | Automated refresh runs full candidate gate; no unreviewed latest tool changes at publication. |
| Artifact integrity | Produce checksums, SBOM, source SHA/compiler metadata and provenance for archives, image digests, wheels and tarballs. Verify downloaded bytes from registry before announcing release. | Consumer verifies expected version and checksum; manifest identifies every required channel and exact digest. |
| Security posture | Replace platform-wide monitoring credential with a narrowly scoped scrape capability if adding granular authorization; enforce target-resource authority in shared store layer; fuzz decoders/SSE/numeric boundaries. | Least-privilege tests and corpus run in CI; no stale privilege acceptance beyond documented in-flight semantics. |
| Resource budgets | Add sustained tests of normal and failure paths for tasks, sessions, recordings, SSE subscribers, queues, caches and subprocess logs. | Memory stabilizes within declared count/byte budgets; slow/disconnected clients release resources. |
| Deployment lifecycle | Test migrations, interrupted writes, backup/restore, graceful shutdown and supported rolling updates using actual images/chart. | Failed upgrades are recoverable; registry/session limitations are exercised and documented. |
| Reproducibility | Exact-version package installs on supported OS/Node/Python bookends; both normal and failing example/action workflows. | Installed package behavior matches candidate source, including failure outputs and process cleanup. |

## Complete implementation register

Each acceptance entry supplements the full test list and code snippets in its persona report. All IDs must have an owner and closure evidence; no finding disappears between phases.

| ID | Phase / owner | Files / action | Acceptance evidence | Documentation |
|---|---|---|---|---|
| SR-01 | 1 / Security | route_authz, tenancy handlers and both stores; role-target containment then atomic authorization | Admin cannot mutate platform target via individual/bulk paths; ordinary own-key paths work | Role matrix/API/key rotation |
| SR-02 | 1 / Security + Platform | metrics floor → platform; coordinate DS-07 | Two tenants cannot see global labels; authorized scrape works | Monitoring/auth docs |
| SR-03 | 1 / Security | store/cache credential-version authority or shared-backend cache containment | Prime B, revoke/rotate/demote A, B rejects immediately per policy; PostgreSQL included | Revocation and HA semantics |
| SR-04 | 1 / Recording | redact.go/proxy.go/import call sites; bounded frame assembly/errors | Every split masks synthetic tokens and replays valid frames; EOF/size errors visible | Record/replay privacy |
| SR-05 | 1 / Release | go.mod, CI/release builder version, Docker builder, scan gate | Chosen compiler scan clean, binary compiler recorded, artifacts re-scanned | Security/release policy |
| CL-01 | 1 / CLI | test.go + semantic/cross-document validators | Mixed broken discovery/empty suite cannot exit0; 0/1/2 subprocess matrix | Test command exit codes |
| CL-02 | 2 / Engine testing | runner.go execution IDs and session cleanup | Repeat/concurrent names do not share turn state; multi-turn case preserved | Runner isolation |
| CL-03 | 1 / Contract | contract.go root constraints + input validation | additionalProperties/type/enum/combinators tightening blocks; additive cases specified | Contract subset/policy |
| CL-04 | 2 / Docs | .gitignore, new AGENTS.md, RELEASING, CONTRIBUTING, Makefile comments | Tracked agent entry links correct commands/current versions/platforms | Onboarding/runbooks |
| CL-05 | 2 / Developer tooling | config_docs_test.go controlled source roots | Unrelated inaccessible cache ignored, real undocumented env still fails | Verification troubleshooting |
| AR-01 | 1 / A2A | server.go/shared task store + config defaults | Count/byte/TTL budgets hold for send/stream without evicting active task | A2A retention/config |
| AR-02 | 1 / A2A | snapshots for send/get/cancel and future updates | Sequential snapshots unchanged; concurrent marshal/cancel race-clean | Snapshot contract |
| AR-03 | 1 / Engine | state/session.go + generator integration | Error leaves no partial state/uncapped history; retry gets same turn | Failure/turn semantics |
| AR-04 | 1 / Vector API | chroma.go query projection/include | Pinned SDK returns stored documents/URIs; default/explicit fields accurate | Chroma query matrix |
| AR-05 | 1 / Vector API | chroma.go metric parsing and distances | l2/cosine/ip defaults/config/known distances correct | Metric configuration |
| AR-06 | 1 / Vector API | qdrant.go metric threshold/wire domain | Distance5 survives threshold6; exact threshold and other metrics covered | Score semantics |
| AR-07 | 1 / A2A | shared send/stream transition with taskId | input-required continues same ID; absent/terminal/context errors valid | A2A lifecycle example |
| AR-08 | 1 / Vector + API | store arithmetic bounds + adapter encoder errors | No unexpected 2xx empty body on overflow/encoding failure | Numeric/error contract |
| DS-01 | 1 / Go SDK | server.go shared log sink/lifecycle | Linux race test concurrent writers/readers; bounded capture | Process logs |
| DS-02 | 1 / Python SDK | server.py/pytest_plugin.py output draining and cleanup | Verbose child serves; timeout/context entry leaves no child | Startup/teardown |
| DS-03 | 1 / TypeScript SDK | server.ts wait/kill/reap/timer handling | Stubborn Linux child killed/reaped; already-exited/concurrent stop | Process timeout semantics |
| DS-04 | 1 / JS packages | helper peer range + lock | Packed SDK0.5/helper0.5 clean strict-peer install and import | Installation examples |
| DS-05 | 1 / Release | reusable verify + release dependency graph/artifact preflight | Failing non-Go gate or missing binary blocks all dependent publication | Release flow/recovery |
| DS-06 | 1 / Release | stable prerelease classification/metadata/dist-tags | rc never updates latest/stable tags; malformed tag rejected | Channel policy |
| DS-07 | 1 / Platform | ServiceMonitor values/template authorization | Rendered secret selector; platform200, no key401, viewer403 after SR-02 | Metrics secret rotation |
| DS-08 | 1 / Launchers | _binary.py/binary.js versioned atomic caches | A→B→A exact version; concurrent/interrupted install never accepted incomplete | Cache/offline overrides |
| DS-09 | 2 / Platform | Helm deployment guard + values schema | Empty DSN/secret/HPA behavior explicit; default single-pod | Runtime state topology |
| DS-10 | 1 / Release | install-paths workflow/report script | Missing platform/channel/duplicate/stale rows fail; exact version verified | Install status/source of truth |
| DS-11 | 1 / CI integration | composite action output before failing command | Failing suite retains JUnit path/XML and failure status | always() reporter example |
| DS-12 | 1 / Python packaging | add sdk/python/README.md | Wheel/sdist metadata has correct nonempty description; metadata check passes | Package long description |

DS-09 and CL-02 are included in the default remediation scope through R14 and R05 respectively. Phase numbering is sequencing, not a waiver: both must close before R18 signoff, along with every other confirmed finding.

## Final release acceptance matrix

| Gate | Required command or check | Passing evidence |
|---|---|---|
| Candidate identity | Clean checkout of approved SHA; tag/package/peer/chart compatibility validated | Commit, tag, versions and toolchains in manifest; no unreviewed generated diffs |
| Go correctness | `go vet ./...`; `go test ./... -count=1 -timeout 5m` on Windows/Linux; `CGO_ENABLED=1 go test -race ./... -count=1 -timeout 5m` on Linux | Entire suite green, no excluded cache test workaround in release environment; new regressions pass |
| Tenant DB | Existing CI PostgreSQL service with `MOCKAGENTS_TEST_PG_DSN`; StoreConformance plus two-instance security regressions | SQLite and Postgres target-authority/revocation/transactions pass |
| Docs/config/schema | `go run ./tools/driftcheck`; `go run ./tools/liquidcheck`; fresh binary `validate examples/`; targeted CL exit tests | All pass; no claimed semantic guarantee relies only on field-set matching |
| Python | Fresh venv; SDK dev install; pytest SDK/examples; wheel/sdist build/metadata and clean import | Supported-version bookends green; subprocess leak test green |
| JS | npm ci + test + build SDK/helper; npx tests; clean packed strict-peer consumer install | Correct peers, entry exports and requested binary version; no orphan child |
| GUI | npm ci, typecheck, build, component tests and Playwright Chromium against real server | Browser auth/write/SSE/error flows pass; no spawn-EPERM skip accepted as evidence |
| Security | Patched compiler source and binary govulncheck; GUI production audit; wheel/npm/container dependency checks; focused SR tests | No reachable unremediated High/Critical vulnerabilities; any exceptions specifically justified and accepted |
| Protocol | New AR regression fixtures inverted/fixed; pinned Chroma/Qdrant/A2A consumer cases and existing MCP conformance | Wire response/task/score/document semantics pass; explicit unsupported states reject |
| Deployment | Helm lint/template default/CI/auth/replica negatives; Docker build/run; cluster scrape/readiness/drain/persistence tests | Correct authorization, topology, probes and restart/restore behavior; bounded task/session memory |
| Packaging | GoReleaser check/snapshot; clean wheel/tarball installs; checksums and binary metadata | Every advertised target installs and starts; launcher selects exact package version |
| Publication preflight | Nonpublishing negative tests of shared verify/versions/rc channels/action report | No mutation reachable with failed prerequisite; package ordering and recovery documented |
| Post-publication | Exact-tag downloads/imports/version/checksum checks on every required channel | Complete required result set, all exact versions, no stale/latest substitution; then announce release |

**Approval rule:** the verdict changes only after the fixed candidate has the above evidence and all 30 confirmed findings are closed under the default scope. Any later change in feature/channel scope requires an explicit recorded decision and matching code/documentation changes; it is not assumed by this plan. Keep the original audit SHA, fixed SHA, test artifacts, dependency reports, owners and reviewer decisions together. Do not treat a prior main-branch green badge as evidence for an arbitrary tag.

{% endraw %}
