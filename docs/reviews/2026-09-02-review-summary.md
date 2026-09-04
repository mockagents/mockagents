# Review Summary: MockAgents UI transformation readiness

Date: 2026-09-02  
Reviewer: Codex  
Branch/Commit: local main / f2e8273

## Overall Status

| Area | Status | Notes |
|---|---|---|
| Per-file pass | Completed | Selected UI, API, storage, authorization and documentation surfaces; see per-file report |
| Cross-file integration pass | Completed | Source-traced login, save, pipeline execution, logs, reporting and administration |
| Tests/build checks | Completed | GUI typecheck/build and focused server tests passed; not a full regression run |
| Release/demo readiness | Unknown | No browser interaction, accessibility audit or user study performed |

## Findings

These are source-confirmed defects or risks, not a full security audit. Priorities describe impact, not permission to implement.

| ID | Severity | Status | Owner Area | Summary | Evidence | Recommended Action |
|---|---|---|---|---|---|---|
| UI-R01 | P1 | Open | Identity | Key login probes a platform-only endpoint, rejecting valid lower-privilege users in multi-tenant mode | `gui/lib/auth.ts:80`; `internal/server/route_authz.go` tenants floor | Capability-aware identity probe; do not lower tenant-management authorization |
| UI-R02 | P1 | Open | Authoring | Agent updates have no revision precondition; concurrent valid saves can overwrite each other | `gui/lib/api.ts:640`; `internal/server/agent_write_handlers.go` PutAgent | Add agent revision contract and stale-edit recovery, following pipeline precedent |
| UI-R03 | P1 | Open | Reports | Bounded recent-row cost aggregation is labelled period spend avoided without completeness or pricing coverage | `gui/app/costs/page.tsx:26`; `gui/app/costs/page.tsx:73`; `internal/server/costs_handler.go` | Report hypothetical equivalent cost and disclose coverage, missing prices and retention |
| UI-R04 | P2 | Open | Administration | Tenant deletion submits directly despite cascading key deletion | `gui/app/admin/tenants/page.tsx` delete form | Impact preview and deliberate confirmation; retain backend role floors |
| UI-R05 | P2 | Open | Investigation | Backend session/chaos/truncation fields and paging are not exposed by GUI contracts; SSE errors obscure authentication failure | `internal/storage/models.go`; `gui/lib/api.ts:34`; `gui/app/logs/LogsConsole.tsx`; stream proxy | Typed metadata, server filtering, gap-aware reconnection and actionable errors |
| UI-R06 | P2 | Open | Audit | Fixed event-kind filters omit agent mutations and pipeline saves | `gui/app/audit/page.tsx` KINDS; `internal/audit/types.go` | Complete shared event taxonomy and tested filters |
| UI-R07 | P2 | Open | Status | Shell online state uses liveness, not dependency readiness | `gui/app/layout.tsx`; `internal/server/readiness_handlers.go` | Distinguish reachable, ready, degraded and unauthorized |
| UI-R08 | P2 | Open | Verification | GUI lacks component/e2e scripts; inspected CI does not gate GUI behavior | `gui/package.json`; `.github/workflows/ci.yml` | Add role, save-conflict, log and report browser tests plus GUI CI gates |
| UI-R09 | P3 | Open | Documentation | GUI README still describes validation-only authoring and a future pipeline editor | `gui/README.md`; current editor implementations | Update inventory during first implementation slice |

## Completed Scope

- Compared implemented UI features with the actual management APIs, rather than treating README omissions as missing backend work.
- Preserved existing strengths: server validation, pipeline ETag conflict handling, role floors, same-origin credentialed log proxy, portable YAML and CLI tooling.
- Created a proposed epic and separate Claude Design handoff, with existing, new and gated capabilities identified.

## Incomplete Or Deferred Scope

- Browser visual review, assistive technology testing, user interviews, load testing, full Go suite and external GitHub branch protections were not evaluated.
- Provider engines were not exhaustively audited. Generic fixture studios, match explanations and isolated jobs require new contracts, not merely screens.
- R17–R19 remain design-partner gated. No implementation, deployment or Git publication was performed.

## Validation Evidence

The GUI/build/server results below are evidence from the first iteration. They
were not rerun for the documentation-only second iteration. New document checks
and source tracing are reported separately below.

