# Python SDK Guide

```bash
pip install mockagents
```

## Start here: pytest, zero config

Installing the package registers a pytest plugin (the `pytest11` entry point),
so there is nothing to import and no `conftest.py` to write. Three fixtures
appear in any test session:

| Fixture | Scope | What it gives you |
| --- | --- | --- |
| `mockagents_server` | session | One `MockAgentServer` subprocess for the whole run. |
| `mockagents` | function | The same server, with `OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL`, `GOOGLE_GEMINI_BASE_URL` and dummy API keys patched for the duration of the test — so your *existing* application code is redirected with zero changes. Restored afterwards. |
| `mockagents_client` | function | A `MockAgentClient` bound to that server, for driving the mock directly. |

```python
# test_agent.py — no imports, no conftest
def test_greeting(mockagents):
    from openai import OpenAI            # your real app code, unchanged:
    reply = OpenAI().chat.completions.create(
        model="gpt-4o",
        messages=[{"role": "user", "content": "hello"}],
    )
    assert "How can I help" in reply.choices[0].message.content
```

```console
$ pytest -q
1 passed in 3.23s
```

The agents directory resolves in this order: `--mockagents-agents-dir`, the
`mockagents_agents_dir` ini option, `$MOCKAGENTS_AGENTS_DIR`, then `./agents`.
Point at a specific binary with `--mockagents-binary` or `$MOCKAGENTS_BINARY`.

!!! warning "Gemini caveat"
    The env-var redirect reaches Gemini only through the newer `google-genai`
    client, which reads `GOOGLE_GEMINI_BASE_URL`. The legacy
    `google-generativeai` SDK has no base-URL env var — redirect it explicitly
    with `genai.configure(client_options={"api_endpoint": url})`, or the test
    silently calls the real Google endpoint.

### Assert the trajectory

The reason to use a mock rather than a stub is that you can assert the *shape*
of what the agent did. `run_scenario` drives a multi-turn conversation through
`mockagents_client`:

```python
from mockagents import Scenario, expect, run_scenario

def test_support_flow(mockagents_client):
    result = run_scenario(mockagents_client, Scenario(
        name="support",
        steps=[
            {"role": "user", "content": "what's the weather in London?"},
            {"role": "user", "content": "and where is my order?"},
        ],
    ))
    expect(result).to_have_tool_call_sequence(["get_weather", "search_orders"])
    expect(result).to_have_tool_call_count(2)
    expect(result).to_have_tool_call("get_weather", {"city": "London"})
```

