# Review Summary — NF-02 OpenAI Conversations API

- **Target:** uncommitted working-tree changeset (NF-02), branch-to-be `feat/openai-conversations-api`
- **Reviewed at:** 2026-06-16  ·  **Depth:** deep (adversarial-verify on S0/S1)
- **Scope:** 6 files, all `internal/adapter/` — `conversations.go` (new), `responses.go`, `registry.go`, `conversations_test.go` (new), `responses_test.go`, `registry_test.go`
- **Reviewer:** multi-pass-review skill (Pass 1 fanned out to 2 independent agents to counter author bias)

## Verdict

> **GO WITH FIXES** — static review found **no S0/S1 blockers**; both independent per-file passes confirm the changeset compiles and the multi-turn round-trip is sound. The only hard gate is mechanical: **the build/test suite has not actually been run yet** (tooling outage) — that must pass before merge (A-000). Remaining findings are one S2 consolidation + minor S2/S3 polish.

## Findings by severity

| Severity | Count | Notable IDs |
|----------|-------|-------------|
| S0 Blocker | 0 | — |
| S1 High    | 0 | — |
| S2 Medium  | 3 | X-001 (dup mapping), F-103 (empty-update nils metadata), F-107a (verify on real build) |
| S3 Low     | 6 | F-101, F-105, F-201, F-202, F-203, X-003 |
| _Withdrawn_ | 1 | F-107 (round-trip verified safe — `extractStringContent` handles bare strings) |

## Top risks (the things that matter)

1. **Unverified by compiler** (`A-000`) — the code was written during a tooling outage and never built/tested. Likely-clean (two readers + dependency checks), but **run `go build/vet/test ./...` before merge**.
2. **Duplicated item→message mapping** (`X-001`, S2) — the `function_call_output→tool / function_call→assistant / message→role` switch exists in BOTH `parseResponsesInput` and `conversationItemsToMessages`; they must stay in lockstep or replay semantics diverge.
3. **Empty-body update nils metadata** (`F-103`, S2) — `POST /v1/conversations/{id}` with an empty body wipes existing metadata to `nil` (edge case; real SDKs always send `metadata`).

## Coverage & confidence

- Passes run: 0, 1, 2, 3, 4.
- Deep mode: S0/S1 adversarially verified — **yes** (F-107 refuted & withdrawn after reading `openai.go:314`; `err`/`:=` and unused-var concerns refuted by reading the handler).
- **Not covered / blind spots:**
  - **The compiler did not run.** This review is static only; it cannot catch a subtle type error a build would. A-000 covers it.
  - Streaming path: verified by inspection that the conversation-append sits *before* the `if req.Stream` branch (so streaming turns persist), but not exercised by a test.
  - `docs/api-spec.yaml` / `make drift`: not checked whether new endpoints must be reflected there (X-002, needs-investigation).
  - No full-server (auth/tenancy middleware) mount test for the new routes — relies on the generic `DefaultRegistry`/server mounting tests.

## Where to act

→ Execute **`03-ACTION-PLAN.md`** (A-000 gate first). Detail in `01-PER-FILE.md` (per-file) and `02-INTEGRATION.md` (cross-file).
