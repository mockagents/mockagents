"""Streaming demo — watch token deltas arrive over SSE from MockAgents.

Uses the Agents SDK's streamed runner against the streaming-enabled support
agent. MockAgents chunks the canned reply and paces it with the agent's
streaming config (chunk_size / chunk_delay_ms), so you see incremental output
just like a real provider.

Run:

    python -m app.streaming_demo
"""

from __future__ import annotations

import asyncio

from agents import Runner
from openai.types.responses import ResponseTextDeltaEvent

from .mock_setup import new_conversation_client
from .support_agent import build_support_agent


async def main() -> None:
    new_conversation_client()
    agent = build_support_agent()  # model="acme-support" (streaming: enabled)

    print("=== Streaming demo (token deltas) ===\n")
    print("AGENT: ", end="", flush=True)

    result = Runner.run_streamed(agent, "Hello there!")
    async for event in result.stream_events():
        # Print only the raw text deltas; ignore tool/agent lifecycle events.
        if event.type == "raw_response_event" and isinstance(
            event.data, ResponseTextDeltaEvent
        ):
            print(event.data.delta, end="", flush=True)
    print("\n")


if __name__ == "__main__":
    asyncio.run(main())
