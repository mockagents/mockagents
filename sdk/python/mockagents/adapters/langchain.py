"""LangChain / LangGraph adapter.

Exposes factory functions that return LangChain chat models already
configured to hit a MockAgents server, plus a context manager that sets
the OpenAI and Anthropic base URL environment variables so frameworks
that construct their own clients (like LangGraph prebuilt agents) pick up
the mock without any code changes.

The ``langchain_openai`` / ``langchain_anthropic`` imports are deferred
to call sites so importing ``mockagents`` never pulls in LangChain.
"""

from __future__ import annotations

import contextlib
import os
from typing import Any, Iterator, Union

from ._common import require_module, resolve_base_url


def chat_openai(
    server: Union[str, Any],
    model: str = "gpt-4o",
    api_key: str = "mock-key",
    **kwargs: Any,
) -> Any:
    """Return a ``langchain_openai.ChatOpenAI`` pointed at a MockAgents server.

    Args:
        server: A ``MockAgentServer`` instance or a base URL string.
        model: Model name sent in the request body (matched against
            ``spec.model`` on loaded agents).
        api_key: Placeholder API key — MockAgents does not validate it by
            default, but LangChain requires a non-empty value.
        **kwargs: Extra keyword arguments forwarded to ``ChatOpenAI``.

    Raises:
        ImportError: If ``langchain_openai`` is not installed.
    """
    base_url = resolve_base_url(server) + "/v1"
    module = require_module("langchain_openai", extras="langchain")
    return module.ChatOpenAI(
        model=model,
        base_url=base_url,
        api_key=api_key,
        **kwargs,
    )


def chat_anthropic(
    server: Union[str, Any],
    model: str = "claude-3-5-sonnet-latest",
    api_key: str = "mock-key",
    **kwargs: Any,
) -> Any:
    """Return a ``langchain_anthropic.ChatAnthropic`` pointed at a MockAgents server."""
    base_url = resolve_base_url(server)
    module = require_module("langchain_anthropic", extras="langchain")
    return module.ChatAnthropic(
        model=model,
        base_url=base_url,
        api_key=api_key,
        **kwargs,
    )


@contextlib.contextmanager
def patched_env(
    server: Union[str, Any],
    providers: tuple[str, ...] = ("openai", "anthropic"),
) -> Iterator[dict[str, str]]:
    """Temporarily override provider base-URL environment variables.

    Useful for LangGraph prebuilt agents and any framework that builds
    its own ChatModel internally from env vars. Restores the previous
    values on exit.

    Args:
        server: A MockAgentServer or base URL string.
        providers: Which providers to patch. Supported: ``openai``,
            ``anthropic``.

    Yields:
        The dict of environment variables set by this context manager,
        handy for assertions.
    """
    base = resolve_base_url(server)
    env_map: dict[str, str] = {}
    if "openai" in providers:
        env_map["OPENAI_BASE_URL"] = base + "/v1"
        env_map["OPENAI_API_KEY"] = "mock-key"
    if "anthropic" in providers:
        env_map["ANTHROPIC_BASE_URL"] = base
        env_map["ANTHROPIC_API_KEY"] = "mock-key"

    previous = {k: os.environ.get(k) for k in env_map}
    try:
        for k, v in env_map.items():
            os.environ[k] = v
        yield env_map
    finally:
        for k, old in previous.items():
            if old is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = old
