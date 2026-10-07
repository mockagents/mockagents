# Developer onboarding

Everything here runs from the **repository root**. The playground is part of the mockagents Go module,
so there is no separate install.

## 1. Prerequisites

- Go (the version pinned in the root `go.mod`). That's it: no C compiler, no database, no Node.
- Optional: Docker, Postman, `python3` (pretty-printing in `scripts/demo.sh`).

Windows note: if Defender locks freshly built test binaries (`Access is denied` on `*.test.exe`), set
`GOTMPDIR` to a folder inside the repo (`.gotmp/` is git-ignored).

## 2. Run it

```bash
go run ./demo/agent-playground/cmd/playground serve          # http://127.0.0.1:7070, manual review
go run ./demo/agent-playground/cmd/playground serve --review-mode auto --log-level debug
```

Then follow the [five-minute tour](../README.md#five-minute-tour).

## 3. Test it

```bash
go test ./demo/agent-playground/... -count=1          # everything (~40 s; boots real mockagents servers)
go test ./demo/agent-playground/... -count=1 -short   # skip the end-to-end verify
go run ./demo/agent-playground/cmd/playground verify --embedded   # 52 live checks with a report
make -C demo/agent-playground mock-test               # mockagents TestSuites pinning the fixtures
```

| Test | Proves |
|---|---|
| `internal/retry`, `guard`, `tools`, `config`, `llm` | Units: backoff math, Retry-After, budget; grounding; calculator/argument validation/idempotency; config validation and atomic updates; wire formats, SSE integrity, error classification |
| `internal/workflow` | Engine guarantees: panic, deadline, watchdog, human-wait exemption, first-writer-wins, shutdown, crash recovery, review-book semantics |
| `internal/workflows` | Every workflow path against a real embedded mockagents |
| `internal/api` | OpenAPI ↔ routes parity, `$ref` hygiene, live responses validated against the spec's schemas, merge-patch config, auth, SSE |
| `internal/cli` | `verify` end to end over real HTTP (52 checks) |
| `fixtures_test.go` | All fixtures validate; the `mock-tests/` suites pass |

## 4. Find your way around

Start with [ARCHITECTURE.md](ARCHITECTURE.md). The call path for one model request:

```
api/agents.go invokeAgent
  -> workflow/engine.go Start           (run, deadline, goroutine, panic recovery)
    -> workflows/single_agent.go Run
      -> workflow/exec.go Invoke        (run overrides, approver, audit item, stats, session id)
        -> agents/runtime.go Invoke     (route, tool loop, continuation)
          -> agents/runtime.go complete (fallback chain x retry.Do x attempt timeout)
            -> llm/openai.go Complete   (wire format, SSE, typed errors)
```

## 5. Common tasks

### Add a tool

1. Implement `tools.Tool` in `internal/tools/` (`Spec()` with a JSON schema; `RequiresApproval()`;
   `Execute()`), and register it in `NewRegistry`.
2. Add it to an agent's `tools` in `config/playground.json` (the config validator rejects unknown tools).
3. Add the tool declaration to that agent's mockagents fixture and a scenario that calls it.
4. Unit-test it in `internal/tools/tools_test.go`. Side-effecting tools must be idempotent and should
   return `RequiresApproval() == true`.

### Add an agent

1. **Fixture.** Create `mockagents/<name>.yaml` with a *unique* `model` (e.g. `llm-translator`) and
   scenarios that match the prompt your workflow sends. Use `content_regex` with named groups to echo
   input, and `turn_number` for multi-turn tool loops. Always add a `default` scenario. Run
   `go run ./cmd/mockagents validate demo/agent-playground/mockagents`.
2. **Config.** Add the agent to `config/playground.json` (`role`, `tier`, `models`, prompt, tools,
   optional `retry` / `fallback`) and its model to `pricing` (plus `config/mock-pricing.yaml`).
   `TestDefaultConfigIsValidAndComplete` fails if a model has no price.
3. **Try it:** `playground invoke <name> "..."` or the Agents tab's *Try it*.
4. **Pin it** with a case in `mock-tests/` (`scenario_matched`, `tool_call`, `response_contains`).

### Add a workflow

1. Create `internal/workflows/<name>.go` implementing `workflow.Definition`:
   - `Info()`: name, title, summary, patterns, agents, input JSON Schema, examples.
   - `Validate(input)`: reject bad input before a run exists. Use `rejectUnknown`, `requireString`, …
   - `Run(ctx, x)`: a short program of `x.Step(...)` blocks calling `x.Invoke`, `x.Escalate`,
     `workflow.Parallel` and `x.ReviewLoop`. Return the output, or `workflow.Fail(code, ...)`.
2. Register it in `workflows.All()`, and add its name to the `WorkflowName` enum in `openapi.yaml`.
3. Add tests in `internal/workflows/workflows_test.go` (boots the embedded mock).

Rules for workflow code (they keep the no-stuck guarantee):
- Pass `ctx` everywhere and never block without it. The watchdog fails runs that stop heartbeating.
- Wait on humans only through `x.Gate` / `x.ReviewLoop` / tool approvals, which are bounded by the review timeout.
- Embed user input into prompts with `guard.SanitizeLine`.
- Parse model JSON with `guard.ExtractJSON`, and decide explicitly what happens when it fails.

### Add an API endpoint

1. Add the handler and register it in `routes()` in `internal/api/server.go`.
2. Document it in `openapi.yaml`. `TestOpenAPIMatchesRoutes` fails until route and spec agree.
3. Validate its response in `TestResponsesMatchSchemas` with `e.valid("<Schema>", body)`.
4. Add it to the Postman collection.

### Point it at real providers

The `llm` clients speak the real wire formats. Run an OpenAI-compatible gateway (or each provider)
behind one base URL with `--mock <url> --mock-key <key>`, and change the agents' `models` to real
model ids. The mock-specific parts (the `/api/mock/*` proxy, drill re-arming) degrade gracefully.
Note that the workflows' prompts and expectations were written against the fixtures.

## 6. Conventions

- Standard library and existing module dependencies only. No new third-party modules.
- Errors carry context; HTTP errors use the `{"error": {...}}` envelope.
- Untrusted input (model output, user text) is validated or escaped at the boundary: tool
  arguments, prompt lines, and HTML in the UI (`esc()`).
- Run `go vet ./...` and the tests before a PR. On this repository, also run `go run ./tools/doccheck`
  (Markdown links) after editing docs.

## 7. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `401 authentication_error: missing API key` | You set an empty `--mock-key` on purpose? The playground sends a placeholder key by default; mockagents rejects requests with no key, like Anthropic. |
| A drill case "did not behave as expected" (retries 0) | The mock's `fail_first` counters were spent and could not be re-armed (external mock without the management API, or without the fixture files on its disk). |
| Runs fail with `review_timeout` | Nobody decided within `defaults.review.timeout_ms`. Use `review_mode: auto` for unattended runs. |
| `playground verify` fails on mock checks | `--mock` points at a mockagents without the playground fixtures; start it with `--agents-dir demo/agent-playground/mockagents`. |
| The UI shows "playground unreachable" | The server stopped, or a `--token` is required and missing (set `localStorage['playground-token']` in the browser). |
