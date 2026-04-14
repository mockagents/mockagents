"""MockAgents Python SDK — simulate, test, and validate AI agent integrations."""

from . import adapters
from .assertions import expect
from .client import MockAgentClient
from .scenario import Scenario, ScenarioResult, run_scenario
from .server import MockAgentServer
from .types import ChatResponse, ConfigError, Interaction, ServerError, ToolCall, TokenUsage

__version__ = "0.1.0"

__all__ = [
    "MockAgentServer",
    "MockAgentClient",
    "Scenario",
    "ScenarioResult",
    "run_scenario",
    "expect",
    "ChatResponse",
    "ToolCall",
    "TokenUsage",
    "Interaction",
    "ConfigError",
    "ServerError",
    "adapters",
]
