"""Internal helpers shared by adapter modules."""

from __future__ import annotations

from typing import Any, Union


def resolve_base_url(target: Union[str, Any]) -> str:
    """Accept either a plain URL string or a MockAgentServer instance and
    return the base URL the server is listening on.

    Importing MockAgentServer here would create a circular import at
    module load time, so we duck-type instead: anything with a ``url``
    attribute is treated as a server handle.
    """
    if isinstance(target, str):
        return target.rstrip("/")
    url = getattr(target, "url", None)
    if not url:
        raise TypeError(
            "expected a MockAgentServer or base URL string, got "
            f"{type(target).__name__}"
        )
    return str(url).rstrip("/")


def require_module(module: str, extras: str) -> Any:
    """Import ``module`` or raise an ImportError that points the user at
    the correct optional-dependency extra.
    """
    try:
        import importlib

        return importlib.import_module(module)
    except ImportError as exc:
        raise ImportError(
            f"{module!r} is required for this adapter. "
            f"Install it with: pip install 'mockagents[{extras}]'"
        ) from exc
