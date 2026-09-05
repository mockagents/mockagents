# UI-01 Release A: design conformance audit of UX-01 / 03 / 04 / 05

Date: 2026-09-04
Baseline: `main` at `2b442c1` (Release A complete), working tree clean.
Design of record: `docs/epics/design/reliability-workbench/` (exported 2026-09-04).

## Why this exists

UX-08, UX-01, UX-03, UX-04 and UX-05 were built and merged **before** the approved
design was exported into the repository — the design tool was unreachable from the
implementing session, so those screens were built from the epic text alone. The
design export's own README says a reconciliation pass across them is expected.
This is that pass.

Scope is the four stories named above. UX-02, UX-06 and UX-07 were built against the
design and are out of scope; UX-08 is a harness, not a screen. Nothing here is a
Release B item.

Findings are written against `2b442c1` and describe it in the present tense. As
each is addressed the finding keeps its original text and gains a **Fixed** note
below it, so the delta stays readable rather than being rewritten into agreement
with the code. The verdict table below is the current status.

## Method

Read side by side: the four design sources (`Workbench Handoff.html`,
`Workbench Components.html`, `wb_shell.jsx`, `wb_mocks.jsx`, `wb_lab.jsx`) against
the shipped implementation (`gui/app/**`, `gui/lib/**`), and then against the real
backend for every design element that asserts a capability — because the handoff's
closing line is "Do not claim a backend feature exists because a prototype screen
renders it", and the reverse also holds: an element must not be dismissed as
unimplemented when the API already supports it. Five design elements turned out to be
blocked or wrong against this backend; those are §9, and they are why this audit is
not simply a to-do list.

## Classification

| Class | Meaning |
| --- | --- |
| **A** | Contradicts a binding rule — epic §10, handoff §3 (state names) or §6 (accessibility). Fix. |
| **B** | Design element absent; implementable against the API as it stands; changes what an operator can conclude. Fix. |
| **C** | Vocabulary, ordering or chrome. Low value, cheap. |
| **D** | Implementation deviates and is **defensible or better**. Record the deviation; do not "fix" it. |
| **X** | Not implementable as designed against this backend. Needs a decision — §9. |

## Verdict at a glance

| # | Finding | Story | Class |
| --- | --- | --- | --- |
| X-1 | Instrument strip is on 2 of 17 routes, not in the shell | all | **A** — fixed |
| X-2 | No `unsupported` / capability-gated state anywhere | UX-03 | B — fixed |
| X-3 | Agent deletion uses `window.confirm`, bypassing the destructive-dialog spec | UX-03 | **A** — fixed |
| X-4 | Five-variant state matrix is partial; no shared component | all | C |
| U1-1 | `/identity` merges the proposed `/capabilities` | UX-01 | D |
| U1-2 | Capability gating stops at the nav — catalog Delete and New agent are ungated | UX-01 | B — fixed |
| U1-3 | Identity probed per screen instead of once per navigation | UX-01 | C — fixed |
| U3-1 | No save receipt; the new revision the API returns is discarded | UX-03 | **A** — fixed |
| U3-2 | Conflict drops `currentRevision` and the legacy-writer disclosure | UX-03 | **A** — fixed |
| U3-3 | Diff step never says Apply changes the live runtime | UX-03 | B — fixed |
| U3-4 | No Export draft; nothing says drafts are browser-memory only | UX-03 | B — fixed |
| U3-5 | Editor has no offline state; a write to a dead server reports "unknown" | UX-03 | B — fixed |
| U3-6 | Inventory has no revision or persistence column | UX-03 | X → B — fixed |
| U3-7 | Editor header lacks revision / persisted / file chips | UX-03 | C |
| U4-1 | Gaps render as a banner, not as gap rows in the table | UX-04 | **A** — fixed |
| U4-2 | The gap has no time bounds | UX-04 | **A** — fixed |
| U4-3 | Empty state omits the window and the next step | UX-04 | B — fixed |
| U4-4 | "Bodies are not fetched until revealed" is false — they ship with the list | UX-04 | **X** |
| U4-5 | Capture completeness shown only when truncated | UX-04 | C |
| U4-6 | Implementation refuses "loaded N of N" | UX-04 | D |
| U5-1 | Absent nodes are not rendered as "not executed · unknown" | UX-05 | **A** — fixed |
| U5-2 | `blocked-missing-dependency` is not a state | UX-05 | B — fixed |
| U5-3 | No node inspector — a failed node's evidence cannot be read | UX-05 | B — fixed |
| U5-4 | No run→logs link | UX-05 | **X** |
| U5-5 | Session id not shown before the run | UX-05 | C |
| U5-6 | Pre-run banner does not name the definitions that will execute | UX-05 | C |

