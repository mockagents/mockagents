# Multi-pass documentation re-review

Date: 2026-09-18. Baseline application SHA: `5d5b5172dfd3c19854bcfb1db7d5215af711165e`. Scope: the uncommitted handoff documentation, its generators/exporter, and the shared references it directs readers to. No runtime behavior was changed and nothing was published.

## Result

The documentation had material factual gaps. The re-review corrected the prose and reproduced application behavior that prevents a blanket claim of tenant isolation, retention control, or complete validation. The original delivery report's absence of P1 findings is superseded by this review.

| Pass | Status | Evidence boundary |
| --- | --- | --- |
| Per-file documentation pass | Completed | All 22 handoff pages, shared entry points/references and both documentation tools; generated catalogs reviewed through extraction logic and comparisons, not a manual audit of every model field |
| Cross-component contract pass | Completed | Auth → resource lookup; authoring → validators → engine; startup → OIDC; quota API → storage → live enforcers; Helm guards; pipeline/GUI contracts |
| Behavioral verification | Completed | Full Go suite, vet, binary build, 29 fixture validations; 22 local HTTP observations; runtime enumeration of 67 provider routes; independent quota-enforcer probe |
| Final artifact verification | Completed | 2,062 local targets and 406 anchors across 35 Markdown files; byte-stable catalogs; 30-page wiki plus sidebar; 155 rewritten wiki links and overwrite guard checked |
| Exhaustive product/release certification | Not claimed | No deployed-service, live Postgres, GUI browser, external provider, Linux race, container/Helm execution or registry release certification |

## Findings, ordered by impact

