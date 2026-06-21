# Action Plan — agent-sdk-demos (`4f7df21..4cc09ec`)

_Execute top-down. Each task maps to a finding ID; check off when its "Done when" holds. Self-contained — you don't need the other reports to act on it._

**Gate:** no **P0** items — nothing blocks merge (already merged). P2 items are recommended before the next release; P3 are opportunistic.

## P0 — Blockers (merge-blocking)

_None._

## P1 — High (this cycle)

_None._

## P2 — Medium (schedule)

- [x] **Consolidate the LLM-provider path classifier** — `X-001` · effort:S · **DONE 2026-06-10**
  - **Where:** `internal/server/quota_middleware.go` (`isQuotaPath`) + `internal/server/log_handlers.go` (`isLoggablePath`)
  - **Problem:** Both encoded the LLM-provider path set independently and already differed; a future provider/path change updated in one but not the other silently desyncs quota enforcement from cost/logging.
  - **Fix applied:** Added a single `isLLMProviderPath(path) bool` in `quota_middleware.go`; `isQuotaPath` delegates to it and `isLoggablePath` calls it (keeping `/v1/engines/process` as an explicit logging-only extra). Also **narrowed the Gemini match to `:generateContent`/`:streamGenerateContent`** (folds in `F-003`).
  - **Done when:** ✅ both predicates delegate to the one helper; classifier verified (3 providers + 2 generate methods match; `:countTokens` excluded; `engines/process` loggable-but-not-quota'd); `go test ./internal/server/` green.

- [x] **Pin the Gemini adapter → `ExtractUsage` cost seam with a test** — `X-002` · effort:M · **DONE 2026-06-10**
  - **Where:** `internal/adapter/gemini.go` (`formatGeminiResponse`) ↔ `internal/pricing/extract.go`
  - **Problem:** The extractor reads `modelVersion`/`usageMetadata.*` field names that match the adapter today but weren't pinned; a JSON-tag rename would silently zero Gemini cost and break the 402 cap with green unit tests.
  - **Fix applied:** Added `internal/adapter/gemini_cost_test.go::TestGeminiResponse_CostExtractionSeam` — marshals the **real** `formatGeminiResponse(...)` output and asserts `ExtractUsage` recovers the model + both token counts and that it prices to non-zero.
  - **Done when:** ✅ the test fails if the adapter's usage/model JSON tags drift; `go test ./internal/adapter/` green.

## P3 — Low (opportunistic)

- [~] **Bound recursion in `extractAnthropicContent`** — `F-002` · **WON'T-FIX (bounded)** — `encoding/json` caps decode nesting at ~10000 and 400s before the recursion runs (verified during review); defensive-only, not worth the code.
- [x] **Handle the non-SSE Gemini streaming array in `ExtractUsage`** — `F-001` · effort:S · **DONE 2026-06-10** — unwrap a leading JSON `[`, probe the last element carrying usage. Test: `TestExtractUsageGeminiStreamArray`. ✅
- [x] **Narrow the Gemini quota/log path prefix** — `F-003` · effort:S · **DONE 2026-06-10** (folded into `X-001`'s `isLLMProviderPath`: suffix-matches `:generateContent`/`:streamGenerateContent` only; `:countTokens` excluded, verified by test).
- [x] **Strengthen `TestAnthropic_ToolResultArrayContent`** — `F-004` · effort:S · **DONE 2026-06-10** — now decodes the response and asserts the flattened tool-result fell through to the default scenario (`"How may I assist?"`, `end_turn`), not just `StatusOK`. ✅
- [x] **Replace bare `assert` in the demo smoke tests** — `F-005` · effort:S · **DONE 2026-06-10** — every `assert` → unconditional `raise AssertionError`; verified the 3 scripts byte-compile under `python -O`. ✅

## Workstream clusters

- **LLM-path classification:** `X-001` + `F-003` — do together; the shared helper is the natural place to also narrow the prefix.
- **Gemini cost correctness:** `X-002` + `F-001` — both about the Gemini response → cost path; one test pass can cover the contract and the array-body edge.

## Needs investigation (low-confidence, not yet actionable)

- [x] `F-002` — **resolved during review.** `encoding/json` caps decode nesting at ~10000 levels and errors in `decodeJSONBody` before `extractAnthropicContent` recurses, so the nested-`tool_result` DoS is bounded (400, not overflow). Downgraded to a defensive-only P3.

## Out of scope (pre-existing, surfaced during review — not in this diff)

- `internal/adapter/decode.go` `decodeJSONBody` reads the **entire** request body into memory with **no size limit** (the `maxPooledBodyBufBytes = 1 MiB` constant only governs whether the pooled buffer is *reused*, not how much is read via `buf.ReadFrom(r.Body)`). A single large body on any adapter route (`/v1/chat/completions`, `/v1/messages`, `/v1beta/...`) is an unbounded-allocation DoS. This predates the reviewed commits, so it's not an action item here — but worth a separate ticket to wrap the body in `http.MaxBytesReader`.
