# Mock Reliability Workbench — approved Release A design (exported)

Verbatim export of the approved Claude Design project for **EPIC UI-01 Release A**
(`claude.ai/design/p/799d7d75-5789-455d-953e-8f091350f80f`, project `mock-agents`),
pulled 2026-09-04 so the design is readable from any session — including headless
and SDK runs that cannot reach Claude Design.

**These files are reference material, not shipping code.** Nothing here is built,
linted, or imported by the Next.js console in `gui/`. Do not wire them into the
build.

## Files

| File | What it is |
| --- | --- |
| `Reliability Workbench.html` | Clickable prototype. Open directly in a browser; the "Prototype states" panel (bottom-right) switches role / connection / install / theme. |
| `workbench.css` | Working stylesheet — every component, responsive reflow, focus, reduced motion. Directly portable values. |
| `tokens.css` | Design-system tokens the prototype consumes (light `:root` + `[data-theme="dark"]` mirror). |
| `wb_data.jsx` | Synthetic fixtures with **real wire semantics**: nanosecond latencies, nullable node responses, 422 partial `result` + `error`, chaos fields, truncation flags. |
| `wb_shell.jsx` | Shell, instrument strip, prototype-state panel, Overview (UX-02). |
| `wb_mocks.jsx` | Mocks inventory + agent authoring flow (UX-03). |
| `wb_lab.jsx` | Test Lab runs (UX-05) + Investigate log explorer (UX-04). |
| `wb_admin.jsx` | Reports (UX-06) + Administration (UX-07). |
| `Workbench Handoff.html` | **Read this first.** Screen→story map, exact state names, proposed token additions, responsive + a11y requirements, acceptance walkthrough, and the unresolved decisions. |
| `Workbench Components.html` | Component inventory, token/contrast spec, empty-and-error state matrix, keyboard and focus order. |
| `Workbench Proposal.html` | IA, object model, primary journey, direction rationale (approved: hybrid — Worksheet body + Direction A instrument strip). |

Not exported (superseded exploration): `Direction A - Instrument.html`,
`Direction B - Worksheet.html`, `MockAgents Console.html`, and the older
`views_*.jsx` / `primitives.jsx` / `icons.jsx` / `app.css` set.

## Viewing the prototype

Open `Reliability Workbench.html` in a browser. It loads React 18 UMD + Babel
standalone from unpkg, so it needs network access, and the `<script src="…jsx">`
tags need an HTTP origin rather than `file://` — serve the folder, e.g.:

```
python -m http.server 8000 --directory "docs/epics/design/reliability-workbench"
```

## Reading it against the code

- Approved direction is the **hybrid**: Direction B "Worksheet" body with
  Direction A's always-on instrument strip. Dark is the default; light is supported.
- The state vocabulary in Handoff §3 is binding. Notably: no "saved" indication
  before server confirmation, and absent data is **unknown, never zero**.
- Handoff §8 lists decisions that must not be implemented by inference — the
  agent conditional-write route, durable drafts, the log recovery cursor, cost on
  Overview, and the proposed dark semantic tokens.
- Handoff §4 proposes brightened dark-mode semantic foregrounds
  (`--sr-success-fg` / `-warning-fg` / `-danger-fg` / `-info-fg`) because the
  light-canonical values fail 4.5:1 on dark surfaces. Design-system owner sign-off
  is still outstanding.
- Implementation order (epic §15): UX-08 → UX-01 → UX-02/04/05 → UX-03 → UX-06 → UX-07.
  UX-08/01/03/04/05 are already merged and were built **before** this design landed,
  so a reconciliation pass across those screens is expected.

## Known quirks in the exported prototype

`wb_mocks.jsx` `Conflict` calls an undefined `setExtChange` in both button
handlers, so the conflict-state buttons throw when clicked. Preserved verbatim
rather than patched — it is a prototype defect, not a design decision.
