# Verification record

How each requirement was verified, with the evidence. Recorded 2026-10-07 on Windows 11, Go 1.26.6.
Reproduce with the commands in each row.

## Summary

| Check | Result | Command |
|---|---|---|
| Playground Go tests (unit, integration, contract, end-to-end) | **69 tests, all pass** | `go test ./demo/agent-playground/... -count=1` |
| Statement coverage across playground packages | **73.0%** (agent runtime ≈91%, workflows 82%, guard 91%, retry 89%) | `go test ./demo/agent-playground/... -coverpkg=./demo/agent-playground/... -coverprofile=c.out` |
| End-to-end self-test, embedded mock | **52 / 52 checks pass** | `playground verify --embedded` |
| End-to-end self-test, external `mockagents` binary | **52 / 52 checks pass** | `mockagents start --agents-dir demo/agent-playground/mockagents` + `playground serve --mock …` + `playground verify` |
| Postman collection (executed in order, variables chained) | **41 / 41 requests pass** | collection runner against `playground serve` |
| curl walkthrough | all 8 sections complete; final counts `completed: 6, failed: 0, in_progress: 0` | `scripts/demo.sh` |
| mockagents fixture validation | **26 / 26 documents valid** (25 agents + 1 pipeline) | `mockagents validate demo/agent-playground/mockagents` |
| mockagents fixture contract suites | **8 / 8 cases pass** (3 suites) | `mockagents test --agents-dir … mock-tests/` |
| OpenAPI ↔ server parity + schema validation of live responses | pass | `TestOpenAPIMatchesRoutes`, `TestResponsesMatchSchemas` |
| Web UI, driven in a real browser | every tab works, no console errors | see [UI](#ui) |
| Crash recovery (hard kill during a review wait) | run restored as `failed` / `interrupted`, 0 runs in progress | see [No stale states](#no-stale-states) |
| Container image | builds and serves (`/readyz` ready, a workflow completes) | `docker build -f demo/agent-playground/Dockerfile .` |
| Repository gate (`go vet ./...`, `go test ./...`, doccheck, liquidcheck) | see [Repository gate](#repository-gate) | |

## Requirement by requirement

| Requirement | Evidence |
|---|---|
| **All agents run** | `verify` invokes all 23 agents directly over HTTP. 22 complete; `drill-overloaded` fails cleanly by design (`failed cleanly as designed: agent drill-overloaded failed on openai/llm-overloaded: gave up after 3 attempt(s) (http_503)…`). |
| **All tools execute** | `verify` runs `search_kb` (→ KB-101), `calculator` (`200 * 2^4` → 3200), `lookup_order` (ORD-1002) through the API. `issue_refund` returns **403** when called directly, and executes inside the triage workflow after human approval. Tool unit tests cover argument validation, idempotency and bounds. |
| **Orchestrator flows complete** | research-brief, support-triage (4 paths), decision-review (3 paths) and resilience-drill all reach `completed` in `internal/workflows` tests and in `verify`. |
| **Retry logic works** | `internal/retry` tests: exponential schedule (200/400/800/1000/1000 ms with a 1000 ms cap), jitter band, Retry-After honoured above the cap and capped at 30 s, exhaustion at exactly `max_retries+1` attempts for 0-5, permanent errors not retried, no sleep past the deadline (`ErrBudgetExceeded`), cancellation during backoff. Live: the drill observed `flaky` recovered after 2 retries, `rate-limited` with 2 retries honouring Retry-After then fallback, `connection-reset` recovered after 1 retry, and `timeout` with 2 attempt timeouts then fallback. A policy with 6 retries is rejected with 400 (API, config and run options). |
| **No stale workflow states** | The schema allows only `in_progress`, `completed` and `failed`, and the contract test asserts that `"stuck"` is rejected. Engine tests: deadline, watchdog stall, watchdog spares human waits but not past the deadline, panic, cancel, first-writer-wins, shutdown, restart recovery. `verify` ends by asserting 0 runs `in_progress` after about 30 runs. |
| **API endpoints respond correctly** | `TestResponsesMatchSchemas` validates every JSON response against `openapi.yaml` (unknown fields, missing fields and bad enums are caught; a negative test proves the validator bites). Error paths: 400 (bad input, unknown field, invalid config, disallowed action), 401 (token), 403 (approval-gated tool), 404, 409 (double decision, cancelling a finished run). |
| **OpenAPI spec validates** | All 35 routes are documented and every documented operation is served (both directions); every `$ref` resolves; operationIds are unique; every component schema compiles under JSON Schema 2020-12 (OpenAPI 3.1 dialect) with `santhosh-tekuri/jsonschema/v6`. |
| **SLM/LLM routing works** | Router unit tests (policy, pin, override, missing-tier fallback, escalation). Live: a general ticket uses only the SLM tier (`0 LLM calls`); an ambiguous ticket escalates (SLM 0.48 < 0.6 → LLM technical 0.86); a hallucinated SLM summary escalates to `anthropic/llm-summarizer`; `PATCH tier=llm` reroutes the summarizer on the next call. |
| **Human-review checkpoints appear** | Every AI output creates an `output_audit` item (`verify`: more than 20 audit items). Final outputs gate on `output_gate`. `issue_refund` gates on `tool_approval`. Tested flows: approve; reject → `review_rejected`; revise → editor → new gate → approve; tool decline → alternative reply; unanswered → `review_timeout` with the gate `expired`; `auto` mode → `auto_approved`. |
| **Documentation complete and consistent** | README plus 8 docs. Local Markdown links checked by `tools/doccheck`. Numbers in the docs (agents, routes, checks, requests, tests) were taken from the runs above. |

## UI

Driven in the Claude desktop app's built-in browser against `playground serve`:

- **Workflows:** 4 cards with patterns, agents, examples and input editors; launching navigates to the run.
- **Runs:** support-triage (manual): the live view showed `awaiting_approval` with an inline approval
  card. The trace showed the `issue_refund` tool span waiting on its review. Approve → reply gate →
  revise (the editor appended the feedback) → approve → `completed` (6 steps).
- **Trace waterfall** for the resilience drill: rate-limited shows 3 failing attempts spaced by
  Retry-After, then the `slm-backup` fallback; timeout shows two 800 ms attempt timeouts; 63 spans.
- **Agents & Config:** the editor and *Try it* work (summarizer → `slm`, `openai/slm-summarizer`).
- **Chat:** streaming answer plus a route event chip. **Mock:** status (25 fixtures), cost by model,
  canonical-YAML fixture load, the native pipeline. **API:** 35 operations.
- Console: no errors.

## No stale states

Live crash test: start `decision-review` in manual mode (it parks in `awaiting_review`), kill the
process hard (`Stop-Process -Force`), restart with the same `--state-file`:

```
before: run_044508_0005 in_progress awaiting_review
WARN msg="runs interrupted by the previous shutdown were marked failed" count=1
after:  run_044508_0005 failed {'code': 'interrupted', 'message': 'the server stopped while this run was in progress; it was failed on restart instead of being left stale'}
in_progress now: {'completed': 4, 'failed': 1, 'in_progress': 0}
```

## Container

`docker build -f demo/agent-playground/Dockerfile -t mockagents-playground .` succeeds. The container
serves `/readyz` (embedded mock: 25 agents loaded) and completes a decision-review run.

Caveat: on the verification machine (Podman 6.0.2) container networking failed with a
netavark/nftables error unrelated to the Dockerfile. The build was therefore run with `--network=host`
and the container with host networking. The bridge-network `-p 7070:7070` path and the compose file
were **not** exercised there.

## Repository gate

| Check | Result |
|---|---|
| `gofmt -l demo/agent-playground` | clean |
| `go vet ./...` | clean |
| `go test ./... -count=1` (whole repository, including the playground) | **49 packages pass, 0 failures** (the playground included) |
| `go run ./tools/doccheck` | clean |
| `go run ./tools/liquidcheck` | clean |

## Not verified

- Real provider APIs (by design, everything runs against mockagents; the wire formats are covered by
  provider-fake unit tests and by mockagents' own fidelity).
- Kubernetes manifests in [DEPLOYMENT.md](DEPLOYMENT.md) are examples and were not applied to a cluster.
- Multi-tenant mockagents as the external mock.
- Load or concurrency beyond the parallel steps the workflows use.
