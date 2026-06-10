"""Resilience demo — prove retry/backoff recovers against injected faults.

Points at the "acme-support-flaky" MockAgents agent, which 503s its first 2
requests then heals (chaos: errors.fail_first: 2). We use the plain OpenAI SDK
with retries DISABLED and our own loop so each attempt's outcome is visible —
you literally watch 503, 503, 200.

(The OpenAI Agents SDK would also recover transparently if you leave the
client's default max_retries in place; this script just makes the recovery
observable.)

Run:

    python -m app.resilience_demo

The fail-first counter is per-agent and resets when MockAgents restarts, so
restart the server to re-arm this demo.
"""

from __future__ import annotations

import os
import time

from openai import OpenAI
from openai import APIStatusError

BASE_URL = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080/v1")
API_KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "gpt-4o-mini"   # routes to the flaky agent (acme-support-flaky)
MAX_ATTEMPTS = 6


def main() -> None:
    # max_retries=0 so the SDK does not silently retry — we want to see each try.
    client = OpenAI(base_url=BASE_URL, api_key=API_KEY, max_retries=0)
    print(f"=== Resilience demo against model '{MODEL}' (first 2 requests 503) ===\n")

    backoff = 0.5
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            resp = client.chat.completions.create(
                model=MODEL,
                messages=[{"role": "user", "content": "Are you healthy?"}],
            )
            print(f"attempt {attempt}: 200 OK -> {resp.choices[0].message.content}")
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
