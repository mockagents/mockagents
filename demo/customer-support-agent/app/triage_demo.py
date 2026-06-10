"""Drive the Acme triage agent through MockAgents — the headline demo.

Each query is a full agentic run: the SDK sends the conversation to MockAgents,
MockAgents returns a tool call, the SDK executes the local tool, sends the result
back, and MockAgents returns the final answer. All deterministic, no real LLM.

Run (with MockAgents already up on :8080):

    python -m app.triage_demo
"""

from __future__ import annotations

import asyncio

from agents import Runner

from .mock_setup import new_conversation_client
from .support_agent import build_support_agent

QUERIES = [
    "Hello!",
    "Where is my order?",
    "I'd like a refund please",
    "I need to speak to a human",
]


async def run_one(query: str) -> str:
    # Fresh session id per conversation so MockAgents' turn counter advances
    # across the tool-calling loop (turn 1 -> tool call, turn 2 -> final answer).
    session_id = new_conversation_client()
    agent = build_support_agent()
    result = await Runner.run(agent, query, max_turns=6)
    return f"[{session_id[:14]}…] {result.final_output}"


async def main() -> None:
    print("=== Acme Support Triage (OpenAI Agents SDK → MockAgents) ===\n")
    for query in QUERIES:
        print(f"USER : {query}")
        try:
            answer = await run_one(query)
        except Exception as exc:  # noqa: BLE001 — demo: surface any wiring error
            answer = f"ERROR: {exc}"
        print(f"AGENT: {answer}\n")


if __name__ == "__main__":
    asyncio.run(main())
