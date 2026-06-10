# Acme Support Triage — a MockAgents demo app

A realistic **agentic AI** application built with the **[OpenAI Agents SDK]** that
runs entirely against **[MockAgents]** instead of a real LLM provider. It's a
customer-support *triage* agent: it greets customers, looks up orders, issues
refunds, and escalates to a human — using simulated tool calls, with **zero
tokens, zero cost, and fully deterministic behavior**.

The point of the demo is to **exercise and test MockAgents' features** with a
real agent framework. The only thing that makes it a *mock* integration is the
base URL — the agent code (tools, instructions, run loop) is ordinary Agents
SDK code.

[OpenAI Agents SDK]: https://openai.github.io/openai-agents-python/
[MockAgents]: ../../README.md

## What it demonstrates

| MockAgents feature | Where | How to see it |
| --- | --- | --- |
| **Scenario matching + tool calls** | `mockagents/support-agent.yaml`, `app/triage_demo.py` | The agent's tool-calling loop: turn 1 returns a tool call, turn 2 returns the final answer. |
| **Chaos / fault injection** | `mockagents/support-agent-flaky.yaml`, `app/resilience_demo.py` | First 2 requests 503, then recover — proving retry/backoff works. |
| **Streaming + load-target latency** | `mockagents/load-target.yaml`, `app/streaming_demo.py`, `scripts/k6-loadtest.js` | SSE token deltas + realistic p50/p95 TTFT/ITL for free load testing. |
| **Multi-tenancy + quotas + cost** | `scripts/multitenant_walkthrough.sh` | Per-tenant API keys, rate (429) / spend (402) caps, and the cost aggregate. |

Routing is by **model name**, so each fixture uses a distinct, real (price-table)
model — which also makes the cost dashboard and spend cap work out of the box:

| Model the client requests | MockAgents agent | Role in the demo |
| --- | --- | --- |
| `gpt-4o` | `acme-support` | Triage agent: tools + scenarios + streaming |
| `gpt-4o-mini` | `acme-support-flaky` | Chaos twin: 503s first 2 requests then heals |
| `gpt-3.5-turbo` | `acme-load-target` | Streaming load-test target (lognormal latency) |

## How the agent loop stays deterministic

MockAgents matches scenarios on the **latest user message**, which stays identical
across an agent's tool-calling loop (the tool result returns with role `tool`, not
`user`). So the support agent **gates tool-call scenarios on `turn_number: 1`** and
lets turn 2 fall through to a resolution scenario.

For turn numbers to advance, every request in one conversation must send a **stable
`X-Session-Id`**. The demo sets a fresh one per conversation (see
`app/mock_setup.py → new_conversation_client`). The two SDK switches that make this
work:

1. `set_default_openai_api("chat_completions")` — the Agents SDK defaults to the
   Responses API, which MockAgents doesn't implement; MockAgents speaks Chat
   Completions.
2. A per-conversation `X-Session-Id` header.

## Layout

```
customer-support-agent/
├── mockagents/                 # MockAgents agent definitions (the "fixtures")
│   ├── support-agent.yaml       #  main triage agent: tools + scenarios + streaming
│   ├── support-agent-flaky.yaml #  chaos twin (fail_first 503) for retry testing
│   └── load-target.yaml         #  streaming load-test target (lognormal latency)
├── app/                        # the OpenAI Agents SDK application
│   ├── mock_setup.py            #  point the SDK at MockAgents (the only mock-specific code)
│   ├── support_agent.py         #  the Agent + @function_tool definitions
│   ├── triage_demo.py           #  headline demo: drive the triage agent
│   ├── resilience_demo.py       #  chaos/retry demo
│   ├── streaming_demo.py        #  streaming demo
│   └── deterministic_smoke.py   #  framework-free contract test (plain openai SDK)
├── scripts/                    # curl smoke, multi-tenant walkthrough, k6 load test
├── k8s/                        # Helm values + demo Job for the Kubernetes path
├── Dockerfile · docker-compose.yml · requirements.txt · .env.example
└── TESTING.md                  # step-by-step test guide (Docker + Kubernetes)
```

## Quickstart (Docker Compose)

```bash
cd demo/customer-support-agent

# 1. Start MockAgents with the three demo agents.
docker compose up --build -d mockagents

# 2. Run the headline triage demo.
docker compose run --rm demo                       # python -m app.triage_demo

# 3. Try the others.
docker compose run --rm demo python -m app.deterministic_smoke
docker compose run --rm demo python -m app.streaming_demo
docker compose run --rm demo python -m app.resilience_demo
```

Run the demo app **locally** instead (MockAgents still in Docker, or via
`make run` from the repo root):

```bash
pip install -r requirements.txt
cp .env.example .env            # then `set -a; . ./.env; set +a` or use your tool of choice
python -m app.triage_demo
```

Expected triage output (abridged):

```
USER : Hello!
AGENT: Hi! I'm Acme's support assistant. ...
USER : Where is my order?
AGENT: Good news — order ORD-12345 shipped via UPS (tracking 1Z999AA10123456784) ...
USER : I'd like a refund please
AGENT: Done — refund RF-88231 for $49.99 is approved ...
USER : I need to speak to a human
AGENT: You're all set — I've opened ticket SUP-1042 ...
```

## Full test guide

See **[TESTING.md](./TESTING.md)** for step-by-step instructions covering all four
feature areas, documented separately for **Docker Compose** and **Kubernetes/Helm**.
