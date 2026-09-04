# Cross-file Integration Checks: MockAgents UI transformation

Date: 2026-09-02

## Flow Checks

| Flow | Files Checked | Status | Evidence | Gap/Risk | Action |
|---|---|---|---|---|---|
| Key → login → navigation | auth.ts, api.ts, Shell.tsx, route_authz.go, tenancy/types.go | Fail | Tenants probe demands platform; UI assumes admin | Valid nonplatform users rejected | Identity/capability contract |
| YAML → validate → apply → storage | editor files, api.ts, agent_write_handlers.go | Partial | Server validates and atomically writes; persistence metadata returned | No stale-edit guard; memory-only result hidden | Revision and persistence-aware authoring |
| Graph → validate → save | PipelineEditor.tsx, api.ts, pipeline_handlers.go | Pass | If-Match, 428/412, reload conflict path | Browser behavior not exercised | Reuse this pattern for agents |
| Pipeline → run → result | pipeline detail, pipeline_handlers.go | Partial | Backend run accepts input/session and returns partial failures | No GUI execution client/workflow | UX05 |
| Interaction → history/live → inspector | storage/models.go, log_handlers.go, api.ts, LogsConsole.tsx, stream proxy | Partial | Session/chaos/truncation available server-side | UI loses diagnostic context; no recovery backfill | UX04 |
| Usage → pricing → report | costs_handler.go, costs/page.tsx | Fail | Bounded scan represented as selected-window spend avoided | Incomplete totals and unknown prices misinterpreted | Completeness/provenance contract |
| Mutation → audit → filter | agent/pipeline handlers, audit/types.go, audit/page.tsx | Partial | Mutation events defined; filter list omits kinds | Events visible under all but not selectable by kind | Shared taxonomy |
| Tenant selection → deletion | tenants/page.tsx, route_authz.go | Partial | Platform floor retained; direct delete form | Accidental cascading deletion risk | Impact confirmation, no auth weakening |
| Liveness → shell status | layout.tsx, api.ts, readiness_handlers.go | Partial | Readiness exists but shell uses health | Reachable can be mistaken for ready | Distinct status states |

## Contract Checks

| Contract | Producer | Consumer | Status | Notes |
|---|---|---|---|---|
| Role vocabulary | Go tenancy | GUI types/login | Fail | Platform missing from GUI role union |
| Revision preconditions | Pipeline API | Pipeline editor | Pass | Existing ETag/If-Match precedent |
| Persistence result | Agent write API | GUI save | Partial | Persisted/file not surfaced |
| Diagnostic metadata | Storage/log API | Log inspector | Partial | Session, error, truncated and chaos omitted from UI type |
| Authentication failures | Upstream SSE | GUI proxy/client | Fail | 401/403 translated to 502 |
| Cost completeness | Aggregation | Report | Fail | No complete-window/provenance signal |
| Generic fixture/explain/jobs | Proposed new services | Proposed UI | Not implemented | Epic marks these NEW; no screen should imply availability |

## Integration Findings

- P1: identity, concurrent agent edits and cost report interpretation require foundation contracts (UI-R01–03).
- P2: diagnostic, audit, readiness and administrative recovery paths need explicit interaction states (UI-R04–08).
- These are source-traced checks. Runtime permission matrices, browser state recovery and accessibility remain to be validated during implementation.

## Second iteration: specification-to-code and design checks

| Flow | Files Checked | Status | Evidence | Gap/Risk | Action |
|---|---|---|---|---|---|
| Run → result → design inspector | engine/pipeline.go, server/pipeline_handlers.go, epic UX-05, design Journey 1 | Corrected docs | Nodes array, duration nanoseconds, shared Engine, per-node session suffix, optional 422 result | Prior fictitious fields/causal timeline/isolation | UI-D01; truthful result/safety states |
| Identity → audit/admin/viewer | route_authz.go, epic 8.1, design admin states | Corrected docs | Audit admin, tenants platform, run viewer, log deletion roleOpen | Viewer not universally read-only | UI-D02; no permission widening |
| Load → edit → serialize → save | handlers.go, agent_write_handlers.go, epic UX-03/8.2 | Corrected docs | Full definition read, typed unmarshal/marshal, reload changes registry | Unknown-field loss; legacy/external writers | UI-D03; end-to-end tests and compatibility gate |
| SSE reconnect → history → export | storage/sqlite.go, storage/models.go, epic UX-04/06/8 | Corrected docs | Descending IDs with offset; no stable snapshot cursor in inspected contract | Insert/retention gaps; missing provenance | UI-D04; bounded/unknown, no lossless guarantee |
| Blank install → first-success benchmark | pipeline handlers, epic UX-02/11, design Journey 1 | Corrected docs | Existing pipeline update/run, no create endpoint in current routes | Cannot assume starter exists | UI-D05; seeded and empty flows separated |
| Import → owned job → terminal result | epic UX-11/14/16 and 8.3 | Corrected proposal | No existing generic runner API asserted | Crash/race/ownership/security incomplete | UI-D06; contract gates before code |
| Story dependencies → design → release | epic 7/15, design revision acceptance | Corrected docs | UX-08 first; UX-14a independent of jobs; A vs B labels | Static design could imply backend completion | UI-D07/08; screen/story evidence requirements |

“Corrected docs” validates specification consistency only. Original implementation
findings above are not fixed by these edits. R17–R19 remain gated.