Wrong tool, wrong order, one call too many — three bugs a text assertion cannot
see. See [Assertions](#assertions) for the exact semantics.

### Wiring fixtures by hand

Only needed when you want a different lifetime or several servers at once:

```python
import pytest
from mockagents import MockAgentServer

@pytest.fixture(scope="session")
def mock_server():
    with MockAgentServer(agents_dir="./agents") as server:
        yield server

@pytest.fixture
def client(mock_server):
    return mock_server.client()

def test_greeting(client):
    response = client.chat(
        messages=[{"role": "user", "content": "hello"}],
        model="gpt-4o",
    )
    assert "How can I help" in response.content
```

## MockAgentServer

Manages the MockAgents Go binary as a subprocess.

```python
from mockagents import MockAgentServer

# Context manager (recommended)
with MockAgentServer(agents_dir="./agents") as server:
    client = server.client()
    # ... use client

# Manual lifecycle
server = MockAgentServer(agents_dir="./agents", port=9090)
server.start()
client = server.client()
# ... use client
server.stop()
```

**Parameters:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `agents_dir` | `./agents` | Agent YAML directory |
| `port` | `0` (auto) | Server port. 0 = auto-select free port. |
| `binary_path` | auto-detect | Path to `mockagents` binary (`MOCKAGENTS_BINARY` honored) |
| `log_level` | `warn` | Server log level |
| `config_path` | | Path to a `.mockagents.yaml` project config (stored on the instance; not passed to the server). To serve a single agent YAML file, use `MockAgentServer.from_config(...)` below. |
| `auto_download` | `False` | Download a matching server binary if none is found (also available as the `mockagents-install` console script) |

**Class methods:**

```python
# Load from YAML config file(s)
server = MockAgentServer.from_config("agents/my-agent.yaml")
server = MockAgentServer.from_config(["agents/a.yaml", "agents/b.yaml"])
```

## MockAgentClient

HTTP client for the mock server, supporting both OpenAI and Anthropic protocols.

### OpenAI Chat Completions

```python
from mockagents import MockAgentClient

client = MockAgentClient(base_url="http://localhost:8080")

response = client.chat(
    messages=[{"role": "user", "content": "hello"}],
    model="gpt-4o"
)
print(response.content)        # "Hello!"
print(response.model)          # "gpt-4o"
print(response.finish_reason)  # "stop"
print(response.usage.total_tokens)  # 15
print(response.tool_calls)     # []
```

### Anthropic Messages

```python
response = client.message(
    messages=[{"role": "user", "content": "hello"}],
    model="claude-3-opus",
    system="You are helpful."
)
print(response.content)
```

### Streaming

Raw per-protocol streams, or the protocol-agnostic `iter_stream` that yields
normalized `StreamChunk`s:

```python
# Raw OpenAI chunks
for chunk in client.chat_stream(
    messages=[{"role": "user", "content": "hello"}],
    model="gpt-4o"
):
    delta = chunk["choices"][0]["delta"]
    if "content" in delta:
        print(delta["content"], end="")

# Protocol-agnostic — the same loop works for openai and anthropic
for chunk in client.iter_stream(
    messages=[{"role": "user", "content": "hello"}],
    protocol="anthropic",           # "openai" (default) | "anthropic"
):
    print(chunk.text, end="")
    if chunk.finished:
        print("\nfinish:", chunk.finish_reason)
```

`StreamChunk` fields: `text`, `tool_call_delta` (index, name, arguments
fragment), `finish_reason`, `finished`, `raw`. `message_stream()` is the raw
Anthropic-event equivalent of `chat_stream()`. The TypeScript and Go SDKs
expose the same helper as [`iterStream`](typescript-sdk.md#streaming) /
[`IterStream`](go-sdk.md#streaming).

### Management

```python
client.health()                   # {"status": "ok", ...}
client.list_agents()              # [{"name": "...", ...}]
client.get_agent("my-agent")      # Full agent definition
client.reload_agent("my-agent")   # Hot reload from disk
```

## Scenarios

Define multi-turn conversation tests.

```python
from mockagents import Scenario, run_scenario

scenario = Scenario(
    name="greeting-flow",
    steps=[
        {"role": "user", "content": "hello"},
        {"role": "user", "content": "help me with billing"},
    ],
    model="gpt-4o"
)

with MockAgentServer(agents_dir="./agents") as server:
    client = server.client()
    result = run_scenario(client, scenario)

    print(result.content)          # All response content
    print(result.latency_ms)       # Total latency
    print(result.tool_calls)       # All tool calls
    print(result.last_response)    # Most recent response
```

## Assertions

Fluent assertion library for expressive tests.

```python
from mockagents import expect

# Response content
expect(result).to_have_response_containing("Hello")

# Tool calls — did this call happen at all? (arguments are a PARTIAL match)
expect(result).to_have_tool_call("search")
expect(result).to_have_tool_call("search", {"query": "test"})

# Trajectory — the ordered shape of what the agent did.
# Both read the AGGREGATE across every turn of a ScenarioResult.
expect(result).to_have_tool_call_sequence(["search", "summarize"])  # full equality, not a subsequence
expect(result).to_have_tool_call_count(3)                          # total across all turns
expect(result).to_have_tool_call_count(2, name="search")           # narrowed to one tool (SDK-only)

# Simulated tool errors (tools[].responses[].error fixtures)
expect(result).to_have_tool_error("NOT_FOUND")

# Status and finish reason
expect(result).to_have_status(200)
expect(result).to_have_finish_reason("stop")

# Value assertions
expect(result.latency_ms).to_be_less_than(100)
expect(result.latency_ms).to_be_greater_than(0)
expect(response.content).to_contain("hello")
expect(response.model).to_equal("gpt-4o")

# Chaining
(
    expect(result)
    .to_have_response_containing("Hello")
    .to_have_tool_call("search")
    .to_have_status(200)
)
```

### Trajectory semantics

`to_have_tool_call_sequence` and the one-argument `to_have_tool_call_count` are
deliberately identical to the `tool_call_sequence` / `tool_call_count`
assertions in `kind: TestSuite` YAML, so a check moves between your test file
and `mockagents test` without changing meaning:

- They read **every turn**, in invocation order — a multi-turn trajectory is
  every call the agent made, not just the last response's calls.
- The sequence is **full equality**, not a subsequence. An unexpected extra
  call fails it. That is the point: a silent extra retrieval is a bug.
- `to_have_tool_call_count(n, name=...)` narrows to one tool. That two-argument
  form is an SDK convenience with **no YAML equivalent** — use the unnamed form
  when you want a check that transfers.
- Outcome assertions (`to_have_response_containing`, `to_have_status`,
  `to_have_finish_reason`) read the **final** turn.

Pipeline trajectories are typed and use the same exact-order rule:

```python
result = client.run_pipeline("research", "summarize the evidence")
expect(result).to_have_node_sequence(["plan", "research", "write"])
```

## Framework adapters & MCP

- `mockagents.adapters` — zero-boilerplate factories for LangChain / LangGraph
  / CrewAI (`chat_openai`, `chat_anthropic`, `crewai_mock_llm`, `patched_env`);
  install extras with `pip install 'mockagents[langchain]'` or
  `'mockagents[crewai]'`. See
  [Testing with Agent Frameworks](../guides/framework-testing.md).
- `McpClient` — drives the mock MCP server's bidirectional channel
  (`sampling/createMessage`, `roots/list`) — see the
  [MCP guide](../guides/mcp.md).
