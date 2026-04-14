"""CrewAI adapter.

CrewAI delegates LLM calls to LiteLLM under the hood, which means any
OpenAI-compatible endpoint can be wired in by passing ``base_url`` and
``api_key`` to ``crewai.LLM``. This adapter wraps that boilerplate so
users can just call ``mock_llm(server)`` in their Crew definitions.
"""

from __future__ import annotations

from typing import Any, Union

from ._common import require_module, resolve_base_url

__all__ = ["mock_llm"]


def mock_llm(
    server: Union[str, Any],
    model: str = "gpt-4o",
    api_key: str = "mock-key",
    **kwargs: Any,
) -> Any:
    """Return a ``crewai.LLM`` pointed at a MockAgents server.

    The returned object can be passed directly to ``crewai.Agent`` as its
    ``llm=`` argument. Every underlying request will hit the MockAgents
    server instead of a real LLM provider.

    Args:
        server: A ``MockAgentServer`` instance or a base URL string.
        model: Model identifier reported to LiteLLM. Using the
            ``openai/<name>`` prefix (e.g. ``openai/gpt-4o``) forces the
            OpenAI-compatible route and avoids LiteLLM auto-detection.
        api_key: Placeholder API key. LiteLLM requires a non-empty value.
        **kwargs: Extra keyword arguments forwarded to ``crewai.LLM``.

    Raises:
        ImportError: If ``crewai`` is not installed.
    """
    base_url = resolve_base_url(server) + "/v1"
    module = require_module("crewai", extras="crewai")
    # LiteLLM routes "openai/..." model names through the OpenAI provider,
    # so user input like "gpt-4o" still lands on /v1/chat/completions.
    if not model.startswith("openai/") and "/" not in model:
        model = "openai/" + model
    return module.LLM(
        model=model,
        base_url=base_url,
        api_key=api_key,
        **kwargs,
    )
