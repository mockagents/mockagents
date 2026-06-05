# REF-07 — GUI Pipeline Editor (drag-to-rewire) — Design / Scope

**Status:** Scoped (design), not started · **Priority:** P1 · **Estimate:** 13 pts
**Date:** 2026-06-05 · **Supersedes the "known gap" row in PROGRESS.md §6 / sprint-backlogs.md**

This document scopes the drag-to-rewire editor for `kind: Pipeline`. It is the
design hand-off; implementation is a separate slice (or several).

## 1. Decisions locked in

Two pivotal choices were made up front because they size the whole feature:

| Decision | Choice | Implication |
| -------- | ------ | ----------- |
| **Where edits go** | **Server write-back to disk** | New write endpoint that validates → writes YAML into the agents dir → audits → hot-reloads. The server now mutates the user's working tree (it was read-only before). |
| **DAG canvas** | **React Flow (`@xyflow/react`)** | One new GUI dependency; drag/connect/pan/zoom/selection come for free. React 19 compatible. |

The alternative "export-only" and "extend the hand-rolled SVG" paths were
considered and rejected for this slice.

## 2. Goal & non-goals

**Goal.** From `gui/app/pipelines/[name]`, an editor lets an operator drag
nodes, draw/delete edges (with `when_contains` guards), add/remove agent nodes,
validate live, and **Save** — persisting the edited Pipeline YAML back to its
source file so hot-reload makes it live.

**In scope**
- Interactive canvas for the `graph` topology (the only topology with editable edges).
- Read + lightly edit `sequential` / `parallel` (reorder / add-remove nodes; no free edges).
- Live validation via the existing `POST /api/v1/config/validate`.
- New write endpoint + persistence + audit + reload.
- Editor-role gating in multi-tenant mode.

**Out of scope (this slice)**
- Creating brand-new pipelines from scratch in the GUI (start with **edit existing**; `POST` create is a fast-follow).
- Editing Agent / TestSuite / MCPServer documents (pipeline only).
- Multi-user real-time collaboration / locking beyond optimistic concurrency.
- Versioning / history / undo-across-sessions (in-session undo is React Flow local state only).

## 3. What already exists (reuse, don't rebuild)

- **Data model** — `types.PipelineDefinition` (`internal/types/pipeline.go`): `spec.topology` ∈ {sequential, parallel, graph}, `spec.agents[]{id, ref}`, `spec.edges[]{from, to, when_contains}`.
- **Read API** — `GET /api/v1/pipelines` + `GET /api/v1/pipelines/{name}` (`internal/server/pipeline_handlers.go`).
- **Read-only viewer** — `gui/app/pipelines/[name]/DAGViewer.tsx` (custom SVG + longest-path layering). Keep it for the detail view; the editor is a new sibling surface.
- **Validation** — `config.ValidateBytes` / `config.ValidatePipeline` + `POST /api/v1/config/validate` (`RoleEditor` floor) + `validateYAML()` in `gui/lib/api.ts`, returning `{ok, kind, errors[]}` with line/column/field/message/suggestion.
- **Cross-document validation** — `config.ValidateDocuments` resolves pipeline `agent` refs against loaded agents (`cross_document_validator.go`).
- **Loader source paths** — `config.PipelineLoadResult.FilePath` carries the on-disk path at load time (`internal/config/loader.go`).
- **Route floors / audit** — `managementRouteFloors` (write routes already use `RoleEditor`, e.g. agent reload), `internal/audit` (8 event kinds today; none for pipeline save yet).
- **GUI stack** — Next.js 15, React 19, cookie auth (`mockagents_api_key` → `Authorization: Bearer` via `fetchJSON`), `tsc --strict` build gate, `--sr-*` design tokens. Page files export only `default`; helpers go in sibling `.tsx`.

## 4. Backend design

### 4.1 New endpoint

```
PUT /api/v1/pipelines/{name}        # update an existing pipeline (this slice)
POST /api/v1/pipelines              # create new (fast-follow, same write core)
```

