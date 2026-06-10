"""Drive the Acme triage agent through MockAgents with Google ADK (native Gemini).

Each query is a full agentic run: ADK sends the conversation to MockAgents'
Gemini endpoint, MockAgents returns a functionCall, ADK executes the local Python
tool, sends the functionResponse back, and MockAgents returns the final answer.
All deterministic, no real Gemini API.

Run (with MockAgents already up):

    python -m app.triage_demo
"""

from __future__ import annotations

from google.genai import types
from google.adk.runners import InMemoryRunner

from .mock_setup import configure_gemini_env
from .support_agent import build_support_agent

QUERIES = [
    "Hello!",
    "Where is my order?",
    "I'd like a refund please",
    "I need to speak to a human",
]


def run_one(runner: InMemoryRunner, prompt: str) -> None:
    # Fresh ADK session per conversation (matching is content-based, so this is
    # just clean isolation between queries).
    sess = runner.session_service.create_session_sync(app_name="acme", user_id="u1")
    msg = types.Content(role="user", parts=[types.Part(text=prompt)])
    for event in runner.run(user_id="u1", session_id=sess.id, new_message=msg):
        if not (event.content and event.content.parts):
            continue
        for part in event.content.parts:
            if getattr(part, "function_call", None):
                fc = part.function_call
                print(f"  [function call] {fc.name} {dict(fc.args or {})}")
            elif getattr(part, "text", None):
                print(f"  AGENT: {part.text}")


def main() -> None:
    configure_gemini_env()
    runner = InMemoryRunner(agent=build_support_agent(), app_name="acme")
    print("=== Acme Support Triage (Google ADK → MockAgents Gemini) ===\n")
    for prompt in QUERIES:
        print(f"USER : {prompt}")
        try:
            run_one(runner, prompt)
        except Exception as exc:  # noqa: BLE001 — demo: surface any wiring error
            print(f"  ERROR: {exc}")
        print()


if __name__ == "__main__":
    main()
