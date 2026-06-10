"""Resilience demo — prove retry/backoff recovers against injected faults.

Hits the flaky Gemini agent (model gemini-2.5-flash) directly over HTTP with our
own retry loop so each attempt's outcome is visible — you watch 503, 503, 200.
The agent 503s its first 2 requests then heals (chaos: errors.fail_first: 2).

Run:

    python -m app.resilience_demo

The fail-first counter is per-agent and resets when MockAgents restarts.
"""

from __future__ import annotations

import os
import time

import httpx

BASE = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")
KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "gemini-2.5-flash"  # routes to the flaky agent
URL = f"{BASE}/v1beta/models/{MODEL}:generateContent"
HEADERS = {"x-goog-api-key": KEY, "content-type": "application/json"}
MAX_ATTEMPTS = 6


def main() -> None:
    body = {"contents": [{"role": "user", "parts": [{"text": "Are you healthy?"}]}]}
    print(f"=== Resilience demo against model '{MODEL}' (first 2 requests 503) ===\n")

    backoff = 0.5
    for attempt in range(1, MAX_ATTEMPTS + 1):
        resp = httpx.post(URL, json=body, headers=HEADERS, timeout=30)
        if resp.status_code == 200:
            text = "".join(
                p.get("text", "") for p in resp.json()["candidates"][0]["content"]["parts"]
            )
            print(f"attempt {attempt}: 200 OK -> {text}")
            print("\nRecovered. Retry/backoff worked.")
            return
        retry_after = resp.headers.get("retry-after", "-")
        print(f"attempt {attempt}: {resp.status_code} (retry-after={retry_after}) — backing off {backoff:.1f}s")
        time.sleep(backoff)
        backoff = min(backoff * 2, 8)

    print("\nGave up after max attempts (did you forget to restart MockAgents?).")


if __name__ == "__main__":
    main()
