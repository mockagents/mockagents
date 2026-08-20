# Evals vs. tests

**Evals measure whether the model is good enough. Tests measure whether your
code is correct. They are different questions, they fail for different reasons,
and you need both.**

Most teams building on LLMs have one of these and think they have coverage.

---

## Two questions, not one

| | Eval | Test |
| --- | --- | --- |
| **Question** | Is the model's output good? | Does my code do the right thing with the model's output? |
| **Subject** | The model + your prompt | Your application logic |
| **Verdict** | A score, on a distribution | Pass / fail, on one input |
| **When it fails** | The prompt regressed, the model changed, the task is too hard | You shipped a bug |
| **Cost per run** | Real tokens, real latency | Free, milliseconds |
| **Determinism** | None — that is the point | Total — that is the point |
| **Runs on** | A schedule, a release gate, a prompt change | Every commit |
| **Tools** | promptfoo, Braintrust, LangSmith, DeepEval, your own judge | pytest, Vitest, Go's testing — plus a mock like MockAgents |

An eval that returns pass/fail on a single input is a bad eval. A test whose
result depends on which model version answered is a bad test. Trying to make one
tool do both jobs produces both.

## What only an eval catches

- The new system prompt is 6% worse at extraction.
- Switching to a cheaper model degrades summary faithfulness.
- Your RAG chunking change lowered answer groundedness.
- A jailbreak template gets through 1 in 40 times.

None of this is knowable from a mock. A mock returns exactly what you told it
to; it has no opinion about quality, and asking it for one is circular.

## What only a test catches

- Your retry wrapper doesn't retry on `429` — only on `500`.
- Your tool dispatcher crashes on a `tool_call` whose `arguments` are malformed
  JSON, which real APIs do emit.
- The agent loop never terminates when the model re-issues a tool call it
  already has the result for.
- Your streaming parser drops the last chunk when the stream truncates
  mid-token.
- Your guardrail passes a confidently-wrong answer through, because it only ever
  saw correct answers in development.
- The agent calls `charge_card` before `validate_address`, not after.

None of this is reliably knowable from an eval, because you cannot ask a real
model to fail on demand. You can ask it fifty times and hope. That is not a
test — it is a lottery ticket you are paying for.

## Why conflating them hurts

**Evals used as regression tests** are slow, cost money per run, and flake on
model updates you didn't make. Teams respond by running them less, or by
loosening thresholds until they stop failing. Now they catch nothing.

**Tests used as quality gates** give false confidence. `assert "refund" in
response` passes against a mock and tells you nothing about whether a real model
would say the word "refund" — you asserted your own fixture back at yourself.

The split is not a compromise. Each becomes better at its job once it stops
trying to do the other's.

## Where MockAgents sits

MockAgents is squarely on the **test** side, and deliberately has no eval
features. It mocks the **wire protocol, not the model**: same response shapes,
same streaming frames, same error envelopes — deterministically, offline, with
no tokens spent.

What that buys you is the ability to write the failing case *on purpose*:

```yaml
# agents/flaky.yaml
spec:
  behavior:
    chaos:
      preset: rate-limited      # every request: 429 + Retry-After, every run
```

```python
import pytest


def test_backoff_respects_retry_after(mockagents):
    from openai import OpenAI, RateLimitError

    with pytest.raises(RateLimitError) as exc:
        OpenAI(max_retries=0).chat.completions.create(
            model="gpt-rl",
            messages=[{"role": "user", "content": "hi"}],
        )
    assert exc.value.response.headers["retry-after"] == "1"
```

That is the 429-with-`Retry-After` path your backoff logic exists for, firing on
every run instead of whenever a provider happens to throttle you.

Fault injection, hallucination fixtures, malformed tool arguments, truncated
streams, and connection-layer resets are all configured in YAML and fire every
run. See [Chaos & Fault Injection](../site/docs/guides/chaos.md) and
[Hallucination Testing](../site/docs/guides/hallucination-testing.md).

It also lets you assert the *trajectory* — the ordered shape of what the agent
did, which is application logic and therefore squarely testable:

```python
expect(result).to_have_tool_call_sequence(["search", "summarize"])
expect(result).to_have_tool_call_count(2)
```

See [Testing AI Agents](../site/docs/guides/testing-agents.md).

**What MockAgents will not do, by design:** score outputs, judge quality,
compare prompts, or tell you a model is good enough. Those are eval jobs. The
README says the same thing in one line under
[What it is *not*](../README.md#what-it-is-not), and the project roadmap lists
"evaluating prompt/model quality" among the things it has explicitly
[declined](ADOPTION_REQUIREMENTS.md#declined--from-both-documents-restated-so-they-stay-declined).

## A CI shape that works

```
every commit ──▶ mock-backed tests      free, deterministic, <10s, blocking
nightly     ──▶ eval suite              real tokens, scored, alerting
prompt/model change ──▶ eval suite      blocking on a threshold you chose
pre-release ──▶ both
```

The point of the top row is that it is *cheap enough to always run*. The moment
your per-commit suite calls a real model, someone will eventually turn it off.

## If you only do one thing

Start with the tests. Not because they matter more — because they are the ones
you can run on every commit, and a check that runs on every commit is worth more
than a better check that runs monthly. Add evals when you start changing prompts
or models often enough that quality drift is a real risk, which is usually
sooner than teams expect.

Then keep them apart.

---

*Disagree? [Open a discussion](https://github.com/mockagents/mockagents/discussions)
— this is a stance, not a spec.*
