"""Tests for mockagents.server — server lifecycle management."""

import os
import tempfile

import pytest
import yaml

from mockagents.server import MockAgentServer
from mockagents.types import ConfigError

DUMMY_BINARY = "nonexistent-binary-for-test"


def write_temp_yaml(content: dict) -> str:
    """Write YAML to a temp file and return its path (file is closed)."""
    fd, path = tempfile.mkstemp(suffix=".yaml")
    with os.fdopen(fd, "w") as f:
        yaml.dump(content, f)
    return path


def test_find_free_port():
    port = MockAgentServer._find_free_port()
    assert isinstance(port, int)
    assert 0 < port < 65536


def test_find_free_port_unique():
    ports = {MockAgentServer._find_free_port() for _ in range(10)}
    assert len(ports) >= 5


def test_server_url_property():
    server = MockAgentServer.__new__(MockAgentServer)
    server.port = 9999
    assert server.url == "http://localhost:9999"


def test_server_is_running_false_initially():
    server = MockAgentServer.__new__(MockAgentServer)
    server._process = None
    assert not server.is_running


def test_server_logs_empty_initially():
    server = MockAgentServer.__new__(MockAgentServer)
    server._logs = []
    assert server.logs == []


def test_from_config_file_not_found():
    with pytest.raises(FileNotFoundError, match="not found"):
        MockAgentServer.from_config("/nonexistent/path.yaml")


def test_from_config_invalid_yaml():
    fd, path = tempfile.mkstemp(suffix=".yaml")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(": invalid: yaml: [")
        with pytest.raises(ConfigError, match="YAML parse error"):
            MockAgentServer.from_config(path, binary_path=DUMMY_BINARY)
    finally:
        os.unlink(path)


def test_from_config_wrong_api_version():
    path = write_temp_yaml({"apiVersion": "wrong/v2", "kind": "Agent"})
    try:
        with pytest.raises(ConfigError, match="Invalid apiVersion"):
            MockAgentServer.from_config(path, binary_path=DUMMY_BINARY)
    finally:
        os.unlink(path)


def test_from_config_not_a_mapping():
    fd, path = tempfile.mkstemp(suffix=".yaml")
    try:
        with os.fdopen(fd, "w") as f:
            f.write("- just\n- a\n- list\n")
        with pytest.raises(ConfigError, match="not a YAML mapping"):
            MockAgentServer.from_config(path, binary_path=DUMMY_BINARY)
    finally:
        os.unlink(path)


def test_from_config_valid_file():
    path = write_temp_yaml({
        "apiVersion": "mockagents/v1",
        "kind": "Agent",
        "metadata": {"name": "test"},
        "spec": {
            "protocol": "openai-chat-completions",
            "behavior": {"scenarios": [{"name": "default", "response": {"content": "hi"}}]},
        },
    })
    try:
        server = MockAgentServer.from_config(path, port=9999, binary_path=DUMMY_BINARY)
        assert server.port == 9999
        assert server.agents_dir == os.path.dirname(os.path.abspath(path))
        assert server.binary_path == DUMMY_BINARY
    finally:
        os.unlink(path)


def test_from_config_multiple_files():
    paths = []
    try:
        for i in range(2):
            path = write_temp_yaml({
                "apiVersion": "mockagents/v1",
                "kind": "Agent",
                "metadata": {"name": f"agent-{i}"},
                "spec": {
                    "protocol": "openai-chat-completions",
                    "behavior": {"scenarios": [{"name": "default", "response": {"content": "hi"}}]},
                },
            })
            paths.append(path)

        server = MockAgentServer.from_config(paths, binary_path=DUMMY_BINARY)
        assert server.port > 0
    finally:
        for p in paths:
            os.unlink(p)


def test_find_binary_not_found():
    original_path = os.environ.get("PATH", "")
    try:
        os.environ["PATH"] = ""
        with pytest.raises(FileNotFoundError, match="mockagents binary not found"):
            MockAgentServer._find_binary()
    finally:
        os.environ["PATH"] = original_path


def test_stop_when_not_running():
    server = MockAgentServer.__new__(MockAgentServer)
    server._process = None
    server._logs = []
    server.stop()  # Should not raise.


def test_client_returns_mock_agent_client():
    server = MockAgentServer.__new__(MockAgentServer)
    server.port = 8080
    client = server.client()
    assert client.base_url == "http://localhost:8080"
    client.close()
