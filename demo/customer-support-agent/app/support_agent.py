"""The Acme support triage agent — ordinary OpenAI Agents SDK code.

Nothing here knows it's talking to a mock. The tools below are *executed locally*
by the SDK when the model returns a tool call; MockAgents only decides *which*
tool call to return (see mockagents/support-agent.yaml). The tool names and
parameters must line up with the scenario ``tool_calls`` in that YAML so the SDK
can run them.
"""

from __future__ import annotations

import json

from agents import Agent, function_tool


@function_tool
def lookup_order(order_id: str) -> str:
    """Look up the status of an order by its ID."""
    # A real implementation would hit an order-management service. For the demo
    # the canned data mirrors what the mock advertises, so the loop is coherent.
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


@function_tool
def escalate_to_human(reason: str) -> str:
    """Open a ticket and hand the conversation to a human specialist."""
    return json.dumps({"ticket_id": "SUP-1042", "reason": reason, "queue": "tier-2"})


def build_support_agent(model: str = "gpt-4o") -> Agent:
    """Construct the triage agent. ``model`` selects which MockAgents agent
    handles the traffic (routing is by model name — "gpt-4o" -> acme-support)."""
    return Agent(
        name="Acme Support Triage",
        instructions=(
            "You are Acme Corp's friendly tier-1 support triage agent. "
            "Use lookup_order for order status, issue_refund for refunds, and "
            "escalate_to_human when the customer wants a person. Be concise."
        ),
        model=model,
        tools=[lookup_order, issue_refund, escalate_to_human],
    )
