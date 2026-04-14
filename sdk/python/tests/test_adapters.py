"""Tests for the framework adapters.

These tests never import the real LangChain or CrewAI packages. Instead
they install fake modules into ``sys.modules`` before the adapter code
runs its lazy ``importlib.import_module`` call, so we can assert the
adapter passes the expected constructor kwargs without requiring users
to install any optional dependency just to run the test suite.
"""

from __future__ import annotations

import os
import sys
import types
from typing import Any

import pytest


class _FakeChatOpenAI:
    def __init__(self, **kwargs: Any) -> None:
        self.kwargs = kwargs


class _FakeChatAnthropic:
    def __init__(self, **kwargs: Any) -> None:
        self.kwargs = kwargs


class _FakeCrewAILLM:
    def __init__(self, **kwargs: Any) -> None:
        self.kwargs = kwargs


@pytest.fixture
def fake_langchain_openai(monkeypatch: pytest.MonkeyPatch) -> types.ModuleType:
    mod = types.ModuleType("langchain_openai")
    mod.ChatOpenAI = _FakeChatOpenAI  # type: ignore[attr-defined]
    monkeypatch.setitem(sys.modules, "langchain_openai", mod)
    return mod


@pytest.fixture
def fake_langchain_anthropic(monkeypatch: pytest.MonkeyPatch) -> types.ModuleType:
    mod = types.ModuleType("langchain_anthropic")
    mod.ChatAnthropic = _FakeChatAnthropic  # type: ignore[attr-defined]
    monkeypatch.setitem(sys.modules, "langchain_anthropic", mod)
    return mod


@pytest.fixture
def fake_crewai(monkeypatch: pytest.MonkeyPatch) -> types.ModuleType:
    mod = types.ModuleType("crewai")
    mod.LLM = _FakeCrewAILLM  # type: ignore[attr-defined]
    monkeypatch.setitem(sys.modules, "crewai", mod)
    return mod


class _FakeServer:
    """Duck-typed MockAgentServer stand-in."""

    def __init__(self, url: str = "http://localhost:8080") -> None:
        self.url = url


# --- chat_openai ---


def test_chat_openai_accepts_server_handle(fake_langchain_openai: Any) -> None:
    from mockagents.adapters import chat_openai

    server = _FakeServer("http://localhost:9090")
    llm = chat_openai(server, model="gpt-4o")
    assert llm.kwargs["model"] == "gpt-4o"
    assert llm.kwargs["base_url"] == "http://localhost:9090/v1"
    assert llm.kwargs["api_key"] == "mock-key"


def test_chat_openai_accepts_plain_url(fake_langchain_openai: Any) -> None:
    from mockagents.adapters import chat_openai

    llm = chat_openai("http://mock:8080/", model="gpt-4o-mini", temperature=0.0)
    assert llm.kwargs["base_url"] == "http://mock:8080/v1"
    assert llm.kwargs["model"] == "gpt-4o-mini"
    assert llm.kwargs["temperature"] == 0.0


def test_chat_openai_rejects_bad_type() -> None:
    from mockagents.adapters import chat_openai

    with pytest.raises(TypeError):
        chat_openai(object())  # type: ignore[arg-type]


def test_chat_openai_raises_when_dep_missing(monkeypatch: pytest.MonkeyPatch) -> None:
    from mockagents.adapters import chat_openai

    # Ensure no real module is in sys.modules, and importlib.import_module raises.
    monkeypatch.setitem(sys.modules, "langchain_openai", None)
    import importlib

    def fake_import(name: str) -> Any:
        raise ImportError(f"No module named {name!r}")

    monkeypatch.setattr(importlib, "import_module", fake_import)
    with pytest.raises(ImportError, match="langchain_openai"):
        chat_openai("http://localhost:8080")


# --- chat_anthropic ---


def test_chat_anthropic_uses_raw_base_url(fake_langchain_anthropic: Any) -> None:
    from mockagents.adapters import chat_anthropic

    llm = chat_anthropic("http://mock:8080")
    # Anthropic client takes the bare host; /v1 is appended by the library.
    assert llm.kwargs["base_url"] == "http://mock:8080"
    assert llm.kwargs["model"] == "claude-3-5-sonnet-latest"


# --- patched_env ---


def test_patched_env_sets_and_restores(monkeypatch: pytest.MonkeyPatch) -> None:
    from mockagents.adapters import patched_env

    monkeypatch.delenv("OPENAI_BASE_URL", raising=False)
    monkeypatch.setenv("ANTHROPIC_BASE_URL", "preexisting")

    with patched_env("http://mock:8080") as env:
        assert env["OPENAI_BASE_URL"] == "http://mock:8080/v1"
        assert os.environ["OPENAI_BASE_URL"] == "http://mock:8080/v1"
        assert os.environ["ANTHROPIC_BASE_URL"] == "http://mock:8080"

    # OPENAI_BASE_URL was absent, so it should be gone again.
    assert "OPENAI_BASE_URL" not in os.environ
    # ANTHROPIC_BASE_URL existed beforehand, so it should be restored.
    assert os.environ["ANTHROPIC_BASE_URL"] == "preexisting"


def test_patched_env_only_openai(monkeypatch: pytest.MonkeyPatch) -> None:
    from mockagents.adapters import patched_env

    monkeypatch.delenv("OPENAI_BASE_URL", raising=False)
    monkeypatch.delenv("ANTHROPIC_BASE_URL", raising=False)

    with patched_env("http://mock:8080", providers=("openai",)):
        assert "OPENAI_BASE_URL" in os.environ
        assert "ANTHROPIC_BASE_URL" not in os.environ


# --- CrewAI ---


def test_crewai_mock_llm_prepends_openai_prefix(fake_crewai: Any) -> None:
    from mockagents.adapters import crewai_mock_llm

    llm = crewai_mock_llm("http://mock:8080", model="gpt-4o")
    assert llm.kwargs["model"] == "openai/gpt-4o"
    assert llm.kwargs["base_url"] == "http://mock:8080/v1"
    assert llm.kwargs["api_key"] == "mock-key"


def test_crewai_mock_llm_respects_prefixed_model(fake_crewai: Any) -> None:
    from mockagents.adapters import crewai_mock_llm

    llm = crewai_mock_llm("http://mock:8080", model="openai/gpt-4o-mini")
    # Already prefixed, should not double-prefix.
    assert llm.kwargs["model"] == "openai/gpt-4o-mini"


def test_crewai_mock_llm_accepts_server_instance(fake_crewai: Any) -> None:
    from mockagents.adapters import crewai_mock_llm

    server = _FakeServer("http://localhost:9000")
    llm = crewai_mock_llm(server)
    assert llm.kwargs["base_url"] == "http://localhost:9000/v1"


def test_crewai_mock_llm_raises_when_missing(monkeypatch: pytest.MonkeyPatch) -> None:
    from mockagents.adapters import crewai_mock_llm

    import importlib

    def fake_import(name: str) -> Any:
        raise ImportError(f"No module named {name!r}")

    monkeypatch.setattr(importlib, "import_module", fake_import)
    with pytest.raises(ImportError, match="crewai"):
        crewai_mock_llm("http://localhost:8080")
