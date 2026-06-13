"""The Acme support agent — ordinary OpenAI Agents SDK code.

Nothing here knows it's talking to a mock, and nothing here knows it's on the
Responses API rather than Chat Completions — that's the drop-in promise. The
tools are executed *locally* by the SDK when the model returns a tool call;
MockAgents only decides *which* tool call to return (see
mockagents/responses-agent.yaml). Tool names + parameters must line up with the
scenario ``tool_calls`` in that YAML so the SDK can run them.
"""

from __future__ import annotations

import json

from agents import Agent, function_tool


@function_tool
def lookup_order(order_id: str) -> str:
    """Look up the status of an order by its ID."""
    return json.dumps(
        {
            "order_id": order_id,
            "status": "shipped",
            "carrier": "UPS",
            "tracking": "1Z999AA10123456784",
            "eta": "2026-04-10",
        }
    )


@function_tool
def issue_refund(order_id: str, amount: float) -> str:
    """Issue a refund against an order."""
    return json.dumps(
        {"order_id": order_id, "amount": amount, "refund_id": "RF-88231", "state": "approved"}
    )


def build_agent(model: str = "gpt-4o") -> Agent:
    """Construct the support agent. ``model`` selects which MockAgents agent
    handles the traffic (routing is by model name — "gpt-4o" -> acme-responses)."""
    return Agent(
        name="Acme Support (Responses)",
        instructions=(
            "You are Acme Corp's friendly tier-1 support agent. "
            "Use lookup_order for order status and issue_refund for refunds. Be concise."
        ),
        model=model,
        tools=[lookup_order, issue_refund],
    )
