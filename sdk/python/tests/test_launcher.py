"""Tests for the `mockagents` launcher console entry point (RR-14 / pipx)."""

from __future__ import annotations

from mockagents import launcher
from mockagents._binary import BinaryNotFoundError


def test_launcher_execs_resolved_binary(monkeypatch):
    monkeypatch.setattr(launcher, "ensure_binary", lambda **kw: "/fake/mockagents")
    monkeypatch.setattr(launcher.sys, "argv", ["mockagents", "start", "--port", "9"])
    # Force the subprocess branch (os.execv would replace the test process).
    monkeypatch.setattr(launcher.os, "name", "nt")
    captured = {}
    monkeypatch.setattr(launcher.subprocess, "call", lambda argv: captured.__setitem__("argv", argv) or 0)

    assert launcher.main() == 0
    assert captured["argv"] == ["/fake/mockagents", "start", "--port", "9"]


def test_launcher_passes_exclude_self(monkeypatch):
    seen = {}

    def fake_ensure(**kw):
        seen.update(kw)
        return "/fake/mockagents"

    monkeypatch.setattr(launcher, "ensure_binary", fake_ensure)
    monkeypatch.setattr(launcher.sys, "argv", ["/venv/bin/mockagents"])
    monkeypatch.setattr(launcher.os, "name", "nt")
    monkeypatch.setattr(launcher.subprocess, "call", lambda argv: 0)

    launcher.main()
    # Must exclude its own path so it can never re-exec itself (fork bomb).
    assert seen.get("auto_download") is True
    assert seen.get("exclude") == ["/venv/bin/mockagents"]


def test_launcher_reports_missing_binary(monkeypatch, capsys):
    def boom(**kw):
        raise BinaryNotFoundError("not found here")

    monkeypatch.setattr(launcher, "ensure_binary", boom)
    monkeypatch.setattr(launcher.sys, "argv", ["mockagents", "start"])

    assert launcher.main() == 1
    assert "mockagents: not found here" in capsys.readouterr().err
