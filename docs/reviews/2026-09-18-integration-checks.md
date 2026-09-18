# Cross-file integration checks: developer handoff

Date: 2026-09-18. Baseline: `5d5b5172dfd3c19854bcfb1db7d5215af711165e`.

## Flow checks

| Flow | Files checked | Status | Evidence and implication | Action |
| --- | --- | --- | --- | --- |
| Provider → principal → engine scope | adapter registry, server mount/open routes, tenancy middleware, server tenant propagation, engine resolution | Documented | Exempt routes best-effort resolve valid credential; only context-derived tenant is authoritative | Correct OpenAPI intro and handoff |
| Agent turn → matching → template → tools → state | engine.go, matcher, generator, processor, state/session.go | Documented | Build transaction commits only on success; transport happens later | Explain retries and error-result semantics |
| Agent edit → source/registry → GUI | write handlers, revision code, config validation, GUI API/auth/editor contract | Documented | Conditional writes use source+effective ETag; receipt distinguishes persistence; legacy unconditional remains | Explain 412 and strict-field behavior |
| Pipeline → scoped agents → partial result → logs | pipeline executor/registry, handler, runner, interaction model, GUI client | Documented | DFS first arrival, declaration-order parallel results, ns duration, node source label | Diagrams and orchestration guide |
| Provider mounts → quota/capture/metrics | adapter registry + Ollama/Bedrock route declarations, quota_middleware.go, log_handlers.go, metrics_handlers.go | Coverage gap D-02 | Mounted paths are absent from `isLLMProviderPath`; capture and metrics use related classifier | Specify supported route allowlist; product decision + regression tests follow |
| Kind loader → schema/validator | loader, types, validators, schema directory, driftcheck | Coverage gap D-04 | Seven kinds, five standalone schema files; A2A/Search Go validation exists | Document Go validators as source; schema follow-up |
| Route registration → OpenAPI | generated route inventory, docs/api-spec.yaml | Coverage gap D-01 | Responses/vector/search and pipeline run omitted from OpenAPI | Provide complete route index; future operation coverage check |
| Chaos CLI → engine → docs | start.go, engine/chaos.go, chaos guide, configuration reference | Corrected D-03 | Zero inherited rate leaves explicit triggers possible | Replace fleet-wide disable claim |
| SQLite/Postgres → quota → deployment | tenancy schema/Store, quota SpendBackend, start wiring, Helm values | Documented with runtime-test limit | Shared spend backend and cached checks; rate/runtime state remain local | No distributed-all-state claim; live Postgres untested here |
| Record → redaction → cassette → replay | proxy, cassette, matcher/replay, CLI strict-mode validation | Documented | Original hash retained; encoding and per-key sequence; strict miss no fallback | State offline/network boundaries |
| Console version → overview documentation | package manifest, README, architecture, GUI README | Corrected D-05 | Manifest major version was newer than overview prose | Align overview labels |
| SDK in-process client → embedded routes | Go SDK inprocess.go and inherited client methods | Documented | Only four route families are mounted | Direct full-server tests to real server/binary |
| Binary/package versions → release pipeline | go.mod, package manifests, Dockerfile, release runbook/verify workflow | Documented | Build and distribution versions/candidate artifacts coupled | Exact-SHA release evidence still required |
| README pipeline example → loaded fixture | README URL, examples/research-pipeline.yaml, run handler | Corrected D-08 | Fixture name is `research-pipeline`; live HTTP run returned researcher then summarizer | Example corrected and exercised |

## Contract checks

| Contract | Producer | Consumer | Status | Notes |
| --- | --- | --- | --- | --- |
| Agent JSON/YAML fields | Go types | Schemas, SDK/GUI, examples | Selected drift gate passes | 35 schema/type checks are not exhaustive route/API coverage |
| Principal/capability fields | Identity handler | GUI API/auth | Source trace consistent | Local role is null; capabilities derived from mounted routes |
| Pipeline latency | `time.Duration` JSON | GUI duration interpretation | Documented | Nanoseconds vs interaction milliseconds |
| Interaction source/truncated | Storage DTO | GUI API/report/logs | Source trace consistent | Body-free query projection differs from full detail |
| MCP embedded resource | Custom MarshalJSON | MCP client parser | Documented | Authored flat fields become nested resource object |
| Secret material | NewAPIKeyResult/APIKey and Store | Management client | Documented | Plaintext only on issue/rotate; normal key metadata lacks hash/plaintext |
| Health/readiness | Server checks | Helm/GUI | Documented | Alive vs ready vs unknown; readiness is a limited dependency set |

## Integration findings

D-01/D-04 are missing machine-readable coverage, not missing runtime validators/handlers. D-02 is a route coverage gap across mounted providers and shared observability/quota policy; this review does not silently change charging or telemetry semantics. D-03/D-05/D-07 were documentation corrections. Stale comments (D-06) remain a maintenance follow-up because copying them into new contracts would reintroduce contradictions.

Go tests passed for the baseline and documentation tooling. GUI/browser, external service, deployment and release gates were not executed. See [summary](2026-09-18-review-summary.md) for exact evidence and limits.
