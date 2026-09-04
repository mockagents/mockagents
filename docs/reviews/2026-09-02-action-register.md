# Review Action Register: MockAgents UI transformation

Date: 2026-09-02

| ID | Severity | Status | Owner Area | Finding | Evidence | Recommended Action | Validation Expected | Dependencies |
|---|---|---|---|---|---|---|---|---|
| UI-R01 | P1 | Open | Identity | Platform-only login probe | auth.ts:80; route_authz.go | UX01 capability-aware identity | viewer/editor/admin/platform, tenant and SSO browser/API matrix | Product approval |
| UI-R02 | P1 | Open | Authoring | Concurrent agent overwrite | agent_write_handlers.go; api.ts | UX03 revisions and conflict recovery | Two editors, stale save, unsaved draft, memory-only save | API contract |
| UI-R03 | P1 | Open | Reporting | Incomplete equivalent costs labelled avoided spend | costs_handler.go; costs/page.tsx:26 | UX06 provenance and completeness | Over-limit history, missing prices, retention, exported parity | Aggregation contract |
| UI-R04 | P2 | Open | Admin | Immediate cascading deletion | tenants/page.tsx | UX07 impact confirmation | Cancel leaves state intact; role enforcement; success/error states | UX01 |
| UI-R05 | P2 | Open | Logs | Diagnostic and reconnect contract gaps | models.go; LogsConsole.tsx; stream proxy | UX04 typed fields, server paging and recovery | Session filter, duplicates/gaps, auth expiry, truncated bodies, keyboard access | Log API alignment |
| UI-R06 | P2 | Open | Audit | Missing kind filters | audit/types.go; audit/page.tsx | UX06 complete taxonomy | Every emitted kind selectable, tenant scoped and exportable | UX01 |
| UI-R07 | P2 | Open | Shell | Liveness presented as availability | layout.tsx; readiness_handlers.go | UX02 readiness states | Ready/degraded/offline/unauthorized states | Error model |
| UI-R08 | P2 | Open | Test infrastructure | Missing GUI behavior gates | package.json; ci.yml | UX08 component/e2e + CI | Login/save conflict/run partial failure/log recovery/report completeness | Test fixtures |
| UI-R09 | P3 | Open | Docs | Stale GUI inventory | gui/README.md | Update alongside Release A | Links and feature inventory checked against implementation | None |

## Status Summary

| Status | Count |
|---|---:|
| Open | 9 |
| In Progress | 0 |
| Blocked | 0 |
| Ready for Verification | 0 |
| Completed | 0 |
| Deferred | 0 |

All actions are proposals. Open does not mean implementation has been authorized or scheduled.

## Second-iteration document corrections

These items are complete only at the specification level. The original nine
implementation findings remain open; counts above apply to UI-R items only.

| ID | Severity | Status | Owner Area | Finding | Evidence | Recommended Action | Validation Expected | Dependencies |
|---|---|---|---|---|---|---|---|---|
| UI-D01 | P1 | Completed (docs) | Execution | Wrong wire fields and isolation assumptions | engine/pipeline.go; epic UX-05 | Correct nodes/units/partial outcomes and active run | Source → epic → brief trace completed | Runtime tests when UX-05 starts |
| UI-D02 | P1 | Completed (docs) | Identity | Viewer incorrectly implied universal read-only | route_authz.go | Add action matrix and separate policy decision | All listed floors checked against code | Product choice for stricter roles |
| UI-D03 | P1 | Completed (docs) | Authoring | Unknown fields and other writers not protected | agent_write_handlers.go; handlers.go | Require backend round-trip/compatibility tests | Serialization and revision assumptions traced | UX-03 API decision |
| UI-D04 | P2 | Completed (docs) | Evidence | Recovery/provenance guarantees unsupported | storage/sqlite.go; UX-04/06 | Explicit partial/unknown and snapshot scope | Paging/export clauses reconciled | Stable cursor only if later approved |
| UI-D05 | P2 | Completed (docs) | Onboarding | Seeded pipeline prerequisite missing | UX-02; Journey 1 | Separate seeded/blank installation paths | Demo and measurement use same prerequisite | Seed fixture in implementation |
| UI-D06 | P2 | Completed (docs) | Lifecycle | Missing retry/crash/import ownership contract | Epic 8.3 | Add readiness gates and negative cases | Job/import requirements cross-checked | Durable-store decision before UX-11 |
| UI-D07 | P2 | Completed (docs) | Delivery | Dependency/order ambiguity | Epic 7/15 | Clarify ordering and UX-14 split | Story dependencies and gates reviewed | Release approval |
| UI-D08 | P2 | Completed (docs) | Design | Narrow layout lost authoring | Design brief | Preserve accessible actions | Epic/brief responsiveness reconciled | Prototype validation later |

Document correction summary: 8 completed; 0 open. This does not close any
implementation issue or resolve the explicitly listed product decisions.
