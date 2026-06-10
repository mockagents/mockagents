"""Drive the Acme triage agent through MockAgents with the Claude Agent SDK.

Each query is a full agentic run: the SDK runs the `claude` CLI, the CLI calls
MockAgents' /v1/messages, MockAgents returns a tool_use, the CLI executes the
in-process MCP tool, sends the result back, and MockAgents returns the final
answer. All deterministic, no real Anthropic API.

Run (with MockAgents already up):

    python -m app.triage_demo
"""

from __future__ import annotations

import anyio

from claude_agent_sdk import (
    query,
    AssistantMessage,
    TextBlock,
    ToolUseBlock,
    ResultMessage,
)

from .mock_setup import support_options

QUERIES = [
    "Hello!",
    "Where is my order?",
    "I'd like a refund please",
    "I need to speak to a human",
]


async def run_one(prompt: str) -> None:
    async for msg in query(prompt=prompt, options=support_options()):
        if isinstance(msg, AssistantMessage):
            for block in msg.content:
                if isinstance(block, ToolUseBlock):
                    print(f"  [tool call] {block.name} {block.input}")
                elif isinstance(block, TextBlock):
                    print(f"  AGENT: {block.text}")
        elif isinstance(msg, ResultMessage) and msg.is_error:
            print(f"  ERROR: {msg.subtype}")


async def main() -> None:
    print("=== Acme Support Triage (Claude Agent SDK → MockAgents) ===\n")
    for prompt in QUERIES:
        print(f"USER : {prompt}")
        try:
            await run_one(prompt)
        except Exception as exc:  # noqa: BLE001 — demo: surface any wiring error
            print(f"  ERROR: {exc}")
        print()


if __name__ == "__main__":
    anyio.run(main)
