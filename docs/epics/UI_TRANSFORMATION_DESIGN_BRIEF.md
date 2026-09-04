# Claude Design brief: MockAgents Reliability Workbench

Date: 2026-09-02  
Status: Proposed design input, not an instruction to implement or deploy

## Brief to paste into Claude Design

Design an evolution of the existing MockAgents developer console into a
**Mock Reliability Workbench**. It is a local/self-hosted deterministic AI-protocol
testing product, not a consumer chatbot or model-quality evaluation platform.

Read [the implementation epic](UI_TRANSFORMATION_EPIC.md). Preserve its capability
boundaries. Design Release A in detail and prototype the signature Release B
journey. No production data or credentials should be used in design fixtures.

The product promise: “Explain an agent failure, reproduce it, and make it a
regression test.” The core journey is configure → validate → exercise → explain
→ improve → share evidence. Design for developers, QA authors, read-only domain
reviewers, tenant admins and platform operators. Prefer progressive disclosure
over a separate simplified product for each persona.

## Existing implementation to preserve

- Next.js/React with server-rendered pages and small client islands.
- Light/dark theme and a shared CSS-token shell.
- Agent catalog/detail, YAML validate/save, agent delete/reload.
- React Flow pipeline editor with ETag conflict handling.
- Request logs with SSE, cost estimates, audit and tenant/key management.
- Do not design these as net-new capabilities or replace React Flow gratuitously.

## Information architecture

Overview, Mocks, Test Lab, Investigate, Reports, Administration. Keep server,
tenant, role, revision and known execution mode visible. Distinguish offline,
unreachable, not-ready, unauthorized and unsupported; never use one “empty” screen
for all of them. Keep current deep links viable.

## Deliverables

1. Two distinct visual directions with rationale, then one recommended direction.
2. Navigation and object model with keyboard command-palette behavior.
3. High-fidelity desktop and narrow/reflow layouts. Prioritize triage on mobile,
   but preserve authorized authoring through stacked forms/list alternatives;
   do not make browser zoom or narrow width silently disable editing.
4. Clickable prototype for the two journeys below, including failure recovery.
5. Component inventory: resource table, filter bar, code/form editor, diff view,
   request inspector, execution list, graph, status badge, error summary,
   confirmation dialog, data-completeness banner, report cover and empty states.
6. Design tokens for type, spacing, color, density, focus, motion and elevation;
   annotate contrast targets and text/non-color status equivalents.
7. Handoff annotated with story IDs, real vs proposed APIs, and exact state names.
   Do not deliver only a beautiful static dashboard.

## Journey 1 — safe first success (Release A)

Developer connects to existing local server → sees readiness and starter → opens
an agent → edits response using form/YAML → validates → reviews diff → applies →
sees persisted vs runtime-only receipt → explicitly runs a preconfigured pipeline
against active runtime → inspects node result → exports bounded run evidence.
The prototype uses a documented agent/pipeline starter. Also design empty-install
onboarding: pipeline creation is not part of Release A; provide setup guidance.
Do not promise an arbitrary-request composer or isolated test before Release B.

Include: invalid schema, viewer denied a configuration edit, stale-edit conflict, disconnected server,
runtime-only save, missing fixture, pipeline partial failure and empty log store.
Drafts survive a failed action; conflict resolution does not discard work silently.
Show active-runtime effects before Run. A fresh session is not isolated configuration.
Unknown outcome after disconnection must not offer an automatic retry. The real
pipeline response contains `nodes` and nanosecond `latency`, not `node_sequence`;
422 may include partial `result`. Null responses and parallel definition order are
not a chronological trace. Do not fabricate log links, faults or historical revisions.

## Journey 2 — failure to regression (Release B prototype)

QA opens failed request → reads session and effective chaos → opens Match Explain
→ sees the exact failing predicate → opens an isolated reproduction draft → adds
an expected assertion → compares baseline/fault variant → exports regression
capsule. An incomplete capture disables exact replay with a useful explanation.

The explanation must visually distinguish historical execution evidence from
current-configuration re-evaluation. The design cannot pretend the proposed
preview/run/history APIs already exist.

## Mandatory additional screens/states

- Mocks inventory: Agents/Pipelines now, future resource kinds capability-gated.
- Log explorer: metadata-first, safe body reveal, truncated capture, dropped feed,
  reconnect, selected row preserved, shareable non-sensitive filters.
- Reports: scope, revision, generation time, estimate vs measurement, completeness,
  omissions and export-content preview; missing information is “unknown,” not zero.
  Release A exports loaded snapshots, not complete-history or server-attested reports.
- Admin: role-aware controls; exact-name confirmation for tenant deletion,
  one-time key display, locked-out/recovery state, audit actor/target timeline.
  Audit requires admin; tenant management and quota changes require platform.
  Viewer can run pipelines and perform some existing actions: it is not a strict
  read-only role. Do not add clear-log controls or redesign role permissions.
- Cost page: “estimated upstream cost for captured requests,” not guaranteed
  savings. Sampled aggregates must look visibly partial.
- Accessible graph: list/table alternative for node order and edge changes;
  every drag operation has non-drag and keyboard alternatives.

## Design constraints

- Dense and calm, not decorative: useful table scanning before chart abundance.
- No giant topology map on the default home page.
- No fake activity, invented percentages, magical reliability score or implied
  model intelligence. Use realistic synthetic fixtures with explicit provenance.
- No public-share button, automatic Git push, live-provider switch, universal
  stop-server button or automatic chaos toggle in the initial release.
- Destructive actions are reviewable; sensitive text is not copied automatically.
- WCAG 2.2 AA target; visible focus, contrast, reduced motion, accessible labels,
  keyboard recovery and adequately sized targets. Keep error messages actionable.
- Mobile priority is triage; authoring is desktop-first but retains accessible
  non-drag alternatives at narrow widths and zoom.
- MockStack topology and cross-protocol causal traces stay gated by R17–R19.

## Prototype acceptance test

A first-time developer can identify the connected server, create a valid draft,
understand whether a save is durable, run a supported test and locate the failure
without facilitator explanation. A viewer cannot mistake disabled authoring for
a broken service. An administrator understands what deletion destroys before
confirming. A reviewer can tell measured evidence from missing or inferred data.

Present unresolved product decisions explicitly, rather than deciding new backend
features through visual design. Styling may change; the safety and data contracts
in the epic must not.

## Revision 2 handoff acceptance

Annotate every screen with Release A/B, story ID, required role/capability, data
source and unknown/unavailable behavior. Global server revision, guaranteed feed
backfill and universal read-only viewer are not existing capabilities. Close stale
tenant data and live feeds when identity changes. Preserve action focus, announce
errors and show unresolved log gaps rather than a false complete timeline.

Provide realistic synthetic API fixtures for happy, partial, denied, stale and
unsupported states. Release B prototypes must be visibly marked as proposed.
Include design tokens and component states, keyboard/focus order, responsive
behavior, acceptance walkthroughs and open decisions. Design sign-off does not
approve new roles, persistence technology, external integrations or implementation.
