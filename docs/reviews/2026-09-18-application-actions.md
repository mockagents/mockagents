# Application and coverage action closure

Date: 2026-09-18. Documentation baseline published on GitHub main: `2adba15`.
This report covers the subsequent application changes in the commit containing
this file. The earlier documentation review remains a historical record of
behavior at `5d5b5172dfd3c19854bcfb1db7d5215af711165e`.

| Action | Implementation and evidence |
| --- | --- |
| DR-01 — Responses tenant isolation | [History store](../../internal/adapter/responses.go) keys by `(tenant, response ID)` under one mutex and retains the global 1,024-entry FIFO bound. [Adapter regressions](../../internal/adapter/responses_test.go) cover named/anonymous owners, foreign IDs, streaming, and concurrent eviction. [Authenticated HTTP regression](../../internal/server/responses_tenant_test.go) exercises real credentials and rejects cross-tenant continuation despite a caller-selected tenant header. |
| DR-02 — Responses retention | `store:false` skips standalone history insertion; default/true retain it. Tests verify later continuation returns 404 for false-store responses in both JSON and SSE modes. Existing [Conversation tests](../../internal/adapter/conversations_test.go) preserve independent item appending. |
| DR-06 — Validator/schema bounds | [Validator](../../internal/config/validator.go) and [Agent schema](../../schema/mockagents-v1-agent.json) require turns >=1 and tool error rates in [0,1]; Go rejects nonfinite rates. [File/bytes tests](../../internal/config/agent_bounds_test.go) and [validate/PUT tests](../../internal/server/agent_bounds_test.go) cover boundaries. Duplicate tool defaults remain valid; [processor regression](../../internal/engine/tool_processor_test.go) pins last-default precedence. |
| DR-09 — Contract coverage | [OpenAPI](../api-spec.yaml) adds core provider families and pipeline execution; validates JSON/YAML wrapper semantics, nested authentication errors, nullable validation errors, and quota maps. [Supplemental schemas](../api-models.json) derive fields from Go with explicit semantic overlays. New [A2AServer](../../schema/mockagents-v1-a2aserver.json) and [SearchService](../../schema/mockagents-v1-searchservice.json) schemas cover configuration bounds. [Contract tests](../../tools/contractcheck/main_test.go) compile schemas, compare generated artifacts, check fixture/boundary parity, and validate live provider requests/responses. [Route checks](../../tools/handoffcatalog/coverage_test.go) reconcile 118 source mounts with 53 OpenAPI operations and 65 exact exclusions. [Scope](../api-contracts.md) names every excluded compatibility profile; this is not a claim of exhaustive upstream API schemas. |
| DR-10 — Provider accounting | [Classifier](../../internal/server/quota_middleware.go) includes Ollama chat and Bedrock Converse/ConverseStream. [Capture](../../internal/server/log_handlers.go) recognizes NDJSON and AWS eventstream alongside SSE. [Usage extraction](../../internal/pricing/extract.go) reads Ollama and Bedrock token fields and resolves Bedrock models from request paths for logs, aggregates, and spend hooks. [Regression matrix](../../internal/server/provider_accounting_test.go) covers tenant/anonymous calls, success/errors, streams, rate/spend rejection, logs and metrics. |
| DR-11 — Source comments | Corrected ownership, quota persistence/cache behavior, tenancy scope, supported validators, partial OIDC setup, provider registry, and race-gate comments. Regenerated handoff field/source/dependency catalogs and updated current behavior guides. |

Two-pass review traced each changed file, then followed credential → request
context → Responses store; YAML/file → validator → validate HTTP/agent PUT;
provider handler → classifier → capture/metrics → usage → cost/spend; and
source route/type → generated schema → OpenAPI → examples/live responses.

Verification passed: go vet ./...; go test ./... -count=1 -timeout 5m; binary
build; validation of all 29 examples; seven direct CLI boundary probes; focused
HTTP, retention, accounting, schema, and route coverage regressions. Linux race
coverage is delegated to the repository GitHub CI job for the published commit.

Residual scope: Responses history remains bounded and process-local. Streaming
body-derived token spend remains uncounted for SSE, NDJSON and AWS eventstream;
request quotas and logs/metrics apply. Anonymous provider access remains an
intentional compatibility policy. The 65 OpenAPI exclusions are explicit SDK,
RPC, WebSocket, or diagnostic profiles; adding an excluded REST profile to
OpenAPI remains optional expansion, not hidden coverage. No SDK, GUI or deployment
behavior was changed by this patch.
