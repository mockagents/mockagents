"""The RAG application under test.

Three steps, and every one of them is application logic that can be wrong
independently of how good the model is:

1. **Retrieve** documents for the question.
2. **Abstain** when retrieval came back empty, instead of asking the model to
   answer from nothing.
3. **Verify grounding**: every `[doc-N]` citation in the answer must name a
   document that was actually retrieved. A citation to anything else is a
   fabrication, and shipping it is how a RAG app loses a user's trust.

Step 3 is the guardrail this demo exists to exercise. You cannot get a real
model to fabricate a citation on demand, so in a live-model test suite this
branch is dead code that nobody has ever executed.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field

from openai import OpenAI

from .retrieval import Document, search

#: `[doc-1]`, `[doc-42]`, ...
CITATION_RE = re.compile(r"\[(doc-[0-9a-zA-Z_-]+)\]")

MODEL = "gpt-4o-rag"

ABSTAIN = "I don't have anything in the knowledge base about that."


class UngroundedAnswer(Exception):
    """The model cited documents that retrieval never returned."""

    def __init__(self, answer: str, fabricated: list[str], retrieved: list[str]):
        self.answer = answer
        self.fabricated = fabricated
        self.retrieved = retrieved
        super().__init__(
            f"answer cites {fabricated} but only {retrieved} were retrieved"
        )


@dataclass
class Answer:
    text: str
    documents: list[Document] = field(default_factory=list)
    abstained: bool = False

    @property
    def citations(self) -> list[str]:
        return CITATION_RE.findall(self.text)


def build_prompt(question: str, documents: list[Document]) -> str:
    context = "\n".join(f"[{d.id}] {d.title}: {d.text}" for d in documents)
    return f"Context:\n{context}\n\nQuestion: {question}"


async def answer(question: str, *, client: OpenAI | None = None) -> Answer:
    """Answer `question` from the knowledge base, or abstain."""
    documents = await search(question)

    # Guardrail 1: nothing retrieved -> do not ask the model. Calling it here
    # is what produces a confident answer with no source behind it.
    if not documents:
        return Answer(text=ABSTAIN, documents=[], abstained=True)

    client = client or OpenAI()
    completion = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": build_prompt(question, documents)}],
    )
    result = Answer(text=completion.choices[0].message.content or "", documents=documents)

    # Guardrail 2: every citation must name a retrieved document.
    retrieved_ids = {d.id for d in documents}
    fabricated = sorted(set(result.citations) - retrieved_ids)
    if fabricated:
        raise UngroundedAnswer(result.text, fabricated, sorted(retrieved_ids))

    return result
