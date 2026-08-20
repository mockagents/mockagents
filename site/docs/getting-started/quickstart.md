# Quickstart Guide

Get your first mock agent running in under 5 minutes.

## 1. Install MockAgents

=== "Binary (recommended)"

    Release assets carry the version in the filename, so set it once:

    ```bash
    VERSION=0.4.0

    # macOS / Linux — swap linux_amd64 for darwin_arm64, darwin_amd64, or linux_arm64
    curl -fsSL "https://github.com/mockagents/mockagents/releases/download/v${VERSION}/mockagents_${VERSION}_linux_amd64.tar.gz" | tar xz
    sudo mv mockagents /usr/local/bin/

    # Verify
    mockagents --version
    ```

    Windows (amd64 only — there is no Windows arm64 build):
    download `mockagents_${VERSION}_windows_amd64.zip` from the
    [releases page](https://github.com/mockagents/mockagents/releases) and unzip it.

    Checksums for every asset are in `checksums.txt` on the same release.

=== "Go Install"

    ```bash
    go install github.com/mockagents/mockagents/cmd/mockagents@latest
    ```

    Needs Go 1.26+. Takes about half a minute on a cold module cache.

=== "Docker"

    ```bash
    docker run -p 8080:8080 mockagents/mockagents
    ```

    !!! warning "Not published yet"
        The Docker image 404s today — the v0.4.0 release pipeline failed partway
        through, so no registry has anything to serve. The
        [install-paths workflow](https://github.com/mockagents/mockagents/actions/workflows/install-paths.yml)
        checks every advertised path daily and is the source of truth. Use the
        binary or `go install` until it goes green.

## 2. Create a Project

```bash
mockagents init my-project
cd my-project
```

This creates:

```
my-project/
├── .mockagents.yaml        # Project config
├── agents/
│   └── example-agent.yaml  # Sample agent
├── tests/
│   └── example-test.yaml   # Sample test
└── README.md
```

## 3. Start the Mock Server

```bash
mockagents start
```

Output:

```
INFO loaded agent name=example-agent model=gpt-4o protocol=openai-chat-completions scenarios=3
INFO MockAgents server started addr=http://localhost:8080 agents=1
```

## 4. Send Your First Request

=== "OpenAI SDK (Python)"

    ```bash
    pip install openai
    ```

    ```python
    import openai

    client = openai.OpenAI(
        base_url="http://localhost:8080/v1",
        api_key="mock"  # Any string works
    )

    response = client.chat.completions.create(
        model="gpt-4o",
        messages=[{"role": "user", "content": "hello"}]
    )
    print(response.choices[0].message.content)
    # → "Hello! I'm your mock assistant. How can I help?"
    ```

=== "Anthropic SDK (Python)"

    ```bash
    pip install anthropic
    ```

    ```python
    import anthropic

    client = anthropic.Anthropic(
        base_url="http://localhost:8080",
        api_key="mock"
    )

    message = client.messages.create(
        model="gpt-4o",
        max_tokens=1024,
        messages=[{"role": "user", "content": "hello"}]
    )
    print(message.content[0].text)
    ```

=== "curl"

    ```bash
    curl -s http://localhost:8080/v1/chat/completions \
      -H "Content-Type: application/json" \
      -d '{
        "model": "gpt-4o",
        "messages": [{"role": "user", "content": "hello"}]
      }' | python3 -m json.tool
    ```

## 5. Write a Test

You do not need step 3 for this. The SDK registers a pytest plugin, so the
`mockagents` fixture starts and stops a server for you and points the provider
SDKs at it — no import, no conftest, no fixture wiring:

!!! warning "`pip install mockagents` does not work yet"
    The Python SDK is not on PyPI — same stalled release as the Docker image
    above. Until it is, install it from a checkout, which is the *only* way to
    get the pytest plugin today:

    ```bash
    git clone https://github.com/mockagents/mockagents.git
    pip install -e mockagents/sdk/python
    pip install pytest openai
    ```

    Everything below then works exactly as written.

```python
# test_agent.py
def test_greeting(mockagents):
    from openai import OpenAI            # your real app code, unchanged:
    reply = OpenAI().chat.completions.create(
        model="gpt-4o",
        messages=[{"role": "user", "content": "hello"}],
    )
    assert "How can I help" in reply.choices[0].message.content
```

```bash
pytest -q
# 1 passed in 3.23s
```

The fixture reads `./agents` by default — the directory `mockagents init` just
created. Point it elsewhere with `--mockagents-agents-dir`, the
`mockagents_agents_dir` ini option, or `MOCKAGENTS_AGENTS_DIR`.

### Assert the trajectory, not just the text

The reason to reach for MockAgents in a test is to check the *shape* of what the
agent did. `mockagents_client` is a second fixture bound to the same server:

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

`to_have_tool_call_sequence` reads the aggregate across every turn and compares
for **full equality** — an unexpected extra call fails it. That is deliberately
the same semantics as the `tool_call_sequence` assertion in `kind: TestSuite`
YAML, so a check moves between your test file and `mockagents test` unchanged.

The agent used above is
[`examples/tool-routing-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/tool-routing-agent.yaml)
from the repo. To run this as written, put it in an agents directory *by
itself* and point the fixture there:

```console
$ mkdir -p routing-agent && cp path/to/tool-routing-agent.yaml routing-agent/
$ pytest --mockagents-agents-dir routing-agent
```

!!! warning "One `spec.model`, one agent"
    Over the provider HTTP surfaces an agent is selected by `spec.model` —
    there is no agent-name header. The scaffolded `example-agent` and
    `tool-router` both declare `model: gpt-4o`, so dropping both in one
    directory makes `model: "gpt-4o"` resolve to whichever name sorts first
    (the server logs a `model claimed by multiple agents` warning naming the
    winner). Give each agent a distinct `spec.model`, or keep them in separate
    directories.

### Without pytest

If you are not on pytest, drive the server yourself — same assertions:

```python
from mockagents import MockAgentServer, expect

def test_greeting():
    with MockAgentServer(agents_dir="./agents") as server:
        response = server.client().chat(
            messages=[{"role": "user", "content": "hello"}],
            model="gpt-4o",
        )
        expect(response).to_have_response_containing("How can I help")
        expect(response).to_have_status(200)
```

### TypeScript

`setupMockAgents()` is the same idea for Vitest or Jest — one server per test
file, provider env vars patched and restored:

```ts
import { Scenario, expect as expectAgent, runScenario } from "@mockagents/sdk";
import { setupMockAgents } from "@mockagents/vitest";
import { expect, test } from "vitest";

const mock = setupMockAgents({ agentsDir: "./agents" });

test("greeting", async () => {
  const { OpenAI } = await import("openai");
  const reply = await new OpenAI().chat.completions.create({
    model: "gpt-4o",
    messages: [{ role: "user", content: "hello" }],
  });
  expect(reply.choices[0].message.content).toContain("How can I help");
});

test("support flow", async () => {
  const result = await runScenario(mock.client, new Scenario({
    name: "support",
    steps: [
      { role: "user", content: "what's the weather in London?" },
      { role: "user", content: "and where is my order?" },
    ],
  }));
  expectAgent(result)
    .toHaveToolCallSequence(["get_weather", "search_orders"])
    .toHaveToolCallCount(2);
});
```

## Next Steps

- [Testing AI Agents](../guides/testing-agents.md) — tool-call and MCP cookbooks
- [CLI Reference](../guides/cli-reference.md) — All commands and flags
- [YAML Schema](../guides/yaml-schema.md) — Agent definition reference
- [Python SDK Guide](../sdk/python-sdk.md) — Client, server, assertions
- [TypeScript SDK Guide](../sdk/typescript-sdk.md) — Same API in TS
- [Management API](../guides/management-api.md) — REST endpoints
