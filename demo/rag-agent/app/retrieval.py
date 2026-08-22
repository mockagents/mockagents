"""Deterministic document retrieval through MockAgents VectorMock."""
from __future__ import annotations
import asyncio, json, os
from dataclasses import dataclass
from urllib import error, request

MIN_SCORE = 0.5
COLLECTION = "knowledge-base"

@dataclass(frozen=True)
class Document:
    id: str
    title: str
    text: str
    score: float

class RetrievalError(RuntimeError):
    """The vector backend failed or returned something unparseable."""

def _base_url() -> str:
    if explicit := os.environ.get("MOCKAGENTS_BASE_URL"):
        return explicit.rstrip("/")
    return os.environ.get("OPENAI_BASE_URL", "http://127.0.0.1:8080/v1").removesuffix("/v1").rstrip("/")

def _query_fixture(query: str) -> tuple[list[float], str]:
    query = query.strip().lower()
    if "invoice" in query: return [1.0, 0.0, 0.0], "billing"
    if "refund" in query: return [0.0, 1.0, 0.0], "refunds"
    if "cowrie" in query: return [0.0, 0.0, 1.0], "weak-payment"
    return [1.0, 0.0, 0.0], "no-match"

async def search(query: str, top_k: int = 5) -> list[Document]:
    embedding, topic = _query_fixture(query)
    payload = json.dumps({"vector": embedding, "limit": top_k, "with_payload": True,
        "filter": {"must": [{"key": "topic", "match": {"value": topic}}]}}).encode()
    url = f"{_base_url()}/collections/{COLLECTION}/points/search"
    def send() -> bytes:
        req = request.Request(url, data=payload, headers={"Content-Type": "application/json"}, method="POST")
        with request.urlopen(req, timeout=5) as response: return response.read()
    try:
        matches = json.loads(await asyncio.to_thread(send))["result"]
    except (error.URLError, TimeoutError, json.JSONDecodeError, KeyError, TypeError) as exc:
        raise RetrievalError(f"VectorMock search failed at {url}: {exc}") from exc
    docs = [Document(str(m["id"]), str(m.get("payload", {}).get("title", "")),
        str(m.get("payload", {}).get("text", "")), float(m["score"])) for m in matches]
    return [doc for doc in docs if doc.score >= MIN_SCORE][:top_k]