Six class-A findings. Four of the five most consequential (X-1, U3-1, U4-1/2, U5-1)
share one root cause: **the screens report state correctly but do not report the
evidence for it.** The state vocabulary from handoff §3 is present almost everywhere;
what is missing is the revision, the range, the absent node, the server context.

---

## 4. Cross-cutting

### X-1 — The instrument strip is not in the shell · **A**

The Components doc defines the strip as *"persistent context"* carrying server /
liveness / readiness / tenant+role / exec mode / revision / engine / freshness **on
every screen**, and the handoff's screen map puts it in the row `Shell + instrument
strip`. It is the approved hybrid direction's defining device.

Shipped: it is rendered by exactly two pages —
[`overview/page.tsx:83`](../../gui/app/overview/page.tsx#L83) and
[`reports/page.tsx:105`](../../gui/app/reports/page.tsx#L105).
[`Shell.tsx`](../../gui/app/Shell.tsx), the actual shell, does not render it, and
neither does [`layout.tsx`](../../gui/app/layout.tsx).

So on every screen this audit covers there is no server context at all. Concretely:
the agent editor offers **Apply** without stating which server, which tenant and role,
or whether the process is even ready; the run panel says "runs against active
configuration" without naming the configuration. Those are the two most expensive
mistakes available in Release A, and they are made on the screens with the least
context.

Fix: render it from `layout.tsx`. `getServerStatus()` and `getIdentity()` already
exist, and the layout already calls `getHealth()` on every navigation, so the cost is
one extra readiness probe rather than a new architecture. `InstrumentStrip` is a
server component and `Shell` is a client component, so it has to be passed as a slot
prop rather than imported inside `Shell`.

This subsumes U1-3 and is the prerequisite for U3-5.

**Fixed 2026-09-04.** The strip and offline bar are rendered by
[`layout.tsx`](../../gui/app/layout.tsx) and passed into `Shell` as an
`instrument` slot — a slot rather than an import because the strip is a server
component and `Shell` is a client component. The two pages that rendered their
own no longer do, and a browser test asserts `toHaveCount(1)` across nine
routes, so a page that adds its own back fails rather than showing two
disagreeing "last refresh" stamps.

U1-3 went with it: `getIdentity` and `getServerStatus` are now wrapped in React's
`cache()` ([`api.ts`](../../gui/lib/api.ts)), so the layout and a page that also
needs them share one call per render pass. Verified in the server log — an edit
page render issues exactly one `/api/v1/identity` even though both ask for it.
`getServerStatus` needed it for a second reason: it stamps `checkedAt` internally,
so two uncached calls would have put two different refresh times on one screen.

One thing was removed rather than added. The topbar carried an online/offline
pill built from a single health probe, which could read "online" beside a
NOT-READY strip — the exact conflation UX-02 exists to undo. The strip states
liveness and readiness separately and carries the engine version, so the pill is
gone; two tests keep it gone.

### X-2 — There is no `unsupported` state anywhere · B

The Components doc's state matrix has five mandatory variants; the fifth is
`unsupported` — *"Not enabled on this server version · capability-gated tag; reads
allowed, writes disabled"*. The Mocks screen demonstrates it with a disabled "Vector
collections" tab, and the lede commits to it in prose: *"Future resource kinds appear
here capability-gated — disabled with explanation, never hidden or dead."*

Nothing in `gui/` implements this variant. The nav's denied-item branch in
[`Shell.tsx`](../../gui/app/Shell.tsx) is close in spirit but expresses a *role
floor*, not *a capability this build does not have*.

Low urgency while there is one resource kind — but the design's claim is a promise
about how the console behaves when there is a second, and it is currently unfounded.

**Fixed 2026-09-05**, and against a real instance rather than a hypothetical
future kind. The pipeline routes mount only when the server starts with a
`kind: Pipeline` document, so on a server without one they 404 — and
`listPipelines` mapped that 404 to `[]`, rendering an absent capability as an
empty inventory and sending an operator to look for a Create button that cannot
exist. `listPipelineInventory` now distinguishes the two, and `/pipelines` and
Overview say which one they are looking at. Unreachable and unauthorized still
raise: folding those in would tell someone to change how the server was started
when the real problem is a credential or a dead process.

Only the positive direction is assertable in the browser — `/pipelines` is a
server component, so its fetch happens inside Next and `page.route` cannot reach
it. The 404 mapping is pinned as a unit test against the real helper.

### X-3 — Agent deletion bypasses the destructive-dialog spec · **A**

Handoff §6: *"Destructive dialogs: exact-name entry gates the action; destructive
button never auto-focused."* Components doc: *"Impact summary → irreversible
consequences → exact-name entry gates the destructive button. Focus trapped; Esc
cancels."*

Shipped — [`AgentCatalog.tsx:92`](../../gui/app/AgentCatalog.tsx#L92):

```
if (!window.confirm(`Delete agent "${a.name}"? It stops serving immediately.`)) return;
```

A native modal: no impact summary, no consequence list, no exact-name gate, and the
default button *is* focused, so a stray Enter deletes. Deleting an agent stops it
serving immediately and removes its backing file
([`agent_write_handlers.go:157`](../../internal/server/agent_write_handlers.go#L157));
for a runtime-only agent it is unrecoverable.

This is the cheapest class-A fix in the audit:
[`DangerConfirm`](../../gui/app/DangerConfirm.tsx) already implements the spec
exactly — focus trap, Esc, exact match with no trimming or case folding, safest
control focused — and is already used on three admin pages. Swap it in.

Secondary benefit: `window.confirm` is a blocking browser modal that jsdom stubs, so
the delete path currently has no component-level test that means anything.

### X-4 — The five-variant state matrix is partial and unshared · C

The Components doc is explicit that these are one component with five variants,
*"never a shared generic empty screen"*. Present: unreachable (logs, editor,
overview), unauthorized (401 copy, nav floors), truly-empty (but see U4-3), not-ready
(Overview only — which X-1 fixes globally). Absent: `unsupported` (X-2). Each is
hand-written per screen, so the copy patterns drift. A shared `<StateNotice
variant=…>` would make the matrix checkable rather than aspirational.

---

## 5. UX-01 — identity and capability contract

### U1-1 — Two proposed routes shipped as one · **D, keep**

The handoff proposes `/identity` *and* `/capabilities`. Implementation ships a single
`GET /api/v1/identity` whose capability list is derived from the routes the server has
actually mounted ([`identity_handlers.go`](../../internal/server/identity_handlers.go)),
so an advertised capability cannot 404. That was an explicit product decision during
implementation and it is stronger than the design. Record it; do not revisit.

### U1-2 — Capability gating stops at the nav · B

The handoff gates per screen, not just per destination: *"viewer sees explained
read-only"*, *"capability-gated kinds disabled with reason"*, *"role floors stated in
place of empty panels"*.

Done well on the two write surfaces that were built with it in mind: the editor
resolves `agents.write` into `canWrite`
([`agents/[name]/edit/page.tsx:66`](../../gui/app/agents/[name]/edit/page.tsx#L66))
and the run panel resolves `pipelines.run.write` into `canRun`
([`pipelines/[name]/page.tsx:30`](../../gui/app/pipelines/[name]/page.tsx#L30)). Both
correctly stay **enabled** when identity cannot be read and let the server refuse —
that is the right default and matches epic §10's "authorized by the server, not a
hidden button".

Missed on the catalog. The Delete button
([`AgentCatalog.tsx:121`](../../gui/app/AgentCatalog.tsx#L121)) and the New agent link
([`page.tsx:52`](../../gui/app/page.tsx#L52)) render unconditionally, so a viewer gets
a live Delete that fails with a 403 rendered as an error string inside the card. The
design's rule is *disabled with the reason stated* — not hidden, and not
live-and-failing.

### U1-3 — Identity is probed per screen · C

`getIdentity()` is called independently by `overview`, the agent editor and the
pipeline page, while `layout.tsx` separately calls `getAuthStatus()`. Same data, up to
four round trips per navigation. Folded into X-1's layout change.

---

## 6. UX-03 — mocks inventory and agent authoring

### U3-1 — There is no save receipt · **A**

The design has a **Save receipt** card with two variants (`persisted` ok /
`runtime-only` warn) listing *new revision · runtime · file · audit ref*, and states
the rule that makes it necessary: *"Appears only after server confirmation — no
optimistic 'saved' toast exists in this system."* The handoff's acceptance walkthrough
requires the operator to *"apply → read persisted receipt"*.

Shipped: [`ResultBanner`](../../gui/app/agents/[name]/edit/AgentEditor.tsx#L378) is a
one-line banner. It gets the hard part right — it distinguishes persisted from
runtime-only, it appears only after the server answers, and there is no optimistic
toast anywhere in the console. What it discards is the evidence: **the new revision is
returned by the API and thrown away.** `ConditionalSaveResult` carries `revision`
([`api.ts:1017`](../../gui/lib/api.ts#L1017)); the banner never renders it.

That revision is the operator's handle on what they just did — it is what a subsequent
`If-Match` is based on, and it is how the change is correlated with the audit log. Its
absence is why the runtime-only variant currently reads as a warning rather than as a
receipt for a specific version.

Revision, runtime state and file are all in hand today. An audit reference is not (it
would need a correlated `GET /api/v1/audit`, which is admin-floored) — so state that
omission rather than inventing the field.

**Fixed 2026-09-05.** The banner is now a Save receipt card with the two variants
the design specifies, listing new revision / runtime / file, and stating plainly
that no audit reference is returned rather than leaving the row out. A browser
test compares the revision the receipt shows against the ETag the server serves
immediately afterwards, so a receipt naming the wrong version fails the gate —
that matters more than the wording, because the next `If-Match` is built from it.

### U3-2 — The conflict card drops the facts and the disclosure · **A**

The design's conflict card states the revision comparison in its first sentence —
*"the server revision is now `c1d9` (yours was based on `a41f`)"* — shows two receipt
lines (their change with actor and time, your draft with its base), and carries a
disclosure banner: *"Legacy unconditional writers can still bypass If-Match."* Handoff
§8 makes that disclosure binding regardless of which conditional-write route was
chosen.

Shipped — [`AgentEditor.tsx:124`](../../gui/app/agents/[name]/edit/AgentEditor.tsx#L124):

```
setConflict({ message: r.message });
```

`r.currentRevision` is on the result object
([`api.ts:1019`](../../gui/lib/api.ts#L1019)) and is dropped on the floor. The card
shows neither revision, and there is no legacy-writer disclosure anywhere in `gui/`.

The behaviour underneath is right and should not be disturbed: both versions are
genuinely preserved, "Compare with current" replaces the *base* only so the diff then
shows draft-vs-landed, and discarding is explicit.

**One deliberate divergence to keep.** The Components doc says the conflict card is
`role="alertdialog"`. The implementation uses a non-modal `role="alert"` banner
([`AgentEditor.tsx:342`](../../gui/app/agents/[name]/edit/AgentEditor.tsx#L342)).
`alertdialog` denotes a *modal* dialog with trapped focus; applying it to an inline
card that traps nothing would announce a modal that is not there, which is worse for a
screen-reader user than the honest `alert`. Either promote the card to a real modal
(design-faithful) or keep the banner and record the deviation — but the content gaps
above need fixing either way.

**Fixed 2026-09-05.** `currentRevision` is carried through, and both revisions
are shown side by side and named; a server that does not name one renders as
*unknown, not unchanged*. The legacy-writer disclosure is in, worded as what it
actually is — a precondition rather than a lock, which a client writing without
`If-Match` can still bypass. The `role="alert"` banner was kept over
`alertdialog` for the reason stated above; that deviation is now deliberate and
recorded rather than incidental.

### U3-3 — The diff step never says Apply changes the live runtime · B

The design's diff card carries a warning between the diff and the Apply button:
*"Applying changes the active runtime. The pipeline support-triage references this
agent; its next run will use the new response. No isolated preview exists in Release
A."*

[`DiffPanel`](../../gui/app/agents/[name]/edit/AgentEditor.tsx#L446) has no such
banner. This is the same active-runtime honesty UX-05 does state before a run — the
editor is the other half of it, and the half with the larger blast radius, because an
edit affects every future run rather than one. The referencing pipelines are
computable from `listPipelines()`.

**Fixed 2026-09-05.** The diff step now names the pipelines whose next run would
use the change. `listPipelines()` returns counts rather than refs, so each
definition is read — bounded by how many pipelines a server has, since they are
hand-authored documents rather than user data. A failure to read any one of them
makes the whole answer *unknown* rather than silently shorter: an empty list and
an unreadable list are different facts, and only one of them means nothing
depends on this agent.

### U3-4 — No Export draft, and nothing says drafts are ephemeral · B

Handoff §8 settles this: *"Release A designs assume browser-memory drafts + explicit
export; a durable draft store needs retention/encryption policy sign-off."* The design
therefore ships an **Export draft** button and a definition-state card whose last row
reads *"durable drafts — not in Release A"*.

Neither exists. A draft dies with the tab and the operator is never told — which also
removes the escape hatch the design offers a viewer (*"You can still run pipelines in
Test Lab and export drafts"*) and the one it offers during an outage.

**Fixed 2026-09-05.** Export draft is present and available to everyone — a
viewer who cannot apply, and an operator whose server is down. The browser-only
statement is unconditional rather than appearing once something has been typed.
A blocked download reports itself instead of silently doing nothing, which
matters because the alternative is an operator believing a draft was saved when
nothing left the page.

### U3-5 — The editor has no offline state · B

The design disables Validate and Apply when unreachable and says so: *"Server
unreachable — your draft is preserved locally (this session)… Nothing is lost if you
keep this tab open; use Export draft to save it externally."*

The editor has no liveness awareness. Both controls stay enabled, and an Apply against
a stopped server returns `status: "error"`, rendered as **"Outcome unknown."**
([`AgentEditor.tsx:439`](../../gui/app/agents/[name]/edit/AgentEditor.tsx#L439)). For a
write that never left the machine, "unknown" is defensible but needlessly alarming —
and the strip from X-1 already knows the server is down.

**Fixed 2026-09-05.** Validate, Review and Apply are disabled while the server is
unreachable, each with a reason naming the server rather than the document or the
role. The read-only-for-your-role banner is suppressed at the same time: two
banners blaming different things for one symptom sends someone hunting for a
permission that is not the problem. Editing and exporting stay available, because
the draft is the user's and does not depend on the server.

### U3-6 — The inventory has no revision or persistence column · X → B

Design table: *name · protocol · scenarios · tools · **revision** · **persistence***,
with `persisted` as a per-row badge. Shipped: a card grid with name / model / protocol
/ scenarios / tools / tags ([`AgentCatalog.tsx`](../../gui/app/AgentCatalog.tsx)).

`AgentSummary` exposes neither field, in Go
([`handlers.go:85`](../../internal/server/handlers.go#L85)) or TypeScript
([`api.ts:26`](../../gui/lib/api.ts#L26)) — so this reads as blocked. It is not. The
registry tracks a per-agent source path (`Registry.Source(name, tenantID)`, used at
[`agent_revision.go:154`](../../internal/server/agent_revision.go#L154)), and
`revisionFor` already derives from it exactly what the design's two columns need: an
effective revision hash, plus `SourceMissing` / `SourceUnparseable` / drift. Adding
`revision`, `persisted` and `file` to `AgentSummary` is a small additive change;
fetching them per row from the client instead would be N requests and should not be
done.

Worth doing because it is the inventory-level view of U3-1: without it, "which of my
agents survive a restart?" can only be answered one agent at a time.

**Fixed 2026-09-05.** `AgentSummary` now carries `effective_revision`,
`persistence` and `file`. Persistence has three values rather than a boolean,
because a tracked file that has been deleted out of band is neither persisted
nor runtime-only — it is still serving and will not come back, which is the
state worth surfacing. The revision is the EFFECTIVE one, named as such: the
ETag also covers the backing file, so publishing it here would mean reading
every agent's file on every listing, and a caller mistaking one for the other
would send a precondition that always fails. An older server omits the fields
entirely, and the catalog renders nothing rather than guessing "runtime".

The spec's `AgentSummary` schema was already wrong before this — it documented
`tools_count` / `scenarios_count` / `status`, none of which exist, and a
two-value protocol enum. Corrected alongside.

Related, lower value: the design puts Agents and Pipelines in one **Mocks** screen with
tabs. The console keeps them as separate routes, deliberately — `Shell.tsx` records
that renaming routes moves every deep link and epic §5 wants redirects, so it is its
own slice. That deferral is sound; it is noted here so it stays a decision rather than
an oversight.

### U3-7 — Editor header chrome · C

The design puts `revision a41f`, a `persisted` badge and `agents/support-starter.yaml`
beside the title, and tags an unsaved draft `draft · unsaved`. The implementation puts
the revision in a hint below the diff panel
([`AgentEditor.tsx:321`](../../gui/app/agents/[name]/edit/AgentEditor.tsx#L321)) and
never shows the file path. It also opens on **Form** where the design opens on YAML —
harmless, but the design's ordering follows its own acceptance walkthrough.

---

## 7. UX-04 — Investigate

### U4-1 — Gaps are a banner, not gap rows · **A**

The design puts the gap **in the table, between the rows it separates**:

```
<tr class="gap-row"><td colspan=6>
  ⚠ unresolved gap · 13:58:41Z – 14:20:06Z · feed dropped, bounded recovery
  incomplete — records in this range may exist but are not shown
