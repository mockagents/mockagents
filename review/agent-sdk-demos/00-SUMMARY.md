# Review Summary — agent-sdk-demos (commits `4f7df21..4cc09ec`)

- **Target:** the 3 merged commits on `main` — Anthropic adapter fixes, Gemini-surface parity, and the OpenAI/Claude/Google-ADK demos.
- **Reviewed at:** 2026-06-10  ·  **Depth:** standard (thorough on the 7 Go source/test files; 3 demos reviewed as clusters)
- **Scope:** 75 files (7 Go: ~146 changed LOC; 68 demo files: ~4,200 LOC). _Related-but-unchanged: `internal/adapter/gemini.go` (the extractor's contract source), `internal/server/server.go` (spend hook), `internal/quota/quota.go`._
- **Reviewer:** multi-pass-review skill

## Verdict

> **GO** — already merged + pushed; no S0/S1 found. The Go changes are small, internally consistent, and test-covered. Findings are two S2 maintainability/test-gap items and a handful of S3 nits. Nothing blocks.

## Findings by severity

| Severity | Count | Notable IDs |
|----------|-------|-------------|
| S0 Blocker | 0 | — |
| S1 High    | 0 | — |
| S2 Medium  | 2 | X-001, X-002 |
| S3 Low     | 5 | F-001, F-002, F-003, F-004, F-005 |

## Top risks (the things that matter)

1. **Duplicated LLM-path classification** (`X-001`, S2) — `isQuotaPath` and `isLoggablePath` independently encode "which paths are LLM endpoints"; they already diverge subtly and a 4th provider will silently desync quota vs logging.
2. **Untested cost seam: Gemini adapter → `ExtractUsage`** (`X-002`, S2) — three cost/spend consumers depend on the extractor parsing the adapter's exact field names (`modelVersion`/`usageMetadata`); only a hand-written body is tested, so a field rename would silently zero Gemini cost + defeat the 402 cap.
3. **Non-SSE Gemini streaming cost is always 0** (`F-001`, S3) — `streamGenerateContent` (no `?alt=sse`) returns a JSON array `ExtractUsage` can't parse; consistent with OpenAI/Anthropic streaming (best-effort) but now reachable since the route is logged.

## Coverage & confidence

- Passes run: 0, 1, 2, 3, 4.
- _Deep mode:_ n/a (standard). S2 findings reasoned + blast-radius-confirmed, not adversarially refuted.
- **Not covered / blind spots:**
  - The 68 demo files were reviewed as **3 clusters** (shared structure), not 68 isolated passes — they were already runtime-verified end-to-end this session, so review focused on security/correctness patterns, not every line.
  - No fresh `go test ./...` run as part of this review (the changed packages passed during authoring: `adapter`, `pricing`, `server`).
  - Docker images were **not built** (noted in the demos' own READMEs); `multitenant_walkthrough.sh` needs `jq` (verified via equivalent curl/Python).
  - Pass-2 import-cycle / layering check is light: the changes touch no import edges.

## Where to act

→ Execute **`03-ACTION-PLAN.md`** (no P0/P1; start at P2). Detail in `01-PER-FILE.md` (per-file) and `02-INTEGRATION.md` (cross-file).
