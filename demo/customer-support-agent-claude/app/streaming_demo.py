"""Streaming demo — watch token deltas arrive over SSE from MockAgents.

Uses the plain Anthropic SDK's streaming helper against the streaming-enabled
support agent. MockAgents chunks the canned reply and paces it with the agent's
streaming config, so you see incremental text just like the real API.

Run:

    python -m app.streaming_demo
"""

from __future__ import annotations

import os

from anthropic import Anthropic

BASE_URL = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")
API_KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "claude-3-5-sonnet-latest"


def main() -> None:
    client = Anthropic(base_url=BASE_URL, api_key=API_KEY)
    print("=== Streaming demo (token deltas) ===\n")
    print("AGENT: ", end="", flush=True)
    with client.messages.stream(
        model=MODEL,
        max_tokens=256,
        messages=[{"role": "user", "content": "hello there"}],
    ) as stream:
        for text in stream.text_stream:
            print(text, end="", flush=True)
    print("\n")


if __name__ == "__main__":
    main()
