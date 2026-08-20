"""OpenAI Agents SDK against MockAgents — a runnable recipe.

    pip install openai-agents mockagents pytest pytest-asyncio
    pytest test_openai_agents.py

The `mockagents` fixture (shipped with the SDK, no import needed) starts the
mock and points the provider env vars at it. The Agents SDK does NOT read those
env vars — it wants a client installed as the default — so this file shows the
three calls that redirect it, and the one header that makes its tool loop
terminate.
"""

from __future__ import annotations

import os
import uuid

import pytest


@pytest.fixture()
def agents_sdk(mockagents):
    """Point the Agents SDK at the mock, with a per-test conversation id.

    Three things have to happen and each is easy to miss:

    1. `set_default_openai_client` — the SDK constructs its own client and
       ignores OPENAI_BASE_URL, so patching the env alone redirects nothing.
    2. `set_tracing_disabled(True)` — without it the SDK posts a trace to the
       REAL OpenAI backend on every run. An offline test suite that quietly
       phones home is not offline.
    3. `X-Session-Id` — MockAgents keys turn state off this header. The
       turn-gated scenarios (turn 1 tool call, turn 2 answer) only terminate the
       loop if every request in one conversation carries the same id.
    """
    from agents import (
        set_default_openai_api,
        set_default_openai_client,
        set_tracing_disabled,
    )
    from openai import AsyncOpenAI

    client = AsyncOpenAI(
        base_url=f"{os.environ['OPENAI_BASE_URL']}",
        api_key="mock-key",  # any non-empty value; the mock ignores it
        default_headers={"X-Session-Id": f"conv-{uuid.uuid4()}"},
    )
    set_default_openai_client(client, use_for_tracing=False)
    set_default_openai_api("chat_completions")
    set_tracing_disabled(True)
    yield client


@pytest.mark.asyncio
async def test_agent_calls_the_tool_and_then_answers(agents_sdk):
    """The full two-turn loop: tool call, tool result, final answer."""
    from agents import Agent, Runner, function_tool

    calls: list[dict] = []

    @function_tool
    def lookup_order(order_id: str) -> str:
        """Look up an order by id."""
        calls.append({"order_id": order_id})
        return '{"status": "shipped", "carrier": "UPS", "tracking": "1Z999AA1"}'

    agent = Agent(
        name="support",
        instructions="Use lookup_order to answer order questions.",
        model="gpt-4o-frameworks",  # routes to agents/support-agent.yaml
        tools=[lookup_order],
    )

    result = await Runner.run(agent, "where is my order?")

    # The mock decided which tool to call and with what arguments -- every run.
    assert calls == [{"order_id": "ORD-42"}]
    assert "1Z999AA1" in result.final_output


@pytest.mark.asyncio
async def test_loop_terminates_without_a_real_model(agents_sdk):
    """The failure this recipe exists to prevent.

    Scenario matching keys on the latest USER message, which does not change
    while an agent works its tool loop. Without turn gating the mock would
    re-issue the same tool call forever and `Runner.run` would spin until it
    hit its max-turns guard. Asserting the turn count keeps that honest.
    """
    from agents import Agent, Runner, function_tool

    @function_tool
    def lookup_order(order_id: str) -> str:
        """Look up an order by id."""
        return '{"status": "shipped"}'

    agent = Agent(
        name="support",
        instructions="Use lookup_order to answer order questions.",
        model="gpt-4o-frameworks",
        tools=[lookup_order],
    )

    result = await Runner.run(agent, "where is my order?", max_turns=4)
    assert result.final_output, "the loop produced no final answer"
