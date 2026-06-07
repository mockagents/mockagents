"""pytest plugin for MockAgents.

Installed automatically with the ``mockagents`` package (registered via the
``pytest11`` entry point), so ``import``-ing nothing is required — the fixtures
below are available in any test session.

Fixtures
--------
``mockagents_server`` (session scope)
    Starts one MockAgents server subprocess for the whole test session and
    tears it down at the end. Yields the :class:`MockAgentServer`.

``mockagents`` (function scope)
    Depends on ``mockagents_server`` and, via ``monkeypatch``, points the
    OpenAI / Anthropic / Gemini SDK environment variables at it so your
    *existing* application code is redirected to the mock with **zero changes**.
    Env vars are restored after each test.

``mockagents_client`` (function scope)
    A :class:`MockAgentClient` bound to the session server.

Configuration
-------------
The agents directory is resolved in this order: ``--mockagents-agents-dir``
CLI option, then the ``mockagents_agents_dir`` ini option, then the
``MOCKAGENTS_AGENTS_DIR`` env var, then ``./agents``.

Example
-------
    def test_my_agent(mockagents):
        # OPENAI_BASE_URL now points at the mock — real SDK code "just works".
        from openai import OpenAI
        client = OpenAI()
        out = client.chat.completions.create(
            model="gpt-4o",
            messages=[{"role": "user", "content": "hello"}],
        )
        assert "Hi" in out.choices[0].message.content
"""

from __future__ import annotations

import os
from typing import Iterator, Optional

import pytest

from .client import MockAgentClient
from .server import MockAgentServer


def pytest_addoption(parser: pytest.Parser) -> None:
    group = parser.getgroup("mockagents", "MockAgents mock LLM server")
    group.addoption(
        "--mockagents-agents-dir",
        action="store",
        default=None,
        help="Directory of MockAgents agent YAML definitions (default: ./agents).",
    )
    group.addoption(
        "--mockagents-binary",
        action="store",
        default=None,
        help="Path to the mockagents binary (default: auto-detect on PATH).",
    )
    parser.addini(
        "mockagents_agents_dir",
        help="Directory of MockAgents agent YAML definitions.",
        default="./agents",
    )


def _resolve_agents_dir(config: pytest.Config) -> str:
    return (
        config.getoption("--mockagents-agents-dir")
        or os.environ.get("MOCKAGENTS_AGENTS_DIR")
        or config.getini("mockagents_agents_dir")
        or "./agents"
    )


@pytest.fixture(scope="session")
def mockagents_server(request: pytest.FixtureRequest) -> Iterator[MockAgentServer]:
    """Session-scoped MockAgents server subprocess."""
    agents_dir = _resolve_agents_dir(request.config)
    binary: Optional[str] = request.config.getoption("--mockagents-binary")
    server = MockAgentServer(agents_dir=agents_dir, binary_path=binary)
    server.start()
    try:
        yield server
    finally:
        server.stop()


@pytest.fixture()
def mockagents(
    mockagents_server: MockAgentServer,
    monkeypatch: pytest.MonkeyPatch,
) -> Iterator[MockAgentServer]:
    """Redirect the OpenAI / Anthropic SDKs at the mock server.

    Sets the base-URL (and dummy API-key) env vars the official SDKs read, so
    application code under test reaches the mock without modification. Restored
    automatically at the end of each test.

    **Gemini caveat:** the env-var redirect works only with the newer
    ``google-genai`` client, which reads ``GOOGLE_GEMINI_BASE_URL``. The legacy
    ``google-generativeai`` SDK has *no* base-URL env var — redirect it
    explicitly with ``genai.configure(client_options={"api_endpoint": <url>})``,
    or your test will silently hit the real Google endpoint.
    """
    base = mockagents_server.url
    # OpenAI (incl. Azure-style clients honoring OPENAI_BASE_URL).
    monkeypatch.setenv("OPENAI_BASE_URL", f"{base}/v1")
    monkeypatch.setenv("OPENAI_API_KEY", "mock-key")
    # Anthropic.
    monkeypatch.setenv("ANTHROPIC_BASE_URL", base)
    monkeypatch.setenv("ANTHROPIC_API_KEY", "mock-key")
    # Google Gemini — honored by the newer `google-genai` client only (see the
    # caveat above). Set both API-key vars the SDK may read.
    monkeypatch.setenv("GOOGLE_GEMINI_BASE_URL", base)
    monkeypatch.setenv("GEMINI_API_KEY", "mock-key")
    monkeypatch.setenv("GOOGLE_API_KEY", "mock-key")
    yield mockagents_server


@pytest.fixture()
def mockagents_client(mockagents_server: MockAgentServer) -> MockAgentClient:
    """A MockAgentClient bound to the session server."""
    return mockagents_server.client()
