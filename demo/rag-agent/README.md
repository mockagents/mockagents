# RAG demo — a grounded answerer, and the guardrail that catches it lying

A small retrieval-augmented-generation app with a real test suite, running
entirely against [MockAgents]: **no network, no tokens, no flakes.** Ten tests,
green in a few seconds, deterministic on every run.

The point isn't that the happy path works. It's that the *failure* paths are
exercised on every commit — an empty index, a low-confidence-only result set,
and a model that confidently cites a document it was never given. You cannot
ask a real model to do that last one on cue.

[MockAgents]: ../../README.md

## Run it

You need the `mockagents` binary. From a checkout of this repo:

```bash
make build                      # produces ./mockagents at the repo root
cd demo/rag-agent
pip install -r requirements.txt
pip install -e ../../sdk/python # the SDK isn't on PyPI yet — see the repo README
```

Then the one command:

```bash
MOCKAGENTS_BIN=../../mockagents pytest
```

```
..........                                                       [100%]
10 passed
```

That command is verbatim on Windows too — `make build` writes an extensionless
`mockagents` there as well. If the binary is already on your `PATH`, drop the
env var entirely and it is just `pytest`.

Nothing else starts a server, opens a port, or needs a compose file: the pytest
plugin that ships with the SDK spawns one MockAgents server for the session.
The app uses its LLM and VectorMock endpoints exactly as production clients do.

## What's here

```
agents/
  knowledge-base.yaml   kind: VectorCollection — deterministic retrieval data
  rag-answerer.yaml     kind: Agent      — the generation half
app/
  retrieval.py          Qdrant-compatible vector client and score filtering
  rag.py                the app under test: retrieve → abstain → answer → verify
tests/
  test_rag.py           11 tests
```

## The app

Three steps, each of which can be wrong independently of how good the model is:

1. **Retrieve** documents for the question, dropping anything scoring below
   `MIN_SCORE`. A 0.11-scoring hit is not a hit; passing it along produces a
   confident answer resting on an irrelevant document.
2. **Abstain** when retrieval came back empty, rather than asking the model to
   answer from nothing.
3. **Verify grounding**: every `[doc-N]` citation in the answer must name a
   document that was actually retrieved. Anything else is a fabrication.

Step 3 is the guardrail this demo exists for. In a suite that runs against a
live model, that branch is dead code nobody has ever executed — because the
model has to misbehave first, and it won't on request.

## The tests, and what each one proves

| Test | Failure mode it pins |
| --- | --- |
| `test_retrieval_returns_ranked_documents` | Ranking and parsing of a normal result set. |
| `test_retrieval_can_return_nothing` | **Empty index.** The most common RAG failure in production. |
| `test_low_confidence_results_are_dropped` | **Weak-match-only.** Worse than empty, because it looks like a hit. |
| `test_grounded_answer_cites_a_retrieved_document` | The happy path, with citations checked. |
| `test_empty_retrieval_abstains_without_calling_the_model` | Guardrail 1. |
| `test_fabricated_citation_is_caught` | **Guardrail 2 — the headline.** |
| `test_the_naive_guardrail_would_have_shipped_it` | **The guardrail *bug*.** The obvious check passes the fabrication. |
| `test_the_fixture_declares_itself_a_hallucination` | The test knows ground truth the app doesn't. |
| `test_unknown_question_falls_through_to_abstention` | The default scenario doesn't leak a fake answer. |
| `test_unavailable_vector_backend_is_a_clear_error` | Backend failure produces an actionable error. |

### The headline test

`agents/rag-answerer.yaml` answers the refund question with this, every time:

> You can return anything within 90 days for a full cash refund, shipping
> included. **[doc-7]**

Retrieval returned exactly one document, `doc-3`, which says *30 days, store
credit, excluding shipping*. So the answer both contradicts its source and cites
a document that does not exist in this context. The fixture declares itself:

```yaml
hallucination:
  type: ungrounded
  ground_truth: "doc-3 says 30 days, store credit only, excluding shipping."
```

which MockAgents advertises on the response as `X-Mockagents-Hallucination:
ungrounded`. The app never reads that header — that would be cheating — but the
test does, so a passing run means the guardrail fired *for the right reason* and
not because the wording happened to differ.

Delete the citation check in `app/rag.py` and this test fails. That is the
whole argument for the demo: the check is only load-bearing if something
actually exercises it.

### The guardrail bug the fixture exposes

Here is the version of that check most teams write first:

```python
def naive_grounding_ok(answer: str) -> bool:
    return bool(CITATION_RE.search(answer))   # "did the model cite anything?"
```

It looks careful. It passes code review. And it returns **True** for the
fabricated answer, because that answer does cite something — it cites `doc-7`.
So the answer ships, with a citation to a document that does not exist, and the
user has no way to tell.

`test_the_naive_guardrail_would_have_shipped_it` runs the fabricated answer
through both checks and asserts the difference, so it is a fact the suite
proves rather than a claim in a comment.

The reason this is demonstrable at all is that the fixture misbehaves on
demand. Against a live model the naive check would have looked correct in every
test run anyone ever did, because the model would have cited a real document
nearly every time — and the one time it didn't would have been in production.

## Why VectorMock

Retrieval uses MockAgents' Qdrant-compatible VectorMock surface backed by the
`VectorCollection` fixture. Query embeddings are deterministic local fixtures;
ranking, metadata filters, score thresholds, empty results, and vector failure
modes execute through the same provider-compatible HTTP layer an application
uses in production. The same collection can also be queried through Pinecone
and Chroma profiles.

## Notes that will save you time

- **Agents are selected by `spec.model`, not by name**, over the provider HTTP
  surfaces — there is no agent-name header. Give each agent a distinct model
  (this demo uses `gpt-4o-rag`) or requests resolve to whichever agent name
  sorts first, and the server logs a `model claimed by multiple agents` warning.
- **One directory, two kinds.** `agents/` holds both the `kind: Agent` and
  `kind: VectorCollection` documents; `mockagents start` validates and loads both.
- **One server is spawned for you.** The pytest plugin owns the LLM and vector
  surfaces together. There is no second process or teardown path.

## Related

- [Evals vs. tests](../../docs/EVALS_VS_TESTS.md) — why this suite is a test suite and not an eval
- [Hallucination Testing](../../site/docs/guides/hallucination-testing.md) — the fixture format
- [VectorMock](../../site/docs/guides/vector-mock.md) — provider routes and fixtures
- [Testing AI Agents](../../site/docs/guides/testing-agents.md) — the tool-call cookbook
