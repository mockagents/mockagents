"""CrewAI against MockAgents — a runnable recipe.

    pip install crewai mockagents pytest
    pytest test_crewai.py

Install CrewAI in its OWN environment. It currently pins `openai<3` while the
OpenAI Agents SDK requires `openai>=3`, so the two cannot share a virtualenv —
which is why the CI job for this directory builds one venv per framework.

CrewAI routes every model call through LiteLLM, so pointing it at the mock is a
`base_url` + `api_key` on `crewai.LLM`. Two wrinkles the adapter handles, both
of which produce confusing failures when done by hand:

- LiteLLM needs the `openai/` model prefix, or it auto-detects a provider from
  the model name and routes somewhere else entirely.
- The base URL needs the `/v1` suffix; the adapter appends it.
"""

from __future__ import annotations

import os

import pytest


def _mock_url() -> str:
    """The mock's root URL — the adapter appends `/v1` itself."""
    return os.environ["OPENAI_BASE_URL"].removesuffix("/v1")


def test_adapter_routes_through_litellms_openai_provider(mockagents):
    """The one-liner: `crewai_mock_llm` returns a `crewai.LLM` you can hand
    straight to an `Agent`.

    Note where the prefix ends up. You pass `openai/gpt-4o`, and CrewAI 1.x
    splits it into `provider="openai"` + `model="gpt-4o"` — so asserting on
    `llm.model` and expecting the prefix back will surprise you.
    """
    from mockagents.adapters import crewai_mock_llm

    llm = crewai_mock_llm(_mock_url(), model="gpt-4o-frameworks")

    assert llm.provider == "openai", "LiteLLM must take the OpenAI route"
    assert llm.model == "gpt-4o-frameworks"
    assert llm.base_url.endswith("/v1")


def test_a_plain_question_returns_text(mockagents):
    """The default scenario: no tool call, just content."""
    from mockagents.adapters import crewai_mock_llm

    llm = crewai_mock_llm(_mock_url(), model="gpt-4o-frameworks")
    reply = llm.call("hello there")

    assert isinstance(reply, str)
    assert "order" in reply.lower()  # the fixture's default nudge


def test_a_tool_question_returns_tool_calls_not_text(mockagents):
    """The shape that trips people up.

    `LLM.call` returns a **string** for a content answer and a **list of tool
    calls** when the model asked for a tool. Code that assumes a string breaks
    the first time routing works. The mock lets you cover both branches
    deterministically, which a live model will not.
    """
    from mockagents.adapters import crewai_mock_llm

    llm = crewai_mock_llm(_mock_url(), model="gpt-4o-frameworks")
    reply = llm.call("where is my order?")

    assert isinstance(reply, list), f"expected tool calls, got {type(reply).__name__}"
    assert reply[0].function.name == "lookup_order"
    assert '"order_id"' in reply[0].function.arguments


def test_by_hand_without_the_adapter(mockagents):
    """The same wiring spelled out, for a project that would rather not add the
    mockagents SDK as a dependency of its application code."""
    from crewai import LLM

    llm = LLM(
        model="openai/gpt-4o-frameworks",  # the openai/ prefix is required
        base_url=os.environ["OPENAI_BASE_URL"],  # ...and so is the /v1
        api_key="mock-key",
    )
    assert isinstance(llm.call("hello there"), str)


def test_an_agent_runs_a_task_against_the_mock(mockagents):
    """End to end: a real `Agent` executing a real `Task`, no tokens spent, the
    same answer every run.

    The task deliberately avoids the word "order" so the fixture answers with
    content. An agent with no tools registered receiving a tool call is a
    CrewAI-side error, not a mock behaviour — keep the fixture and the agent's
    tool list in agreement.
    """
    from crewai import Agent, Crew, Task
    from mockagents.adapters import crewai_mock_llm

    agent = Agent(
        role="Support Triage",
        goal="Greet customers",
        backstory="You answer customer questions.",
        llm=crewai_mock_llm(_mock_url(), model="gpt-4o-frameworks"),
        verbose=False,
    )
    task = Task(
        description="Say hello to the customer.",
        expected_output="A short greeting.",
        agent=agent,
    )
    result = Crew(agents=[agent], tasks=[task], verbose=False).kickoff()

    assert str(result).strip(), "the crew produced no output"
