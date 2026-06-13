"""Stream a Responses-API conversation and print the token deltas.

``Runner.run_streamed`` drives the same agent but surfaces the Responses
streaming event ladder (response.created -> output_text.delta* ->
response.completed) as SDK stream events. Here we pull the raw text deltas out
of ``ResponseTextDeltaEvent`` and print them as they arrive.

Prerequisite: MockAgents running with this demo's fixtures. Then:

    python -m app.streaming_demo
"""

from __future__ import annotations

import asyncio

from agents import Runner
from openai.types.responses import ResponseTextDeltaEvent

from .agent import build_agent
from .mock_setup import new_conversation_client


async def main() -> None:
    agent = build_agent()
    new_conversation_client()

    print("agent> ", end="", flush=True)
    result = Runner.run_streamed(agent, "hello, what can you do?")
    async for event in result.stream_events():
        # raw_response_event carries the underlying Responses SSE events; the
        # text deltas are ResponseTextDeltaEvent payloads.
        if event.type == "raw_response_event" and isinstance(
            event.data, ResponseTextDeltaEvent
        ):
            print(event.data.delta, end="", flush=True)
    print("\n\nStream complete (response.* events consumed via the SDK).")


if __name__ == "__main__":
    asyncio.run(main())
