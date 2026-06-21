# Action Plan — NF-02 OpenAI Conversations API

_Execute top-down. Each task maps to a finding ID; check off when its "Done when" holds. Self-contained — you do not need the other reports to act._

**Gate:** A-000 (build/test green) is the hard merge gate. No S0/S1 findings, so the rest is polish.

## P0 — Blockers (merge-blocking)

- [ ] **Run the actual build/test gate** — `A-000` · effort:S · owner:unassigned
  - **Where:** whole changeset (written during a tooling outage, never compiled)
  - **Problem:** the review is static only; the compiler/tests have not run.
  - **Fix:** `mkdir -p "$PWD/.gotmp"; export GOTMPDIR="$PWD/.gotmp"; GOTOOLCHAIN=local GOFLAGS=-mod=mod go build ./... && go vet ./... && go test ./internal/adapter/ -count=1` (then full `./...`).
  - **Done when:** build + vet + `go test ./...` all green; `gofmt -l` clean on the 6 files.

## P1 — High (this cycle)

_None._

## P2 — Medium (schedule)

- [ ] **Consolidate the item→message mapping** — `X-001` · effort:S · owner:unassigned
  - **Where:** `responses.go` `parseResponsesInput` + `conversations.go` `conversationItemsToMessages`
  - **Problem:** the same `function_call_output→tool / function_call→assistant / message→role` switch lives in two files; they must stay identical or inline-vs-conversation replays diverge.
  - **Fix:** extract a shared `responsesItemToMessage(...)` helper; call it from both.
  - **Done when:** one function owns the mapping; both call sites delegate; existing responses + conversations tests still pass.
- [ ] **Don't nil metadata on empty-body update** — `F-103` · effort:S · owner:unassigned
  - **Where:** `conversations.go` `HandleUpdate`
  - **Problem:** an empty/`metadata`-omitted `POST /v1/conversations/{id}` wipes existing metadata to `nil`.
  - **Fix:** decode into `*map[string]any` (or a presence flag) and only replace when the field is present.
  - **Done when:** a test updating with `{}` leaves prior metadata intact; updating with `{"metadata":{...}}` replaces it.

## P3 — Low (opportunistic)

- [ ] **Rename `userMessageItem` → `messageItem`** — `F-105` · effort:S — it builds assistant items too.
- [ ] **Robust empty-body detection** — `F-101` · effort:S — prefer `r.ContentLength == 0` over matching the `"unexpected end of JSON input"` string (works today, but brittle).
- [ ] **Comment the first-turn-only instructions behavior** — `F-202` · effort:S — state the intent at the `conv != nil` branch.
- [ ] **Preserve input item kind on append (fidelity)** — `F-201`/`F-108` · effort:S — map `tool`/`function_call` input roles back to their canonical item kind instead of forcing `message`, so the items endpoint shows canonical shapes.
- [ ] **(cosmetic) mutual-exclusion error ordering** — `F-203` · effort:S — only if the exclusion message is preferred over the malformed-JSON one.
- [ ] **Same-conversation concurrent-turn atomicity** — `X-003` · effort:S — add a per-conversation turn lock only if strict ordering under concurrent turns is ever required (not needed for a mock today).

## Post-merge follow-ups (not review findings — from the feature scope)

- [ ] CHANGELOG `[Unreleased]` + README entry for the Conversations API.
- [ ] Optional: an `examples/` conversation-based multi-turn demo; echo `conversation` in the Responses response object.

## Workstream clusters

- **Mapping/replay correctness:** `X-001`, `F-201`, `F-108` — all touch the item↔message translation; do together.
- **conversations.go polish:** `F-103`, `F-105`, `F-101` — same file, batch the edits.

## Needs investigation (low-confidence, not yet actionable)

- [ ] `X-002` — do new adapter routes need to appear in `docs/api-spec.yaml`? — check whether the batches/anthropic-batches adapters were added there; if the spec doesn't enumerate per-adapter routes, close as no-action.
