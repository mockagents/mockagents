# Action Plan — feature/template-fake-helpers

_Execute top-down. Each task maps to a finding ID; check off when its "Done when" holds. This file is self-contained — you do not need the other reports to act on it._

**Gate:** all **P0** boxes must be checked before merge. _(There are no P0s in this review.)_

## P0 — Blockers (merge-blocking)

_None._

## P1 — High (this cycle)

_None._

## P2 — Schedule (recommended before merge)

- [x] **Fix `title` UTF-8 corruption on non-ASCII words** — `F-001` · effort:S · owner:claude (fixed)
  - **Where:** `internal/engine/response_generator.go:243-248`
  - **Problem:** `strings.ToUpper(w[:1]) + strings.ToLower(w[1:])` splits a multibyte leading rune by byte, so `title("über")` returns `"��uber"` (two U+FFFD).
  - **Fix:** decode the first rune before upper-casing:
    ```go
    r, sz := utf8.DecodeRuneInString(w)
    words[i] = string(unicode.ToUpper(r)) + strings.ToLower(w[sz:])
    ```
    (add `"unicode"` and `"unicode/utf8"` imports).
  - **Done when:** `title("über")` == `"Über"` and a non-ASCII test case passes.

- [x] **Cover `title` edge cases** — `F-004` · effort:S · owner:claude (added über/empty/multi-space cases)
  - **Where:** `internal/engine/response_generator_test.go` (`TestResponseGenerator_TemplateFunctions` table)
  - **Problem:** only ASCII is tested, so the F-001 break path is unexercised.
  - **Fix:** add cases — `"über"` → `"Über"`, `""` → `""`, `"a   b"` → `"A B"`.
  - **Done when:** the three cases exist and pass (after F-001 is fixed).

- [x] **Document the four new template functions** — `X-001` · effort:S · owner:claude (added 4 rows + title to yaml-schema.md)
  - **Where:** `site/docs/guides/yaml-schema.md:111-123` (template-function reference table)
  - **Problem:** `title`, `fake_phone`, `fake_company`, `fake_username` are live in the engine but absent from the user-facing docs.
  - **Fix:** add a row for each, matching the existing column format (syntax · example · description).
  - **Done when:** all four appear in the table; optionally refresh the `README.md:20` example list.

## P3 — Opportunistic

- [x] **Clarify `title` whitespace behavior in its doc comment** — `F-002` · effort:S (comment now documents whitespace normalization)
- [x] **Strengthen the `fake_company` test assertion** — `F-003` · effort:S (now `assert.Regexp(t, \`^C: \w+ \w+$\`, c)`)

## Workstream clusters

- **`title` correctness:** `F-001` + `F-004` + `F-002` — same function; fix the rune handling, its tests, and its comment in one edit.
- **Discoverability:** `X-001` (docs table) — independent; can ship in the same commit as the code fixes.

## Needs investigation (low-confidence, not yet actionable)

_None — all findings are High confidence._
