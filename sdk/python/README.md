# MockAgents Python SDK

MockAgents runs deterministic local stand-ins for OpenAI, Anthropic, Gemini,
MCP, and A2A APIs so application and agent tests do not call live providers.

## Install

```bash
pip install mockagents
```

Python 3.10 through 3.13 is supported. The package controls the MockAgents Go
binary. Install it separately, set `MOCKAGENTS_BINARY=/absolute/path/mockagents`,
or opt in to verified release downloads with `MOCKAGENTS_AUTO_DOWNLOAD=1`.
Downloaded binaries are checksum verified and cached by package version and
platform. An explicit binary path always overrides the cache.

## Start a server

```python
from mockagents import MockAgentServer

with MockAgentServer(agents_dir="./agents") as server:
    response = server.client().chat([
        {"role": "user", "content": "hello"},
    ])
```

The context manager owns the subprocess. It continuously drains stdout and
stderr, stops the child when startup fails, and reaps it on exit.

## pytest fixture

The installed pytest plugin provides `mockagents_server`, `mockagents`, and
`mockagents_client` fixtures. Configure definitions in `pytest.ini`:

```ini
[pytest]
mockagents_agents_dir = ./agents
```

The session fixture also accepts `--mockagents-agents-dir` and
`--mockagents-binary`. A fixture startup failure is cleaned up before pytest
reports the error.

See the [repository documentation](https://github.com/mockagents/mockagents/tree/main/docs)
for agent schemas, protocol examples, and API details.
