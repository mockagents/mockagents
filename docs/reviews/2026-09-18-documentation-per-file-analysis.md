# Per-file documentation analysis

Historical baseline report. The subsequent six application/coverage fixes and their verification are recorded in [application action closure](2026-09-18-application-actions.md).

Date: 2026-09-18. Application baseline: `5d5b5172dfd3c19854bcfb1db7d5215af711165e`. This is the first pass of the documentation re-review. Local observations were followed through their implementation and tests in the [second-pass matrix](2026-09-18-documentation-integration-checks.md).

| File | Responsibility | Local review result / action | Backing evidence or remaining limit |
| --- | --- | --- | --- |
| [handoff/README](../handoff/README.md) | Scope and navigation | Added latest review and explicit inventory limits | Original baseline retained; new review supersedes initial gap assessment |
| [product](../handoff/product.md) | Capabilities and journeys | No additional correction required | Seven loader kinds; five Agent protocol values; fixture execution is not autonomous planning |
| [codebase-map](../handoff/codebase-map.md) | Ownership and change map | No additional correction required | Directory inventory and Go import map; package descriptions traced to owners |
| [architecture](../handoff/architecture.md) | Runtime seams | Corrected Responses isolation exception | `server.go` wrapper order; `engine.go`; `responses.go` ID-only history |
| [diagrams](../handoff/diagrams.md) | Six text diagrams | Clarified graph completion path and main-listener deployment scope | Pipeline depth-first traversal; separate listener matrix; text reviewed, no renderer run |
| [agents](../handoff/agents.md) | Lifecycle/matching/state | Added authoring contains/regex exclusion and unreachable turn-zero caveat | Matcher AND behavior differs from validator authoring constraint |
| [tools](../handoff/tools.md) | Resolution and extension | Corrected duplicate-default acceptance; documented error-rate validation gap | Tool processor chooses last default; validator does not enforce schema's rate bound |
| [orchestration](../handoff/orchestration.md) | Pipeline and runner | No additional correction required | Executor node order, graph first-visit, sessions, HTTP 422/428/412; runner assertions |
| [behavior](../handoff/behavior.md) | Limits and errors | Added Responses, partial OIDC and quota propagation behavior | Synthetic probes and source traces; open application work distinguished |
| [data-models](../handoff/data-models.md) | Validation/persistence | Split Conversations/Responses; narrowed validator/defaulting claims | Separate stores; ValidateBytes; schema/Go differences; pipeline PUT unknown fields |
| [api](../handoff/api.md) | HTTP/RPC contracts | Corrected ownership, retention, validation/envelopes; added query scoping and PowerShell request | Middleware, handlers and local HTTP probes; exhaustive operation schemas still incomplete |
| [integrations](../handoff/integrations.md) | SDK/GUI seams | No additional correction required | Limited Go in-process mounts; GUI API timeout constants/cookie forwarding; package manifests |
| [design-tradeoffs](../handoff/design-tradeoffs.md) | Design alternatives | No new measured-performance claim introduced | Qualitative analysis explicitly distinguished from historical ADRs; current actions linked through review |
| [deployment](../handoff/deployment.md) | Listeners/storage/OIDC/Helm | Added per-mode binds, quota setting propagation/fallback, precise replica guard, corrected OIDC startup | CLI listeners, enforcer/handler/startup, Helm conditional guards |
| [onboarding](../handoff/onboarding.md) | Contributor workflow | Commands/toolchain verified; root architecture corrected | Go build/tests/fixture validation; optional SDK/GUI setup not executed |
| [testing](../handoff/testing.md) | Evidence boundaries | Added documentation-probe findings and test coverage limits | Green suite did not reject demonstrated ownership/retention behavior |
| [glossary](../handoff/glossary.md) | Terminology | No additional correction required | Engine/login/MCP sessions distinguished; ETag vs effective/source revision checked |
| [wiki](../handoff/wiki.md) | Export instructions | Reviewed against exporter | PowerShell 7, empty-output guard, fixed baseline review set, source revision guidance |
| [reference/routes](../handoff/reference/routes.md) | Generated mount inventory | Compared with live adapter registry | All 67 runtime provider patterns present; 118 total includes management and standalone mounts; not 118 schemas |
| [reference/model-fields](../handoff/reference/model-fields.md) | Generated Go tags | Extraction and scope reviewed | 331 named tagged structs; inline structs/maps/custom wire serialization require handler review |
| [reference/dependencies](../handoff/reference/dependencies.md) | Direct imports | Compared relevant package imports | 40 tracked non-test package directories; not a runtime-network diagram |
| [reference/source-inventory](../handoff/reference/source-inventory.md) | Tracked files | Scope and deterministic generation reviewed | Excludes untracked additions until staged/tracked; inventory does not mean line-by-line code audit |
| [root README](../../README.md) | Product entry point | Prior corrected pipeline URL exercised | `research-pipeline` run succeeded; documentation navigation retained |
| [root architecture](../../ARCHITECTURE.md) | Shared walkthrough | Corrected identified contradictions, DR-08 | Race CI, public probes, seven kinds, separate protocol paths, chaos inherited settings, required tenancy store |
| [GUI README](../../gui/README.md) | Console setup | Next.js version checked | Manifest declares Next 16; no GUI build/browser run |
| [configuration reference](../../site/docs/reference/configuration.md) | Environment contract | Corrected OIDC and quota replica semantics | `buildSSO`, `loadQuotaOverrides`, `SetTenantQuota`, Secure-cookie computation |
| [OpenAPI](../api-spec.yaml) | Machine API contract | Coverage and auth introduction checked | Selected operations only; component drift checker does not establish operation completeness |
| [docs index](../README.md) | Documentation entry | Points to latest verification report | Shared entry links checked in final artifact pass |
| [catalog generator](../../tools/handoffcatalog/main.go) | AST extraction | Reviewed tracked input selection, tag rendering, route expression handling | Runtime provider-pattern comparison complements AST self-regeneration |
| [wiki exporter](../../tools/export-handoff-wiki.ps1) | Portable export | Reviewed filename map, source URL rewrite and empty destination guard | Export verification includes added review pages; no publication |
| [original summary](2026-09-18-review-summary.md) | Earlier evidence | Added superseding-review notice | Earlier no-P1 statement is historical, not current clearance |
| [original per-file ledger](2026-09-18-per-file-analysis.md) | Earlier source observations | Preserved as dated evidence | Does not substitute for this documentation pass |
| [original integration checks](2026-09-18-integration-checks.md) | Earlier flow review | Preserved; new matrix records discovered exceptions | In particular Responses must not inherit the Conversations conclusion |
| [original action register](2026-09-18-action-register.md) | Earlier follow-ups | Preserved; open D-01/02/04/06 carried forward | Current register adds newly verified issues |

No untouched product surface is certified by a "no additional correction" result. It means the chapter's material claims checked in this pass did not require another edit. Source links, existing behavioral tests and direct probes provide different strengths of evidence; the next matrix labels them explicitly.
