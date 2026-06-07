"""Locate — or download — the MockAgents Go binary for the Python SDK.

The SDK drives the `mockagents` binary as a subprocess (see ``server.py``). A
fresh ``pip install mockagents`` ships no binary, so this module resolves one:

1. ``$MOCKAGENTS_BINARY`` if set,
2. the binary on ``PATH``,
3. ``./mockagents`` in the working directory,
4. the SDK's per-user cache (populated by ``mockagents-install`` /
   ``auto_download``).

If none is found, :func:`ensure_binary` either downloads the matching release
asset (when ``auto_download`` is on or ``$MOCKAGENTS_AUTO_DOWNLOAD`` is truthy)
or raises :class:`BinaryNotFoundError` with copy-paste install guidance — never
a bare ``FileNotFoundError``.
"""

from __future__ import annotations

import hashlib
import io
import os
import platform
import shutil
import stat
import tarfile
import zipfile
from pathlib import Path
from typing import Optional

GITHUB_REPO = "mockagents/mockagents"


class BinaryNotFoundError(FileNotFoundError):
    """The mockagents binary is unavailable and was not (or could not be) downloaded."""


def _is_windows() -> bool:
    # platform.system() (not sys.platform) so tests can monkeypatch the target OS.
    return platform.system().lower() == "windows"


def binary_filename() -> str:
    return "mockagents.exe" if _is_windows() else "mockagents"


def asset_os_arch() -> tuple[str, str]:
    """Map the running platform to goreleaser's (os, arch) asset tokens."""
    os_map = {"linux": "linux", "darwin": "darwin", "windows": "windows"}
    sysname = platform.system().lower()
    if sysname not in os_map:
        raise BinaryNotFoundError(f"unsupported OS for auto-download: {platform.system()!r}")
    arch_map = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}
    machine = platform.machine().lower()
    if machine not in arch_map:
        raise BinaryNotFoundError(
            f"unsupported architecture for auto-download: {platform.machine()!r}"
        )
    return os_map[sysname], arch_map[machine]


def cache_dir() -> Path:
    """Per-user cache directory the SDK downloads the binary into."""
    if _is_windows():
        base = os.environ.get("LOCALAPPDATA") or str(Path.home() / "AppData" / "Local")
        return Path(base) / "mockagents" / "bin"
    if platform.system().lower() == "darwin":
        return Path.home() / "Library" / "Caches" / "mockagents" / "bin"
    base = os.environ.get("XDG_CACHE_HOME") or str(Path.home() / ".cache")
    return Path(base) / "mockagents" / "bin"


def _looks_like_python_wrapper(path: str) -> bool:
    """Detect a pip/pipx console-script wrapper named ``mockagents`` (a Python
    script with a ``#!.../python`` shebang) so the launcher never resolves — and
    re-execs — itself, which would be a fork bomb.
    """
    try:
        with open(path, "rb") as fh:
            head = fh.read(64)
    except OSError:
        return False
    return head[:2] == b"#!" and b"python" in head.lower()


def _acceptable(path: str, skip: set) -> bool:
    return (
        os.path.isfile(path)
        and os.path.realpath(path) not in skip
        and not _looks_like_python_wrapper(path)
    )


def find_binary(exclude: Optional[list] = None) -> Optional[str]:
    """Return a path to the mockagents Go binary, or ``None`` if not resolvable.

    Search order: ``$MOCKAGENTS_BINARY``, ``PATH``, ``./mockagents(.exe)``, the
    SDK cache. ``exclude`` is a list of paths to ignore (the ``mockagents``
    console-script launcher passes its own path so it never selects itself); a
    PATH entry that is a Python console-script wrapper is also skipped.
    """
    skip = {os.path.realpath(p) for p in (exclude or []) if p}

    explicit = os.environ.get("MOCKAGENTS_BINARY")
    if explicit and Path(explicit).is_file():
        return str(Path(explicit).resolve())

    # Scan PATH manually (not shutil.which, which only returns the first hit) so
    # a self-named console-script wrapper can be skipped and a real Go binary
    # later on PATH still found.
    name = binary_filename()
    for d in os.environ.get("PATH", "").split(os.pathsep):
        if not d:
            continue
        cand = os.path.join(d, name)
        if os.access(cand, os.X_OK) and _acceptable(cand, skip):
            return str(Path(cand).resolve())

    for candidate in ("./mockagents", "./mockagents.exe", "mockagents.exe"):
        if _acceptable(candidate, skip):
            return str(Path(candidate).resolve())

    cached = cache_dir() / binary_filename()
    if cached.is_file():
        return str(cached)

    return None


def _release_base(version: str) -> str:
    tag = version if version.startswith("v") else f"v{version}"
    return f"https://github.com/{GITHUB_REPO}/releases/download/{tag}"


def _asset_name(version: str, os_tok: str, arch_tok: str) -> str:
    ver = version[1:] if version.startswith("v") else version
    ext = "zip" if os_tok == "windows" else "tar.gz"
    return f"mockagents_{ver}_{os_tok}_{arch_tok}.{ext}"


