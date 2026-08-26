# Migrating from ad hoc AI mocks

Ad hoc mocks often begin as a patched SDK method, a fake HTTP response, or a
handwritten test server. They are quick to create, but each test then owns part
of the provider protocol. MockAgents moves that behavior into validated,
declarative fixtures while your application continues to use the real SDK.

Migrate only AI-provider behavior. Keep small in-process fakes for application
interfaces that do not cross an AI protocol boundary.

## Before: patching the OpenAI client

This Python test bypasses request serialization and response parsing entirely:

```python
from types import SimpleNamespace
from unittest.mock import AsyncMock

async def test_refund_answer(monkeypatch, openai_client):
    create = AsyncMock(return_value=SimpleNamespace(
        choices=[SimpleNamespace(
            message=SimpleNamespace(content="Refunds take 5 days.")
        )]
    ))
    monkeypatch.setattr(openai_client.chat.completions, "create", create)

    answer = await answer_customer(openai_client, "When is my refund?")

    assert answer == "Refunds take 5 days."
    create.assert_awaited_once()
```

The test cannot catch an incorrect endpoint, malformed request, SDK upgrade,
stream parser bug, or retry triggered by a provider-shaped error.

## After: run the real SDK against MockAgents

Create `agents/support.yaml`:

```yaml
apiVersion: mockagents/v1
kind: Agent
metadata:
  name: support
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  behavior:
    scenarios:
      - name: refund-policy
        match:
          content_contains: refund
        response:
          content: Refunds take 5 days.
```

Validate the fixture before using it:

```bash
mockagents validate agents/support.yaml
```

The pytest plugin starts one session server when a test requests the
function-scoped `mockagents` fixture. That fixture redirects provider
environment variables for the test and restores them afterwards:

```python
import os
from openai import AsyncOpenAI

async def test_refund_answer(mockagents):
    client = AsyncOpenAI(
        api_key="mock",
        base_url=os.environ["OPENAI_BASE_URL"],
    )

    answer = await answer_customer(client, "When is my refund?")

    assert answer == "Refunds take 5 days."
```

Set `MOCKAGENTS_AGENTS_DIR=agents` for the test process. The application still
uses the official OpenAI client, so the test exercises its normal transport and
response model.

## Translate behavior, not implementation

| Ad hoc mock technique | MockAgents representation |
|---|---|
| Branch on prompt text | `match.content_contains` or `content_regex` |
| Return a different value on the nth call | Ordered scenarios, `turn_number`, or sequence responses |
| Return a tool/function call | `spec.tools` and `response.tool_calls` |
| Raise a rate-limit/server exception | Provider-shaped chaos error or named chaos preset |
| Sleep before returning | Deterministic chaos latency |
| Yield hand-built stream chunks | Declarative streaming and stream fault controls |
| Inspect mock call arguments | Trajectory and journal assertions |
| Reset global mock state | Use the SDK lifecycle and test-isolated fixture directories |

Do not copy implementation details such as `MagicMock` response trees into
YAML. Describe the request condition and wire-visible response that the
application depends on.

## Tools, retries, and streams

A mock method frequently hides the most failure-prone application paths. Port
those paths explicitly:

```yaml
spec:
  protocol: openai-chat-completions
  model: gpt-4o
  tools:
    - name: lookup_order
      parameters:
        type: object
        properties:
          order_id: { type: string }
        required: [order_id]
  behavior:
    scenarios:
      - name: request-order-tool
        match: { content_contains: order }
        response:
          tool_calls:
            - name: lookup_order
              arguments: { order_id: ORD-123 }
```

Use deterministic chaos for retry tests instead of making a mock method throw
based on process-global counters. Configure a seed, scope the fault to the
smallest useful operation or scenario, and assert the resulting retry timeline
through the journal.

For streaming tests, let MockAgents emit the provider's actual SSE shape. Test
the application-level output and selected trajectory rather than asserting a
private list of chunks owned by the old fake.

## Migrate incrementally

1. Inventory every patched SDK method and handwritten provider endpoint.
2. Group cases by user-visible scenario rather than by test function.
3. Create and validate one Agent file for the first group.
4. Point only those tests at MockAgents and remove their patches.
5. Add explicit fixtures for error, tool, multi-turn, and streaming paths.
6. Run the suite with provider egress blocked before deleting the old helper.

Test-specific behavior belongs in a small scenario pack or a test-only agents
directory. Reusable failures should have stable names such as `rate-limited` or
`flaky-then-healthy`; avoid one fixture copy per test when the behavior is the
same.

## What should remain an in-process fake

Keep an ad hoc fake when it models a narrow application-owned interface, such
as a clock, UUID source, feature flag, repository, or pure business rule. Also
keep a temporary fake for an unsupported provider route until MockAgents has
wire-compatible coverage.

MockAgents is not a replacement for every test double. It is the boundary for
AI protocols where exercising the real SDK, deterministic faults, and
inspectable interactions provide value.

## Completion checklist

1. Every migrated fixture passes `mockagents validate`.
2. Tests construct the real SDK client with a MockAgents base URL.
3. No migrated test patches provider SDK methods or constructs provider
   response objects by hand.
4. Tool, retry, streaming, and multi-turn paths use explicit scenarios.
5. Provider network egress is blocked in CI and the suite remains green.
6. Application-owned unit-test fakes remain local and focused.