</td></tr>
```

and the Components doc lists *"gap rows for unresolved log ranges"* as a property of
the resource table itself, alongside sticky headers and row selection.

Shipped: a banner above the table
([`LogsConsole.tsx:404`](../../gui/app/logs/LogsConsole.tsx#L404)); `.gap-row` does not
exist in `globals.css`.

The difference is not cosmetic. The rows either side of a gap are adjacent on screen
and read as consecutive traffic, while the warning that they are not scrolls out of
view. An operator scanning for "what happened at 14:05" sees an unbroken sequence with
no marker at the discontinuity. Handoff §3 names this state `gap-unresolved` precisely
so it cannot be silently spliced, and the design's own copy says *"never silently
spliced"*.

The rest of the gap machinery is right and should not be touched: `interpretRecovery`
([`logfeed.ts:85`](../../gui/lib/logfeed.ts#L85)) treats a full recovery page as
*cannot prove the gap is closed*, which is the honest reading of a bounded fetch with
no cursor.

### U4-2 — The gap has no time bounds · **A**

The design states the range. `GapState`
([`logfeed.ts:61`](../../gui/lib/logfeed.ts#L61)) carries only a prose `reason`, and
the unresolved message quotes a *count* ("More than 200 requests arrived…") but never a
time.

Without bounds an operator cannot tell whether the gap overlaps the incident they are
investigating, which is the only reason the state exists. Both ends are already
available client-side: the timestamp of the last row seen before the drop, and the
oldest row in the recovery page.

### U4-3 — The empty state omits the window and the next step · B

Components doc, truly-empty variant: *"No records in this window (window shown)"* —
dashed border **+ window label + next step**. The design's copy also does the teaching:
*"The log store is reachable and empty — distinct from unreachable (connection error)
and unauthorized (403)."*

Shipped ([`LogsConsole.tsx:249`](../../gui/app/logs/LogsConsole.tsx#L249)): the live
variant has a next step ("Send a request to the MockAgents server"), the filtered
variant has one ("Widen it, or clear it"), and the plain variant is bare — *"No
interactions recorded yet."* No window, no next step.

Credit where due: the three states the design insists must be distinct **are** distinct
here. Unreachable and unauthorized are caught at the page level
([`logs/page.tsx:62`](../../gui/app/logs/page.tsx#L62)) and never render as "no
traffic". That is the substance of the requirement; this is the copy.

### U4-4 — "Bodies are not fetched until revealed" is not true · **X**

See §9.1.

### U4-5 — Capture completeness is shown only when truncated · C

The design has a `capture` row that always renders — `complete` or `truncated at
64 KB`. The implementation renders a banner only when `row.truncated`, so *complete* is
communicated by the absence of a warning. Defensible (the field is a boolean the server
sets, so absence really does mean complete) but it is the one place on this screen
where silence carries meaning.

### U4-6 — Refusing "loaded N of N" · **D, keep**

The design's window chip reads `loaded 6 of 6`. The implementation deliberately refuses
any "N of M" because the API returns no total
([`LogsConsole.tsx:378`](../../gui/app/logs/LogsConsole.tsx#L378)) and shows `showing
the N most recent in this window — older requests exist` instead. That is more honest
than the design and directly serves the design's own "unknown never zero" rule. Keep.
The *window* itself (since/until) is knowable and should be added per U4-3.

---

## 8. UX-05 — Test Lab

### U5-1 — Absent nodes are not rendered as unknown · **A**

The clearest violation in the audited set, because absent-as-unknown is the design's
central rule and this is the screen it was written for.

The design's partial-run result appends a row for every node that did not execute —
badge `not executed · unknown`, latency `—` — and the banner spells out why: *"the
summarizer node is **absent — unknown, not zero**"*. The handoff's screen map states it
as the acceptance condition: *"422 partial shows nodes + absent-as-unknown."*

Shipped: `RunEvidence`
([`RunPanel.tsx:195`](../../gui/app/pipelines/[name]/RunPanel.tsx#L195)) renders only
the nodes the server returned and never mentions the ones it did not. The banner says
*"The nodes that did complete are shown below"* — true, and it stops there.

Verified against the backend rather than assumed: `runSequential`
([`pipeline.go:113`](../../internal/engine/pipeline.go#L113)) appends the failing node's
result then returns, so every downstream node is genuinely missing from `result.nodes`.
This is not hypothetical.

Implementable today: the page already has the full node list — `pipeline.spec.agents` is
passed to `DAGViewer` at
[`pipelines/[name]/page.tsx:91`](../../gui/app/pipelines/[name]/page.tsx#L91) — so the
absent set is definition order minus returned node ids.

### U5-2 — `blocked-missing-dependency` is not a state · B

Epic §10 and handoff §3 both name it; the design renders a dedicated **Run blocked**
card. `PipelineRunOutcome` ([`api.ts:873`](../../gui/lib/api.ts#L873)) has `ok |
partial | denied | invalid | unavailable | unknown` — a missing agent falls into
`partial` and is indistinguishable from a scenario that failed to match.

Distinguishable at the wire: a missing ref produces `ErrAgentNotFound` wrapped as
`pipeline %q node %q: agent not found: %q`
([`pipeline.go:302`](../../internal/engine/pipeline.go#L302)) inside the 422. String
sniffing would work and is brittle; adding a `code` field to `pipelineRunError`
([`pipeline_handlers.go:121`](../../internal/server/pipeline_handlers.go#L121)) is
small, additive and typed. Prefer the latter.

**Do not copy the design's wording for this state** — see §9.2.

### U5-3 — There is no node inspector · B

The design devotes a panel to the selected node: the full response as JSON, and for a
null response an explanation of what null means. The implementation truncates
`response.content` to 120 characters inside a table cell
([`RunPanel.tsx:275`](../../gui/app/pipelines/[name]/RunPanel.tsx#L275)) with no way to
see the rest, no tool calls, and nothing beyond the scenario name.

The handoff's acceptance walkthrough is *"find failed node (rag-lookup, null response)
→ export evidence"*. You can currently find it. You cannot read it.

**Fixed 2026-09-05.** Selecting a node opens an inspector with its full response
as JSON, bounded at 64 KB with a clipped notice rather than an unbounded render.
Three states are distinguished, because they mean different things: a response
the engine produced, a node that ran and produced none, and a node that never
ran. The middle one — the failure the design walks through — explains that null
is not an empty answer, and stops short of naming which predicate failed,
because the server does not report that. Selection is a button rather than a row
handler, so it works from the keyboard.

### U5-4 — No run→logs link · **X**

See §9.3. The implementation's current hint is correct and should be kept.

### U5-5 — Session id is not shown before the run · C

The design shows a read-only Session ID field with the hint *"fresh per run — avoids
conversational reuse; does not pin definitions"*. The implementation mints it at click
time ([`RunPanel.tsx:62`](../../gui/app/pipelines/[name]/RunPanel.tsx#L62)) and reveals
it only afterwards. The prose is already correct in the pre-run banner; only the value
is late. Showing it beforehand lets an operator have a log filter ready before
committing to a stateful action.

### U5-6 — The pre-run banner does not name what will execute · C

The design lists the exact definitions and revisions that will run — *"support-starter
b07e, rag-agent 77c0, summary-writer b2d8"* — turning "recent edits apply immediately"
from a warning into a checkable statement. The implementation's banner
([`RunPanel.tsx:86`](../../gui/app/pipelines/[name]/RunPanel.tsx#L86)) is correct but
generic. Depends on U3-6 exposing per-agent revisions.

---

## 9. Design elements that are wrong or blocked against this backend

These must not be implemented as drawn. Each needs a decision.

### 9.1 — Bodies cannot be "fetched only on reveal" · UX-04

The design states it twice: *"Bodies load only on explicit reveal"* and *"Bodies are not
fetched until revealed."* That is a **fetch** boundary — a privacy property.

`GET /api/v1/logs` returns bodies inline. `InteractionLog` carries `request_body` and
`response_body` ([`storage/models.go:13`](../../internal/storage/models.go#L13)) and the
list query populates both
([`storage/sqlite.go:282`](../../internal/storage/sqlite.go#L282)). Every body in the
window has therefore already crossed the network and is sitting in the client's memory
and in the server-rendered payload before anyone clicks Reveal.

The implementation's reveal is a **display** boundary, and its copy is accurate about
that — *"Bodies are hidden by default — they can contain whatever a client sent"*
([`LogsConsole.tsx:652`](../../gui/app/logs/LogsConsole.tsx#L652)). It does not
overclaim. But the design's property is unmet, and adopting the design's wording would
make the console assert a privacy guarantee it does not provide.

`MOCKAGENTS_LOG_BODIES` (SEC-05) is not the answer: it is a server-wide capture policy,
not a per-request projection.

Options: **(a)** keep the display boundary and the honest copy — no work; **(b)** add a
`?fields=meta` projection to the list route so bodies load per row on reveal, making the
design's claim true. (b) is small and additive. Until then (a) is the correct behaviour
and the design text is the thing that is wrong.

### 9.2 — "The run was not started" is false for a missing dependency · UX-05

The design's Run-blocked card reads: *"Pipeline references an agent that is not loaded:
`rag-agent`. **The run was not started.**"*

The run **is** started. `invokeNode` resolves the ref at node-execution time, not at
submission ([`pipeline.go:302`](../../internal/engine/pipeline.go#L302)), so for a
sequential pipeline every node before the missing one has already executed and advanced
its session state ([`pipeline.go:113`](../../internal/engine/pipeline.go#L113)). The
design's sentence is only true when the missing ref is on the first node.

Shipping it verbatim would tell an operator nothing happened when something did — and
the whole point of the state is to let them decide whether to re-run. Suggested copy:
*"Stopped at node `X` — agent `Y` is not loaded. Nodes before it ran and their session
state has advanced."*, with the completed nodes shown as evidence.

### 9.3 — "Check logs for this session" cannot work · UX-05

The design's unknown-outcome card offers a **Check logs for this session** button.

It would return nothing. The log filter is exact equality — `AND session_id = ?`
([`sqlite.go:228`](../../internal/storage/sqlite.go#L228)) — while a pipeline run logs
each node under a *scoped* session, `fmt.Sprintf("%s::%s::%s", sessionID, pipelineName,
node.ID)` ([`pipeline.go:307`](../../internal/engine/pipeline.go#L307)). So
`?session_id=gui-run-abc` never matches `gui-run-abc::support-triage::router`.

The implementation's hint says exactly this — the results *"are not linked to
interaction-log entries… so finding the corresponding requests means searching the logs
by time or agent"*
([`RunPanel.tsx:236`](../../gui/app/pipelines/[name]/RunPanel.tsx#L236)). It is right,
and it must not be "fixed" into a button that silently returns an empty list.

Unblocking is cheap and worth doing: a `session_prefix` filter (`session_id LIKE ? ||
'%'`, index-friendly against `idx_logs_session`) turns the design's button into a
working link and would make run-to-evidence navigation real for the first time.
Recommended as its own small backend slice.

### 9.4 — Dark semantic tokens are still unsigned-off · all

Handoff §4 proposes brightened `--sr-success-fg` / `-warning-fg` / `-danger-fg` /
`-info-fg` under `[data-theme="dark"]`, because the light-canonical values fail 4.5:1 on
dark surfaces, and §8 lists design-system owner sign-off as **outstanding**. The console
uses these tokens throughout in both themes. This audit did not measure contrast — the
browser sweep runs axe on both themes, which covers rendered contrast for what it can
see, but the token decision itself is still open upstream. Flagged so it is not assumed
closed.

### 9.5 — The prototype's own defect, for the record

`wb_mocks.jsx` `Conflict` calls an undefined `setExtChange` in both button handlers. The
export README already notes it. Nothing to do; it is not a design decision and must not
be mirrored.

---

## 10. Suggested order

Grouped so each is one reviewable slice with its own tests.

1. ~~**X-1** — strip into the shell.~~ **Done 2026-09-04** (took U1-3 with it). Unblocks U3-5, subsumes U1-3, and puts server context
   on every audited screen. Largest single gain.
2. ~~**X-3** — `DangerConfirm` for agent deletion.~~ **Done 2026-09-05** (took U1-2's
   catalog half with it). Removed an accidental-Enter data-loss path and made the
   delete path testable.
3. ~~**U5-1 + U5-2**~~ **Done 2026-09-05** — absent nodes as `not executed · unknown`, and the blocked state with
   §9.2's corrected copy. The design's core rule, on the screen it was written for.
4. ~~**U4-1 + U4-2 + U4-3**~~ **Done 2026-09-05** — gap rows with time bounds, and the empty-window copy.
5. ~~**U3-1 + U3-2**~~ **Done 2026-09-05** — save receipt with the new revision; conflict card showing both
   revisions plus the legacy-writer disclosure. Presentation over machinery that is
   already correct.
6. ~~**U3-3 + U3-4 + U3-5**~~ **Done 2026-09-05** — active-runtime warning on the diff,
   Export draft, editor offline state.
7. ~~**U5-3, X-2**~~ **Done 2026-09-05** — node inspector, `unsupported` variant.
   (U1-2's catalog half shipped with X-3.)
8. Backend slices, if approved: `session_prefix` log filter (§9.3), `?fields=meta` log
   projection (§9.1). (`code` on `pipelineRunError` shipped with U5-2; `AgentSummary`
   revision/persistence shipped as U3-6 on 2026-09-05.)
9. **C-class** — U3-7, U4-5, U5-5, U5-6, X-4.

## 11. What this audit did not cover

- UX-02, UX-06, UX-07 — built against the design; not in scope.
- Contrast measurement of the proposed dark semantic tokens (§9.4).
- `workbench.css` value-level porting (radii, spacing scale, type ramp). This audit is
  about behaviour and state, not visual fidelity; a separate pass against `tokens.css`
  and the Components doc's type/spacing table would be needed to claim visual
  conformance, and nothing here should be read as claiming it.
- Performance targets from epic §10 (1,000-row interactivity, p95 metadata route).
