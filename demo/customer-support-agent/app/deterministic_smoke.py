"""Deterministic smoke test using the plain OpenAI SDK (no agent framework).

This is the robust, framework-independent proof that the tool-calling round-trip
works against MockAgents. It drives the two turns by hand so you can see exactly
what the mock returns, and asserts the contract:

    turn 1  ->  finish_reason == "tool_calls"  (mock asks us to call lookup_order)
    turn 2  ->  finish_reason == "stop"        (mock returns the final answer)

Both turns send the same stable X-Session-Id so MockAgents' turn counter advances.

Run:

    python -m app.deterministic_smoke
"""

from __future__ import annotations

import json
import os
import sys
import uuid

from openai import OpenAI

BASE_URL = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080/v1")
API_KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "gpt-4o"   # routes to the main triage agent (acme-support)


def main() -> int:
    client = OpenAI(base_url=BASE_URL, api_key=API_KEY)
    session_id = f"smoke-{uuid.uuid4()}"
    headers = {"X-Session-Id": session_id}

    messages = [{"role": "user", "content": "Where is my order?"}]

    # --- Turn 1: expect a tool call.
    r1 = client.chat.completions.create(
        model=MODEL, messages=messages, extra_headers=headers
    )
    choice1 = r1.choices[0]
    print(f"turn 1: finish_reason={choice1.finish_reason}")
    assert choice1.finish_reason == "tool_calls", "expected a tool call on turn 1"
    tool_call = choice1.message.tool_calls[0]
    print(f"        tool={tool_call.function.name} args={tool_call.function.arguments}")
    assert tool_call.function.name == "lookup_order"

    # Execute the "tool" locally and feed the result back, same session.
    tool_result = json.dumps({"status": "shipped", "tracking": "1Z999AA10123456784"})
    messages.append(choice1.message.model_dump(exclude_none=True))
    messages.append(
        {"role": "tool", "tool_call_id": tool_call.id, "content": tool_result}
    )

    # --- Turn 2: expect the final answer.
    r2 = client.chat.completions.create(
        model=MODEL, messages=messages, extra_headers=headers
    )
    choice2 = r2.choices[0]
    print(f"turn 2: finish_reason={choice2.finish_reason}")
    assert choice2.finish_reason == "stop", "expected a final answer on turn 2"
    print(f"        answer={choice2.message.content}")

    print("\nSMOKE PASSED ✅")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except AssertionError as exc:
        print(f"\nSMOKE FAILED ❌  {exc}")
        sys.exit(1)
