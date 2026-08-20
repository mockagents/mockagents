"""Document retrieval over MCP.

This is the half of a RAG app that would normally talk to a vector database.
Here it calls a `search_docs` tool on an MCP server that MockAgents serves from
`agents/knowledge-base.yaml`, spawned as a subprocess over stdio — the standard
MCP client/server arrangement, so nothing here is mock-specific except the
command being spawned.
"""

from __future__ import annotations

import json
import os
import shutil
from dataclasses import dataclass
from typing import Any

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

#: Documents scoring below this are treated as no result at all. Retrieval that
#: returns only weak matches is worse than returning nothing, because the
#: generator will happily write a confident answer on top of it.
MIN_SCORE = 0.5


@dataclass(frozen=True)
class Document:
    id: str
    title: str
    text: str
    score: float


class RetrievalError(RuntimeError):
    """The retrieval backend failed or returned something unparseable."""


def _binary() -> str:
    """Locate the mockagents binary the same way the SDK does."""
    explicit = os.environ.get("MOCKAGENTS_BIN") or os.environ.get("MOCKAGENTS_BINARY")
    if explicit:
        return explicit
    found = shutil.which("mockagents")
    if found:
        return found
    raise RetrievalError(
        "mockagents binary not found: put it on PATH or set MOCKAGENTS_BIN"
    )


def _agents_dir() -> str:
    return os.environ.get("MOCKAGENTS_AGENTS_DIR", "./agents")


async def search(query: str, top_k: int = 5) -> list[Document]:
    """Return documents relevant to `query`, strongest first.

    Documents scoring below MIN_SCORE are dropped, so "found only weak matches"
    and "found nothing" are the same thing to the caller — which is what the
    generator should be told.
    """
    params = StdioServerParameters(
        command=_binary(),
        args=["mcp", "--transport", "stdio", "--agents-dir", _agents_dir()],
    )
    async with stdio_client(params) as (read, write):
        async with ClientSession(read, write) as session:
            await session.initialize()
            result = await session.call_tool(
                "search_docs", {"query": query, "top_k": top_k}
            )

    if _is_error(result):
        raise RetrievalError(f"search_docs failed: {_text_of(result)}")

    try:
        raw: Any = json.loads(_text_of(result))
    except json.JSONDecodeError as exc:
        raise RetrievalError(f"search_docs returned non-JSON: {exc}") from exc
    if not isinstance(raw, list):
        raise RetrievalError(f"search_docs returned {type(raw).__name__}, expected a list")

    docs = [
        Document(
            id=str(d["id"]),
            title=str(d.get("title", "")),
            text=str(d.get("text", "")),
            score=float(d.get("score", 0.0)),
        )
        for d in raw
    ]
    strong = [d for d in docs if d.score >= MIN_SCORE]
    strong.sort(key=lambda d: d.score, reverse=True)
    return strong[:top_k]


def _is_error(result: Any) -> bool:
    """Read the tool-result error flag across MCP SDK majors.

    The wire field is `isError`; the Python SDK models it as a pydantic field
    whose attribute name changed from `isError` (1.x) to `is_error` (2.0).
    Reading both keeps this demo working on either, which is the same thing a
    real app has to do while the ecosystem crosses that version boundary.
    """
    flag = getattr(result, "is_error", None)
    if flag is None:
        flag = getattr(result, "isError", False)
    return bool(flag)


def _text_of(result: Any) -> str:
    """Concatenate the text blocks of an MCP tool result.

    A text block is identified by having a `.text`, not by its `.type` tag:
    the tag is stable but the block classes are not, and `.text` is what we
    actually need.
    """
    return "".join(
        block.text for block in result.content if getattr(block, "text", None) is not None
    )