| ID | Severity / state | Confirmed fact and documentation correction |
| --- | --- | --- |
| DR-01 | P1 / Open application issue; prose corrected | Responses `previous_response_id` uses an ID-only store. A second authenticated tenant reused prior synthetic input; Conversations correctly returned 404 for a foreign tenant. The guide's combined tenant-aware Conversations/Responses description was false. [Lookup](../../internal/adapter/responses.go#L178), [handler](../../internal/adapter/responses.go#L304). |
| DR-03 | P1 / Completed documentation correction | The deployment guide generalized loopback binding to the CLI. A2A actually binds `:8083` on all interfaces and has no bind flag. Added a per-mode listener matrix. [Listener](../../cmd/mockagents/a2a.go#L94). |
| DR-02 | P2 / Open application issue; prose corrected | `store: false` is echoed but Responses history is still saved and addressable. A continuation succeeded. Documented the retention exception. [Unconditional write](../../internal/adapter/responses.go#L398), [echo](../../internal/adapter/responses.go#L679). |
| DR-04 | P2 / Completed documentation correction | Incomplete OIDC configuration disables SSO instead of failing startup. An issuer-only configuration reached readiness 200; authenticated `/auth/login` returned 404. Corrected handoff and configuration reference, including required domain map and Secure-cookie behavior. [buildSSO](../../cmd/mockagents/start.go#L764). |
| DR-05 | P2 / Completed documentation correction | Quota PUT updates only the receiving process's override map; another live enforcer retains its old settings. Shared persistence/spend is not live settings propagation. Helm also rejects shared chart persistence with multiple replicas even after acknowledgment. [Quota write](../../internal/server/quota_handlers.go#L60), [startup load](../../cmd/mockagents/start.go#L400), [Helm guard](../../deploy/helm/mockagents/templates/deployment.yaml#L13). |
| DR-06 | P2 / Open validation-parity work; prose corrected | The docs overstated validator coverage and implied duplicate tool defaults were rejected. Go validation accepts turn zero, duplicate defaults and tool `error_rate: 2`; the latter violates the authoring schema. Conversely, combined contains/regex matches are rejected although runtime matching uses AND. [Validator](../../internal/config/validator.go#L372), [schema](../../schema/mockagents-v1-agent.json#L119). |
| DR-07 | P2 / Completed documentation correction | Management auth errors are objects, not handler-style error strings; malformed definition validation returns 200/`ok:false`, not necessarily 4xx. Added exact middleware envelopes, wrapper contract, null error-list possibility, lack of registry-reference checks and platform key query scoping. [Auth envelope](../../internal/tenancy/middleware.go#L260), [validation response](../../internal/server/validate_handler.go#L85). |
| DR-08 | P2 / Completed documentation correction | The linked root architecture contradicted race support, chaos environment defaults, public probes, document kinds, required-store startup behavior and independent MCP/A2A paths. Reconciled the identified contradictions with code and CI; removed its blanket every-claim-verified wording. [Race gate](../../Makefile#L33), [server](../../internal/server/server.go), [loaders](../../internal/config/loader.go). |
| DR-09 | P2 / Open, carries D-01/D-04 | A route inventory and tagged Go structs are not a complete per-operation wire schema. OpenAPI still lacks mounted operations such as Responses and pipeline run; anonymous structs/dynamic maps need explicit contracts; A2AServer/SearchService still lack standalone JSON Schemas. The original exhaustive API/model objective remains only partially satisfied. [OpenAPI](../api-spec.yaml), [catalog scope](../handoff/reference/model-fields.md), [schemas](../../schema/). |
| DR-10 | P2 / Open, carries D-02 | Ollama/Bedrock remain omitted from shared HTTP quota/capture/metrics classification. Documentation correctly exposes the limit; no runtime fix was made. [Classifier](../../internal/server/quota_middleware.go#L24), [capture](../../internal/server/log_handlers.go#L595). |
| DR-11 | P3 / Open, carries D-06 | Stale source comments remain: metadata tenant header, quota persistence, and comments above `buildSSO`/`ValidateBytes` describe behavior different from their implementations. Generated notes must not be treated as semantic authority. [Metadata](../../internal/types/agent.go), [quota](../../internal/quota/quota.go), [validation](../../internal/config/validate_bytes.go). |
| DR-12 | P3 / Completed documentation correction | Telling Windows readers to substitute `curl.exe` did not make Bash backslash continuations valid PowerShell. Added a native PowerShell pipeline request and clarified shell choice. [Examples](../handoff/api.md#copyable-requests). |

Six actions remain open; six documentation corrections are complete. Open application items remain open even though their documentation is now accurate. See the [action register](2026-09-18-documentation-action-register.md) for owners and acceptance tests.

## Reproduced observations

The binary was built from the baseline application source. Tests used a temporary copy of the example fixtures plus a synthetic echo Agent, isolated SQLite data, two new synthetic tenants and a loopback-only main listener. No real provider credentials or external services were used. The process was stopped and environment settings restored afterward.

| Probe | Observed result |
| --- | --- |
| Public readiness / unauthenticated identity | 200 / 401 with `error.type=authentication_error` |
| Incomplete OIDC (issuer only) | Startup succeeded; authenticated login path 404 |
| Tenant identity control | Two credentials resolved to distinct tenant IDs |
| Responses continuation with another tenant's known ID | 200; echoed the first tenant's synthetic marker |
| Responses created with `store:false` | Still addressable by `previous_response_id` |
| Conversations foreign-tenant read | 404, confirming the separate store's ownership check |
| Malformed YAML through validate | 200, `ok:false` |
| Contains + regex in one rule | `ok:false` |
| `turn_number: 0`, duplicate tool defaults, `error_rate: 2` | Each accepted by Go validation |
| Pipeline update without/stale ETag | 428 / 412 |
| Pipeline run with unknown field | 400 |
| Documented research pipeline request | 200; nodes `researcher,summarizer` |
| Invalid provider credential | Chat request continued anonymously and returned 200 |
| Runtime adapter registry | All 67 provider patterns found in the 118-mount catalog |
| Shared store, two separate quota enforcers | After PUT: persisted rate 7, receiving enforcer 7, other live enforcer 0 |

The Responses observation requires possession of a response ID; the test does not establish ID predictability. It does establish that ownership is not checked at lookup. Continuation can expose prior user content because the mock engine matches/renders the assembled prior messages. The quota probe used two enforcers with SQLite, not two deployed Postgres replicas; the source establishes the same in-memory override ownership irrespective of backend.

## Validation evidence

| Check | Result |
| --- | --- |
| `go test ./... -count=1 -timeout 5m` | Pass, all packages including Go SDK |
| `go vet ./...` | Pass |
| CLI binary build | Pass; nonfatal module stat-cache permission warning, binary executed successfully |
| Built CLI `validate examples/` | Pass, 29 files |
| HTTP and independent Go probes above | Pass as observations of current behavior; these are not assertions that all behavior is desirable |
| Drift, Liquid, tracked-doc check | Pass; 35 OpenAPI schemas matched, no unterminated Liquid, 119 tracked Markdown files checked |
| New/changed Markdown links and anchors | Pass; 2,062 local links and 406 heading/source-line anchors across 35 files |
| Generated catalog reproducibility | Pass; all four catalog files byte-stable; 331 models, 118 mounts, 40 packages |
| Wiki export and rewritten links | Pass; 30 pages plus sidebar, 155 internal wiki links, nonempty destination rejected |
| `git diff --check` | Pass; only normal LF/CRLF conversion notices |

Windows/amd64, Go 1.26.6, writable temporary `GOCACHE`. The full Go tests were rerun for this review rather than inferred from the first delivery. No runtime source or package dependencies changed. Mermaid was reviewed as text; no graphical renderer or external link availability was tested.

## Review artifacts

- [Per-file ledger](2026-09-18-documentation-per-file-analysis.md)
- [Cross-component claims and backing facts](2026-09-18-documentation-integration-checks.md)
- [Prioritized actions](2026-09-18-documentation-action-register.md)
- [Updated handoff](../handoff/README.md)
