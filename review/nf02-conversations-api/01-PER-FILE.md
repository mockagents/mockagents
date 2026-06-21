# Per-File Findings (Pass 1) — NF-02 OpenAI Conversations API

_Each file judged in isolation. Cross-file issues live in `02-INTEGRATION.md`._

## `internal/adapter/conversations.go`  ·  Go, ~390 LOC  ·  role: adapter (new)

Both compile-correctness and concurrency came back clean: all imports used, no undefined identifiers (given the package's existing helpers), store-mutex vs state-mutex are never nested (no deadlock), per-tenant isolation keys every access by `engine.TenantIDFromContext`. Slice eviction/deletion (`items[len-cap:]`, three-index `append(s[:i:i], …)`) verified correct.

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-103 | S2 | Med | P2 | S | :~317 (`HandleUpdate`) | Correctness | `st.setMetadata(req.Metadata)` runs even when the body omitted `metadata` (or was empty, tolerated just above) → an empty-body update wipes metadata to `nil`. Decode into `*map[string]any` (presence check) and only replace when present, or document that update replaces wholesale. |
| F-101 | S3 | High | P3 | S | :~528 (`isEmptyBodyDecodeErr`) | Error handling | `err.Error() == "unexpected end of JSON input"` is brittle string-matching. **Verified reliable here** (`decodeJSONBody` returns the unwrapped `json.Unmarshal` error), so not a bug — but a future decode change could silently break empty-body create. Prefer detecting `r.ContentLength == 0` before decoding. |
| F-105 | S3 | High | P3 | S | :~478 (`userMessageItem`) | Readability | `userMessageItem` is also called to build the *assistant* item (`assistantItemsFromResponse`), so the name misleads. Rename to `messageItem(role, content)`. |
| F-108 | S3 | Low | P3 | S | :~458 (`conversationItemsToMessages`) | Correctness | A `function_call` item flattens to an empty-content assistant message, dropping name/arguments on replay. Intentional for a mock loop (the call text never needs to re-match), but note it; serialize the call into content only if the engine ever needs it. |

_Verified-correct, no action (listed so the reader knows they were checked): nil inner-map index is safe (`s.m[tenant][id]` → ok=false); `wire()` returns the live metadata map but it is serialized immediately under no further mutation; eviction/deletion slicing correct._

## `internal/adapter/responses.go`  ·  Go (modified)  ·  role: adapter

Compile-correctness verified: `convID, err :=` and `resp, err :=` are valid (each introduces a new LHS var alongside the already-declared `err`); the new `tenant` and `conv` locals are used on their paths; the conversation-append block sits **before** the `if req.Stream` branch, so streaming turns persist too.

| ID | Severity | Conf | Priority | Effort | Line | Dimension | Evidence → Fix |
|----|----------|------|----------|--------|------|-----------|----------------|
| F-201 | S3 | Med | P3 | S | append block (`for _, m := range inputMsgs`) | Fidelity | Re-fed `function_call_output` (role `tool`) / echoed `function_call` (role `assistant`, empty) get stored as `Type:"message"` items, losing the call shape. Round-trips fine for replay (F-107 verified), but the items endpoint then shows a non-canonical `{type:message, role:tool}`. Map non-message roles back to the proper item kind, or document the simplification. |
| F-202 | S3 | Med | P3 | S | `conv != nil` branch | Logic/clarity | `instructions` is prepended only when the conversation has no prior items, so instructions resent on a later turn are dropped. Defensible (instructions = first-turn system seed). Add a one-line comment stating the intent. |
| F-203 | S3 | Low | P3 | S | conversation/prev-id checks | UX | If `conversation` is malformed JSON *and* `previous_response_id` is set, the malformed-conversation 400 fires before the mutual-exclusion 400. Cosmetic ordering; leave unless the exclusion message is preferred. |

## `internal/adapter/registry.go` — ✅ no findings
Two-line change (shared `conversations := newConversationStore()` injected into `NewResponsesHandler(eng, conversations)` + appended `NewConversationsHandler(conversations)`). Wiring consistent; adapter ordering fine.

## `internal/adapter/conversations_test.go` — ✅ no findings
Covers CRUD, items CRUD, empty-body create, multi-turn state accumulation (exact item counts 2→4 verified against the agent's deterministic scenarios: `hello`→"Hi there!", `and again`→default "How can I help?", both no-tools), 404/400 paths, object-form param, tenant isolation. Note (not a finding): the accumulation assertion is coupled to `responsesAgent()`'s scenario outputs — acceptable for an in-package test.

## `internal/adapter/responses_test.go` — ✅ no findings
Mechanical: 12 `NewResponsesHandler(...)` call sites updated to pass `, nil`.

## `internal/adapter/registry_test.go` — ✅ no findings
Added `"openai-conversations"` to the ordered names list + the 7 conversation route patterns.
