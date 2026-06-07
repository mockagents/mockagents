"""Download and cache the MockAgents binary.

Usage:
    python -m mockagents.install [VERSION] [--force]
    mockagents-install [VERSION] [--force]      # console script

With no VERSION, installs the binary matching the SDK version. ``--force``
re-downloads even if a binary is already resolvable.
"""

from __future__ import annotations

import sys
from typing import Optional, Sequence

from . import __version__
from ._binary import BinaryNotFoundError, download_binary, find_binary


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    force = "--force" in args
    positional = [a for a in args if not a.startswith("-")]
    version = positional[0] if positional else __version__

    if not force:
        existing = find_binary()
        if existing:
            print(f"mockagents already available at {existing} (use --force to re-download)")
            return 0

    try:
        path = download_binary(version)
    except BinaryNotFoundError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(f"installed mockagents {version} -> {path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
