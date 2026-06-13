"""Run the Agents SDK against MockAgents over the Responses API.

Three conversations exercise the deterministic fixtures end to end:

  1. greeting        — single turn, no tool.
  2. order status    — turn 1 returns a lookup_order tool call, the SDK runs it
                       locally, turn 2 returns the resolution.
  3. refund          — same two-turn tool loop with issue_refund.

Each conversation gets a fresh X-Session-Id so MockAgents' turn counter advances
and the loops terminate (see mock_setup.new_conversation_client).

Prerequisite: MockAgents running with this demo's fixtures, e.g.

    mockagents start demo/responses-api-agent/mockagents

Then:  python -m app.run_demo
"""

from __future__ import annotations

import asyncio

from agents import Runner

from .agent import build_agent
from .mock_setup import new_conversation_client


PROMPTS = [
    ("greeting", "hello there"),
    ("order status", "where is my order?"),
    ("refund", "I'd like a refund please"),
]


async def main() -> None:
    agent = build_agent()
    for label, prompt in PROMPTS:
        session_id = new_conversation_client()
        result = await Runner.run(agent, prompt)
        print(f"\n=== {label}  (session {session_id}) ===")
        print(f"user> {prompt}")
        print(f"agent> {result.final_output}")

    print("\nAll conversations completed against the /v1/responses surface.")


if __name__ == "__main__":
    asyncio.run(main())
