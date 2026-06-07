"""`mockagents` console entry point — resolve/download the binary and run it.

Enables ``pipx run mockagents <args>`` (and a plain ``mockagents <args>`` after
``pip install mockagents``): the Go binary is resolved on PATH / the SDK cache,
downloaded from GitHub Releases on first use if absent (sha256-verified,
fail-closed via ``_binary``), then executed with the passed arguments.

This is distinct from ``mockagents-install`` (which only downloads/caches the
binary and exits).
"""

from __future__ import annotations

import os
import subprocess
import sys

from ._binary import BinaryNotFoundError, ensure_binary


def main() -> int:
    try:
        # exclude=sys.argv[0]: this console script is itself named `mockagents`,
        # so the resolver must never select (and re-exec) it — that would be a
        # fork bomb.
        binary = ensure_binary(auto_download=True, exclude=[sys.argv[0]])
    except BinaryNotFoundError as exc:
        print(f"mockagents: {exc}", file=sys.stderr)
        return 1

    args = sys.argv[1:]
    # On POSIX, replace this process so signals (Ctrl-C) go straight to the
    # server. On Windows, os.execv has awkward semantics, so spawn + wait.
    if os.name == "posix":
        os.execv(binary, [binary, *args])
        return 0  # unreachable
    return subprocess.call([binary, *args])


if __name__ == "__main__":
    raise SystemExit(main())
