# Per-File Findings (Pass 1) — agent-sdk-demos

_Each file judged in isolation. Cross-file issues live in `02-INTEGRATION.md`._

## `internal/adapter/anthropic.go`  ·  Go, ~345 LOC  ·  role: adapter

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-002 | S3 | Low | P3 | S | :253-262 | Security (DoS) | `extractAnthropicContent` recurses on `tool_result` whose `content` is `[]any`. **Verified bounded:** `encoding/json` caps decode nesting at ~10000 levels and errors in `decodeJSONBody` *before* this function runs, so a pathological nest 400s rather than overflowing. Near non-issue; only act if a depth cap is wanted defensively. _(Separate, pre-existing & out-of-scope: `decode.go` imposes no request-body **size** cap — see note below.)_ |

_Notes (no finding): `System any` decoded then flattened via `extractAnthropicContent` is correct; string/array/nil all handled, and a stray object would `fmt.Sprintf` to a harmless string. The existing string-form `system`/`tool_result` paths are preserved (TestAnthropic_SystemPrompt still green)._

## `internal/pricing/extract.go`  ·  Go, ~90 LOC  ·  role: pricing

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-001 | S3 | Med | P3 | S | :41 | Correctness (edge) | `json.Unmarshal(body, &probe)` expects an object; a non-SSE `streamGenerateContent` response is a JSON **array** (`[]*GeminiResponse`) → unmarshal fails → `Usage{}` (0 cost). Acceptable (matches OpenAI/Anthropic streaming = best-effort 0), but now reachable since the Gemini route is logged. Optionally unwrap a leading array element before probing. |

_Notes (no finding): fallback order OpenAI → Anthropic → Gemini is correct and never sums across shapes; `Model` falls back to `modelVersion`. Verified the field names match the adapter (see X-002 for the missing test)._

## `internal/pricing/pricing.go`  ·  Go, ~104 LOC  ·  role: pricing — ✅ no findings

_Pure data additions (5 Gemini prices); comment already flags them as approximate public list prices, consistent with the file's stated convention._

## `internal/server/quota_middleware.go`  ·  Go, ~67 LOC  ·  role: server/middleware

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-003 | S3 | High | P3 | S | :17-21 | Correctness (over-broad) | `strings.HasPrefix(path, "/v1beta/models/")` also matches hypothetical `:countTokens`/`:embedContent` (not just `:generateContent`/`:streamGenerateContent`). Harmless today (MockAgents routes only the two generate methods), but would quota-count non-billable methods if added later. Tighten to a `:generateContent`/`:streamGenerateContent` suffix check if those routes ever appear. |

## `internal/server/log_handlers.go`  ·  Go, ~565 LOC  ·  role: server/logging

_The change (`isLoggablePath` + the `/v1beta/models/` prefix, :547-554) is correct and mirrors `isQuotaPath`. The shared-classifier duplication is **X-001** (Pass 2). Same over-broad-prefix note as F-003 applies but is benign here (logging an extra path is harmless)._  — ✅ no file-local findings

## `internal/adapter/anthropic_test.go`  ·  Go, +58 LOC  ·  role: test

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-004 | S3 | Med | P3 | S | (TestAnthropic_ToolResultArrayContent) | Tests | Asserts only `StatusOK`. It does prove "not the empty-message 400" (the fix's point), but doesn't assert the flattened marker actually reached scenario matching. Strengthen by decoding the response and asserting the matched scenario's content, so a future regression that flattens to the *wrong* text is caught. |

_Notes (no finding): `TestExtractAnthropicContent_ToolResultArray` + `TestAnthropic_SystemAsArray` assert the real conditions (not strawmen) and would fail if the fix regressed._

## `internal/pricing/pricing_test.go`  ·  Go, +19 LOC  ·  role: test — ✅ no findings

_`TestExtractUsageGemini` asserts both token counts and that `Model` comes from `modelVersion`; the table test now includes the 5 Gemini ids. (The missing **adapter→extractor** integration test is X-002, not a flaw in this unit test.)_

## demos — `app/deterministic_smoke.py` (×3: openai, claude, google-adk)  ·  Python  ·  role: demo/test

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-005 | S3 | High | P3 | S | `assert` statements | Tests | The contract checks use bare `assert`, which is stripped under `python -O`, turning the smoke test into a silent no-op. Acceptable for a demo, but a `raise AssertionError`/explicit check is more robust if anyone runs it optimized. |

## demos — agent YAMLs + remaining `app/*.py` (all 3 demos)  ·  YAML/Python  ·  role: demo — ✅ no findings

_Reviewed as clusters. Validated by `mockagents validate` and runtime-verified this session. Marker-before-keyword scenario ordering is correct; tool names match the registered functions (`mcp__acme__*` for Claude, plain for OpenAI/Gemini); model names match each `model:` routing key. No secrets (placeholder `mock-key`), no injection sinks (clients only). The global-default-client mutation in the OpenAI demo's `new_conversation_client` and the env-neutralization in the Claude demo are both documented and intentional._
