# Scenario Packs

Ready-to-run example agents that cover common AI-app shapes — drop one into your
`agents/` dir (or run them straight from the repo's `examples/`) and point your
app at the mock. Each pack composes the same primitives: scenario matching, tool
calls, streaming, chaos/faults, and hallucination fixtures.

```bash
mockagents start --agents-dir examples      # serves every pack below
export OPENAI_BASE_URL=http://localhost:8080/v1
```

## Scaffold a starter pack

Prefer a fresh project over copy-paste? `mockagents init --template <name>`
scaffolds a ready-to-run project (agent + a matching TestSuite + README) from a
curated pack:

```bash
mockagents init --list-templates                 # see what's available
mockagents init my-bot --template customer-support
cd my-bot
mockagents validate agents
mockagents start --agents-dir agents             # serve it
mockagents test --agents-dir agents tests/support-suite.yaml   # assert it
```

| `--template` | Scaffolds |
|---|---|
| `basic` | Minimal single agent: greeting, a tool call, and a default fallback. |
| `customer-support` | First-line support flow — greeting, order-lookup tool, refunds, escalation. |
| `rag` | Retrieval agent with a **grounded** answer and an **ungrounded** hallucination fixture. |
| `coding-agent` | Coding assistant with **multi-step** tool use (read a file, then edit it). |
| `planner` | Multi-step planner that decomposes a goal and runs one step at a time. |

Every template ships a TestSuite that passes out of the box, so `mockagents test`
is a working starting point you edit toward your own assertions.

## Packs

| Pack (`examples/…`) | Demonstrates |
|---|---|
| [`support-flow-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/support-flow-agent.yaml) | **Customer-support flow** — greeting + order-lookup (tool call) + an **ungrounded** policy answer (hallucination fixture) |
| [`tool-routing-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/tool-routing-agent.yaml) + [`-suite.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/tool-routing-suite.yaml) | **Tool-call routing** — assert the right tool with the right args ([guide](testing-agents.md)) |
| [`rag-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/rag-agent.yaml) | **RAG** Q&A agent |
| [`code-assistant.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/code-assistant.yaml) | **Coding agent** |
| [`research-pipeline.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/research-pipeline.yaml) + [`research-suite.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/research-suite.yaml) | **Multi-agent pipeline** (planner/researcher/summarizer) with a TestSuite |
| [`hallucination-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/hallucination-agent.yaml) | **Hallucination fixtures** — one per type ([guide](hallucination-testing.md)) |
| [`chaos-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/chaos-agent.yaml) | **Failure injection** — latency, 5xx/429 errors, rate limits |
| [`flaky-then-healthy-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/flaky-then-healthy-agent.yaml) | **Retry/backoff fixture** — `fail_first` fails the first N calls (503), then recovers |
| [`access-denied-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/access-denied-agent.yaml) | **Chaos preset** — `preset: access-denied` (one-liner 403); see also server-down/rate-limited |
| [`semantic-errors-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/semantic-errors-agent.yaml) | **Semantic agent errors** — truncation (`finish_reason: length`), refusal, malformed tool-call args |
| [`load-target-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/load-target-agent.yaml) | **Load-test target** — realistic TTFT/inter-token latency percentiles ([guide](load-testing.md), k6 + Locust scripts) |
| [`stream-faults-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/stream-faults-agent.yaml) | **SSE faults** — TTFT/jitter, truncated + malformed streams |
| [`weather-mcp.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/weather-mcp.yaml) | **MCP server** mock ([guide](testing-agents.md)) |
| [`gemini-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/gemini-agent.yaml) | **Gemini** `generateContent` |

## Compose them

The packs are deliberately small so you can mix the modes: add a `chaos:` block
to a support flow to test 429 retries, mark a scenario as a `hallucination`
fixture to test grounding, or add `streaming:` faults to test your SSE parser.
See the [YAML schema](yaml-schema.md), [Testing AI Agents](testing-agents.md),
and [Hallucination Testing](hallucination-testing.md) guides.

Or start from a curated pack with [`mockagents init --template`](#scaffold-a-starter-pack).
