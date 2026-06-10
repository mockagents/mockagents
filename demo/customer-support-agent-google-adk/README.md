# Acme Support Triage — a MockAgents demo (Google ADK)

A realistic **agentic AI** application built with the **[Google Agent Development
Kit (ADK)]** that runs entirely against **[MockAgents]** instead of a real model
provider. It's a customer-support *triage* agent: it greets customers, looks up
orders, issues refunds, and escalates to a human — using ADK function tools, with
**zero tokens, zero cost, and fully deterministic behavior**.

This is the Google-side member of the demo trio (alongside the
[OpenAI Agents SDK](../customer-support-agent/) and
[Claude Agent SDK](../customer-support-agent-claude/) demos): same use case, same
four feature areas. It ships **two backends**:

- **Native Gemini (primary)** — ADK with a Gemini model, pointed at MockAgents'
  native Gemini endpoint (`/v1beta/models/<model>:generateContent`). Exercises
  MockAgents' Gemini wire format + function calling.
- **LiteLLM bridge (alternative)** — ADK's `LiteLlm` wrapper pointed at
  MockAgents' OpenAI-compatible endpoint (`/v1/chat/completions`).

[Google Agent Development Kit (ADK)]: https://google.github.io/adk-docs/
[MockAgents]: ../../README.md

## How it talks to the mock

**Native Gemini:** ADK builds a `google-genai` client internally. The genai SDK
reads the **`GOOGLE_GEMINI_BASE_URL`** env var, so setting it (plus selecting the
Gemini API over Vertex) redirects ADK at MockAgents — no change to the agent code.
See `app/mock_setup.py`.

**LiteLLM bridge:** `LiteLlm(model="openai/gpt-4o", api_base=".../v1", api_key=...)`
points ADK at MockAgents' OpenAI surface. See `app/litellm_demo.py`.

### MockAgents changes this demo required

Pointing real Gemini traffic at MockAgents surfaced that the Gemini surface was a
second-class citizen versus OpenAI/Anthropic. The demo depends on four
improvements (all general, not demo-specific):

1. **Gemini prices** added to the cost table (it served Gemini but priced it at 0).
2. **Quota enforcement** now covers the Gemini route (`/v1beta/models/...`).
3. **Interaction logging** (and thus the cost dashboard + spend accrual) now
   covers the Gemini route.
4. **Cost extraction** now parses the Gemini response shape (`modelVersion` +
   `usageMetadata`).

## What it demonstrates

| MockAgents feature | Where | How to see it |
| --- | --- | --- |
| **Scenario matching + tool calls** | `mockagents/support-agent-gemini.yaml`, `app/triage_demo.py` | The agentic loop: turn 1 returns a functionCall, ADK runs the Python tool, turn 2 returns the final answer. |
| **Chaos / fault injection** | `mockagents/support-agent-flaky-gemini.yaml`, `app/resilience_demo.py` | First 2 requests 503, then recover — proving retry/backoff works. |
| **Streaming + load-target latency** | `mockagents/load-target-gemini.yaml`, `app/streaming_demo.py`, `scripts/k6-loadtest.js` | Gemini SSE deltas + realistic p50/p95 TTFT/ITL for free load testing. |
| **Multi-tenancy + quotas + cost** | `scripts/multitenant_walkthrough.sh` | Per-tenant API keys, rate (429) / spend (402) caps, and the cost aggregate. |

## A note on matching (native Gemini)

ADK is a library (not a CLI subprocess), so the user message is clean and the
genai client sends no `X-Session-Id` — MockAgents' turn counter stays 1, so the
Gemini fixtures match on **content**, not `turn_number`. In the Gemini protocol
the tool result returns as a `functionResponse`; MockAgents surfaces the function
name + response JSON into that turn's content. Each tool returns a unique MARKER
(`ORDER_RESULT` / `REFUND_RESULT` / `ESCALATION_RESULT`); the `*-resolved`
scenarios match the marker and emit the final answer (listed first so a marker
resolves instead of re-firing the tool — that ends the loop).

