"""Tests for the `mockagents-install` CLI (install.py)."""

from __future__ import annotations

from mockagents import install
from mockagents._binary import BinaryNotFoundError


def test_install_already_available_skips_download(monkeypatch, capsys):
    monkeypatch.setattr(install, "find_binary", lambda: "/bin/mockagents")
    called = {}
    monkeypatch.setattr(install, "download_binary", lambda v: called.setdefault("v", v))
    assert install.main([]) == 0
    assert "v" not in called  # must NOT download when one is already resolvable
    assert "already available" in capsys.readouterr().out


def test_install_force_downloads_package_version(monkeypatch):
    monkeypatch.setattr(install, "find_binary", lambda: "/bin/mockagents")
    got = {}
    monkeypatch.setattr(install, "download_binary", lambda v: got.update(v=v) or "/cache/mockagents")
    assert install.main(["--force"]) == 0
    assert got["v"] == install.__version__


def test_install_explicit_version(monkeypatch):
    monkeypatch.setattr(install, "find_binary", lambda: None)
    got = {}
    monkeypatch.setattr(install, "download_binary", lambda v: got.update(v=v) or "/cache/mockagents")
    assert install.main(["0.2.3"]) == 0
    assert got["v"] == "0.2.3"


def test_install_download_error_returns_1(monkeypatch, capsys):
    monkeypatch.setattr(install, "find_binary", lambda: None)

    def boom(_v):
        raise BinaryNotFoundError("nope")

    monkeypatch.setattr(install, "download_binary", boom)
    assert install.main([]) == 1
    assert "error: nope" in capsys.readouterr().err