def download_binary(version: str, dest_dir: Optional[Path] = None, *, verify: bool = True) -> str:
    """Download the release binary for this platform into the cache; return its path."""
    import requests  # local import: only needed when actually downloading

    os_tok, arch_tok = asset_os_arch()
    asset = _asset_name(version, os_tok, arch_tok)
    base = _release_base(version)
    url = f"{base}/{asset}"

    resp = requests.get(url, timeout=60)
    if resp.status_code == 404:
        raise BinaryNotFoundError(
            f"no release asset at {url}\n"
            f"Version {version!r} may not be published yet.\n\n" + install_hint()
        )
    resp.raise_for_status()
    data = resp.content

    if verify:
        _verify_checksum(base, asset, data)

    dest_dir = dest_dir or cache_dir()
    dest_dir.mkdir(parents=True, exist_ok=True)
    out = dest_dir / binary_filename()
    _extract_binary(asset, data, out)
    if not _is_windows():
        out.chmod(out.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    return str(out)


def _verify_checksum(base: str, asset: str, data: bytes) -> None:
    """Fail-CLOSED sha256 check against the release ``checksums.txt``.

    The downloaded file is executed as a subprocess, so the checksum is the only
    integrity gate. goreleaser always publishes ``checksums.txt`` for a real
    release, so any failure to fetch, parse, or locate the asset's line is
    treated as a verification failure — we refuse to install rather than run an
    unverified binary. Callers that genuinely want no verification pass
    ``verify=False`` (only reached via that explicit opt-out).
    """
    import requests

    try:
        r = requests.get(f"{base}/checksums.txt", timeout=30)
    except requests.RequestException as exc:
        raise BinaryNotFoundError(
            f"could not fetch checksums.txt to verify {asset}: {exc}. "
            f"Refusing to install unverified (pass verify=False to override)."
        ) from exc
    if r.status_code != 200:
        raise BinaryNotFoundError(
            f"checksums.txt unavailable (HTTP {r.status_code}); refusing to install "
            f"{asset} unverified (pass verify=False to override)."
        )

    want: Optional[str] = None
    for line in r.text.splitlines():
        parts = line.split()
        # goreleaser format: "<sha256>  <asset>" (the "*" prefix marks binary mode).
        if len(parts) >= 2 and parts[-1].lstrip("*") == asset:
            want = parts[0]
            break
    if want is None:
        raise BinaryNotFoundError(
            f"no checksum for {asset} in checksums.txt; refusing to install unverified."
        )
    got = hashlib.sha256(data).hexdigest()
    if got != want:
        raise BinaryNotFoundError(
            f"checksum mismatch for {asset}: expected {want}, got {got}. Refusing to install."
        )


def _extract_binary(asset: str, data: bytes, out: Path) -> None:
    want = binary_filename()
    if asset.endswith(".zip"):
        with zipfile.ZipFile(io.BytesIO(data)) as zf:
            member = _match_member(zf.namelist(), want)
            with zf.open(member) as src, open(out, "wb") as dst:
                shutil.copyfileobj(src, dst)
        return
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as tf:
        member = _match_member(tf.getnames(), want)
        src = tf.extractfile(member)
        if src is None:
            raise BinaryNotFoundError(f"archive {asset} did not contain {want}")
        with src, open(out, "wb") as dst:
            shutil.copyfileobj(src, dst)


def _match_member(names, want: str) -> str:
    for n in names:
        if Path(n).name == want:
            return n
    for n in names:
        if Path(n).name in ("mockagents", "mockagents.exe"):
            return n
    raise BinaryNotFoundError(f"could not find {want} in the downloaded archive")


def install_hint() -> str:
    return (
        "Install the mockagents binary with one of:\n"
        "  brew install mockagents/tap/mockagents\n"
        "  go install github.com/mockagents/mockagents/cmd/mockagents@latest\n"
        "  docker run -p 8080:8080 mockagents/mockagents\n"
        "Or let the SDK fetch it for you:\n"
        "  python -m mockagents.install              # download + cache the binary\n"
        "  MockAgentServer(..., auto_download=True)   # fetch on first use\n"
        "  export MOCKAGENTS_AUTO_DOWNLOAD=1          # same, via env\n"
        "Or point the SDK at an existing binary:\n"
        "  MockAgentServer(..., binary_path='/path/to/mockagents')\n"
        "  export MOCKAGENTS_BINARY=/path/to/mockagents"
    )


def _env_truthy(name: str) -> bool:
    return os.environ.get(name, "").strip().lower() in ("1", "true", "yes", "on")


def ensure_binary(
    version: Optional[str] = None,
    *,
    auto_download: bool = False,
    exclude: Optional[list] = None,
) -> str:
    """Return a path to the mockagents binary, downloading it if permitted.

    ``exclude`` is forwarded to :func:`find_binary` (the launcher passes its own
    path). Raises :class:`BinaryNotFoundError` with actionable install guidance
    when the binary is absent and auto-download is disabled (or fails).
    """
    found = find_binary(exclude=exclude)
    if found:
        return found

    if auto_download or _env_truthy("MOCKAGENTS_AUTO_DOWNLOAD"):
        from . import __version__ as pkg_version

        return download_binary(version or pkg_version)

    raise BinaryNotFoundError(
        "the 'mockagents' binary was not found.\n\n" + install_hint()
    )
