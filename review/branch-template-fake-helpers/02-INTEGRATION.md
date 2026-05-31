# Cross-File Integration Findings (Pass 2) — feature/template-fake-helpers

## Relationship / blast-radius map

_The new helpers are registered in the engine's template `FuncMap` and surface to YAML scenario authors via `text/template`. The only external consumer of "what functions exist" is the user-facing docs table._

```
response_generator.go
  NewResponseGenerator().funcMap   ──registers──▶  title, fake_phone, fake_company, fake_username
        │                                                   │
        ▼ (read-only after construction)                    ▼ invoked by author templates
  renderContent() ──text/template.Execute──▶  scenario YAML  {{ title .Message }} / {{ fake_phone }} …
        ▲                                                   │
        │ documents the available set                       ▼
  site/docs/guides/yaml-schema.md  (template-function reference table)  ◀── STALE (X-001)
```

No new imports, no signature changes, no interface implementors, no shared mutable state introduced — so Pass-2 checks 1–4, 7, 8, 10 are clean (details below). The only seam that broke is docs/contract drift.

## Findings

| ID | Severity | Conf | Priority | Effort | Related sites | Check | Evidence → Fix |
|----|----------|------|----------|--------|---------------|-------|----------------|
| X-001 | S2 | High | P2 | S | `internal/engine/response_generator.go:74,79-81` ; `site/docs/guides/yaml-schema.md:111-123` | Contract/doc drift | The funcMap gained `title`, `fake_phone`, `fake_company`, `fake_username`, but the user-facing template-function reference table lists through `lower`/`to_json` only and was not extended. Authors reading the docs cannot discover the new functions. → Add four rows to the table (and optionally the example in `README.md:20`, which already hedges with "and more"). |

## Checks performed (for auditability)

- [x] **Signature & call-contract consistency** — clean. New funcs are additive `FuncMap` entries; no existing signature changed, no call sites to update.
- [x] **Interface ↔ implementation** — n/a. No interfaces involved; helpers are plain funcs.
- [x] **Data flow across boundaries** — clean. Helpers return `string`; no value crosses a layer boundary with changed shape/nullability.
- [x] **Layering & import direction** — clean. Changes are entirely within `internal/engine`; no new package imports, no cycle, no `engine`→`tenancy` violation.
- [x] **Duplication & divergence** — minor/acceptable. `fakeUsername`/`fakeEmail`/`fakeName` each re-pick from `fakeFirstNames`/`fakeLastNames` inline; this matches the file's existing idiom and the copies are not correctness-coupled. Not raised as a finding.
- [x] **Contract/schema/wire-format drift** — **1 finding (X-001).** Docs table stale.
- [x] **Concurrency & shared state** — clean. `funcMap` and the new package-level slices are written once at construction/init and read-only thereafter; `renderContent` only reads them. No new races.
- [x] **Error propagation** — n/a. Helpers cannot error (consistent with sibling helpers); a bad `{{ }}` template still surfaces through the existing `executing template` wrap in `renderContent`.
- [x] **Test integration gaps** — the wired path (`Generate` → `renderContent` → `text/template`) is exercised by the existing `TestResponseGenerator_TemplateFunctions` table, which the new cases join. Adequate for these pure helpers. (Per-file weakness tracked as F-003/F-004.)
- [x] **Lifecycle & ordering** — clean. Functions registered in the constructor before any `Generate` call; no ordering hazard.
