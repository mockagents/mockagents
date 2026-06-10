"""Alternative backend: Google ADK reaching MockAgents via the LiteLLM bridge.

Instead of the native Gemini endpoint, this uses ADK's `LiteLlm` model wrapper
pointed at MockAgents' OpenAI-compatible endpoint (/v1/chat/completions). Same
agent, same tools — only the model backend differs.

Because LiteLLM speaks the OpenAI wire format, a tool result comes back as a
role:"tool" message, which MockAgents does NOT surface as the latest user message.
So the OpenAI-protocol fixture (support-agent-litellm.yaml) gates tool calls on
`turn_number`, and we give each conversation a stable X-Session-Id (via LiteLLM
`extra_headers`) so the turn counter advances.

Run (with MockAgents up):

    python -m app.litellm_demo
"""

from __future__ import annotations

import os
import uuid

from google.adk.agents import Agent
from google.adk.models.lite_llm import LiteLlm
from google.adk.runners import InMemoryRunner
from google.genai import types

from .support_agent import lookup_order, issue_refund, escalate_to_human

QUERIES = [
    "Hello!",
    "Where is my order?",
    "I'd like a refund please",
    "I need to speak to a human",
]


def openai_base_url() -> str:
    # LiteLLM's openai provider needs the /v1 suffix (unlike the native Gemini path).
    return os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080").rstrip("/") + "/v1"


def api_key() -> str:
    return os.environ.get("MOCKAGENTS_API_KEY", "mock-key")


def build_agent(session_id: str) -> Agent:
    model = LiteLlm(
        model="openai/gpt-4o",  # routes to support-agent-litellm.yaml
        api_base=openai_base_url(),
        api_key=api_key(),
        # Stable per-conversation session id so MockAgents' turn counter advances
        # (the OpenAI fixture is turn_number-gated).
        extra_headers={"X-Session-Id": session_id},
    )
    return Agent(
        name="acme_support",
        model=model,
        instruction=(
            "You are Acme Corp's friendly tier-1 support triage agent. Use "
            "lookup_order, issue_refund, and escalate_to_human as needed. Be concise."
        ),
        tools=[lookup_order, issue_refund, escalate_to_human],
    )


def run_one(prompt: str) -> None:
    runner = InMemoryRunner(agent=build_agent(f"conv-{uuid.uuid4()}"), app_name="acme")
    sess = runner.session_service.create_session_sync(app_name="acme", user_id="u1")
    msg = types.Content(role="user", parts=[types.Part(text=prompt)])
    for event in runner.run(user_id="u1", session_id=sess.id, new_message=msg):
        if not (event.content and event.content.parts):
            continue
        for part in event.content.parts:
            if getattr(part, "function_call", None):
                fc = part.function_call
                print(f"  [tool] {fc.name} {dict(fc.args or {})}")
            elif getattr(part, "text", None):
                print(f"  AGENT: {part.text}")


def main() -> None:
    print("=== Acme Support Triage (Google ADK + LiteLLM bridge → MockAgents OpenAI) ===\n")
    for prompt in QUERIES:
        print(f"USER : {prompt}")
        try:
            run_one(prompt)
        except Exception as exc:  # noqa: BLE001
            print(f"  ERROR: {exc}")
        print()


if __name__ == "__main__":
    main()
