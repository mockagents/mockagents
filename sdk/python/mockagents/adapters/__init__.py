"""Framework adapters that point LangChain, LangGraph and CrewAI at a
running MockAgents server.

Each adapter lazy-imports its framework so installing mockagents does not
drag in LangChain or CrewAI. A helpful ImportError is raised on first use
if the optional dependency is missing.
"""

from .crewai import mock_llm as crewai_mock_llm
from .langchain import chat_anthropic, chat_openai, patched_env

__all__ = [
    "chat_openai",
    "chat_anthropic",
    "patched_env",
    "crewai_mock_llm",
]
