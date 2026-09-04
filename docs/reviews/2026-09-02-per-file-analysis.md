# Per-file Analysis: MockAgents UI transformation

Date: 2026-09-02

| File | Responsibility | Local Status | Findings | Test/Validation Gap | Action |
|---|---|---|---|---|---|
| `gui/package.json` | Frontend scripts/dependencies | Issue | No component/e2e scripts | Behavioral gates | UX08 |
| `gui/README.md` | GUI inventory | Issue | Validation-only/future-editor descriptions stale | Documentation parity | Refresh |
| `gui/app/Shell.tsx` | Navigation/context | Issue | Static navigation and binary online state | Role/readiness states | UX01–02 |
| `gui/app/layout.tsx` | Initial status | Issue | Uses health, not ready | Degraded dependencies | UX02 |
| `gui/app/page.tsx` | Agent catalog | Issue | API failures collapse into unreachable | 401/403/503 recovery | UX02 |
| `gui/app/editor/YamlEditor.tsx` | YAML authoring | Issue | Save exists; lacks revision/diff workflow | Stale and dirty drafts | UX03 |
| `gui/app/editor/page.tsx` | Authoring actions | Issue | Error categorization loses API distinction | Permission/storage failures | UX03 |
| `gui/lib/api.ts` | Management API contracts | Issue | Log fields omitted; Role lacks platform; save drops persistence metadata | Contract parity | UX01/03/04 |
| `gui/lib/auth.ts` | Cookie login/status | Issue | Tenants probe and admin assumption; status only checks key cookie | Role and SSO parity | UX01 |
| `gui/lib/guard.ts` | Proxy origin protection | OK | Cross-site rejection present | Preserve regression coverage | Retain |
| `gui/next.config.ts` | Browser security policy | OK | CSP and framing/referrer controls present | Preserve with new routes | Retain |
| `gui/app/logs/page.tsx` | Initial logs | Issue | Fixed initial window | Paging/filter URL state | UX04 |
| `gui/app/logs/LogsConsole.tsx` | Live logs/inspector | Issue | 200-row cap; no dedupe/backfill; click-only rows | Keyboard/reconnect/partial bodies | UX04 |
| `gui/app/api/logs/stream/route.ts` | SSE proxy | Issue | All upstream failures become 502 | Auth expiry recovery | UX04 |
| `gui/app/pipelines/[name]/edit/PipelineEditor.tsx` | Graph editing | OK | Existing React Flow editor, validation and conflict recovery | Browser/non-drag tests | Preserve and extend |
| `gui/app/pipelines/[name]/page.tsx` | Pipeline detail | Issue | No run journey | API execution integration | UX05 |
| `gui/app/costs/page.tsx` | Cost dashboard | Issue | Limit 1000; spend-avoided label | Partial/unknown pricing states | UX06 |
| `gui/app/audit/page.tsx` | Audit viewer | Issue | Fixed incomplete kind filters | New event filters | UX06 |
| `gui/app/admin/tenants/page.tsx` | Tenant management | Issue | Direct destructive submission | Cascade confirmation | UX07 |
| `internal/server/route_authz.go` | Management role floors | OK | Platform distinct from tenant admin; local mode separate | UI identity alignment | Preserve floors |
| `internal/tenancy/types.go` | Roles | OK | Platform role exists | Frontend union alignment | UX01 |
| `internal/server/server.go` | Route registration | OK | Existing management scope mapped | Proposed new endpoints need design | Do not invent capabilities |
| `internal/server/agent_write_handlers.go` | Agent validation/persistence | Issue | No revision precondition; supports nonpersistent registration | Concurrent edits/persistence UX | UX03 |
| `internal/server/pipeline_handlers.go` | Pipeline save/run | OK | ETags and 428/412; execution returns partial failure result | GUI run/failure coverage | UX05 |
| `internal/types/pipeline.go` | Pipeline graph schema | OK | Agent refs and conditional edges | Do not assume generic service DAG | Preserve boundary |
| `internal/storage/models.go` | Interaction records | OK | Session/tenant/chaos/truncation metadata available | Frontend omissions | UX04 |
| `internal/server/log_handlers.go` | Log query | OK | Session and offset supported | GUI query contract | UX04 |
| `internal/server/costs_handler.go` | Cost aggregation | Issue | Bounded scan; unknown pricing can produce zero | Completeness/pricing provenance | UX06 |
| `internal/server/readiness_handlers.go` | Dependency health | OK | Named checks and 503 state available | GUI integration | UX02 |
| `internal/server/metrics_handlers.go` | Prometheus metrics | Needs Review | Existing metrics are not automatically a tenant dashboard API | Tenant scope/history semantics | UX17 contract first |
| `internal/audit/types.go` | Event kinds | OK | More kinds than GUI filters | Shared taxonomy tests | UX06 |
| `cmd/mockagents/test.go` | CLI test command | OK | JSON/JUnit support found | Web jobs not established | Reuse output contracts |
| `cmd/mockagents/drift.go` | Drift CLI | OK | JSON/SARIF/JUnit and baseline/exception controls found | Import schema and permissions | UX14 |
| `docs/ADOPTION_REQUIREMENTS.md` | Adoption gates | OK | R17–19 require design-partner gate | Preserve decisions | Defer unified stack |
| `ARCHITECTURE.md` | System boundaries | OK | Next.js/Go split | Keep contracts explicit | Incremental extension |
| `.github/workflows/ci.yml` | CI | Issue | No GUI behavior/build gate found in inspected workflow | Browser contracts | UX08 |
| `.github/workflows/docs.yml` | Documentation CI | Issue | Push-main/manual trigger, not PR | Premerge documentation assurance | Consider PR validation |

## Notes

- Selected-file review: some large files were inspected in relevant sections, not line-by-line in full. OK means no issue identified in inspected scope, not a correctness certification.
- Cross-file leads were checked in the integration pass before being promoted into findings.

## Second iteration: document-by-document pass

| File | Responsibility | Local Status | Findings | Test/Validation Gap | Action |
|---|---|---|---|---|---|
| `docs/epics/UI_TRANSFORMATION_EPIC.md` | Implementation scope/contracts | Corrected | Pipeline wire names, isolation, onboarding, role matrix, revision/unknown-field handling, log gaps and job lifecycle | Runtime behavior still unimplemented | UI-D01–07; added acceptance gates |
| `docs/epics/UI_TRANSFORMATION_DESIGN_BRIEF.md` | Claude handoff | Corrected | Seed dependency, active-run warning, responsive authoring, capability and unknown-data annotations | Prototype not built | UI-D01/02/04/05/08 |
| `docs/reviews/2026-09-02-review-summary.md` | Findings/evidence | Corrected | Prior runtime tests could appear to be rerun; document corrections needed separate status | No new full-suite claim | Separate historical validation and docs completion |
| `docs/reviews/2026-09-02-per-file-analysis.md` | Pass-one evidence | Updated | Original scope did not include review of generated documents | Selected scope only | Added this pass |
| `docs/reviews/2026-09-02-integration-checks.md` | Contract verification | Updated | Earlier pipeline check omitted wire units/order/isolation; log recovery limitations | Source checks, not browser checks | Added exact cross-file traces |
| `docs/reviews/2026-09-02-action-register.md` | Work status | Updated | Needed to distinguish fixed specification from open code defects | No implementation closure | Separate UI-D register/counts |