| Check | Result | Notes |
|---|---|---|
| `npm run typecheck` in gui | Pass | TypeScript compilation only |
| `npm run build` in gui | Pass | Next.js 15.5.15 production build |
| `go test ./internal/server -run 'Test(ManagementRouteFloors\|.*Pipeline.*\|.*Agent.*Write.*\|.*Ready.*)'` | Pass | Focused regex selection, not full server coverage |
| Browser/user testing | Not run | Smallest next step: execute baseline configure/save/run/investigate journeys with representative roles |
| `go run ./tools/liquidcheck ./docs` | Pass | Used temporary GOCACHE after default cache access was denied |
| Local Markdown link validation | Pass | All relative links resolved in the six new research documents |

## Next Actions

1. Approve the Release A scope in [UI-01](../epics/UI_TRANSFORMATION_EPIC.md).
2. Give Claude Design the separate [design brief](../epics/UI_TRANSFORMATION_DESIGN_BRIEF.md).
3. Implement foundation stories before advanced workbench features; see the [action register](2026-09-02-action-register.md).

## Second iteration: document review and corrections

Scope: all six UI research documents, followed by contract checks against current
pipeline execution, authorization, agent read/write and log storage code. Both
passes completed for this bounded document review; this is not a new whole-codebase
audit. Existing UI-R01–09 remain open implementation work.

| ID | Severity | Status | Owner Area | Summary | Evidence | Recommended Action |
|---|---|---|---|---|---|---|
| UI-D01 | P1 | Completed (docs) | Execution | Epic named nonexistent node_sequence/results and implied isolation/log linkage | `internal/engine/pipeline.go:24`; `internal/server/pipeline_handlers.go:72` | Corrected UX-05 to nodes/latency, partial result, active runtime and unknown outcome |
| UI-D02 | P1 | Completed (docs) | Authorization | Read-only stakeholder language omitted actual role exceptions | `internal/server/route_authz.go` | Added role/action matrix and explicit separate decision for stricter viewer permissions |
| UI-D03 | P1 | Completed (docs) | Authoring | Frontend preservation and simple ETags did not cover backend field loss or other writers | `internal/server/agent_write_handlers.go:203`; `internal/server/handlers.go:117` | Added round-trip, reload/external-write, compatibility and uncertain-save acceptance |
| UI-D04 | P2 | Completed (docs) | Logs/reports | Guaranteed gap recovery and provenance exceeded current storage/API | `internal/storage/sqlite.go:240`; epic UX-04/06 | Added bounded recovery/unknown states and local snapshot versus attested export distinction |
| UI-D05 | P2 | Completed (docs) | Onboarding | First-success promise assumed unavailable pipeline creation and an implicit starter | Epic UX-02/05; design Journey 1 | Seeded benchmark and empty-install path now explicit |
| UI-D06 | P2 | Completed (docs) | Jobs/imports | Lifecycle, retry, restart, ownership and redaction semantics underspecified | Epic UX-11/14/16 | Added lifecycle, idempotency, retention/migration gates and untrusted-import rules |
| UI-D07 | P2 | Completed (docs) | Delivery | Numeric priority conflicted with dependencies; standalone imports blocked on jobs | Epic section 7 | Added readiness/release gates, corrected dependencies and split UX-14 |
| UI-D08 | P2 | Completed (docs) | Design | Narrow layouts removed editing while claiming accessible authoring | Design deliverables/constraints; epic accessibility gates | Required responsive forms/list alternatives and screen-level capability annotations |

Completion means the specifications were corrected and cross-checked, not that
the proposed behavior exists. Product choices remain explicitly proposed. External
research claims were not expanded in this iteration; no new competitive research
or visual validation was performed.

### Second-iteration validation

| Check | Result | Scope |
|---|---|---|
| Per-document and cross-document/source passes | Completed | Six documents; evidence listed in companion reports |
| `go run ./tools/liquidcheck ./docs` | Pass | Temporary GOCACHE; no unterminated Liquid openers |
| Relative Markdown links | Pass | All six documents checked after edits |
| Working tree scope | Pass | Only the six research Markdown files are untracked/changed; no application changes |
| GUI/Go runtime suites and browser UX | Not rerun | Documentation-only corrections; first-iteration results above are historical |
