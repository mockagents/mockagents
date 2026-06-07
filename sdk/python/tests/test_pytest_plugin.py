"""Tests for the pytest plugin's configuration resolution (RR-12)."""

from __future__ import annotations

from mockagents import pytest_plugin


class _Cfg:
    """Minimal stand-in for pytest.Config exposing getoption/getini."""

    def __init__(self, opt=None, ini="./agents"):
        self._opt = opt
        self._ini = ini

    def getoption(self, _name):
        return self._opt

    def getini(self, _name):
        return self._ini


def test_resolve_agents_dir_precedence(monkeypatch):
    # 1. CLI option wins over everything.
    monkeypatch.setenv("MOCKAGENTS_AGENTS_DIR", "/env/agents")
    assert pytest_plugin._resolve_agents_dir(_Cfg(opt="/cli/agents")) == "/cli/agents"

    # 2. Then the env var.
    assert pytest_plugin._resolve_agents_dir(_Cfg(opt=None)) == "/env/agents"

    # 3. Then the ini option.
    monkeypatch.delenv("MOCKAGENTS_AGENTS_DIR", raising=False)
    assert pytest_plugin._resolve_agents_dir(_Cfg(opt=None, ini="/ini/agents")) == "/ini/agents"

    # 4. Finally the default.
    assert pytest_plugin._resolve_agents_dir(_Cfg(opt=None, ini="")) == "./agents"
