# Python SDK Guide

```bash
pip install mockagents
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
| `binary_path` | auto-detect | Path to `mockagents` binary |
| `log_level` | `warn` | Server log level |

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

```python
# Stream OpenAI chunks
for chunk in client.chat_stream(
    messages=[{"role": "user", "content": "hello"}],
    model="gpt-4o"
):
    delta = chunk["choices"][0]["delta"]
    if "content" in delta:
        print(delta["content"], end="")
```

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

# Tool calls
expect(result).to_have_tool_call("search")
expect(result).to_have_tool_call("search", {"query": "test"})

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

## pytest Integration

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
        model="gpt-4o"
    )
    assert "Hello" in response.content
```
