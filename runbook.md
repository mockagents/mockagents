---
name: sdlc-autobuild-runbook
description: "The canonical protocol an hourly autonomous SDLC cron follows to build the MockAgents enhancement backlog (auto-push to main, deploy=build-artifacts, stop when backlog empty)."
metadata: 
  node_type: memory
  type: project
  originSessionId: b5ff5a48-e315-4dd2-8867-5bfe91ef5be5
---

# SDLC Autobuild Runbook (hourly autonomous loop)

Armed 2026-06-13. Each cron firing is **one fresh session** that reads this
runbook, reads the progress tracker, picks ONE backlog item, runs the full SDLC
with multiple agents, and ships it. Decisions locked by the user:

- **Git policy: SHIP VIA PULL REQUEST** (updated 2026-06-13 per user — supersedes the prior "auto-push to main"). After the in-loop review + green gate, ship each code change as a PR and merge it: `git checkout -b feature/<slug>` → commit (stage ONLY the slice's files; never `review/`) → `git push --no-verify -u origin feature/<slug>` (the tracked `hooks/pre-push` blocks non-main branches → `--no-verify` is the documented override) → open the PR → merge it (squash, delete branch) → the merge fast-forwards local `main` → `git archive-sync`. **gh CLI:** authenticated (account `anandtopu`, `repo` scope) but NOT on PATH — it's at `C:\Program Files\GitHub CLI\gh.exe`; invoke it via the **PowerShell tool** (`& "C:\Program Files\GitHub CLI\gh.exe" pr create ... ` / `gh pr merge <N> --squash --delete-branch`). End PR bodies with the Claude Code generated-by line. NEVER push/merge a red tree. (Pure-memory/docs-tracker updates that aren't repo code changes don't need a PR.)
- **Deploy phase = build & verify artifacts.** `make release` (goreleaser --snapshot --clean) + `make docker` + `make helm-lint`; assert each builds clean. Skip a step only if its toolchain is absent here — and LOG that.
- **Stop condition: backlog empty.** When no codeable P1/P2 item remains (all done or all need-user), the loop self-disables (CronDelete) and writes a final progress row. Also stop an ITEM (mark blocked, skip next firing) if its gate fails twice across firings.

## Files

- **Backlog (source of truth for WHAT to build):** `git show release/roadmap_docs:docs/research/2026-06-10-enhancement-backlog.md`
- **Progress tracker (cross-firing state):** `…/memory/autobuild-progress.md` — read it first, append a row last.
- **This runbook:** `…/memory/sdlc-autobuild-runbook.md`.

## What to build, in order (skip anything needing user/registration actions)

Skip ALL of P0 D-00..D-08 (org/namespace registration, release-tagging, PyPI/npm/Docker publish — these need the USER). Pull codeable items in this order:

1. **P1:** A-01 ✅done · **A-02** `/v1/embeddings` (S) · **A-03** structured-outputs strict mode (M) · **M-01** Streamable-HTTP MCP transport (L) · **M-02** MCP conformance badge (S) · **E-01** pytest-mockagents (M) · **E-02** @mockagents/vitest (M) · **E-03** setup-mockagents Action (S) · **DOC-01** per-framework testing docs (M)
2. **P2:** R-01..R-05 (record/replay v2) · MCP-03/04 · A-04 (Anthropic depth) · A-05 (vision) · A-06 (Azure routing) · A-07 (`/v1/moderations`) · A-08 (Batch API) · H-01 (repo health) · the pre-existing connection-faults good-first-issue (FB-03 slice 5)

Prefer smaller (S/M) items first to keep each firing shippable. For an L item, make COMPILING + GREEN incremental progress and mark it `in-flight` so the next firing continues it. Never leave main red.

## Per-firing SDLC protocol

> You have explicit user opt-in to use the **Workflow** tool for multi-agent
> fan-out. Use it for the review/verify phases especially.

**STEP 0 — Orient & pick.** Read the progress tracker + backlog. Read `CHANGELOG.md` `[Unreleased]` and `git log --oneline -15` to see what already shipped. Pick the highest-priority codeable item that is not done and not `in-flight`-by-another (there's only one loop, so in-flight = continue it). If none remain → **STOP**: `CronDelete` this job, write a final tracker row, update the memory checkpoint, and report.

**STEP 1 — Plan (architect agent).** Spawn 1 architect agent: produce an implementation plan — files to touch, wire contract / API shape, test list, docs to update, risks. Mirror the existing adapter translate-in/translate-out pattern and the conventions in `CLAUDE.md`.

**STEP 2 — Build (developer).** Implement per the plan. Match surrounding package style. Keep the change cohesive and scoped to the item.

**STEP 3 — Review (multi-agent, Workflow).** Fan out reviewers across dimensions — correctness, API-contract fidelity (does it match the real provider's wire shape?), security, performance, reuse/simplification. Then a cross-file / integration pass: how does the new code interact with the engine, adapters, server wiring, tenancy, streaming? Use `pipeline(dimensions, review → adversarially-verify)`.

**STEP 4 — Adversarial verify + bug hunt.** For each finding, spawn skeptics prompted to REFUTE (refute-by-default, majority vote) so only real findings survive. Separately, spawn a dedicated bug-hunter agent on the new code (edge cases, nil derefs, concurrency, error paths).

**STEP 5 — Fix.** Apply only confirmed findings + real bugs. Re-review the fixes if non-trivial.

**STEP 6 — Test (unit + e2e).** Add/extend table-driven unit tests AND an end-to-end test (httptest server, or boot the built binary and curl it). Cover the happy path, error envelopes, and streaming if applicable.

**STEP 7 — Gate (MANDATORY — not done until green).** Use an ABSOLUTE GOTMPDIR (relative `./.gotmp` breaks `t.TempDir()`):
```
mkdir -p "$PWD/.gotmp"; export GOTMPDIR="$PWD/.gotmp"
GOTOOLCHAIN=local GOFLAGS=-mod=mod go build ./...
GOTOOLCHAIN=local GOFLAGS=-mod=mod go vet ./...
GOTOOLCHAIN=local GOFLAGS=-mod=mod go test ./... -count=1
```
(`dangerouslyDisableSandbox: true` on the Bash tool for build/test.) Plus `cd gui && npm run build` if gui touched; `./mockagents validate examples` if config/types/schema/templates touched. govulncheck stdlib findings ≤1.26.2 are already remediated by the pinned toolchain — not real.

**STEP 8 — Deploy/verify artifacts.** `make release` (goreleaser --snapshot --clean), `make docker`, `make helm-lint`. Assert clean. If a toolchain (docker/helm/goreleaser) is unavailable in this env, log "skipped — <tool> unavailable" and continue (don't fail the firing for a missing local tool).

**STEP 9 — Document.** Update `CHANGELOG.md` `[Unreleased]` (Added/Fixed/Security as fits), `README.md` if user-facing, and the relevant guide. Keep `AGENTS.md`/`CLAUDE.md` in sync — but ONLY on the archive (they're gitignored on main, so editing them on main is a no-op; skip).

**STEP 10 — Ship.** Commit to main: Conventional-Commit subject + body, end with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Do NOT commit `review/`, internal `docs/roadmap/*`, or build artifacts. `git push origin main`. Then `git archive-sync`. (Use a heredoc for the commit message — raw backticks in `-m` get eaten by the shell.)

**STEP 11 — Log.** Append a row to the progress tracker (item, status, commit SHA, gate result, notes). Update the memory checkpoint `[[autobuild-loop-armed]]` if status changed.

## Hard rules

- **Never push red.** If the gate can't go green this firing, revert/stash the slice (`git checkout -- .` / `git stash`), leave main clean, and log the failure + reason. Two consecutive failures on the same item → mark it `blocked`, skip it next firing.
- **One item per firing** (or incremental green progress on an L item). Don't sprawl.
- **A rejected push or a dirty unexpected tree → STOP and log;** do not force.
- Honor `CLAUDE.md` conventions (no cgo, tenancy import direction, page-file rules, keep chaos-preset name sets in sync, rerun bench-report for perf-affecting changes).

Related: [[autobuild-loop-armed]], [[session-2026-06-08-autobuild-fb]] (prior loop), [[session-2026-06-10-pmf-research]] (the backlog).
