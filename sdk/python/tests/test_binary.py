"""Tests for binary resolution / download (RR-04)."""

from __future__ import annotations

import hashlib
import io
import os
import platform
import tarfile

import pytest

from mockagents import _binary
from mockagents._binary import BinaryNotFoundError


def test_asset_name_and_ext():
    assert _binary._asset_name("0.1.0", "linux", "amd64") == "mockagents_0.1.0_linux_amd64.tar.gz"
    assert _binary._asset_name("v0.1.0", "darwin", "arm64") == "mockagents_0.1.0_darwin_arm64.tar.gz"
    assert _binary._asset_name("0.1.0", "windows", "amd64") == "mockagents_0.1.0_windows_amd64.zip"


def test_asset_os_arch_mapping(monkeypatch):
    monkeypatch.setattr(platform, "system", lambda: "Linux")
    monkeypatch.setattr(platform, "machine", lambda: "x86_64")
    assert _binary.asset_os_arch() == ("linux", "amd64")

    monkeypatch.setattr(platform, "system", lambda: "Darwin")
    monkeypatch.setattr(platform, "machine", lambda: "arm64")
    assert _binary.asset_os_arch() == ("darwin", "arm64")

    monkeypatch.setattr(platform, "machine", lambda: "sparc")
    with pytest.raises(BinaryNotFoundError):
        _binary.asset_os_arch()


def test_find_binary_honors_explicit_env(tmp_path, monkeypatch):
    fake = tmp_path / "mockagents"
    fake.write_text("#!/bin/sh\n")
    monkeypatch.setenv("MOCKAGENTS_BINARY", str(fake))
    assert _binary.find_binary() == str(fake.resolve())


def test_ensure_binary_raises_actionable_error(monkeypatch):
    monkeypatch.setattr(_binary, "find_binary", lambda **kw: None)
    monkeypatch.delenv("MOCKAGENTS_AUTO_DOWNLOAD", raising=False)
    with pytest.raises(BinaryNotFoundError) as exc:
        _binary.ensure_binary()
    msg = str(exc.value)
    # The error must teach the user how to fix it, not just say "not found".
    assert "brew install mockagents/tap/mockagents" in msg
    assert "MOCKAGENTS_BINARY" in msg
    assert "auto_download" in msg


def test_find_binary_skips_python_console_wrapper(tmp_path, monkeypatch):
    # Regression for the fork-bomb: a pip/pipx console-script wrapper named like
    # the binary must NOT be selected, or `mockagents` would re-exec itself.
    bindir = tmp_path / "bin"
    bindir.mkdir()
    wrapper = bindir / _binary.binary_filename()
    wrapper.write_text("#!/usr/bin/python\nimport sys\n")
    wrapper.chmod(0o755)
    cwd = tmp_path / "cwd"
    cwd.mkdir()
    monkeypatch.chdir(cwd)
    monkeypatch.setenv("PATH", str(bindir))
    monkeypatch.delenv("MOCKAGENTS_BINARY", raising=False)
    monkeypatch.setattr(_binary, "cache_dir", lambda: tmp_path / "empty-cache")

    assert _binary.find_binary() is None
    assert _binary.find_binary(exclude=[str(wrapper)]) is None


def test_ensure_binary_auto_download_via_env(monkeypatch):
    monkeypatch.setattr(_binary, "find_binary", lambda **kw: None)
    monkeypatch.setenv("MOCKAGENTS_AUTO_DOWNLOAD", "1")
    called = {}

    def fake_download(version):
        called["version"] = version
        return "/cache/mockagents"

    monkeypatch.setattr(_binary, "download_binary", lambda v: fake_download(v))
    assert _binary.ensure_binary(version="0.1.0") == "/cache/mockagents"
    assert called["version"] == "0.1.0"


def test_download_binary_extracts_and_verifies(tmp_path, monkeypatch):
    responses = pytest.importorskip("responses")

    monkeypatch.setattr(platform, "system", lambda: "Linux")
    monkeypatch.setattr(platform, "machine", lambda: "x86_64")

    payload = b"#!/bin/sh\necho mock-binary\n"
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tf:
        info = tarfile.TarInfo(name="mockagents")
        info.size = len(payload)
        tf.addfile(info, io.BytesIO(payload))
    archive = buf.getvalue()

    version = "0.1.0"
    asset = "mockagents_0.1.0_linux_amd64.tar.gz"
    base = f"https://github.com/mockagents/mockagents/releases/download/v{version}"
    sha = hashlib.sha256(archive).hexdigest()

    with responses.RequestsMock() as rsps:
        rsps.add(responses.GET, f"{base}/{asset}", body=archive, status=200)
        rsps.add(responses.GET, f"{base}/checksums.txt", body=f"{sha}  {asset}\n", status=200)
        path = _binary.download_binary(version, dest_dir=tmp_path)

    assert os.path.basename(path) == "mockagents"
    with open(path, "rb") as fh:
        assert fh.read() == payload


