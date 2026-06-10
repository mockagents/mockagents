"""Resilience demo — prove retry/backoff recovers against injected faults.

Points the plain Anthropic SDK at the "acme-support-flaky-claude" agent (model
claude-3-opus-20240229), which 503s its first 2 requests then heals
(chaos: errors.fail_first: 2). Retries are DISABLED and we run our own loop so
each attempt's outcome is visible — you watch 503, 503, 200.

Run:

    python -m app.resilience_demo

The fail-first counter is per-agent and resets when MockAgents restarts.
"""

from __future__ import annotations

import os
import time

from anthropic import Anthropic, APIStatusError

BASE_URL = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")
API_KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "claude-3-5-sonnet-20241022"  # routes to the flaky agent
MAX_ATTEMPTS = 6


def main() -> None:
    client = Anthropic(base_url=BASE_URL, api_key=API_KEY, max_retries=0)
    print(f"=== Resilience demo against model '{MODEL}' (first 2 requests 503) ===\n")

    backoff = 0.5
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            resp = client.messages.create(
                model=MODEL,
                max_tokens=128,
                messages=[{"role": "user", "content": "Are you healthy?"}],
            )
            text = "".join(b.text for b in resp.content if b.type == "text")
            print(f"attempt {attempt}: 200 OK -> {text}")
            print("\nRecovered. Retry/backoff worked.")
            return
        except APIStatusError as exc:
            retry_after = exc.response.headers.get("retry-after", "-")
            print(
                f"attempt {attempt}: {exc.status_code} "
                f"(retry-after={retry_after}) — backing off {backoff:.1f}s"
            )
            time.sleep(backoff)
            backoff = min(backoff * 2, 8)

    print("\nGave up after max attempts (did you forget to restart MockAgents?).")


if __name__ == "__main__":
    main()
