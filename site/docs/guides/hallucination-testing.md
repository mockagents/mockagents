# Testing Hallucination Handling

You can't reliably make a real model hallucinate on demand — so you can't easily
test that your app **catches** a hallucination. MockAgents lets you inject a
**deterministic** confidently-wrong output (a fabricated fact, a made-up
citation, an ungrounded RAG answer, a bogus tool result) so you can assert your
guardrails, validators, and fallback logic handle it.

> Test your hallucination **handling**, not just hope to detect it. Eval tools
> (promptfoo, Giskard, DeepEval, Guardrails) *detect* hallucinations in real
> models — MockAgents *injects* them, deterministically.

## Mark a response as a hallucination fixture

Add a `hallucination` block to any scenario response. The `content` is the
planted bad output; the block labels it:

```yaml
- name: refund-policy-ungrounded
  match: { content_contains: "refund" }
  response:
    content: "You can return anything within 90 days for a full cash refund."
    hallucination:
      type: ungrounded            # fabricated_fact | fabricated_citation | ungrounded | bad_tool_result | other
      ground_truth: "KB says 30 days, store credit only."
      note: "Contradicts the knowledge base."
```

When that scenario matches, the mock responds normally **and** sets a header:

```
X-Mockagents-Hallucination: ungrounded
```

So a negative test can detect the planted fixture (and knows the ground truth)
without parsing the body — then assert that *your* guardrail flagged it.

See [`examples/hallucination-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/hallucination-agent.yaml)
for one fixture per type, and
[`examples/support-flow-agent.yaml`](https://github.com/mockagents/mockagents/blob/main/examples/support-flow-agent.yaml)
for a hallucination embedded in a realistic support flow.

## Assert your guardrail catches it (pytest)

```python
def test_grounding_guardrail_flags_ungrounded_answer(mockagents):
    import httpx
    from openai import OpenAI

    # Capture response headers to confirm we hit the planted fixture.
    seen = {}
    client = OpenAI(http_client=httpx.Client(event_hooks={
        "response": [lambda r: seen.update(h=r.headers.get("x-mockagents-hallucination"))]
    }))
    out = client.chat.completions.create(
        model="gpt-4o-halluc",   # the hallucination-agent's model (routes here unambiguously)
        messages=[{"role": "user", "content": "what's the refund policy?"}],
    ).choices[0].message.content

    assert seen["h"] == "ungrounded"          # we got the planted hallucination
    assert my_grounding_guardrail(out) is False  # ...and YOUR guardrail rejects it
```

## Fixture types

| `type` | Use it to test… |
|---|---|
| `fabricated_fact` | a confidently-wrong factual answer |
| `fabricated_citation` | a made-up source/URL/paper |
| `ungrounded` | a RAG answer not supported by the supplied context |
| `bad_tool_result` | a fabricated tool result presented as fact |
| `other` | anything else you want flagged as a planted bad output |

Pair this with [chaos + streaming faults](yaml-schema.md) and
[scenario packs](scenario-packs.md) to test the full unhappy-path surface.
