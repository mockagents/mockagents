# autobuild/state — cloud autobuild loop state branch

This **orphan branch** carries the cross-firing state for the MockAgents SDLC
autobuild loop, adapted to run as a **durable cloud routine** (each firing is a
fully isolated cloud Claude Code session with only the git repo — no local
machine, no local `~/.claude` memory, no local-only archive branch).

It holds three files, seeded 2026-06-13 from the local loop's memory:

| File | What it is |
|---|---|
| `runbook.md` | The canonical per-firing SDLC protocol (plan→build→review→verify→test→gate→deploy-artifacts→ship-via-PR→log). |
| `backlog.md` | The enhancement backlog (source of truth for WHAT to build). |
| `progress.md` | Cross-firing state: codeable-item status table + per-firing log. **The cloud agent updates this here every firing.** |

## Cloud-firing adaptations (these OVERRIDE the local-flavored bits of `runbook.md`)

`runbook.md` was written for the local session loop and references local paths /
conventions that DO NOT exist in the cloud. Apply these substitutions:

1. **State location.** Ignore the runbook's `…/.claude/.../memory/` paths and the
   `release/roadmap_docs` archive branch — they are local-only. The runbook,
   backlog, and progress tracker are the files **on this `autobuild/state`
   branch**. Read them from here.

2. **Read state at STEP 0:**
   ```
   git fetch origin autobuild/state
   git show origin/autobuild/state:progress.md   # what's done / in-flight / blocked
   git show origin/autobuild/state:runbook.md     # the protocol
   git show origin/autobuild/state:backlog.md     # what to build
   ```
   Pick the highest-priority codeable item not done and not in-flight (skip P0
   D-00..D-08 and any publish/registration step that needs the USER).

3. **Ship the CODE via PR to `main`** (STEP 10). Work on a feature branch off
   `main`, run the green gate, then:
   ```
   gh pr create --base main ...      # gh is on PATH & authed in the cloud env
   gh pr merge <N> --squash --delete-branch
   ```
   No `--no-verify` games and no `C:\Program Files\...\gh.exe` path — those were
   local-only. A fresh clone has no `core.hooksPath` set, so the pre-push hook is
   inactive. **Never push/merge a red tree.**

4. **Update progress HERE (STEP 11), not in local memory.** After shipping, append
   a row to `progress.md` and commit it to this branch:
   ```
   git fetch origin autobuild/state
   git checkout -B autobuild/state origin/autobuild/state
   # edit progress.md: flip the item to ✅ done + append a per-firing log row
   git add progress.md
   git commit -m "chore(autobuild): log <ITEM> firing"
   git push origin autobuild/state
   ```
   This is how the NEXT firing knows what's done. Skip the local-only
   `git archive-sync` step.

5. **Skip** any runbook step that names a local-only tool/branch: `archive-sync`,
   editing `AGENTS.md`/`CLAUDE.md` "on the archive", the local gh path.

6. **Deploy/verify artifacts** (`make release` / `make docker` / `make helm-lint`)
   — run them if the toolchain is present in the cloud env; if a tool is absent,
   log "skipped — <tool> unavailable" and continue (don't fail the firing).

7. **Gate is mandatory and must be green** before any PR. Use the absolute
   `GOTMPDIR` incantation from `runbook.md` STEP 7.

## Stop condition

When no codeable P1/P2 item remains (all done or all need-user), the loop is done:
append a final `progress.md` row noting "backlog empty — stop", push it, and report.
(A cloud routine can't `CronDelete` itself — the user disables it at
https://claude.ai/code/routines.)

Everything else follows `runbook.md` verbatim.
