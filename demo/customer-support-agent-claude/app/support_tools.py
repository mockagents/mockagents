"""In-process MCP tools for the Acme support agent (Claude Agent SDK).

These run *inside* this Python process — the Claude Agent SDK exposes them to the
`claude` CLI as an in-process MCP server named "acme", so the CLI sees them as
`mcp__acme__lookup_order`, etc. That namespaced name is exactly what the
MockAgents scenario must emit as the tool call (see mockagents/support-agent.yaml).

Each tool returns a result prefixed with a unique MARKER (ORDER_RESULT /
REFUND_RESULT / ESCALATION_RESULT). On the next turn the Anthropic protocol sends
that result back as the user message, and the mock's "*-resolved" scenarios match
the marker to produce the final answer — which is how the loop terminates without
an X-Session-Id / turn counter.
"""

from __future__ import annotations

import json

from claude_agent_sdk import tool, create_sdk_mcp_server


@tool("lookup_order", "Look up the status of an order by its ID.", {"order_id": str})
async def lookup_order(args):
    payload = {
        "order_id": args.get("order_id", "UNKNOWN"),
        "status": "shipped",
        "carrier": "UPS",
        "tracking": "1Z999AA10123456784",
        "eta": "2026-04-10",
    }
    return {"content": [{"type": "text", "text": "ORDER_RESULT " + json.dumps(payload)}]}


@tool("issue_refund", "Issue a refund against an order.", {"order_id": str, "amount": float})
async def issue_refund(args):
    payload = {
        "order_id": args.get("order_id"),
        "amount": args.get("amount"),
        "refund_id": "RF-88231",
        "state": "approved",
    }
    return {"content": [{"type": "text", "text": "REFUND_RESULT " + json.dumps(payload)}]}


@tool("escalate_to_human", "Open a ticket and hand off to a human specialist.", {"reason": str})
async def escalate_to_human(args):
    payload = {"ticket_id": "SUP-1042", "reason": args.get("reason"), "queue": "tier-2"}
    return {"content": [{"type": "text", "text": "ESCALATION_RESULT " + json.dumps(payload)}]}


def build_acme_mcp_server():
    """Bundle the three tools into an in-process MCP server named "acme"."""
    return create_sdk_mcp_server(
        name="acme",
        version="1.0.0",
        tools=[lookup_order, issue_refund, escalate_to_human],
    )


# The namespaced tool names the CLI exposes (and that the mock must emit).
ACME_TOOLS = [
    "mcp__acme__lookup_order",
    "mcp__acme__issue_refund",
    "mcp__acme__escalate_to_human",
]
