# Review Summary — feature/template-fake-helpers

- **Target:** branch `feature/template-fake-helpers` (`git diff main...HEAD`)
- **Reviewed at:** 2026-05-30  ·  **Depth:** standard
- **Scope:** 2 files, +72/-2 LOC — `internal/engine/response_generator.go`, `internal/engine/response_generator_test.go`
- **Reviewer:** multi-pass-review skill

## Verdict

> **GO WITH FIXES** — no S0/S1 blockers; the feature builds, vets, and its tests pass. One real correctness bug (`title` mangles non-ASCII input, F-001) and a user-facing doc gap (X-001) should be fixed before merge as hygiene, but neither blocks it.

## Findings by severity

| Severity | Count | Notable IDs |
|----------|-------|-------------|
| S0 Blocker | 0 | — |
| S1 High    | 0 | — |
| S2 Medium  | 3 | F-001, F-004, X-001 |
| S3 Low     | 2 | F-002, F-003 |

## Top risks (the things that matter)

1. **`title` corrupts non-ASCII words** (`F-001`, S2) — byte-slicing a multibyte leading rune replaces it with U+FFFD; `title("über")` → `"��ber"` (verified). General-purpose helper that authors may apply to `.Message` (user input).
2. **Template-function reference is now stale** (`X-001`, S2) — 5 new functions added to the engine but the user-facing table in `site/docs/guides/yaml-schema.md` wasn't updated, so authors can't discover them.
3. **`title` has no test for the bug path** (`F-004`, S2) — non-ASCII / empty / multi-space inputs are unexercised, which is exactly why F-001 slipped through.

## Coverage & confidence

- Passes run: 0, 1, 2, 3, 4.
- Standard depth — single-vote findings. F-001 was additionally reproduced (`go run`): `title("über")` → `"��ber"`, confidence High/verified.
- **Not covered / blind spots:** did not run the new functions through the live server or a YAML scenario end-to-end (unit tests only); `docs/LLD.md`'s internal func table (a design snapshot, lines 2313-2324) treated as non-authoritative and left out of scope; benchmark impact of `title` not measured (it is not on the default hot path — only runs when an author uses `{{ title }}`).

## Where to act

→ Execute **`03-ACTION-PLAN.md`** (there are no P0s; start at P2). Per-file detail in `01-PER-FILE.md`; cross-file detail in `02-INTEGRATION.md`.

## Disposition

- **Reviewed:** 2026-05-30 via multi-pass-review (standard depth).
- **Action taken:** all 5 findings (F-001..F-004, X-001) resolved in this branch — `title` made rune-safe, edge-case tests added, `fake_company` assertion tightened, docs table updated.
- **Tested:** `go build ./...` clean · `go vet ./internal/engine` clean · `gofmt` clean · `go test ./internal/engine/...` green · F-001 reproduced then confirmed fixed (`title("über")` → `"Über"`).
- **Status:** ✅ reviewed · ✅ tested · ready to merge to `main`.
