# Agent Playground

A multi-agent application that runs entirely on **[mockagents](../../README.md)**. It has 23 agents,
4 workflows, a REST API with an OpenAPI 3.1 spec, a web UI and a CLI. Every model call goes over the
real OpenAI, Anthropic or Gemini wire format to a mockagents server, so every run is free, offline and
the same each time, including the failures.

It is a working reference for the patterns that matter in agentic systems:

- orchestrator/worker plans
- tool use
- SLM/LLM routing with escalation
- multi-agent arbitration
- hallucination guards
- retries with exponential backoff and fallback chains
- human review checkpoints
- full traces
- runs that cannot get stuck

```bash
go run ./demo/agent-playground/cmd/playground serve      # from the repository root
# open http://127.0.0.1:7070
```

That one command starts the playground **and** an embedded mockagents server loaded with the
playground's fixtures. Nothing else to install: no API keys, no Docker, no Python.

---

## Contents

- [Five-minute tour](#five-minute-tour)
- [What it demonstrates](#what-it-demonstrates)
- [Workflows](#workflows) · [Agents and tools](#agents-and-tools)
- [Ways to run it](#ways-to-run-it)
- [Using the API](#using-the-api) · [Using the CLI](#using-the-cli)
- [Configuration](#configuration)
- [Testing and verification](#testing-and-verification)
- [Layout](#layout)
- [Limitations](#limitations)
- Documentation: [Architecture](docs/ARCHITECTURE.md) · [Workflows](docs/WORKFLOWS.md) ·
  [Agents & tools](docs/AGENTS.md) · [Patterns](docs/PATTERNS.md) · [API](docs/API.md) ·
  [Developer onboarding](docs/ONBOARDING.md) · [Deployment](docs/DEPLOYMENT.md) ·
  [Verification record](docs/VERIFICATION.md)

## Five-minute tour

1. **Workflows tab.** Launch **Research brief** with the `retry-strategies` example. The run view
   shows the planner (LLM), the researcher calling `search_kb` and `calculator` in parallel, the SLM
   summary, the grounding guard and the LLM judge. Then it stops at **awaiting_review**.
2. **Reviews tab.** Approve the brief, or choose **revise** with a comment and watch the editor agent
   redraft it and send it back for review.
3. Launch **Research brief** again with `catch-a-hallucination`. The SLM summarizer returns a planted
   fabrication ("97% cost savings… 2024 study… [KB-999]"). The guard flags the unsupported figures and
   the unknown citation, the judge agrees, and the summary is regenerated on the LLM tier.
4. Launch **Support triage** with `duplicate-charge` (review mode *manual*). The billing agent looks up
   the order, then asks to call `issue_refund`, a side-effecting tool that **waits for your approval**.
   Approve it, or decline it and see the agent write a different reply.
5. Launch **Resilience drill**. Eight injected faults (503s, 429 + Retry-After, timeouts, TCP resets,
   truncated answers, malformed tool arguments, a cut SSE stream, a full outage), each recovered or
   failed cleanly. The **Trace** waterfall shows every attempt, backoff and fallback.
6. **Agents & Config tab.** Pin the summarizer to the LLM tier, give an agent 5 retries, or raise the
   escalation threshold, then rerun. Invalid settings (for example 6 retries) are rejected with
   field-level errors.
7. **Mock tab.** The mockagents side of the same traffic: per-request scenario matches, token counts
   and cost; the native mockagents pipeline; live fixture editing.

## What it demonstrates

| Requirement | How the playground does it | Where |
|---|---|---|
| Uses mockagents directly from the repo | Embedded mockagents server (same Go module), the in-repo Go SDK for the management API, YAML fixtures, a native `Pipeline`, `TestSuite` contract tests | [`internal/mockhost`](internal/mockhost/mockhost.go), [`internal/mockctl`](internal/mockctl/mockctl.go), [`mockagents/`](mockagents), [`mock-tests/`](mock-tests) |
| Multiple agents, tools, orchestrators, workflows | 23 agents, 4 tools, 4 workflows plus single-agent invoke | [Agents](docs/AGENTS.md), [Workflows](docs/WORKFLOWS.md) |
| Agents exposed over an API (REST, Postman, curl) | 35 operations, OpenAPI 3.1 at `/openapi.yaml`, a Postman collection, a curl walkthrough | [`openapi.yaml`](openapi.yaml), [`postman/`](postman), [`scripts/demo.sh`](scripts/demo.sh), [API](docs/API.md) |
| End-user agent configuration | JSON config file plus runtime JSON merge-patch (`PATCH /api/agents/{name}`, `PATCH /api/config`) and a UI editor. Every change is validated and atomic | [`config/playground.json`](config/playground.json), [`internal/config`](internal/config/config.go) |
| Multi-agent reasoning, decision review, hallucination reduction | Cross-provider proposers, consensus short-circuit, critic and arbiter, confidence escalation; a deterministic grounding guard plus an LLM judge | [`decision_review.go`](internal/workflows/decision_review.go), [`research_brief.go`](internal/workflows/research_brief.go), [`internal/guard`](internal/guard/guard.go) |
| SLMs for summarization, LLMs for reasoning | Role-based router (summarize/classify/extract/generate -> SLM; plan/reason/verify/judge/chat -> LLM) with escalation on low confidence or a failed guard | [`internal/router`](internal/router/router.go) |
| Retry with exponential backoff, up to 5 | Validated policy (0-5 retries), jitter, `Retry-After`, a per-attempt timeout, never sleeps past the deadline, then the fallback chain | [`internal/retry`](internal/retry/retry.go), [`internal/agents`](internal/agents/runtime.go) |
| No stuck workflows | Status is only `in_progress`, `completed` or `failed`; deadlines, review timeouts, a watchdog, panic recovery, first-writer-wins finish, crash recovery on restart | [`internal/workflow`](internal/workflow/types.go) |
| Playground UI and CLI | Embedded web UI (no build step) and the `playground` CLI | [`web/`](web), [`internal/cli`](internal/cli/commands.go) |
| Human review of all AI output | Every output is an audit item; final outputs pass a blocking gate (approve / reject / revise); side-effecting tools need approval | [`internal/workflow/exec.go`](internal/workflow/exec.go) |
| Logging and trace visualization | Span tree per run (UI waterfall, CLI tree), live SSE events, structured logs, Prometheus `/metrics`, mock interaction logs correlated by run id | [`internal/trace`](internal/trace/trace.go), [Architecture](docs/ARCHITECTURE.md#observability) |

## Workflows

| Workflow | Agents | Patterns |
|---|---|---|
| **research-brief** | planner → researcher (+tools) → summarizer (SLM) → guard + verifier → [regenerate on LLM] → human | orchestrator-worker, structured output, parallel tool calls, grounding guard, LLM-as-judge, escalation, revise loop |
| **support-triage** | classifier (SLM, Gemini) → [LLM if unsure] → billing / technical / general specialist → human | router, confidence thresholds, multi-turn tool loop, approval-gated idempotent tool, feedback injection |
| **decision-review** | proposer A (OpenAI) ‖ proposer B (Anthropic) → consensus? → critic → arbiter → [human if unsure] | parallel fan-out, cross-model consistency, debate, arbitration, confidence escalation |
| **resilience-drill** | 8 fault-injection agents + backup | retries, Retry-After, timeouts, fallback, transport errors, continuation, argument validation, stream downgrade, bounded failure |

Details, diagrams and example outputs: [docs/WORKFLOWS.md](docs/WORKFLOWS.md).

## Agents and tools

23 agents ([full catalog](docs/AGENTS.md)): `planner`, `researcher`, `summarizer` (SLM + LLM), `verifier`,
`classifier` (SLM + LLM), `billing-specialist`, `tech-specialist`, `reply-writer`, `editor`,
`proposer-a`, `proposer-b`, `critic`, `arbiter`, `assistant` (streaming chat), `backup`, and eight
`drill-*` fault agents. They span **OpenAI, Anthropic and Gemini** wire formats. Each agent's model name
selects its mockagents fixture.

Tools: `search_kb` (knowledge base), `calculator` (safe parser, no eval), `lookup_order`, and
`issue_refund` (side-effecting: needs human approval, idempotent per order).

## Ways to run it

| How | Command | Mock |
|---|---|---|
| Go, one process | `go run ./demo/agent-playground/cmd/playground serve` | embedded |
| Make (repo root) | `make playground` | embedded |
| Auto-approve reviews | `… serve --review-mode auto` | embedded |
| External mockagents | `make -C demo/agent-playground mock` then `… serve --mock http://127.0.0.1:8080` | separate process |
| Docker | `docker build -f demo/agent-playground/Dockerfile -t mockagents-playground .` then `docker run -p 7070:7070 mockagents-playground` | embedded |
| Compose | `cd demo/agent-playground && docker compose up --build` (or `--profile external`) | embedded / container |

Persist runs across restarts with `--state-file runs.json`. Protect mutating calls with `--token`.
See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for Kubernetes, environment variables and pointing the
playground at real providers.

## Using the API

```bash
# Start a run and wait (up to 30 s) for it to finish or to need a human
curl -s 'http://127.0.0.1:7070/api/workflows/research-brief/runs?wait=30' \
  -H 'Content-Type: application/json' \
  -d '{"input":{"topic":"retry strategies"},"options":{"review_mode":"auto"}}'

# Invoke one agent; stream its tokens as Server-Sent Events
curl -N http://127.0.0.1:7070/api/agents/assistant/invoke \
  -H 'Content-Type: application/json' -d '{"input":"how do retries work?","stream":true}'

# Decide a pending review
curl -s http://127.0.0.1:7070/api/reviews/rev_0002/decision \
  -H 'Content-Type: application/json' -d '{"action":"approve","reviewer":"me"}'
```

- **OpenAPI 3.1:** `GET /openapi.yaml` (or [`openapi.yaml`](openapi.yaml)); an API explorer is in the UI's API tab.
- **Postman:** import [`postman/agent-playground.postman_collection.json`](postman/agent-playground.postman_collection.json)
  (41 requests; folder 3 walks the human-review flow and chains `run_id` / `review_id`).
- **curl tour:** [`scripts/demo.sh`](scripts/demo.sh).
- Reference: [docs/API.md](docs/API.md).

## Using the CLI

```bash
alias pg='go run ./demo/agent-playground/cmd/playground'
pg workflows                                   # catalog + examples
pg run support-triage --example duplicate-charge --review-mode manual
pg reviews                                     # pending checkpoints
pg approve rev_0002 --comment "duplicate confirmed"
pg revise rev_0004 --comment "add an apology"
pg trace run_101500_0001                       # span tree: attempts, retries, tools, reviews
pg invoke assistant "what can you do?" --stream
pg configure summarizer tier=llm retry.max_retries=5
pg config set router.escalation_threshold=0.8
pg verify --embedded                           # 52-check self-test
```

## Configuration

[`config/playground.json`](config/playground.json) holds the agent catalog plus `defaults` (retry
policy, timeouts, max tool turns, review policy), `router` (role → tier, escalation threshold),
`workflows` (arbitration threshold, grounding regenerations, watchdog stall) and `pricing`. Pass your
own with `--config`. At runtime use the UI, `PATCH /api/agents/{name}`, `PATCH /api/config` or
`POST /api/config/reset`. Field reference: [docs/AGENTS.md#configuration-reference](docs/AGENTS.md#configuration-reference).

## Testing and verification

```bash
go test ./demo/agent-playground/... -count=1          # 69 tests: unit, integration, contract, end-to-end
go run ./demo/agent-playground/cmd/playground verify --embedded   # 52 live checks
go run ./cmd/mockagents test --agents-dir demo/agent-playground/mockagents demo/agent-playground/mock-tests/
```

The Go tests boot a real mockagents server in-process and drive every workflow path. They check the
route table against `openapi.yaml` in both directions and validate live responses against the spec's
schemas. They also prove the watchdog, deadline, panic and crash-recovery guarantees. They run in the
repository's `go test ./...` and therefore in CI. Results: [docs/VERIFICATION.md](docs/VERIFICATION.md).

## Layout

```
demo/agent-playground/
├── cmd/playground/          # main: serve + CLI
├── internal/
│   ├── llm/                 # OpenAI / Anthropic / Gemini clients (stdlib only), SSE, error classes
│   ├── retry/               # backoff policy, Retry-After, deadline-aware Do
│   ├── router/              # SLM/LLM tier routing + escalation
│   ├── agents/              # agent runtime: route -> retry -> fallback -> tool loop -> continuation
│   ├── tools/               # search_kb, calculator, lookup_order, issue_refund + argument validation
│   ├── guard/               # grounding guard, tolerant JSON extraction, prompt-line sanitizing
│   ├── trace/               # per-run span tree
│   ├── workflow/            # engine: runs, steps, reviews, watchdog, persistence
│   ├── workflows/           # research-brief, support-triage, decision-review, resilience-drill, agent
│   ├── config/              # config model, validation, atomic live store
│   ├── metrics/             # Prometheus text exposition
│   ├── api/                 # REST + SSE handlers, OpenAPI serving, mock proxy, UI
│   ├── mockhost/            # embedded mockagents server
│   ├── mockctl/             # mockagents management client (uses sdk/go/mockagents)
│   ├── app/                 # wiring + lifecycle
│   └── cli/                 # CLI commands + `verify`
├── mockagents/              # 25 agent fixtures + 1 native pipeline (mockagents YAML)
├── mock-tests/              # mockagents TestSuites pinning the fixtures
├── config/                  # playground.json, mock-pricing.yaml
├── web/                     # UI (vanilla JS/CSS, embedded)
├── openapi.yaml · postman/ · scripts/ · docs/
└── Dockerfile · docker-compose.yml · Makefile
```

## Limitations

The playground is honest about what a mock can and cannot do:

- **The fixtures are scripted.** Agents answer from mockagents scenarios keyed on the prompt (topic
  keywords, ticket words, question keywords and turn numbers). Topics outside the keyed set (retries,
  routing, hallucination) get a generic, still-grounded brief. Swap the base URL for a real provider
  and the same code produces real answers. Only the content changes.
- **The editor "revises"** by appending your feedback to the draft. That is enough to exercise the
  revise loop, not to demonstrate writing quality.
- **Gemini is text-only** in this demo (used for the SLM classifier). Tool-using agents run on OpenAI
  or Anthropic wire formats, and the config validator enforces that.
- **State is in memory**, with an optional JSON state file. Runs that were in flight when the process
  stopped are marked `failed` (`interrupted`) on restart. They are never resumed or left stale.
- **Single process.** The engine is not distributed; see [Deployment](docs/DEPLOYMENT.md#scaling-notes).
