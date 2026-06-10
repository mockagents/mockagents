"""Acme customer-support triage demo app (OpenAI Agents SDK → MockAgents)."""

# Force UTF-8 stdout/stderr so the scripts' Unicode output (✅, →, em dashes in
# the agent's replies) prints on consoles whose default encoding isn't UTF-8 —
# notably Windows cp1252, where it would otherwise raise UnicodeEncodeError.
# Containers/Linux are already UTF-8, so this is a no-op there.
import sys as _sys

for _stream in (_sys.stdout, _sys.stderr):
    _reconfigure = getattr(_stream, "reconfigure", None)
    if _reconfigure is not None:
        try:
            _reconfigure(encoding="utf-8")
        except Exception:  # noqa: BLE001 — best effort; never block on this
            pass
