# Quickstart Guide

Get your first mock agent running in under 5 minutes.

## 1. Install MockAgents

=== "Binary (recommended)"

    Download the latest release for your platform:

    ```bash
    # macOS / Linux
    curl -fsSL https://github.com/mockagents/mockagents/releases/latest/download/mockagents_linux_amd64.tar.gz | tar xz
    sudo mv mockagents /usr/local/bin/

    # Verify
    mockagents --version
    ```

=== "Go Install"

    ```bash
    go install github.com/mockagents/mockagents/cmd/mockagents@latest
    ```

=== "Docker"

    ```bash
    docker pull mockagents/mockagents:latest
    ```

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

## 5. Write a Test (Python SDK)

```bash
pip install mockagents
```

```python
# test_agent.py
from mockagents import MockAgentServer, expect, Scenario, run_scenario

def test_greeting():
    with MockAgentServer(agents_dir="./agents") as server:
        client = server.client()
        response = client.chat(
            messages=[{"role": "user", "content": "hello"}],
            model="gpt-4o"
        )
        expect(response).to_have_response_containing("Hello")
        expect(response).to_have_status(200)
```

```bash
pytest test_agent.py -v
```

## Next Steps

- [CLI Reference](../guides/cli-reference.md) — All commands and flags
- [YAML Schema](../guides/yaml-schema.md) — Agent definition reference
- [Python SDK Guide](../sdk/python-sdk.md) — Client, server, assertions
- [Management API](../guides/management-api.md) — REST endpoints
