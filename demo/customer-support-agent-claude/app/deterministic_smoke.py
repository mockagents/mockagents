"""Framework-free contract test using the plain Anthropic SDK (no agent harness).

Drives the two-turn tool round-trip on /v1/messages by hand so you can see exactly
what the mock returns, and asserts the Anthropic wire contract:

    turn 1  ->  stop_reason == "tool_use"   (mock returns a lookup_order tool_use)
    turn 2  ->  stop_reason == "end_turn"    (mock returns the final answer)

In the Anthropic protocol the tool result is sent back as a *user* message
containing a tool_result block — which becomes turn 2's matched content.

Run:

    python -m app.deterministic_smoke
"""

from __future__ import annotations

import os
import sys

from anthropic import Anthropic

BASE_URL = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")
API_KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "claude-3-5-sonnet-latest"  # routes to the triage agent

TOOLS = [
    {
        "name": "mcp__acme__lookup_order",
        "description": "Look up the status of an order by its ID.",
        "input_schema": {
            "type": "object",
            "properties": {"order_id": {"type": "string"}},
            "required": ["order_id"],
        },
    }
]


def main() -> int:
    client = Anthropic(base_url=BASE_URL, api_key=API_KEY, max_retries=0)
    messages = [{"role": "user", "content": "Where is my order?"}]

    # --- Turn 1: expect a tool_use.
    r1 = client.messages.create(model=MODEL, max_tokens=256, messages=messages, tools=TOOLS)
    print(f"turn 1: stop_reason={r1.stop_reason}")
    assert r1.stop_reason == "tool_use", "expected a tool_use on turn 1"
    tool_use = next(b for b in r1.content if b.type == "tool_use")
    print(f"        tool={tool_use.name} input={tool_use.input}")

    # Feed the tool result back as a user message (Anthropic protocol).
    messages.append({"role": "assistant", "content": [b.model_dump() for b in r1.content]})
    messages.append(
        {
            "role": "user",
            "content": [
                {
                    "type": "tool_result",
                    "tool_use_id": tool_use.id,
                    "content": [{"type": "text", "text": "ORDER_RESULT {\"status\":\"shipped\"}"}],
                }
            ],
        }
    )

    # --- Turn 2: expect the final answer.
    r2 = client.messages.create(model=MODEL, max_tokens=256, messages=messages, tools=TOOLS)
    print(f"turn 2: stop_reason={r2.stop_reason}")
    assert r2.stop_reason == "end_turn", "expected a final answer on turn 2"
    text = "".join(b.text for b in r2.content if b.type == "text")
    print(f"        answer={text}")

    print("\nSMOKE PASSED ✅")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except AssertionError as exc:
        print(f"\nSMOKE FAILED ❌  {exc}")
        sys.exit(1)
