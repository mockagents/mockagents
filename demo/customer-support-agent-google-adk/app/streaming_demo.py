"""Streaming demo — watch token deltas arrive over SSE from MockAgents.

Uses the google-genai client's streaming helper (the same client ADK uses)
pointed at MockAgents via HttpOptions.base_url. MockAgents chunks the canned reply
and paces it per the agent's streaming config (Gemini streamGenerateContent?alt=sse).

Run:

    python -m app.streaming_demo
"""

from __future__ import annotations

import os

from google import genai
from google.genai import types

BASE = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")
KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "gemini-2.0-flash"


def main() -> None:
    client = genai.Client(api_key=KEY, http_options=types.HttpOptions(base_url=BASE))
    print("=== Streaming demo (token deltas) ===\n")
    print("AGENT: ", end="", flush=True)
    for chunk in client.models.generate_content_stream(model=MODEL, contents="hello there"):
        if chunk.text:
            print(chunk.text, end="", flush=True)
    print("\n")


if __name__ == "__main__":
    main()
