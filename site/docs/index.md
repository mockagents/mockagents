# MockAgents

**Simulate, test, and validate AI agent integrations — without calling real LLMs or burning tokens.**

MockAgents is an open-source platform that lets you spin up realistic mock agents with configurable behaviors, tool responses, latency profiles, and failure modes.

## Why MockAgents?

- **Zero API keys** — Test agent integrations without real LLM calls
- **Drop-in replacement** — Point your OpenAI or Anthropic SDK at MockAgents with a single `base_url` change
- **Deterministic tests** — Same input always produces the same output
- **Tool call simulation** — Match-based tool responses with error injection
- **SSE streaming** — Realistic streaming with configurable chunk size and delay
- **Multi-agent pipelines** — Sequential, parallel, and graph topologies (`kind: Pipeline`)
- **Chaos engineering** — Inject latency, errors, and rate limits per agent
- **Record & replay** — Capture real upstream traffic once, replay offline forever
- **Contract testing** — Diff breaking changes in CI
- **Mock MCP server** — JSON-RPC 2.0 over HTTP, stdio, and an SSE duplex channel
- **Three SDKs** — Python, TypeScript, and Go, all with streaming helpers and parity
- **Multi-tenant control plane** — Tenants, RBAC API keys, audit log, and cost estimates
- **Web console** — Next.js console: catalog, live log feed, cost dashboard, YAML editor, admin

## Quick Example

```yaml
# agents/support-agent.yaml
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support-agent
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: greeting
        match:
          content_contains: "hello"
        response:
          content: "Hello! How can I help you today?"
      - name: default
        response:
          content: "I'm here to help."
```

```bash
mockagents start --agents-dir ./agents
```

```python
import openai

client = openai.OpenAI(base_url="http://localhost:8080/v1", api_key="mock")
response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "hello"}]
)
print(response.choices[0].message.content)
# → "Hello! How can I help you today?"
```

## Get Started

Follow the [Quickstart Guide](getting-started/quickstart.md) to go from zero to your first mock agent in under 5 minutes.
