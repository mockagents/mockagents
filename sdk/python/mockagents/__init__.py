"""MockAgents Python SDK — simulate, test, and validate AI agent integrations."""

from . import adapters
from ._binary import BinaryNotFoundError, ensure_binary, find_binary
from .assertions import expect
from .client import MockAgentClient
from .mcp import McpClient, McpEvent, McpEventStream
from .scenario import Scenario, ScenarioResult, run_scenario
from .server import MockAgentServer
from .types import (
    ChatResponse,
    ConfigError,
    Interaction,
    ServerError,
    StreamChunk,
    ToolCall,
    TokenUsage,
)

__version__ = "0.4.0"

__all__ = [
    "MockAgentServer",
    "MockAgentClient",
    "McpClient",
    "McpEvent",
    "McpEventStream",
    "Scenario",
    "ScenarioResult",
    "run_scenario",
    "expect",
    "ChatResponse",
    "StreamChunk",
    "ToolCall",
    "TokenUsage",
    "Interaction",
    "ConfigError",
    "ServerError",
    "BinaryNotFoundError",
    "ensure_binary",
    "find_binary",
    "adapters",
]
