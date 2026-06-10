"""Acme customer-support triage demo (Claude Agent SDK → MockAgents)."""

# Force UTF-8 stdout/stderr so Unicode output (✅, →, em dashes in the agent's
# replies) prints on consoles whose default encoding isn't UTF-8 — notably
# Windows cp1252. A no-op on Linux/containers, which are already UTF-8.
import sys as _sys

for _stream in (_sys.stdout, _sys.stderr):
    _reconfigure = getattr(_stream, "reconfigure", None)
    if _reconfigure is not None:
        try:
            _reconfigure(encoding="utf-8")
        except Exception:  # noqa: BLE001
            pass
