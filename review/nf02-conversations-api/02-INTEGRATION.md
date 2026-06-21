# Cross-File Integration Findings (Pass 2) — NF-02 OpenAI Conversations API

## Relationship / blast-radius map

```
                         shared *conversationStore (registry.go)
                          │                         │
          NewConversationsHandler            NewResponsesHandler(eng, conv)
          (conversations.go)                  (responses.go)
                │                                   │
   /v1/conversations(+/items) CRUD        POST /v1/responses  ──conversation param──┐
                │                                   │                                │
                └────────── conversationState.items ◀── appendItems(turn input+output)
                                   │
                  conversationItemsToMessages() ──▶ []engine.RequestMessage ──▶ Engine
                                   ▲
                  (same mapping as responses.go parseResponsesInput)  ← X-001

  NewResponsesHandler signature changed → callers: registry.go ✔, responses_test.go ✔ (12)
  Adapter mounting: DefaultRegistry → server mux (registry_test.go pins names+routes)
```

## Findings

| ID | Severity | Conf | Priority | Effort | Related sites | Check | Evidence → Fix |
|----|----------|------|----------|--------|---------------|-------|----------------|
| X-001 | S2 | Med | P2 | S | `responses.go parseResponsesInput`, `conversations.go conversationItemsToMessages` | Duplication & divergence | The item→message switch (`function_call_output`→`tool`, `function_call`→empty `assistant`, else `message`→role + `extractStringContent(decodeContent(...))`) is implemented in BOTH places. They must stay identical or a conversation replays differently than the same items sent inline. Extract one shared helper (e.g. `responsesItemToMessage(type, role, content, output)`) and call it from both. |
| X-002 | S3 | Low | (needs-invest) | S | `internal/adapter/conversations.go` routes, `docs/api-spec.yaml`, `make drift` | Contract/schema drift | New public routes added. Unknown whether `api-spec.yaml`/driftcheck must enumerate adapter routes (the recent batches/anthropic-batches adapters are the precedent — check if they were added). If the spec doesn't list per-adapter routes, no action. |
| X-003 | S3 | Low | P3 | S | `responses.go` (`conv.messages()` then `conv.appendItems()`) | Concurrency & shared state | Two concurrent `/v1/responses` turns on the *same* conversation can interleave between the read (`messages()`) and the append (`appendItems()`) — each call is individually locked, but the read-modify-write across them is not atomic. Acceptable for a mock (no concurrency guarantee on same-conversation turns); note it, or take a per-conversation turn lock if strict ordering is ever required. |

## Checks performed (for auditability)

- [x] Signature & call-contract consistency — **clean.** `NewResponsesHandler(eng, conv)` updated at both the registry and all 12 test sites (grep-confirmed); `resp.HandleResponses` (used by the batches map) is unchanged.
- [x] Interface ↔ implementation — **clean.** `ConversationsHandler` satisfies `adapter.Adapter` (`Name()`+`Routes()`); `ResponsesHandler` still does (its `Name/Routes` are untouched).
- [x] Data flow across boundaries — **clean** (F-107 withdrawn): conversation item content marshaled to a JSON string round-trips via `extractStringContent`'s `case string` (`openai.go:314`).
- [x] Layering & import direction — **clean.** `conversations.go` imports only `internal/engine` + stdlib; no cycle, no `engine`→adapter back-edge.
- [x] Duplication & divergence — **1 finding (X-001).**
- [x] Contract/schema/wire drift — **1 needs-investigation (X-002);** new adapter registered + pinned in `registry_test.go`.
- [x] Concurrency & shared state — **1 finding (X-003);** store/state mutexes individually sound, no nested-lock deadlock.
- [x] Error propagation — **clean.** 400 (bad param / mutual-exclusion), 404 (unknown conversation/item) mapped at the handler; no internal leakage.
- [x] Route mounting — **clean.** Patterns are distinct by method/segment-count (`/v1/conversations`, `/v1/conversations/{id}`, `/v1/conversations/{id}/items`, `…/items/{item_id}`); no ServeMux conflict; not added to `skipAuth`/`isLLMProviderPath` — consistent with files/batches (auth-required, not quota'd) so no server-package change needed.
- [x] Test integration gaps — the wired Responses↔conversation path IS covered (`TestConversation_RespondsTurnAccumulatesState`); no full-middleware server test (acceptable, generic mount tests exist).
