# MockAgents — QA Troubleshooting Guide

**Document ID:** MA-QA-TS-001 · **Version:** 1.0 · **Last updated:** 2026-07-15
**Companion to:** `MANUAL-TEST-PLAN.md` (MA-QA-TP-001 v1.2)

Symptom → cause → fix, for the issues QA actually hits while executing the test plan.
Every entry says whether it's a **product defect** (log it in §18 of the plan) or an
**environment issue** (fix your setup and continue). Issues found in QA cycle 1 are
marked with their defect id.

---

## 1. Server & Docker

### `mkdir /starter: permission denied` on `mockagents init` in a container — MA-DEF-001
- **Cause (pre-`888b59d`):** the runtime image had no `WORKDIR`, so the non-root
  `mockagents` user ran from unwritable `/`.
- **Fixed in `888b59d`:** the image runs from the writable `/data` volume. On current
  builds this error means you're on a stale image — re-run `make docker` and check
  `docker run --rm --entrypoint pwd mockagents:latest` prints `/data`.
- To scaffold onto the host instead: `-v "$PWD/scaffold-out:/work" -w /work`.

### WARN `interaction logging disabled ... unable to open database file: out of memory (14)` — MA-DEF-002
- **This is NOT memory exhaustion.** SQLite error 14 is `SQLITE_CANTOPEN` — the DB file's
  directory is not writable. The driver's message text is misleading.
- **Fixed in `888b59d`:** DBs land in `/data` (persisted by the compose volume), and the
  WARN now includes the DB path plus a hint. On current builds, startup must log
  `interaction logging enabled` / `audit logging enabled`.
- If you deliberately run with a read-only or unwritable workdir, set
  `MOCKAGENTS_DATA_DIR=/some/writable/dir` — it relocates `.mockagents.db`,
  `.mockagents-audit.db`, and `.mockagents-tenancy.db` (directory auto-created).
- **Consequence when disabled:** the server still serves traffic (degraded mode), but
  `/api/v1/logs`, `/api/v1/costs`, `/api/v1/audit` and the GUI `/logs`/`/costs`/`/audit`
  pages will be empty. Don't log GUI-page defects while this WARN is present.

### Health check fails / connection refused on 8080
- Container port not published (`-p 8080:8080`), another process owns 8080
  (`netstat -ano | findstr :8080` on Windows), or the server bound to loopback inside
  the container — compose sets `MOCKAGENTS_HOST=0.0.0.0`; plain `docker run` needs
  `--host 0.0.0.0` (the image CMD already passes it; only override knowingly).

### `no valid agent definitions found`
- The agents volume isn't mounted or is empty: `mkdir -p agents && cp examples/*.yaml agents/`
  before `docker compose up` (TC-ENV-02). Validate with
  `docker run --rm -v "$PWD/agents:/agents:ro" mockagents:latest validate /agents`.

## 2. CLI & test runner

### `zsh: no matches found: /tests/*.yaml` — MA-DEF-003
- zsh expands globs *before* the command runs and aborts on no match (the container
  path doesn't exist on your host). **Quote the pattern:**
  `mockagents test 'tests/*.yaml'` — since `888b59d` the CLI expands globs itself,
  identically on zsh/bash/PowerShell and inside `docker run` args. An unmatched quoted
  pattern gives a clear `no test suite files match pattern` error (non-zero exit).

### WARN `model claimed by multiple agents` — MA-DEF-004 (by design, better message now)
- Several example agents share `model: gpt-4o` on purpose. Routing is deterministic:
  the **lexicographically smallest agent name** wins the model claim. Since `888b59d`
  the WARN names the winner (`wins=<agent>`). You do not need to isolate agents to
  find the routing target. Give each agent a distinct `spec.model` if you need every
  agent reachable by model.

### `mockagents: command not found` in exec/entrypoint contexts
- The image entrypoint IS `mockagents`, so `docker run mockagents:latest <subcommand>`
  works, but `docker compose exec mockagents mockagents ...` needs the binary name once
  (the first `mockagents` is the service name).

## 3. GUI

### `Error: listen EADDRINUSE: address already in use :::3001` — MA-DEF-005 (environment)
- Something else owns 3001. Either run on another port — `npx next dev --port 3002`
  from `gui/` — or free it (Windows: `netstat -ano | findstr :3001` →
  `taskkill /PID <pid> /F`; macOS/Linux: `npx kill-port 3001`). The GUI port doesn't
  affect any test expectation; the API stays on 8080.

### Health pill stuck on "offline" (Windows)
- Node prefers IPv6 for `localhost` while the Go server binds IPv4. Set
  `MOCKAGENTS_API_URL=http://127.0.0.1:8080` before `npm run dev`. (See `gui/README.md`.)

### Logs / costs / audit pages empty
- First check the server startup logs for the `logging disabled` WARN (§1 above) —
  that's the usual cause, not a GUI bug. Only file a GUI defect if the API
  (`GET /api/v1/logs`) returns rows the page doesn't show.

## 4. Protocol testing

### PowerShell curl mangles JSON bodies
- `curl` is an alias for `Invoke-WebRequest`; use `curl.exe`, and prefer
  `--data @body.json` files over inline JSON (see plan §4.1).

### SSE/WebSocket output looks buffered or truncated
- Use `curl -N` for SSE and `websocat -t` for Realtime; don't pipe streams through
  tools that buffer (some `jq` invocations). For stream-fault cases a cut-off
  without `data: [DONE]` may be the **configured fault**, not a bug — check the
  agent YAML before filing.

### Realtime VAD cases never fire speech events
- The `<SPEECH>`/`<SILENCE>` chunks must be generated per the recipe in plan §8.3 —
  real PCM16 energy is measured. Also remember: committed audio always transcribes to
  `[audio input]` (no STT), so VAD turns match the agent's *default* scenario.

## 5. When in doubt

1. `docker compose logs mockagents` — startup WARNs explain most "missing data" symptoms.
2. Reproduce via the raw API (`curl`) before blaming the GUI or an SDK.
3. Check §16 of the test plan — if a fix round covers the area, run its linked regression case.
4. New defect: log it in the plan's §18 with severity per §5.3, and add a tracker row.
