"""The Acme support triage agent — ordinary Google ADK code.

The tools are plain Python functions; ADK auto-wraps them as FunctionTools and
sends them to Gemini as function declarations. When the model returns a function
call, ADK executes the function locally and sends the result back. MockAgents only
decides *which* function call to return (see mockagents/support-agent-gemini.yaml).

Each tool returns a dict with a unique MARKER key (ORDER_RESULT / REFUND_RESULT /
ESCALATION_RESULT). On the next turn that result is sent back as a Gemini
functionResponse, and MockAgents surfaces the marker into the matched content so
the "*-resolved" scenario can emit the final answer (which ends the loop).
"""

from __future__ import annotations

from google.adk.agents import Agent


def lookup_order(order_id: str) -> dict:
    """Look up the status of an order by its ID."""
    return {
        "ORDER_RESULT": True,
        "order_id": order_id,
        "status": "shipped",
        "carrier": "UPS",
        "tracking": "1Z999AA10123456784",
        "eta": "2026-04-10",
    }


def issue_refund(order_id: str, amount: float) -> dict:
    """Issue a refund against an order."""
    return {
        "REFUND_RESULT": True,
        "order_id": order_id,
        "amount": amount,
        "refund_id": "RF-88231",
        "state": "approved",
    }


def escalate_to_human(reason: str) -> dict:
    """Open a ticket and hand the conversation to a human specialist."""
    return {"ESCALATION_RESULT": True, "ticket_id": "SUP-1042", "reason": reason, "queue": "tier-2"}


def build_support_agent(model: str = "gemini-2.0-flash") -> Agent:
    """Construct the triage agent. ``model`` selects which MockAgents agent
    handles the traffic (routing is by model name)."""
    return Agent(
        name="acme_support",
        model=model,
        instruction=(
            "You are Acme Corp's friendly tier-1 support triage agent. "
            "Use lookup_order for order status, issue_refund for refunds, and "
            "escalate_to_human when the customer wants a person. Be concise."
        ),
        tools=[lookup_order, issue_refund, escalate_to_human],
    )
