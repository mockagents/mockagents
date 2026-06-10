"""Point Google ADK at MockAgents instead of the real Gemini API.

ADK builds a `google-genai` client internally. The genai SDK reads the
**`GOOGLE_GEMINI_BASE_URL`** env var for the Gemini-API base URL, so setting it
(plus selecting the Gemini API over Vertex) redirects ADK's calls to MockAgents'
native Gemini endpoint (`/v1beta/models/<model>:generateContent`). No code change
to the agent itself — just env.

These must be set BEFORE the agent runs (the client is created lazily), so call
`configure_gemini_env()` at the top of each entry point.
"""

from __future__ import annotations

import os

# Routing model for the triage agent (must match support-agent-gemini.yaml).
SUPPORT_MODEL = "gemini-2.0-flash"


def base_url() -> str:
    # Server ROOT — the genai client appends /v1beta/models/<model>:generateContent.
    return os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")


def api_key() -> str:
    # genai requires an API key to be set; MockAgents doesn't validate it on the
    # Gemini route (single-tenant). In multi-tenant mode, see TESTING.md.
    return os.environ.get("MOCKAGENTS_API_KEY", "mock-key")


def configure_gemini_env() -> None:
    """Redirect ADK's genai client at MockAgents (Gemini API, not Vertex)."""
    os.environ["GOOGLE_GENAI_USE_VERTEXAI"] = "0"
    os.environ["GOOGLE_API_KEY"] = api_key()
    os.environ["GOOGLE_GEMINI_BASE_URL"] = base_url()
