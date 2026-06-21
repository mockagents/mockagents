# Cross-File Integration Findings (Pass 2) — agent-sdk-demos

## Relationship / blast-radius map

How the changed seams connect (the three Gemini-parity fixes are mutually dependent — logging enables capture, the extractor enables cost, quota enforces it):

```
                 POST /v1/chat/completions  (openai)
 HTTP request →  POST /v1/messages           (anthropic) ─┐
                 POST /v1beta/models/{m}:gen* (gemini) ───┤
                                                          ▼
   AuthMiddleware ─→ QuotaEnforce[isQuotaPath] ─→ InteractionCapture[isLoggablePath]
        │                  │ (429/402)                  │  └─ spendHook ─┐
   (tenant on ctx)         └── quota.AllowRequest/CheckSpend             │
                                                                         ▼
                                          pricing.ExtractUsage(responseBody)
                                                  ▲           ▲
                  adapter/gemini.go               │           │  3 consumers:
                  formatGeminiResponse ───emits───┘           ├─ server.go spendHook (402)
                  {modelVersion, usageMetadata}               ├─ costs_handler.go (/api/v1/costs)
                                                              └─ log_handlers.go (cost annotation)
```

Two independent path classifiers gate this chain: `isQuotaPath` (quota_middleware.go) and `isLoggablePath` (log_handlers.go). Both were edited to add the Gemini prefix.

## Findings

| ID | Severity | Conf | Priority | Effort | Related sites | Check | Evidence → Fix |
|----|----------|------|----------|--------|---------------|-------|----------------|
| X-001 | S2 | High | P2 | S | `internal/server/quota_middleware.go:17`, `internal/server/log_handlers.go:547` | Duplication & divergence | Both functions independently answer "is this an LLM provider path?" and **already differ** (`isLoggablePath` also lists `/v1/engines/process`; `isQuotaPath` doesn't). The provider set (`/v1/chat/completions`, `/v1/messages`, `/v1beta/models/`) is copy-pasted. A future 4th provider, or a path tweak, updated in one but not the other silently desyncs quota vs. cost/logging. Extract a shared `isLLMProviderPath(path) bool` (or a single `llmPaths` set) in one place and call it from both; keep the `/v1/engines/process` extra explicit at the logging call site. |
| X-002 | S2 | Med | P2 | M | `internal/adapter/gemini.go:73,85-86` (`formatGeminiResponse`), `internal/pricing/extract.go:41-56`, `internal/pricing/pricing_test.go` (`TestExtractUsageGemini` uses a hand-written body) | Contract drift / test gap | `ExtractUsage` reads `modelVersion` + `usageMetadata.{promptTokenCount,candidatesTokenCount}`; these match the adapter **today** (verified), but nothing pins the coupling. If `formatGeminiResponse`'s JSON tags change, Gemini cost silently → 0 and the 402 spend cap stops working, with green unit tests. Three consumers depend on this (spendHook, costs handler, log annotation). Add an integration test that feeds the **actual** `formatGeminiResponse(...)` output (or a `gemini_e2e` captured body) through `ExtractUsage` and asserts non-zero cost. |

## Checks performed (for auditability)

- [x] **Signature & call-contract consistency** — clean. `isQuotaPath`/`isLoggablePath` are package-private, single-caller; `ExtractUsage` signature unchanged (3 callers unaffected); `extractAnthropicContent` signature unchanged (`string` param → `any` only on `convertAnthropicMessages`'s `system`, whose sole caller passes `req.System`).
- [x] **Interface ↔ implementation** — n/a; no interfaces changed. The three adapters remain siblings; this diff makes the Gemini adapter *more* consistent with the other two (system/tool_result + cost/quota/log parity).
- [x] **Data flow across boundaries** — clean & improved. Gemini `usageMetadata` now flows adapter→log→pricing intact; Anthropic `system`/`tool_result` arrays now flatten to non-empty engine messages (was the empty-message 400).
- [x] **Layering & import direction** — clean. No new import edges; `pricing` stays leaf (math only), middleware stays in `server`.
- [x] **Duplication & divergence** — **X-001**.
- [x] **Contract / schema / wire-format drift** — **X-002** (adapter↔extractor coupling untested). Note: agent-spec schema/types untouched, so no schema/SDK drift.
- [x] **Concurrency & shared state** — clean. No shared state added; the quota enforcer's locking is pre-existing and untouched. (Demo note: the OpenAI demo mutates the global default genai/OpenAI client per conversation — documented as sequential-only, out of scope for the server.)
- [x] **Error propagation across layers** — clean. Chaos 503 surfaces correctly through all three adapters; `ExtractUsage` failure degrades to 0 cost rather than erroring (intended).
- [x] **Test integration gaps** — **X-002** (Gemini cost seam). The multi-tenant 429/402/cost path on the Gemini surface has no automated test (verified manually only) — covered by the same X-002 task.
- [x] **Lifecycle & ordering** — clean. Middleware order (Auth → Quota → Capture) unchanged and still correct for tenant resolution before quota.
