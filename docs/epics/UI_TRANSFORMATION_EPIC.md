# EPIC UI-01: Mock Reliability Workbench

Date: 2026-09-02  
Status: Proposed — product/design approval required; no implementation authorized by this document  
Baseline: local `main` at `f2e8273`  
Audience: product owner, Claude Design, implementation AI agent, reviewers

Revision 2: second-pass document review corrected pipeline wire semantics,
execution isolation, onboarding prerequisites, authorization, log recovery,
revision compatibility and lifecycle/acceptance gaps. These are specification
fixes, not implemented backend capabilities.

## 1. Recommendation

Transform the console from a collection of administrative pages into a developer's
deterministic test workbench. The primary journey is:

**Configure → validate → exercise → explain → improve → share evidence.**

The differentiator should be **“Turn an unexplained agent failure into a reproducible,
reviewable regression test”**, not another chat playground or an attractive collection
of charts. Keep the existing Go engine, YAML artifacts, provider adapters, and
Next.js console. Extend them incrementally rather than rewriting the UI stack.

This proposal is grounded in a two-pass source review and primary-source research.
It is not a usability study or a claim of market uniqueness. The current GUI was
typechecked and built, but not visually exercised in a browser. See the
[review summary](../reviews/2026-09-02-review-summary.md) for evidence and limitations.

## 2. Product boundary and users

| User | Primary job | Desired experience |
|---|---|---|
| Developer | Reproduce a bug and test an integration | YAML/form authoring, SDK snippets, exact matching reasons, repeatable runs, CLI parity |
| QA / test author | Build acceptance and failure scenarios | Guided forms, named test packs, expected-vs-actual results, readable evidence |
| Product / domain reviewer | Check a business journey | Read-only scenario narrative, pass/fail evidence, annotations later; no infrastructure knowledge required |
| Tenant administrator | Control access and configuration | Tenant-scoped keys, audit, confirmation and recovery guidance |
| Platform operator | Operate the service across tenants | Readiness, quotas, storage/log health, cross-tenant privileges explicitly identified |

“End user” means a tester, stakeholder, or operator of MockAgents—not a consumer
chatbot audience. The UI does not evaluate real model intelligence or certify
production safety. Model quality remains the responsibility of eval tooling.

Defaults proposed for approval: local/self-hosted first, one configured server per
console, YAML remains portable, no cloud dependency, no automatic live-provider
calls, no public sharing, no arbitrary code execution. Multi-server switching and
collaboration are not prerequisites for the first useful release.

## 3. What already exists, and what is missing

Legend: **UI** = an existing screen; **API** = backend available but UI missing or
incomplete; **NEW** = backend contract/storage needed; **GATED** = existing product gate.

| Capability | Today | Transformation |
|---|---|---|
| Agent catalog, detail, delete, reload | UI | Searchable mock library, guided first success, impact-aware actions |
| Agent YAML validate/save | UI; save is live immediately | Safe create vs replace, persistence receipt, version checks, draft/preview/apply |
| Pipeline graph | UI with React Flow editing and ETag saves | Add execution and node-result inspector; do not rebuild the canvas |
| Pipeline execution | API `POST /api/v1/pipelines/{name}/run`; uses shared engine | Explicit active-runtime execution, node results and partial-failure evidence; not an isolated preview |
| Logs / live SSE | UI | Session grouping, chaos and truncation fields, URL filters, backfill and provenance |
| Cost estimates | UI; bounded row scan | Honest partial-data labels and full-window aggregates before reporting totals |
| Audit | UI; fixed filter list lags emitted events | Complete event taxonomy, drill-down, actor/target/time filtering |
| Tenant/key administration | UI | Correct role-aware login, destructive-action confirmation, quota UI |
| Readiness and metrics | API | Separate process-up, ready, disconnected and stale states; supported metrics only |
| Vector / search / rerank / moderation | Runtime implementations; no dedicated GUI | Provider-aware fixture editors and probe tools; NEW resource management API |
| MCP / A2A | Standalone runtime surfaces | Explicit connection/inventory boundaries; no fictional shared listener |
| Test suites, imports, drift formats | CLI / libraries | First render uploaded scrubbed artifacts; NEW bounded run/import services for execution |
| Whole-stack topology and correlated journal | GATED R17–R19 | Do not implement implicitly as part of a UI redesign |

