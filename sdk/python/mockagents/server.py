"""MockAgentServer — manages the MockAgents Go binary as a subprocess."""

from __future__ import annotations

import os
import shutil
import signal
import socket
import subprocess
import sys
import time
from typing import Any, Optional

import yaml

from .client import MockAgentClient
from .types import ConfigError, ServerError


class MockAgentServer:
    """Manages the lifecycle of a MockAgents server subprocess.

    The server runs the Go binary as a child process with automatic port
    selection and health check polling.

    Args:
        agents_dir: Directory containing agent YAML definitions.
        port: Server port. 0 means auto-select a free port.
        binary_path: Path to the mockagents binary. Auto-detected if None.
        log_level: Server log level (debug, info, warn, error).
        config_path: Path to .mockagents.yaml project config.

    Example:
        with MockAgentServer(agents_dir="./agents") as server:
            client = server.client()
            resp = client.chat([{"role": "user", "content": "hello"}])
    """

    def __init__(
        self,
        agents_dir: str = "./agents",
        port: int = 0,
        binary_path: Optional[str] = None,
        log_level: str = "warn",
        config_path: Optional[str] = None,
    ):
        self.agents_dir = os.path.abspath(agents_dir)
        self.port = port if port != 0 else self._find_free_port()
        self.binary_path = binary_path or self._find_binary()
        self.log_level = log_level
        self.config_path = config_path
        self._process: Optional[subprocess.Popen[bytes]] = None
        self._logs: list[str] = []

    @classmethod
    def from_config(
        cls,
        config_path: str | list[str],
        port: int = 0,
        binary_path: Optional[str] = None,
        log_level: str = "warn",
    ) -> MockAgentServer:
        """Create a MockAgentServer from YAML configuration file(s).

        Args:
            config_path: Path to agent YAML file(s). Can be a single path
                or a list of paths.
            port: Server port (0 for auto-select).
            binary_path: Path to mockagents binary.
            log_level: Server log level.

        Returns:
            Configured MockAgentServer instance.

        Raises:
            ConfigError: If any YAML file is invalid.
            FileNotFoundError: If a file does not exist.
        """
        if isinstance(config_path, str):
            config_path = [config_path]

        # Validate all config files exist and are valid YAML.
        for path in config_path:
            abs_path = os.path.abspath(path)
            if not os.path.exists(abs_path):
                raise FileNotFoundError(f"Agent config not found: {abs_path}")
            try:
                with open(abs_path, "r") as f:
                    doc = yaml.safe_load(f)
                if not isinstance(doc, dict):
                    raise ConfigError(f"Invalid agent config (not a YAML mapping): {abs_path}")
                if doc.get("apiVersion") != "mockagents/v1":
                    raise ConfigError(
                        f"Invalid apiVersion in {abs_path}: expected 'mockagents/v1'"
                    )
            except yaml.YAMLError as e:
                raise ConfigError(f"YAML parse error in {abs_path}: {e}") from e

        # Determine agents directory from the config file paths.
        agents_dir = os.path.dirname(os.path.abspath(config_path[0]))

        return cls(
            agents_dir=agents_dir,
            port=port,
            binary_path=binary_path,
            log_level=log_level,
        )

    def start(self, timeout: float = 10.0) -> None:
        """Start the MockAgents server subprocess.

        Args:
            timeout: Maximum seconds to wait for the server to be ready.

        Raises:
            ServerError: If the server fails to start.
            TimeoutError: If the server doesn't respond within timeout.
        """
        if self._process is not None:
            raise ServerError("Server is already running")

        cmd = [
            self.binary_path,
            "start",
            "--port", str(self.port),
            "--log-level", self.log_level,
            "--agents-dir", self.agents_dir,
        ]

        try:
            self._process = subprocess.Popen(
                cmd,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=os.environ.copy(),
            )
        except OSError as e:
            raise ServerError(f"Failed to start server: {e}") from e

        self._wait_for_ready(timeout)

    def stop(self) -> None:
        """Stop the MockAgents server subprocess."""
        if self._process is None:
            return

        # Capture remaining output.
        try:
            if self._process.stderr:
                stderr = self._process.stderr.read()
                if stderr:
                    self._logs.append(stderr.decode("utf-8", errors="replace"))
        except Exception:
            pass

        # Send SIGTERM (or terminate on Windows).
        try:
            if sys.platform == "win32":
                self._process.terminate()
            else:
                self._process.send_signal(signal.SIGTERM)
            self._process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self._process.kill()
            self._process.wait(timeout=2)
        finally:
            self._process = None

    def client(self) -> MockAgentClient:
        """Create a MockAgentClient connected to this server.

        Returns:
            Configured MockAgentClient instance.
        """
        return MockAgentClient(base_url=self.url)

    @property
    def url(self) -> str:
        """The server's base URL."""
        return f"http://localhost:{self.port}"

    @property
    def is_running(self) -> bool:
        """Whether the server process is running."""
        return self._process is not None and self._process.poll() is None

    @property
    def logs(self) -> list[str]:
        """Captured server log output."""
        return self._logs

    def __enter__(self) -> MockAgentServer:
        self.start()
        return self

    def __exit__(self, *args: Any) -> None:
        self.stop()

    # --- Private Helpers ---

    def _wait_for_ready(self, timeout: float) -> None:
        """Poll the health endpoint until the server is ready."""
        import requests as req

        deadline = time.monotonic() + timeout
        url = f"{self.url}/api/v1/health"
        last_error: Optional[Exception] = None

        while time.monotonic() < deadline:
            # Check if process died.
            if self._process and self._process.poll() is not None:
                stderr = ""
                if self._process.stderr:
                    stderr = self._process.stderr.read().decode("utf-8", errors="replace")
                raise ServerError(
                    f"Server process exited with code {self._process.returncode}: {stderr}"
                )

            try:
                resp = req.get(url, timeout=1)
                if resp.status_code == 200:
                    return
            except req.ConnectionError as e:
                last_error = e
            except req.Timeout as e:
                last_error = e

            time.sleep(0.1)

        raise TimeoutError(
            f"Server not ready after {timeout}s on port {self.port}: {last_error}"
        )

    @staticmethod
    def _find_free_port() -> int:
        """Find an available TCP port."""
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.bind(("127.0.0.1", 0))
            return s.getsockname()[1]

    @staticmethod
    def _find_binary() -> str:
        """Locate the mockagents binary on PATH."""
        binary = shutil.which("mockagents")
        if binary:
            return binary
        # Check common locations.
        for candidate in ["./mockagents", "./mockagents.exe", "mockagents.exe"]:
            if os.path.isfile(candidate):
                return os.path.abspath(candidate)
        raise FileNotFoundError(
            "mockagents binary not found. Install it or pass binary_path explicitly."
        )
