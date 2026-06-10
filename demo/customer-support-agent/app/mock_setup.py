"""Point the OpenAI Agents SDK at a MockAgents server instead of real OpenAI.

The only thing that makes this a *mock* integration is the base URL + a couple of
SDK switches — your agent code (tools, instructions, the run loop) is unchanged.

Two switches matter:

1. ``set_default_openai_api("chat_completions")`` — the Agents SDK defaults to the
   OpenAI *Responses* API (``/v1/responses``), which MockAgents does not implement.
   MockAgents speaks Chat Completions (``/v1/chat/completions``), so we force it.

2. A stable ``X-Session-Id`` header per conversation — MockAgents keys turn/session
   state off this header. Turn-gated scenarios (``turn_number: 1`` => tool call,
   turn 2 => final answer) need it so the agent's tool-calling loop terminates
   deterministically. We create a fresh client (with a fresh session id) per
   conversation via :func:`new_conversation_client`.
"""

from __future__ import annotations

import os
import uuid

from openai import AsyncOpenAI
from agents import (
    set_default_openai_client,
    set_default_openai_api,
    set_tracing_disabled,
)


def base_url() -> str:
    return os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080/v1")


def api_key() -> str:
    # In single-tenant mode any non-empty value works. In multi-tenant mode set
    # this to a tenant API key so MockAgents resolves the tenant (for quota/cost).
    return os.environ.get("MOCKAGENTS_API_KEY", "mock-key")


def configure_mockagents() -> AsyncOpenAI:
    """Install a default client pointed at MockAgents and select Chat Completions.

    Returns the client (handy if you want to make raw calls too). Suitable when
    you don't need per-conversation session ids (e.g. single-shot calls).
    """
    client = AsyncOpenAI(
        base_url=base_url(),
        api_key=api_key(),
        max_retries=int(os.environ.get("OPENAI_MAX_RETRIES", "5")),
    )
    set_default_openai_client(client, use_for_tracing=False)
    set_default_openai_api("chat_completions")
    set_tracing_disabled(True)  # don't phone home to real OpenAI for traces
    return client


def new_conversation_client() -> str:
    """Install a default client whose every request carries a fresh X-Session-Id.

    Call this immediately before each ``Runner.run(...)`` so all turns of that one
    conversation share a session id (and MockAgents' turn counter advances), while
    different conversations get different ids.

    Returns the session id (useful for correlating with ``GET /api/v1/logs``).

    NOTE: this swaps the *global* default client, so run conversations
    sequentially. For concurrent runs, pass a per-run
    ``RunConfig(model_settings=ModelSettings(extra_headers={"X-Session-Id": sid}))``
    instead.
    """
    session_id = f"conv-{uuid.uuid4()}"
    client = AsyncOpenAI(
        base_url=base_url(),
        api_key=api_key(),
        default_headers={"X-Session-Id": session_id},
        max_retries=int(os.environ.get("OPENAI_MAX_RETRIES", "5")),
    )
    set_default_openai_client(client, use_for_tracing=False)
    set_default_openai_api("chat_completions")
    set_tracing_disabled(True)
    return session_id
