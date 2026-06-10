"""Framework-free contract test for the Gemini surface (raw HTTP, no ADK).

Drives the two-turn function-call round-trip on
/v1beta/models/<model>:generateContent by hand and asserts the Gemini wire
contract:

    turn 1  ->  a functionCall part (mock asks us to call lookup_order)
    turn 2  ->  finishReason == "STOP" with a text answer

In the Gemini protocol the tool result is sent back as a functionResponse part on
a follow-up user turn — which becomes turn 2's matched content.

Run:

    python -m app.deterministic_smoke
"""

from __future__ import annotations

import os
import sys

import httpx

BASE = os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080")
KEY = os.environ.get("MOCKAGENTS_API_KEY", "mock-key")
MODEL = "gemini-2.0-flash"
URL = f"{BASE}/v1beta/models/{MODEL}:generateContent"
HEADERS = {"x-goog-api-key": KEY, "content-type": "application/json"}
TOOLS = [
    {
        "functionDeclarations": [
            {
                "name": "lookup_order",
                "description": "Look up an order by id.",
                "parameters": {
                    "type": "object",
                    "properties": {"order_id": {"type": "string"}},
                    "required": ["order_id"],
                },
            }
        ]
    }
]


def main() -> int:
    # --- Turn 1: expect a functionCall.
    body1 = {
        "contents": [{"role": "user", "parts": [{"text": "where is my order"}]}],
        "tools": TOOLS,
    }
    r1 = httpx.post(URL, json=body1, headers=HEADERS, timeout=30)
    r1.raise_for_status()
    parts1 = r1.json()["candidates"][0]["content"]["parts"]
    fc = next((p["functionCall"] for p in parts1 if "functionCall" in p), None)
    print(f"turn 1: functionCall={fc['name'] if fc else None} args={fc.get('args') if fc else None}")
    assert fc and fc["name"] == "lookup_order", "expected a lookup_order functionCall on turn 1"

    # --- Turn 2: send the functionResponse back, expect a final STOP answer.
    body2 = {
        "contents": [
            {"role": "user", "parts": [{"text": "where is my order"}]},
            {"role": "model", "parts": [{"functionCall": {"name": "lookup_order", "args": {"order_id": "ORD-12345"}}}]},
            {
                "role": "user",
                "parts": [{"functionResponse": {"name": "lookup_order", "response": {"ORDER_RESULT": True, "status": "shipped"}}}],
            },
        ],
        "tools": TOOLS,
    }
    r2 = httpx.post(URL, json=body2, headers=HEADERS, timeout=30)
    r2.raise_for_status()
    cand2 = r2.json()["candidates"][0]
    text = "".join(p.get("text", "") for p in cand2["content"]["parts"])
    print(f"turn 2: finishReason={cand2['finishReason']}")
    print(f"        answer={text}")
    assert cand2["finishReason"] == "STOP", "expected a final STOP answer on turn 2"
    assert text, "expected a non-empty answer on turn 2"

    print("\nSMOKE PASSED ✅")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except AssertionError as exc:
        print(f"\nSMOKE FAILED ❌  {exc}")
        sys.exit(1)