Mounted via `s.mountManaged(...)`; add to `managementRouteFloors`:
`"PUT /api/v1/pipelines/{name}": tenancy.RoleEditor` (mirrors agent reload / config validate).

**Request body:** the full `PipelineDefinition` JSON (the editor already holds it),
plus an optional concurrency token (see 4.4):
```json
{ "definition": { ...PipelineDefinition... }, "base_version": "<sha256-hex>" }
```

**Handler pipeline:**
1. Path `{name}` must equal `definition.metadata.name` and pass the kebab-case/63-char rule → else 400. (Guards path-vs-body mismatch and path traversal — name can't contain `/`.)
2. Marshal the definition to YAML (`gopkg.in/yaml.v3`; struct field order gives stable output).
3. **Validate** the marshaled bytes with `config.ValidateBytes` **and** run the cross-document agent-ref check against the live agent registry. Any errors → `422` with the same `{ok:false, errors[]}` shape the GUI already renders. **Never write on invalid input.**
4. Resolve the **target file** (4.3) and enforce it is inside the agents dir.
5. Optimistic-concurrency check (4.4) → `409` on conflict.
6. `os.WriteFile` atomically (write temp + rename) within the agents dir.
7. Re-register into the in-memory `PipelineRegistry` so it is live immediately, independent of `--watch`.
8. Emit audit `EventPipelineSaved` (new kind, 4.5).
9. Return `200` with the persisted definition + new `version` hash.

### 4.2 Wiring the agents dir + registry source paths

Two gaps to close:
- The server needs the **agents directory** to know where to write. Thread it onto `server.Config` (e.g. `Config.AgentsDir`, set in `cmd/mockagents/start.go` where `LoadAllDocuments(agentsDir)` already runs).
- `engine.PipelineRegistry` (`pipeline_registry.go`) currently stores only `*PipelineDefinition`. Extend registration to also retain the **source `FilePath`** (from `PipelineLoadResult.FilePath`) so an edited pipeline rewrites its own file rather than guessing.

### 4.3 Target-file resolution

- If the registry has a source path for `{name}` → write back to **that** file.
- Else (created in-session / no known file) → write `<agentsDir>/<name>.yaml`.
- Reject any resolved path that escapes `agentsDir` (defense in depth on top of the kebab-case name rule).

### 4.4 Concurrency (optimistic)

- `version` = `sha256(current file bytes)`, returned by `GET /{name}` (add the field) and required as `base_version` on `PUT`.
- On save: recompute the current file's hash; if it differs from `base_version` → `409 Conflict` ("the file changed on disk since you loaded it; reload and re-apply"). Last-writer-wins is **not** silent.
- A process-level write mutex serializes concurrent saves.

### 4.5 Audit

Add `EventPipelineSaved EventKind = "pipeline.saved"` to `internal/audit/types.go` (and the kind list in CLAUDE.md / docs). Record principal + pipeline name + target file.

### 4.6 Multi-tenancy note

Pipelines are **global** today (not tenant-scoped). Writing them in multi-tenant
mode is an operator action → `RoleEditor` floor is the gate, and the audit row
captures who. True per-tenant pipeline ownership is out of scope (it rides with
REF-08's per-tenant collision work).

## 5. Frontend design

### 5.1 Dependency

Add `@xyflow/react` to `gui/package.json`. Confirm the `tsc --strict` build stays
green and note the bundle-size delta.

### 5.2 Routes & files (respecting the page-file constraint)

```
gui/app/pipelines/[name]/edit/page.tsx     # server component: load pipeline + auth, render editor
gui/app/pipelines/[name]/edit/PipelineEditor.tsx   # "use client" — React Flow canvas + toolbar
gui/app/pipelines/[name]/edit/actions.ts   # server action: savePipeline() -> PUT (bearer via cookie)
```
Add an "Edit" link on the existing `gui/app/pipelines/[name]/page.tsx`.

### 5.3 Model mapping

- **Load:** `PipelineDefinition` → React Flow `nodes` (one per `spec.agents[]`, label = `id` + `ref`) and `edges` (from `spec.edges`, or synthesized for `sequential`). Initial layout: reuse the viewer's longest-path layering for x/y seeds.
- **Edit:** drag nodes (persist positions only in-session — positions are **not** part of the YAML schema, so they're cosmetic), connect handles to add edges, click an edge to set `when_contains`, add/remove agent nodes (ref picker populated from `GET /api/v1/agents`).
- **Topology:** editing free edges implies `graph`; offer a topology selector. `sequential`/`parallel` render but restrict edge editing (reorder only / none).
- **Serialize:** canvas → `PipelineDefinition` (drop cosmetic positions).

### 5.4 Validate + Save UX

- Debounced live validation: serialize → `POST /api/v1/config/validate` → inline error chips (reuse the editor's error rendering; map line/field/message).
- **Save** (disabled while invalid): server action → `PUT /api/v1/pipelines/{name}` with `base_version`. Handle `422` (show errors), `409` (offer reload), `200` (toast + refresh).
- Auth: the save goes through a server action so the `mockagents_api_key` cookie is injected as bearer (same pattern as the rest of the GUI); never expose the key client-side.

## 6. Security considerations

- **Path traversal** — name validated kebab-case (no `/`, no `..`); resolved path must stay within `agentsDir`.
- **Authz** — `RoleEditor` floor in multi-tenant; open in single-tenant (matches `config/validate` and agent reload).
- **Validate-before-write** — structurally and cross-document invalid YAML is rejected with `422`; the file is never touched on failure.
- **Atomic write** — temp-file + rename so a crash mid-write can't truncate a live config.
- **Server writes FS** — new capability; bound strictly to `agentsDir`, audited, editor-gated. Document the new trust surface in SECURITY-REVIEW.

## 7. Milestones (suggested split of the 13 pts)

1. **M1 — Write API core (~5 pts).** `Config.AgentsDir`, registry source-path tracking, `PUT /api/v1/pipelines/{name}` (validate → atomic write → reload → audit), `version`/`base_version` concurrency, `EventPipelineSaved`, route floor. Backend tests: happy path, `422` invalid, `409` conflict, path-escape rejection, multi-tenant authz, audit row.
2. **M2 — Editor canvas (~5 pts).** `@xyflow/react`, edit route + `PipelineEditor.tsx`, load/serialize mapping, drag/connect/delete, ref picker, topology selector. `tsc --strict` green.
3. **M3 — Validate + Save UX (~3 pts).** Debounced validation, inline errors, save server action, 409/422/200 handling, "Edit" entry point, design-token styling. Live smoke test against the Go backend in multi-tenant mode.

## 8. Test plan

- **Go:** new `pipeline_handlers_test.go` cases (write happy/invalid/conflict/traversal/authz), registry source-path round-trip, audit assertion, YAML re-marshal round-trips an example pipeline byte-stably enough to re-validate.
- **GUI:** `tsc --strict` build is the gate (component tests still TBD repo-wide); live curl/manual against the backend for the save round-trip.
- **Conformance:** ensure read endpoints + viewer unaffected.

## 9. Open questions

1. **Create flow** — ship `POST /api/v1/pipelines` (new pipelines) in this slice or as a fast-follow? (Recommend fast-follow; `PUT` edit-existing covers the headline ask.)
2. **Node positions** — schema has no coordinates. Keep layout purely derived (re-layout on open), or add an optional non-validated `metadata.annotations` blob to persist positions? (Recommend derived-only for v1.)
3. **`--watch` interaction** — explicit in-process reload after write makes edits live without `--watch`; confirm no double-reload race when `--watch` is also on (debounce already exists in the watcher).
4. **Agent editing** — same write core generalizes to agents later; keep the handler pipeline-specific for now or factor a shared `writeConfigDocument` helper? (Recommend pipeline-specific first, factor when agents land.)

## 10. Rollback / risk

The headline risk is the new "server writes the working tree" capability. Mitigations:
validate-before-write, atomic rename, strict path confinement, editor-role gate, audit
trail, optimistic concurrency. If the write path proves too risky operationally, the
endpoint can be feature-flagged off (env gate) leaving the read-only viewer intact.
