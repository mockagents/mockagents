"""Show the Responses API's stateful `previous_response_id` chaining.

This uses the raw OpenAI client (not the Agents SDK) to make the server-side
conversation state explicit: the second call passes only a NEW input plus the
first response's id, and MockAgents replays the earlier turn from its in-memory
response store. A real OpenAI server behaves the same way; here it is fully
offline and deterministic.

Prerequisite: MockAgents running with this demo's fixtures. Then:

    python -m app.previous_response_demo
"""

from __future__ import annotations

from openai import OpenAI

from .mock_setup import api_key, base_url


def main() -> None:
    client = OpenAI(
        base_url=base_url(),
        api_key=api_key(),
        # Stable session id so the turn counter advances across the chain.
        default_headers={"X-Session-Id": "prev-resp-demo"},
    )

    first = client.responses.create(model="gpt-4o", input="hello")
    print(f"turn 1 id={first.id}")
    print(f"turn 1 output> {first.output_text}\n")

    # Turn 2 references turn 1 by id and sends only the new input. The server
    # remembers the prior turn (input + assistant reply) behind `first.id`.
    second = client.responses.create(
        model="gpt-4o",
        previous_response_id=first.id,
        input="and where is my order?",
    )
    print(f"turn 2 id={second.id}  previous_response_id={second.previous_response_id}")
    print(f"turn 2 output> {second.output_text}")

    assert second.previous_response_id == first.id, "chain id mismatch"
    assert second.id != first.id, "each turn must get a fresh id"
    print("\nprevious_response_id chaining verified against /v1/responses.")


if __name__ == "__main__":
    main()
