# Acme Support Triage — a MockAgents demo (Claude Agent SDK)

A realistic **agentic AI** application built with the **[Claude Agent SDK]**
(`claude-agent-sdk`) that runs entirely against **[MockAgents]** instead of the
real Anthropic API. It's a customer-support *triage* agent: it greets customers,
looks up orders, issues refunds, and escalates to a human — using in-process MCP
tools, with **zero tokens, zero cost, and fully deterministic behavior**.

This is the Claude-side twin of the [OpenAI Agents SDK demo](../customer-support-agent/);
same use case, same four feature areas, exercised over the **Anthropic Messages
API** (`/v1/messages`).

[Claude Agent SDK]: https://docs.claude.com/en/api/agent-sdk/overview
[MockAgents]: ../../README.md

## How it talks to the mock (and why it took two MockAgents fixes)

The Claude Agent SDK doesn't call HTTP itself — it spawns the **`claude` CLI** as
a subprocess, and the CLI calls the model API. The CLI honors `ANTHROPIC_BASE_URL`
+ `ANTHROPIC_API_KEY`, which we pass via `ClaudeAgentOptions(env=...)`, so the CLI
talks to MockAgents' `/v1/messages` surface.

Making this work surfaced — and the demo depends on — two genuine
spec-compliance fixes to MockAgents' Anthropic adapter (`internal/adapter/anthropic.go`),
both of which had blocked *any* real Anthropic SDK/CLI client:

1. `system` may be an **array of content blocks** (not just a string).
2. a `tool_result` block's `content` may be an **array of blocks** (not just a string).

## What it demonstrates

| MockAgents feature | Where | How to see it |
| --- | --- | --- |
| **Scenario matching + tool calls** | `mockagents/support-agent.yaml`, `app/triage_demo.py` | The agentic loop: turn 1 returns a `tool_use`, the CLI runs the in-process MCP tool, turn 2 returns the final answer. |
| **Chaos / fault injection** | `mockagents/support-agent-flaky.yaml`, `app/resilience_demo.py` | First 2 requests 503, then recover — proving retry/backoff works. |
| **Streaming + load-target latency** | `mockagents/load-target.yaml`, `app/streaming_demo.py`, `scripts/k6-loadtest.js` | Anthropic SSE deltas + realistic p50/p95 TTFT/ITL for free load testing. |
| **Multi-tenancy + quotas + cost** | `scripts/multitenant_walkthrough.sh` | Per-tenant API keys, rate (429) / spend (402) caps, and the cost aggregate. |

## Two Claude-specific design notes

- **Tool names are MCP-namespaced.** In-process SDK tools are exposed to the CLI
  as `mcp__<server>__<tool>`. The demo registers an MCP server `acme`, so the mock
  must emit `mcp__acme__lookup_order` (etc.) as the tool call — see the scenario
  `tool_calls` in `mockagents/support-agent.yaml`.
- **Matching is content-based, not turn-based.** The CLI sends no `X-Session-Id`,
  so MockAgents' turn counter can't disambiguate the loop (unlike the OpenAI demo).
  Instead, each tool returns a unique MARKER (`ORDER_RESULT` / `REFUND_RESULT` /
  `ESCALATION_RESULT`); in the Anthropic protocol the tool result comes back as the
  next user message, and the `*-resolved` scenarios match the marker to emit the
  final answer (and are listed first so a marker resolves instead of re-triggering
  the tool — that's what stops the loop).

`app/mock_setup.py` also blanks a few env vars (`CLAUDE_CODE_ENTRYPOINT`, etc.) so
the demo stays deterministic even if you run it *from inside* Claude Code (which
would otherwise inject the parent session's skills into the user message). On a
normal machine those are unset and it's a no-op.

## Models (routing keys)

Routing is by model name; each fixture uses a distinct, real (price-table) model
so the cost dashboard and spend cap work out of the box:

| Model the client requests | MockAgents agent | Role |
| --- | --- | --- |
| `claude-3-5-sonnet-latest` | `acme-support-claude` | Triage agent: tools + scenarios + streaming |
| `claude-3-5-sonnet-20241022` | `acme-support-flaky-claude` | Chaos twin: 503s first 2 requests then heals |
| `claude-3-sonnet-20240229` | `acme-load-target-claude` | Streaming load-test target |

## Layout

```
customer-support-agent-claude/
├── mockagents/                 # MockAgents agent definitions (Anthropic protocol)
│   ├── support-agent.yaml       #  triage: MCP-namespaced tools + content-matched scenarios + streaming
│   ├── support-agent-flaky.yaml #  chaos twin (fail_first 503)
│   └── load-target.yaml         #  streaming load-test target (lognormal latency)
├── app/                        # the Claude Agent SDK application
│   ├── mock_setup.py            #  point the SDK/CLI at MockAgents (the only mock-specific code)
│   ├── support_tools.py         #  in-process MCP tools (@tool) + the "acme" server
│   ├── triage_demo.py           #  headline demo: drive the triage agent (Agent SDK)
│   ├── resilience_demo.py       #  chaos/retry demo (plain Anthropic SDK)
│   ├── streaming_demo.py        #  streaming demo (plain Anthropic SDK)
│   └── deterministic_smoke.py   #  framework-free contract test (plain Anthropic SDK)
├── scripts/                    # curl smoke, multi-tenant walkthrough, k6 load test
├── k8s/                        # Helm values + demo Job for the Kubernetes path
├── Dockerfile · docker-compose.yml · requirements.txt · .env.example
└── TESTING.md                  # step-by-step test guide (Docker + Kubernetes)
```

## Quickstart (Docker Compose)

```bash
cd demo/customer-support-agent-claude

# 1. Start MockAgents with the three Anthropic agents.
docker compose up --build -d mockagents

# 2. Run the headline triage demo (the demo image bundles Node + the claude CLI).
docker compose run --rm demo                       # python -m app.triage_demo

# 3. Try the others.
docker compose run --rm demo python -m app.deterministic_smoke
docker compose run --rm demo python -m app.streaming_demo
docker compose run --rm demo python -m app.resilience_demo
```

Run the demo app **locally** (needs Python **and** the `claude` CLI on PATH —
`npm i -g @anthropic-ai/claude-code`):

```bash
pip install -r requirements.txt
export MOCKAGENTS_BASE_URL=http://localhost:8080    # server ROOT, no /v1
export MOCKAGENTS_API_KEY=mock-key
python -m app.triage_demo
```

Expected triage output (abridged):

```
USER : Hello!
  AGENT: Hi! I'm Acme's support assistant. ...
USER : Where is my order?
  AGENT: Let me pull that order up for you.
  [tool call] mcp__acme__lookup_order {'order_id': 'ORD-12345'}
  AGENT: Good news — order ORD-12345 shipped via UPS ...
USER : I'd like a refund please
  [tool call] mcp__acme__issue_refund {'amount': 49.99, 'order_id': 'ORD-12345'}
  AGENT: Done — refund RF-88231 for $49.99 is approved ...
USER : I need to speak to a human
  [tool call] mcp__acme__escalate_to_human {'reason': 'Customer requested a human agent'}
  AGENT: You're all set — I've opened ticket SUP-1042 ...
```

## Full test guide

See **[TESTING.md](./TESTING.md)** — step-by-step instructions for all four
feature areas, documented separately for **Docker Compose** and **Kubernetes/Helm**.