def test_download_binary_404_is_actionable(tmp_path, monkeypatch):
    responses = pytest.importorskip("responses")
    monkeypatch.setattr(platform, "system", lambda: "Linux")
    monkeypatch.setattr(platform, "machine", lambda: "x86_64")

    version = "9.9.9"
    asset = "mockagents_9.9.9_linux_amd64.tar.gz"
    base = f"https://github.com/mockagents/mockagents/releases/download/v{version}"
    with responses.RequestsMock() as rsps:
        rsps.add(responses.GET, f"{base}/{asset}", status=404)
        with pytest.raises(BinaryNotFoundError) as exc:
            _binary.download_binary(version, dest_dir=tmp_path)
    assert "may not be published yet" in str(exc.value)


def _linux_targz(payload: bytes) -> bytes:
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tf:
        info = tarfile.TarInfo(name="mockagents")
        info.size = len(payload)
        tf.addfile(info, io.BytesIO(payload))
    return buf.getvalue()


def test_download_binary_checksum_mismatch_refuses(tmp_path, monkeypatch):
    responses = pytest.importorskip("responses")
    monkeypatch.setattr(platform, "system", lambda: "Linux")
    monkeypatch.setattr(platform, "machine", lambda: "x86_64")

    archive = _linux_targz(b"tampered binary")
    asset = "mockagents_0.1.0_linux_amd64.tar.gz"
    base = "https://github.com/mockagents/mockagents/releases/download/v0.1.0"
    with responses.RequestsMock() as rsps:
        rsps.add(responses.GET, f"{base}/{asset}", body=archive, status=200)
        # checksums.txt advertises a DIFFERENT hash -> must refuse.
        rsps.add(responses.GET, f"{base}/checksums.txt", body=f"{'0' * 64}  {asset}\n", status=200)
        with pytest.raises(BinaryNotFoundError, match="checksum mismatch"):
            _binary.download_binary("0.1.0", dest_dir=tmp_path)


def test_download_binary_fails_closed_when_checksums_missing(tmp_path, monkeypatch):
    responses = pytest.importorskip("responses")
    monkeypatch.setattr(platform, "system", lambda: "Linux")
    monkeypatch.setattr(platform, "machine", lambda: "x86_64")

    archive = _linux_targz(b"unverifiable binary")
    asset = "mockagents_0.1.0_linux_amd64.tar.gz"
    base = "https://github.com/mockagents/mockagents/releases/download/v0.1.0"
    with responses.RequestsMock() as rsps:
        rsps.add(responses.GET, f"{base}/{asset}", body=archive, status=200)
        rsps.add(responses.GET, f"{base}/checksums.txt", status=404)  # no checksums
        with pytest.raises(BinaryNotFoundError, match="[Rr]efusing to install"):
            _binary.download_binary("0.1.0", dest_dir=tmp_path)


def test_download_binary_windows_zip(tmp_path, monkeypatch):
    import hashlib
    import zipfile

    responses = pytest.importorskip("responses")
    monkeypatch.setattr(platform, "system", lambda: "Windows")
    monkeypatch.setattr(platform, "machine", lambda: "AMD64")

    payload = b"MZ fake windows exe"
    zbuf = io.BytesIO()
    with zipfile.ZipFile(zbuf, "w") as zf:
        zf.writestr("mockagents.exe", payload)
    archive = zbuf.getvalue()

    asset = "mockagents_0.1.0_windows_amd64.zip"
    base = "https://github.com/mockagents/mockagents/releases/download/v0.1.0"
    sha = hashlib.sha256(archive).hexdigest()
    with responses.RequestsMock() as rsps:
        rsps.add(responses.GET, f"{base}/{asset}", body=archive, status=200)
        rsps.add(responses.GET, f"{base}/checksums.txt", body=f"{sha}  {asset}\n", status=200)
        path = _binary.download_binary("0.1.0", dest_dir=tmp_path)

    assert os.path.basename(path) == "mockagents.exe"
    with open(path, "rb") as fh:
        assert fh.read() == payload
