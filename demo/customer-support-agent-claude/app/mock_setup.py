"""Point the Claude Agent SDK at MockAgents instead of the real Anthropic API.

The Claude Agent SDK drives the `claude` CLI subprocess, and the CLI honors
`ANTHROPIC_BASE_URL` + `ANTHROPIC_API_KEY`. We pass those through
`ClaudeAgentOptions(env=...)` so the spawned CLI talks to MockAgents. MockAgents
serves the Anthropic Messages API at `/v1/messages`, so the base URL is the
server ROOT (no `/v1` suffix — the CLI appends the path itself).

`permission_mode="bypassPermissions"` keeps the run non-interactive, and
`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` suppresses the CLI's auxiliary
calls (telemetry / title generation) that would otherwise hit the mock with
unexpected models.
"""

from __future__ import annotations

import os

from claude_agent_sdk import ClaudeAgentOptions

from .support_tools import build_acme_mcp_server, ACME_TOOLS

# The support agent's routing model (must match mockagents/support-agent.yaml).
SUPPORT_MODEL = "claude-3-5-sonnet-latest"


def base_url() -> str:
    # Server ROOT — NOT /v1. The CLI appends /v1/messages.
    return os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")


def api_key() -> str:
    # Single-tenant: any non-empty value. Multi-tenant: a tenant API key, so
    # MockAgents resolves the tenant for quota + cost. Sent as the X-Api-Key header.
    return os.environ.get("MOCKAGENTS_API_KEY", "mock-key")


# Env vars that, when set (e.g. because you run this demo from *inside* Claude
# Code), make the spawned `claude` CLI behave as "nested in an agent" and inject
# the parent session's skills/context into the user message — which pollutes
# MockAgents' content matching. On a normal machine these are unset and this is a
# no-op; we blank them so the demo is deterministic in either environment.
_NEUTRALIZE_NESTED = {
    k: ""
    for k in (
        "CLAUDE_CODE_ENTRYPOINT",
        "AI_AGENT",
        "CLAUDE_CODE_SESSION_ID",
        "CLAUDECODE",
        "CLAUDE_EFFORT",
    )
}


def support_options(model: str = SUPPORT_MODEL, max_turns: int = 6) -> ClaudeAgentOptions:
    env = {
        "ANTHROPIC_BASE_URL": base_url(),
        "ANTHROPIC_API_KEY": api_key(),
        "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    }
    env.update(_NEUTRALIZE_NESTED)
    return ClaudeAgentOptions(
        model=model,
        system_prompt="You are Acme Corp's friendly tier-1 customer-support triage agent.",
        mcp_servers={"acme": build_acme_mcp_server()},
        allowed_tools=ACME_TOOLS,
        permission_mode="bypassPermissions",
        max_turns=max_turns,
        # Don't load this machine's user/project/global settings (skills, etc.).
        setting_sources=[],
        env=env,
    )
