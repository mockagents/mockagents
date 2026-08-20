"""LangGraph (and LangChain) against MockAgents — a runnable recipe.

    pip install langgraph langchain-openai mockagents pytest pytest-asyncio
    pytest test_langgraph.py

Two ways in, both here:

- **You construct the chat model** → pass `base_url` directly, or use the SDK's
  `mockagents.adapters.chat_openai` helper.
- **Something else constructs it** — a prebuilt agent, a factory, a config
  string like `"openai:gpt-4o"` — → patch the environment with
  `mockagents.adapters.patched_env`, since there is no argument to pass.
"""

from __future__ import annotations

import os
import uuid

import pytest


def _mock_url() -> str:
    """The mock's root URL. The `mockagents` fixture sets OPENAI_BASE_URL to
    `<root>/v1`; LangChain wants the `/v1` too, but `patched_env` and the
    adapters want the root, so strip it once here."""
    return os.environ["OPENAI_BASE_URL"].removesuffix("/v1")


def test_chat_model_takes_a_base_url(mockagents):
    """The direct path: LangChain chat models accept base_url + api_key."""
    from langchain_openai import ChatOpenAI

    llm = ChatOpenAI(
        model="gpt-4o-frameworks",
        base_url=os.environ["OPENAI_BASE_URL"],
        api_key="mock-key",
    )
    reply = llm.invoke("where is my order?")

    # Turn 1 of the fixture: a tool call, not prose.
    assert reply.tool_calls, f"expected a tool call, got {reply.content!r}"
    assert reply.tool_calls[0]["name"] == "lookup_order"
    assert reply.tool_calls[0]["args"] == {"order_id": "ORD-42"}


def test_the_sdk_adapter_saves_the_boilerplate(mockagents):
    """Same thing via the shipped adapter, which takes a URL or a
    MockAgentServer and fills in the api_key and the /v1 suffix."""
    from mockagents.adapters import chat_openai

    llm = chat_openai(_mock_url(), model="gpt-4o-frameworks")
    reply = llm.invoke("where is my order?")
    assert reply.tool_calls[0]["name"] == "lookup_order"


def test_prebuilt_agent_with_a_session_id_reaches_turn_two(mockagents):
    """The recipe you want: pass a MODEL INSTANCE, not a provider string.

    MockAgents keys turn state off the `X-Session-Id` header. A model instance
    can carry one; a provider string like `"openai:gpt-4o"` cannot, because the
    framework builds the client itself. With the header, the turn-gated fixture
    behaves like a real conversation: turn 1 is the tool call, turn 2 is a
    different, final answer.
    """
    from langchain.agents import create_agent
    from langchain_core.tools import tool
    from langchain_openai import ChatOpenAI

    seen: list[str] = []

    @tool
    def lookup_order(order_id: str) -> str:
        """Look up an order by id."""
        seen.append(order_id)
        return '{"status": "shipped", "carrier": "UPS", "tracking": "1Z999AA1"}'

    llm = ChatOpenAI(
        model="gpt-4o-frameworks",
        base_url=os.environ["OPENAI_BASE_URL"],
        api_key="mock-key",
        default_headers={"X-Session-Id": f"conv-{uuid.uuid4()}"},
    )
    agent = create_agent(llm, tools=[lookup_order])
    result = agent.invoke({"messages": [("user", "where is my order?")]})

    assert seen == ["ORD-42"], "the agent did not call the tool the mock asked for"
    assert "1Z999AA1" in result["messages"][-1].content


def test_prebuilt_agent_without_a_session_id_still_terminates(mockagents):
    """The awkward case, and why `patched_env` exists — plus the trap in it.

    A prebuilt agent built from a provider string makes its own chat model, so
    there is no `base_url` argument and no header to attach. `patched_env` is
    the only seam, and it restores the environment on exit so it does not leak
    into the next test.

    What you give up: with no `X-Session-Id`, every request is a fresh session
    and `turn_number` is always 1, so the turn-2 scenario never fires. The loop
    still TERMINATES -- the engine drops a tool call that is identical to one
    whose result the model has already been given, and answers with content
    only -- but the answer is turn 1's text, not turn 2's. Assert on the
    behaviour you actually get, and reach for the instance form above when you
    need distinct turns.

    API note: this is `langchain.agents.create_agent`. The older
    `langgraph.prebuilt.create_react_agent` still works but is deprecated as of
    LangGraph 1.0, and its string-model form additionally needs the `langchain`
    package, not just `langgraph` + `langchain-openai`.
    """
    from langchain.agents import create_agent
    from langchain_core.tools import tool
    from mockagents.adapters import patched_env

    seen: list[str] = []

    @tool
    def lookup_order(order_id: str) -> str:
        """Look up an order by id."""
        seen.append(order_id)
        return '{"status": "shipped", "carrier": "UPS", "tracking": "1Z999AA1"}'

    with patched_env(_mock_url()):
        agent = create_agent("openai:gpt-4o-frameworks", tools=[lookup_order])
        result = agent.invoke({"messages": [("user", "where is my order?")]})

    # It terminated, and it called the tool exactly once -- no runaway loop.
    assert seen == ["ORD-42"]
    final = result["messages"][-1]
    assert not getattr(final, "tool_calls", None), "the loop did not converge"
    assert final.content == "Let me look that up."   # turn 1's text, tool calls dropped


def test_patched_env_restores_the_environment(mockagents):
    """`patched_env` is a context manager, not a one-way switch: a test that
    used it must not change what the next test sees."""
    from mockagents.adapters import patched_env

    before = os.environ.get("OPENAI_BASE_URL")
    with patched_env("http://example.invalid:9999"):
        assert os.environ["OPENAI_BASE_URL"] != before
    assert os.environ.get("OPENAI_BASE_URL") == before