Important: `gui/README.md` is stale about saving, the pipeline editor, and SSO.
Source code—not the README's “future” list—defines the baseline.

## 4. Research: patterns worth adapting

These are documented product patterns, not proof of UX superiority. Recommendations
in the last column are our synthesis.

| Reference | Observed pattern | Adaptation for MockAgents |
|---|---|---|
| [WireMock verification](https://wiremock.org/docs/verifying/) | Request journal and near-miss diagnostics | Explain which matcher failed; never auto-relax matching to get a green result |
| [Postman mock logs](https://learning.postman.com/docs/design-apis/mock-apis/mock-server-calls/) | Inspect calls, responses and state with filters | A consistent request inspector across mock types, with captured-state limits visible |
| [Postman simulations](https://learning.postman.com/docs/design-apis/simulate-conditions) | Per-simulation condition overrides without rewriting source scenarios | Isolated failure experiments rather than global fault toggles on shared mocks |
| [Langfuse sessions](https://langfuse.com/docs/observability/features/sessions) | Group related traces into navigable interactions | Group existing session records now; only draw cross-service causal edges when supported by IDs |
| [Langfuse playground](https://langfuse.com/docs/prompt-management/features/playground) | Open an observed generation in an editable comparison workflow | Open a captured request in an offline reproduction lab, not a live model call |
| [Playwright Trace Viewer](https://playwright.dev/docs/trace-viewer) | Inspect action timelines, source, network and snapshots together | A portable failure evidence bundle with synchronized views, bounded to evidence actually captured |
| [WCAG 2.2](https://www.w3.org/TR/WCAG22/) | Accessible controls and non-drag alternatives | Keyboard-complete forms and graph alternatives; status never conveyed only by color |

Do not compete on generic chart volume. Compete on deterministic diagnosis,
portable regression artifacts, and evidence with clearly stated limits.

## 5. Proposed information architecture

Use task-oriented navigation; mock types are filters/tabs, not seven new sidebars.

| Section | Screens / tasks | Primary action |
|---|---|---|
| Overview | First-run checklist, readiness, recent failed runs, useful next action | Connect an SDK / investigate failure |
| Mocks | Agents, pipelines, vector collections, service fixtures; capability-filtered | Create or import a mock |
| Test Lab | Active-runtime pipeline runs (A); request composer, preview, suites and failure matrices (B) | Explicit active run (A) / isolated experiment (B) |
| Investigate | Request/session explorer, match explanation, run comparison | Reproduce / create regression |
| Reports | Run evidence, coverage, drift, estimated cost | Export a reproducible report |
| Administration | Identity, tenant keys, quotas, audit, service health | Review and confirm a change |

Persistent context: server identity, tenant, role/capabilities, configuration revision,
mode (mock/replay/live where known), and connection freshness. Never show “safe” or
“no egress” merely because the server is local or a toggle is selected.

Preserve existing deep links with redirects when routes move. Saved filters must
not put request bodies, secrets or raw API keys in URLs.

Revision is resource-specific; the current server has no global configuration
revision. Show “not available” rather than manufacture one. Release A's Test Lab
contains active-runtime pipeline execution only; annotate isolated preview, suite
history, match explanation and coverage as Release B in design files, not enabled
navigation in the shipping UI.

## 6. Signature features and innovation hypotheses

### A. Match Explain: “Why did this response happen?”

Show ordered candidate scenarios, predicate-level pass/fail, selected scenario,
turn/sequence state, effective chaos source/seed/rate, and known normalization.
Use the actual engine's matcher; a frontend approximation is unacceptable.
Offer a proposed change with before/after cases, not a magic “fix match” button.
Historical explanation requires the executed definition revision and state;
otherwise label it **re-evaluation against current configuration**.

### B. Failure-to-regression workflow

Select a recorded request → inspect redaction/completeness → choose a narrow
matcher → author expected behavior → validate → export fixture + test + reproduction
manifest. Defaults exclude credentials, cookies, personal identifiers and volatile
IDs. Truncated or missing inputs disable exact replay and offer a manual reconstruction.
Observed output is evidence, not automatically the correct expected assertion.

### C. Deterministic failure laboratory

Compare a healthy baseline with named failures such as rate limit, malformed
payload, truncated stream, partial retrieval or tool failure. Display the approved
precedence: request force/off → sequence → fixture → operation → service → global.
Only show configured/supported actions. Strict replay disables chaos and rejects
upstream configuration. An experiment must not mutate shared active fixtures.

### D. Coverage map, not a misleading “agent quality score”

Matrix of fixture/scenario × declared failure/assertion dimension, displaying
tested, untested, failed, unsupported and unknown. State the denominator, selected
suite, revision and time window. Distinguish schema validity, scenario exercise,
assertion coverage and provider-contract drift. None proves prompt/model quality.

### E. Change impact preview

Before applying a fixture edit, identify explicitly referenced pipelines and
versioned tests, then show targeted comparison results. Suggest tests only from
known dependency links. “No known dependents” is not “safe to change.”

### F. Shareable reproduction capsule

A versioned export contains scrubbed fixture revisions, inputs or safe references,
seed, environment/engine version, assertions, known dependencies, report and exact
CLI command. Separate display-only evidence from replayable content. Verify hashes
on import; preserve originals and report lossy conversion. No automatic upload.

### G. Optimization advisor with evidence

Suggest stale fixtures, duplicate cases, missing assertions, repeated failures,
overbroad catch-alls and expensive test paths. Start with deterministic rules and
show the source evidence and an explicit apply action. Any claim of savings must
identify its pricing version, coverage and estimate assumptions. Do not “optimize”
by silently removing difficult tests or weakening assertions.

### H. Optional natural-language authoring assistant (later)

Prompt → reviewable draft → schema validation → preview → explicit apply. Inputs
and outputs are untrusted; suggestions cannot execute tools, change permissions,
publish fixtures or bypass limits. Remote inference needs explicit opt-in and a
redacted payload preview. Core workflows remain useful with this feature disabled.

## 7. Delivery plan: one parent epic, independently shippable stories

Priority below is release grouping, not strict numeric execution order or
vulnerability severity. Sizes are relative
complexity (S/M/L), not calendar estimates. Every story includes contract tests,
UI states and documentation; no story is complete with static screenshots alone.

### Release A — Safe, useful console (first cut)

| ID | Story and acceptance criteria | Dependencies / size |
|---|---|---|
| UX-01 | Capability and identity contract. Add authenticated identity/capability response for viewer/editor/admin/platform and local mode. Viewer login succeeds without tenant-list permission; cross-tenant controls remain forbidden; SSO identity matches server state. Test 401 vs 403 vs offline and role downgrade. Do not widen backend permissions to fit UI. | NEW API; M |
| UX-02 | Trustworthy shell and onboarding. Display readiness separately from liveness, current server/tenant, last refresh and unavailable features. Provide one validated agent starter and copyable SDK settings. Agent creation hands off to UX-03. Empty pipeline inventory explains that pipeline creation is not in Release A and offers setup instructions, never a dead Run button. Network errors never render as an empty catalog. | UX-01; end-to-end first request requires UX-03/05 and a seeded pipeline; M |
| UX-03 | Safe agent authoring. Load complete existing definitions; guided form + YAML; preserve all server-supported fields and reject unsupported fields unless end-to-end lossless round-trip is proven. Preview diff; explicit create/replace; backend version preconditions; distinguish runtime-only from persisted saves. Two tabs editing the same revision produce a conflict without losing either draft. Follow compatibility and external-writer rules in 8.2. | UX-01 + NEW agent revision API; L |
| UX-04 | Request/session explorer. Expose session_id, chaos fields, error and truncated status from existing logs. Add server-backed session/time/paging controls and URL state; label bounded windows; deduplicate SSE and attempt bounded history recovery with an explicit unresolved-gap state. Offset pagination is not a lossless cursor. Preserve selection while live; test retention, concurrent inserts, reconnect and auth expiry. | UX-01; extend API wrapper; stable cursor is NEW if guaranteed recovery is required; M |
| UX-05 | Active-runtime pipeline execution. Reuse existing endpoint and graph editor; enter input and supply a fresh session ID. Consume actual `PipelineResult.nodes` and `latency`; each node has `node_id`, `agent_name`, `response`, `latency`. On 422 inspect optional `result` plus `error`, allowing absent/null node responses. Convert Go duration JSON numbers from nanoseconds for display. Parallel node array order is not completion chronology. No guaranteed log linkage, pinned revision or isolated execution claim. | UX-01; UX-04 optional for independent logs; M |
| UX-06 | Reports you can trust, v1. Export currently loaded bounded evidence as JSON and printable HTML with window, source, capture completeness and omissions. Correct cost labels; never present sampled rows as full totals or estimated cost as verified savings. Raw bodies require an explicit included-data review. | UX-04; M |
| UX-07 | Administration safety. Confirm tenant deletion with impact summary, scope, irreversible consequences and exact-name entry; protect key-rotation workflows and secret display; make role-dependent navigation truthful. Expand audit filters to current emitted events; unknown future event kinds remain readable. Add quota UI using current floors. | UX-01; M |
| UX-08 | UI delivery gate. Add GUI typecheck/build, component interaction tests and real-server browser smoke tests to PR CI. Test viewer/editor/platform journeys, draft conflict, denied mutation, live logs and export redaction. Add accessibility checks and manual keyboard review. Update stale GUI docs. | Starts first; gates UX-01–07; M |

**Release A demo:** editor signs in → opens agent → safely edits and applies a
scenario → explicitly runs a preconfigured pipeline against active runtime →
inspects returned node/partial-failure evidence → separately inspects captured
provider logs where available → exports bounded evidence. Pipeline results do not
promise a corresponding interaction journal or chaos provenance. Viewer cannot
change mock configuration, but is not a universally read-only server role (see 8.1).

### Release B — Reproducible testing and deeper resources

| ID | Story and acceptance criteria | Dependencies / size |
|---|---|---|
| UX-09 | Match Explain + isolated request lab. Add bounded engine-backed preview using a fixture revision, protocol input and isolated state. Explain candidate predicates without changing active counters, fixtures or upstreams. Repeat with identical manifest produces identical canonical decision evidence. | UX-03 + NEW preview contract; L |
| UX-10 | Failure-to-regression. Convert a complete scrubbed interaction into a draft fixture/test, ask for expected assertions, show every omitted field and export CLI-runnable artifacts. Reject unsafe/ambiguous conversions rather than adding catch-alls. | UX-03,04,09; L |
| UX-11 | Suite run service and history. Expose bounded in-process runner capabilities, queued/running/passed/failed/cancelled/timed_out states, ownership and immutable manifest. Limits, cancellation and retention are server-enforced. Generate existing test JSON/JUnit semantics; no arbitrary shell submission. | UX-01,03 + NEW job/store contract; L |
| UX-12 | Scoped chaos matrix. Compare baseline and selected supported faults across a pinned fixture set; show action source, effective seed/rate and strict-replay lockout. Isolation/concurrency test proves no leakage to other users' runs. | UX-09,11; L |
| UX-13 | Vector and service studios. Inventory and versioned read/write contracts for VectorCollection, search, rerank and moderation. Implement one kind per slice. Provide dimensional/filter validation, ranked results and before/after fault comparisons. Runtime data mutation and fixture persistence are distinct actions. | UX-03,09 + NEW resource contracts; L per kind |
| UX-14 | Drift and cassette artifact workbench. Slice 14a is read-only import of existing JSON reports/scrubbed cassettes, size/schema validation and diff inspection. Slice 14b later exposes bounded offline conversion/drift jobs; live collection is separately approved and off by default. Display exception owner/expiry and critical findings, never “approve all.” | 14a: UX-06; 14b: UX-11 + 14a; L |
| UX-15 | Coverage and change impact. Index versioned suite→scenario→pipeline references; show exercised/asserted/unknown dimensions and targeted test selection. Untested scenarios cannot show green; ambiguity and unsupported metrics are explicit. | UX-10,11; L |
| UX-16 | Reproduction capsule and run comparison. Export a versioned manifest and safe artifacts; compare normalized responses, trajectory, faults and measured timing with tolerance. Import tests verify hashes, schema version, tenant ownership and limits; replay claims require complete inputs/state. | UX-10,11,14; L |
| UX-17 | Monitoring and optimization. Add scoped JSON metric snapshots and full-window bounded aggregate jobs/storage queries. Label observed vs estimated data; alert thresholds include window and missing-data behavior. Surface rule-based suggestions with provenance. No global Prometheus labels exposed as tenant-scoped data. | UX-01,06,11 + NEW aggregation contracts; L |

### Deferred extensions—not part of first release approval

- UX-18: MCP/A2A multi-listener inventory and protocol-specific tooling. Connection
  allowlists, transport policy and lifecycle ownership must be designed first.
- UX-19: unified stack topology, coordinated control, correlated journal and
  whole-stack replay. **Blocked on the existing R17–R19 design-partner gate.**
- UX-20: remote AI assistant, team review/approval workflows, shared saved views,
  scheduled report delivery and external issue/Git integrations. Each needs a
  separate authority, data policy and product decision; not a hidden dependency.

Suggested first implementation slice: UX-08's test harness plus UX-01 identity
contract in separate reviewable PRs, then UX-04 (high value with existing data).
Do not start with a new dashboard or rebuild the pipeline canvas.

## 8. Architecture and proposed contracts

Keep Go authoritative for validation, access control, matching, persistence and
execution. Next.js server components/actions and same-origin proxies are the BFF;
credentials never enter ordinary client props or URLs. Reuse the existing SSE
proxy, but preserve authentication/failure semantics and add recovery contracts.
Lazy-load graph/editor viewers; preserve the lightweight catalog baseline.

The following are **proposed**, not existing API paths:

| Contract | Minimum data / behavior | Critical gate |
|---|---|---|
| `GET /api/v1/identity` and `/capabilities` | Principal, tenant, allowed actions, enabled resource kinds, server version and modes; no secret | Server authoritative; local mode distinguished from authenticated viewer |
| Agent revisioned GET/PUT | Canonical hash/ETag, If-Match, persisted flag, conflict response; create uses non-overwrite semantics | No lost updates; migration compatibility for old API clients |
| Resource definitions API | Kind, stable ID, tenant, revision, source and persistence mode | Explicit per-kind handlers and role floors; not arbitrary file paths |
| `POST /api/v1/previews` | Pinned definition/input/state, canonical decisions, match reasons | No writes, upstream, arbitrary code or shared-state advancement |
| Run create/read/cancel/events API | Run ID, actor, tenant, state, limits, manifest, partial results | Enforced caps, isolated state, bounded queues; no shell commands |
| Artifact import/export API | Format version, content hashes, scrub status, size limits, provenance | Validate before persistence; prevent traversal/zip bombs/injection |
| Scoped metrics/aggregate API | Window, count scanned/matched, completeness, units, pricing version | Server-side tenant filtering; unknown/partial never rendered as zero |

UX-06 can initially export already-authorized, loaded data in the browser; this
does not require UX-11 or a new export service. Such an export is a bounded local
snapshot, not a server-attested or complete-window report. New server-generated
exports require authorization at generation and retrieval. Never synthesize an
engine version, historical resource revision, pricing version or redaction policy
ID when the source does not supply one: use explicit unknown plus a schema version
owned by the exporter. Cost scan limits and token/pricing coverage remain separate
from request-capture completeness; a full page does not prove complete retention.

### 8.1 Authorization and execution boundaries

Baseline multi-tenant floors below come from `internal/server/route_authz.go`;
handlers still enforce resource ownership. Role is not an arbitrary tenant switch.

| Action | Current minimum role | UI contract |
|---|---|---|
| Agent reads, logs/history/stream | Any authenticated role | Scope to authorized data; no credential in links |
| Agent mutations/reload, pipeline save, validation | Editor | Disabled/denied for viewer; server remains authoritative |
| Pipeline execution, costs, own quota | Viewer | Execution is an explicit action, not passive inspection |
| Audit, stream metrics | Admin | No viewer audit tab/export; do not reuse global metrics as tenant data |
| Own-tenant key list / key administration | Editor / Admin | Self-service key rotation/burn is separately viewer-allowed |
| Tenant list/create/delete, quota updates | Platform | Tenant admin cannot manage other tenants or raise quota |
| Log deletion | Any authenticated role today | Do not call viewer universally read-only; do not add clear-log UI in this epic |

Changing log-deletion permissions or introducing a truly read-only stakeholder
role requires a separate product/security decision. Existing local unauthenticated
mode is an explicit exception, not a synthetic viewer identity. Capabilities must
include resource scope and refresh on role change; purge cached data and close SSE
on logout/tenant/identity changes. Never forward the user's bearer key to arbitrary
hosts or silently fall back to an operator key when authentication fails.

UX-05 invokes the shared engine and advances per-node session state. Fresh session
IDs avoid accidental conversational reuse; they do not pin mutable definitions or
create an isolated fixture registry. Show “Run against active configuration” and
record submitted input/session and returned evidence. A lost response is an unknown
outcome; do not automatically retry stateful runs. Disable duplicate submission.
Cancellation/timeout cannot guarantee rollback of completed nodes. Do not promise
preview-style isolation or a network-egress guarantee without server enforcement.
UX-09/11 introduce the separate isolated offline boundary for Release B.

### 8.2 Safe revisions, recovery and compatibility

UX-03 must read the complete server-supported definition, not regenerate it from
the reduced GUI summary type. Test round-trip scenarios, defaults, nested fields,
tool schemas and Unicode. Current typed YAML parsing can discard unknown fields;
until a lossless backend round-trip is implemented, reject unsupported fields or
disable Apply with a useful explanation. Frontend retention alone is insufficient.

Revision checks must cover concurrent API writes, reloads, deletions and relevant
external source-file changes, not just two GUI tabs. Separate effective-runtime
revision from source-file revision where they differ. Creation must not overwrite
an existing definition; deletion/reload must invalidate stale edits. File failure
must not report a durable save. Preserve both drafts on conflict and distinguish
“reload current” from an explicit, reviewed overwrite/rebase.

Do not silently make legacy unversioned agent PUT incompatible. Before UX-03,
approve either an additive conditional-write route or a versioned/deprecated
transition. Conditional writes alone do not protect against legacy unconditional
writers; advertise the limit and include those writers in the concurrency test.
GUI/backend capability mismatch is unsupported, not offline: allow safe reads,
disable unsupported writes, preserve/export drafts, and document supported versions.

### 8.3 Release B job and artifact lifecycle

UX-11's contract must include failed infrastructure execution separately from test
assertion failure, plus interrupted work after restart. Define allowed transitions
from queued/running to terminal states and exactly when cancellation wins. Use
tenant-scoped idempotency tokens for run creation, immutable submitted manifests,
bounded input/output/queue sizes and deadlines. Retrying creates a new run linked
to its predecessor; never silently resume mutable work after a crash.

Before implementation, specify durable store ownership, schema migration/rollback,
retention limits, deletion behavior, restart reconciliation and concurrency limits.
Run events need stable sequence/cursor semantics and a snapshot recovery path;
expired cursors report a gap. Tests cover double submission, cancel/completion races,
revoked access, restart and retention while viewing. UX-17 does not include background
notifications: in-UI threshold evaluation only; delivery/scheduling remains UX-20.

Imports are untrusted even when hashes match: hashes detect changes, not authenticity
or tenant ownership. Assign ownership from the authenticated importer; never trust
the manifest's tenant, executable command or path. Validate schema, allowlisted
formats, expansion limits and dependencies before staging; no partial activation.
Preview sanitized changes and require explicit apply. Redaction can alter semantics:
record transformations and call the result reconstructed, not exact replay. Test
secret canaries in nested JSON/YAML, bodies, headers, filenames and error text.

An execution manifest should pin engine version, resource hashes, canonical
request, seed, mode, isolated session/state snapshot reference, assertion version,
and dependency hashes. Record omissions and redactions. Fixed seed alone does
not guarantee replay when state, concurrency or fixtures differ.

Use existing validated documents as the interchange format. Draft storage can
be browser-memory initially; durable drafts need a specified retention/encryption
policy. Browser localStorage must not silently persist captured sensitive bodies.
UI saves are not Git commits: show “saved to server” separately from “exported”
or any later Git workflow. Full YAML comment/format preservation is a separate
design choice; promise semantic preservation only where tested.

## 9. Reporting specification

| Report | Essential contents | Caveat |
|---|---|---|
| Test-run evidence | Manifest, expected/actual, node sequence, faults, failures, partial/cancelled state | Only executed assertions are evidence |
| Fixture coverage | Eligible scenarios, hit count, assertion links, exclusions and revision | Not code/model coverage |
| Drift report | SDK/provider/mock comparison, baseline, criticality, exclusions, exception expiry | Offline baseline may be stale |
| Audit export | Actor, tenant, action, target, outcome and time range | Do not call it compliance certification |
| Operations report | Readiness, error/latency windows, dropped capture, storage/retention state | Disabled telemetry is unknown, not healthy |
| Cost estimate | Pricing version, observed token coverage, sample/full label and units | Hypothetical upstream cost is not realized savings |

All exports: tenant/scope, generation time, engine/schema version, filters, sample
limits, redaction policy ID and completeness. JSON is canonical; printable HTML
is the initial human format. Reuse CLI JUnit/SARIF where semantically applicable;
do not force audit or cost reports into test-result schemas. PDF and scheduled
email delivery are later capabilities, not required dependencies.

## 10. Security, UX and acceptance gates

- Every read/write/export is authorized by the server, not a hidden button.
- Preserve local unauthenticated mode explicitly; deployment guidance must warn
  against exposing it remotely. Allowlisted server connections; no arbitrary URL
  request relay, credential forwarding or silent upstream execution.
- Render untrusted logs as text. Reports must escape HTML/CSV formulas and strip
  unsafe links. Export preview and re-redaction occur before download/storage.
- Define draft, validating, invalid, ready, running, partial, failed, cancelled,
  conflict, persisted and runtime-only states. No “saved” toast before confirmation.
- Run cancellation stops owned work; it must not restart the shared mock server.
- Distinguish same-session sequence order from wall-clock sort; no invented causal
  links across protocols. R19 is still gated.
- WCAG 2.2 AA target: keyboard-only completion, visible focus, accessible names,
  announced async errors, non-drag graph editing, reduced motion and status text.
- Dense tables use pagination/virtualization; body viewers are bounded and lazy.
  Provisional benchmark: 1,000 visible record metadata items remain interactive;
  test 10,000-record history through server paging, not full browser downloads.
- Proposed performance target: local metadata route p95 under 500 ms on a declared
  reference machine, excluding server startup; record baseline before enforcing.
- Test role×tenant×action matrix, malformed schemas, reconnect/gap, stale edit,
  read-only disk, missing pricing, partial capture, cancel/timeout and unsupported
  backend versions. Backend/CLI/GUI fixtures must agree on canonical decisions.
- Existing GUI production build remains green; add component/e2e checks rather
  than treating TypeScript compilation as proof of UX correctness.

## 11. Outcomes and validation plan

Targets are hypotheses until measured with users:

1. At least four of five first-time users edit and exercise a mock in under five
   minutes using a reachable server with the documented agent/pipeline starter
   preloaded. Measure empty-install onboarding separately; Release A does not
   include pipeline creation or an isolated arbitrary-request composer.
2. In a seeded mismatch task, median time to identify the failed predicate under
   two minutes; compare with the current console baseline.
3. A complete captured failure becomes a CLI-reproducible regression artifact in
   under five minutes, with no secret-canary leakage.
4. Zero overwritten concurrent edits in stale-version tests; zero cross-tenant
   leaks in browser/API/export tests; complete keyboard paths for core workflows.
5. Repeat-run canonical output/assertion equality across supported OS fixtures;
   timing compared using declared tolerances, not byte-identical timestamps.

Do usability tests with developer, QA and administrator participants before
expanding beyond Release A. Instrument task success and error recovery locally
or opt-in; do not introduce undisclosed analytics.

## 12. Decisions for product approval

| Decision | Recommended default | What it unlocks |
|---|---|---|
| Scope of first release | Release A only; design key Release B journeys now | Bounded implementation with immediate value |
| Primary audience | Developers + QA, with read-only stakeholder views | Progressive complexity without a second product |
| Deployment | Existing Next.js sidecar, single configured server | No packaging rewrite or remote fleet dependency |
| Mutation model | Explicit apply to server; version checks; portable export | Safe authoring without automatic Git writes |
| Execution boundary | Release A explicitly uses active runtime; Release B adds isolated offline runs | Honest safety claims and staged implementation |
| Agent API compatibility | Additive conditional-write contract; disclose legacy unconditional writers | Avoid unannounced client breakage; exact route/version requires approval |
| Read-only stakeholders | Do not equate current viewer role with no mutations | A stricter role/log-deletion policy is a separate decision |
| Durable run storage | Decide schema, retention, limits and recovery before UX-11 | No implicit storage platform chosen by UI design |
| AI assistance | Optional later, off by default | Avoid privacy/cost dependency on core UX |
| R17–R19 | Preserve design-partner gate | Prevent UI work from silently authorizing MockStack |

## 13. AI-agent implementation contract

Implement one approved story at a time. First inspect current main, instructions,
dirty state and API/tests; this baseline may age. Write a short contract and test
plan before changing files. Reuse Go validators/engine methods rather than
duplicating behavior in TypeScript. Define API error/status/permissions before
designing the happy path. Use realistic backend-backed fixtures for every state.

For each PR provide: story ID, scope, API compatibility impact, permissions,
before/after screenshots from Claude's approved design, test evidence, limitations
and documentation. Do not claim a backend feature exists because a mock screen
renders it. Do not change the adoption gate, publish externally, merge historical
branches, introduce live inference or purchase services without explicit authority.

The current research deliverable changes documentation only. It does not start
an implementation loop or create a GitHub issue/PR.

## 14. Design handoff

Give Claude Design the [design brief](UI_TRANSFORMATION_DESIGN_BRIEF.md), this epic,
and the review evidence. Claude owns visual direction, responsive layouts,
interaction prototypes, component variants and design tokens. The implementation
agent owns API correctness, persistence, authorization, validation and tests.
Neither side should invent a backend capability to fill a screen.

## 15. Story readiness and release acceptance

Before each story starts, attach an API example, role/scope matrix, error states,
test fixture and approved design or explicit nonvisual designation. Split L stories
into contract, UI and verification slices; do not claim an entire L story complete
after only one layer. Suggested dependency order: UX-08 harness → UX-01 → UX-02/04/05;
UX-03 enables authoring; UX-06 follows UX-04 and UX-07 follows UX-01. Add the matching
UX-08 tests with each slice. Release A ships only when all eight stories pass.

| Gate | Required evidence |
|---|---|
| Empty and seeded installations | No dead-end actions; documented seeded first-success journey; no invented pipeline creation |
| Real wire contracts | Captured synthetic 200/422/401/403/404/503 fixtures; nanosecond conversion, nullable nodes, unsupported versions |
| Safe authoring | Concurrent/external changes, failed disk write, unknown fields and lost response preserve data or report uncertainty |
| Investigation/export | Inserts during paging, feed gaps, retention, unknown revisions/prices, redaction and HTML injection tests |
| Identity/scope | Every role and local mode, auth expiry/logout, forbidden cross-tenant reads/writes/exports, correct audit visibility |
| Accessibility/responsiveness | Keyboard and non-drag authoring; narrow/reflow/zoom layouts retain actions, not blanket read-only mode |
| Delivery/rollback | GUI CI plus relevant Go suites; compatible backend rollout, reversible feature flags and draft export on rollback |

Release B approval is separate from Release A. Each new storage/runner contract
must pass lifecycle and tenancy tests before its design is treated as shippable.
