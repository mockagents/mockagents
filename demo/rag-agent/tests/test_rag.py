"""End-to-end tests for the RAG app. No network, no tokens, no flakes.

The `mockagents` fixture comes from the pytest plugin that ships with the
mockagents SDK — there is nothing to import and no conftest.py. It starts one
mock LLM server for the session and points `OPENAI_BASE_URL` at it, so
`app.rag` calls `OpenAI()` exactly as it would in production. Retrieval spawns
its own mock MCP server over stdio (see `app/retrieval.py`).
"""

from __future__ import annotations

import os

import pytest

from app import rag
from app.retrieval import RetrievalError, search


@pytest.fixture(autouse=True)
def _agents_dir(mockagents, monkeypatch):
    """Point retrieval's MCP subprocess at the same agents directory.

    Depending on `mockagents` (rather than `mockagents_server`) is what patches
    OPENAI_BASE_URL for the app, so this one fixture wires up both halves.
    """
    monkeypatch.setenv("MOCKAGENTS_AGENTS_DIR", os.path.join(os.path.dirname(__file__), "..", "agents"))


# --------------------------------------------------------------------------
# Retrieval
# --------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_retrieval_returns_ranked_documents():
    docs = await search("when are invoices sent?")
    assert [d.id for d in docs] == ["doc-1", "doc-2"], "expected both docs, strongest first"
    assert docs[0].score > docs[1].score


@pytest.mark.asyncio
async def test_retrieval_can_return_nothing():
    """The most common RAG failure in production, on demand."""
    assert await search("do you support quantum entanglement billing?") == []


@pytest.mark.asyncio
async def test_low_confidence_results_are_dropped():
    """A 0.11-scoring hit is not a hit. Passing it to the generator would
    produce a confident answer resting on an irrelevant document."""
    assert await search("can i pay in cowrie shells?") == []


# --------------------------------------------------------------------------
# The RAG loop
# --------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_grounded_answer_cites_a_retrieved_document():
    result = await rag.answer("when are invoices sent?")
    assert not result.abstained
    assert "1st of each month" in result.text
    assert result.citations == ["doc-1"]
    assert {d.id for d in result.documents} == {"doc-1", "doc-2"}


@pytest.mark.asyncio
async def test_empty_retrieval_abstains_without_calling_the_model():
    """Guardrail 1. If retrieval found nothing there is nothing to ground an
    answer in, so the model must not be asked at all."""
    result = await rag.answer("do you support quantum entanglement billing?")
    assert result.abstained
    assert result.text == rag.ABSTAIN
    assert result.documents == []


@pytest.mark.asyncio
async def test_fabricated_citation_is_caught():
    """Guardrail 2 — the point of this demo.

    The fixture answers the refund question with a confident, plausible,
    completely ungrounded answer that cites `doc-7`, a document retrieval never
    returned. Without the citation check this reaches the user as fact.

    A real model does this occasionally and unpredictably, so a suite running
    against a live model would exercise this branch approximately never.
    """
    with pytest.raises(rag.UngroundedAnswer) as exc:
        await rag.answer("what is your refund policy?")

    assert exc.value.fabricated == ["doc-7"]
    assert exc.value.retrieved == ["doc-3"]
    assert "90 days" in exc.value.answer  # ...and it contradicts doc-3's 30 days


@pytest.mark.asyncio
async def test_the_fixture_declares_itself_a_hallucination(mockagents_client):
    """Ground truth, straight from the server.

    MockAgents advertises a planted bad output with an
    `X-Mockagents-Hallucination` header. The app never sees it — that would be
    cheating — but the test can, which is how we know the previous test failed
    for the right reason rather than because the wording happened to differ.
    """
    from openai import OpenAI

    raw = OpenAI().chat.completions.with_raw_response.create(
        model=rag.MODEL,
        messages=[{"role": "user", "content": "what is your refund policy?"}],
    )
    assert raw.headers.get("X-Mockagents-Hallucination") == "ungrounded"


@pytest.mark.asyncio
async def test_unknown_question_falls_through_to_abstention():
    result = await rag.answer("who won the 1994 world cup?")
    assert result.abstained


# --------------------------------------------------------------------------
# Failure of the backend itself
# --------------------------------------------------------------------------

def test_error_flag_is_read_across_mcp_sdk_majors():
    """The MCP Python SDK renamed this attribute from `isError` (1.x) to
    `is_error` (2.0). CI installs the latest, so pinning only one spelling
    means the demo breaks the day the ecosystem moves -- which is exactly how
    this was found."""
    from app.retrieval import _is_error

    class OneX:  # mcp 1.x
        isError = True

    class TwoX:  # mcp 2.0
        is_error = True

    class Fine:
        is_error = False

    assert _is_error(OneX()) is True
    assert _is_error(TwoX()) is True
    assert _is_error(Fine()) is False


@pytest.mark.asyncio
async def test_missing_binary_is_a_clear_error(monkeypatch):
    """Retrieval failures should say what to do, not raise FileNotFoundError
    from three frames deep."""
    monkeypatch.setenv("MOCKAGENTS_BIN", "")
    monkeypatch.setenv("MOCKAGENTS_BINARY", "")
    monkeypatch.setattr("app.retrieval.shutil.which", lambda _: None)
    with pytest.raises(RetrievalError, match="not found"):
        await search("when are invoices sent?")