The **LiteLLM bridge** instead sends OpenAI-format requests where the tool result
is a `role:"tool"` message (not surfaced as the latest user message), so that
fixture (`support-agent-litellm.yaml`) is `turn_number`-gated and the demo sets a
per-conversation `X-Session-Id` via LiteLLM `extra_headers`.

## Models (routing keys)

Routing is by model name; each fixture uses a distinct, real (price-table) model
so the cost dashboard and spend cap work out of the box:

| Model the client requests | MockAgents agent | Role |
| --- | --- | --- |
| `gemini-2.0-flash` | `acme-support-gemini` | Triage: functions + scenarios + streaming |
| `gemini-2.5-flash` | `acme-support-flaky-gemini` | Chaos twin: 503s first 2 requests then heals |
| `gemini-1.5-flash` | `acme-load-target-gemini` | Streaming load-test target |
| `gpt-4o` (LiteLLM bridge) | `acme-support-litellm` | Triage over the OpenAI surface |

## Layout

```
customer-support-agent-google-adk/
├── mockagents/                       # MockAgents agent definitions
│   ├── support-agent-gemini.yaml      #  native Gemini triage: functions + scenarios + streaming
│   ├── support-agent-flaky-gemini.yaml#  chaos twin (fail_first 503)
│   ├── load-target-gemini.yaml        #  streaming load-test target
│   └── support-agent-litellm.yaml     #  OpenAI-protocol fixture for the LiteLLM bridge
├── app/                              # the Google ADK application
│   ├── mock_setup.py                  #  redirect ADK's genai client at MockAgents (env)
│   ├── support_agent.py               #  the Agent + Python function tools
│   ├── triage_demo.py                 #  headline demo: native-Gemini triage
│   ├── litellm_demo.py                #  alternative: triage via the LiteLLM bridge
│   ├── resilience_demo.py             #  chaos/retry demo (raw HTTP)
│   ├── streaming_demo.py              #  streaming demo (genai client)
│   └── deterministic_smoke.py         #  framework-free contract test (raw Gemini HTTP)
├── scripts/                          # curl smoke, multi-tenant walkthrough, k6 load test
├── k8s/                              # Helm values + demo Job for the Kubernetes path
├── Dockerfile · docker-compose.yml · requirements.txt · .env.example
└── TESTING.md                        # step-by-step test guide (Docker + Kubernetes)
```

## Quickstart (Docker Compose)

```bash
cd demo/customer-support-agent-google-adk

# 1. Start MockAgents with the demo agents.
docker compose up --build -d mockagents

# 2. Run the headline (native-Gemini) triage demo.
docker compose run --rm demo                       # python -m app.triage_demo

# 3. Try the LiteLLM bridge and the others.
docker compose run --rm demo python -m app.litellm_demo
docker compose run --rm demo python -m app.deterministic_smoke
docker compose run --rm demo python -m app.streaming_demo
docker compose run --rm demo python -m app.resilience_demo
```

Run the demo app **locally** (pure Python — no extra CLI needed):

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
  [function call] lookup_order {'order_id': 'ORD-12345'}
  AGENT: Good news — order ORD-12345 shipped via UPS ...
USER : I'd like a refund please
  [function call] issue_refund {'amount': 49.99, 'order_id': 'ORD-12345'}
  AGENT: Done — refund RF-88231 for $49.99 is approved ...
USER : I need to speak to a human
  [function call] escalate_to_human {'reason': 'Customer requested a human agent'}
  AGENT: You're all set — I've opened ticket SUP-1042 ...
```

## Full test guide

See **[TESTING.md](./TESTING.md)** — step-by-step instructions for all four
feature areas, documented separately for **Docker Compose** and **Kubernetes/Helm**,
covering both the native-Gemini and LiteLLM-bridge backends.
