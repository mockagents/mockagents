"""Fluent assertion library for testing MockAgents responses."""

from __future__ import annotations

from typing import Any, Optional, Union

from .scenario import ScenarioResult
from .types import ChatResponse


def expect(value: Any) -> Union[ResponseExpect, ValueExpect]:
    """Entry point for fluent assertions.

    Args:
        value: A ChatResponse, ScenarioResult, or any comparable value.

    Returns:
        An assertion wrapper appropriate for the value type.

    Example:
        expect(response).to_have_response_containing("hello")
        expect(result).to_have_tool_call("search", {"query": "test"})
        expect(result.latency_ms).to_be_less_than(100)
    """
    if isinstance(value, ScenarioResult):
        return ResponseExpect(value)
    if isinstance(value, ChatResponse):
        return ResponseExpect(ScenarioResult(
            scenario=None,  # type: ignore[arg-type]
            interactions=[_interaction_from_response(value)],
        ))
    return ValueExpect(value)


class ResponseExpect:
    """Fluent assertion wrapper for ChatResponse and ScenarioResult objects."""

    def __init__(self, result: ScenarioResult):
        self._result = result

    def to_have_response_containing(self, text: str) -> ResponseExpect:
        """Assert that any response contains the given substring.

        Raises:
            AssertionError: If no response contains the text.
        """
        for interaction in self._result.interactions:
            if text in interaction.response.content:
                return self
        contents = [i.response.content for i in self._result.interactions]
        raise AssertionError(
            f"Expected response containing {text!r}, "
            f"but none of the {len(contents)} response(s) contained it. "
            f"Got: {contents}"
        )

    def to_have_tool_call(
        self,
        name: str,
        arguments: Optional[dict[str, Any]] = None,
    ) -> ResponseExpect:
        """Assert that any response contains a tool call with the given name.

        Args:
            name: Expected tool function name.
            arguments: Expected arguments (partial match — all specified
                keys must be present with matching values).

        Raises:
            AssertionError: If no matching tool call is found.
        """
        all_calls = self._result.tool_calls
        for tc in all_calls:
            if tc.name != name:
                continue
            if arguments is None:
                return self
            # Partial argument match.
            if all(
                tc.arguments.get(k) == v for k, v in arguments.items()
            ):
                return self

        call_names = [tc.name for tc in all_calls]
        msg = f"Expected tool call {name!r}"
        if arguments:
            msg += f" with arguments {arguments!r}"
        msg += f", but got tool calls: {call_names}"
        if all_calls:
            msg += f" (arguments: {[tc.arguments for tc in all_calls]})"
        raise AssertionError(msg)

    def to_have_tool_error(self, code: str) -> ResponseExpect:
        """Assert that any response contains a tool error with the given code.

        Raises:
            AssertionError: If no matching tool error is found.
        """
        for interaction in self._result.interactions:
            raw = interaction.response.raw
            tool_results = raw.get("tool_results", [])
            for tr in tool_results:
                if isinstance(tr, dict) and tr.get("is_error"):
                    error = tr.get("error", {})
                    if error.get("code") == code:
                        return self
        raise AssertionError(
            f"Expected tool error with code {code!r}, but none found."
        )

    def to_have_status(self, status_code: int) -> ResponseExpect:
        """Assert that any interaction has the given HTTP status code.

        Raises:
            AssertionError: If no interaction has the expected status.
        """
        for interaction in self._result.interactions:
            if interaction.response.status_code == status_code:
                return self
        statuses = [i.response.status_code for i in self._result.interactions]
        raise AssertionError(
            f"Expected status code {status_code}, but got: {statuses}"
        )

    def to_have_finish_reason(self, reason: str) -> ResponseExpect:
        """Assert that any response has the given finish reason.

        Raises:
            AssertionError: If no response has the expected finish reason.
        """
        for interaction in self._result.interactions:
            if interaction.response.finish_reason == reason:
                return self
        reasons = [i.response.finish_reason for i in self._result.interactions]
        raise AssertionError(
            f"Expected finish reason {reason!r}, but got: {reasons}"
        )


class ValueExpect:
    """Fluent assertion wrapper for comparable values (numbers, strings)."""

    def __init__(self, value: Any):
        self._value = value

    def to_be_less_than(self, n: Any) -> ValueExpect:
        """Assert that the value is less than n.

        Raises:
            AssertionError: If value >= n.
        """
        if not (self._value < n):
            raise AssertionError(f"Expected {self._value} to be less than {n}")
        return self

    def to_be_greater_than(self, n: Any) -> ValueExpect:
        """Assert that the value is greater than n.

        Raises:
            AssertionError: If value <= n.
        """
        if not (self._value > n):
            raise AssertionError(f"Expected {self._value} to be greater than {n}")
        return self

    def to_equal(self, expected: Any) -> ValueExpect:
        """Assert that the value equals the expected value.

        Raises:
            AssertionError: If value != expected.
        """
        if self._value != expected:
            raise AssertionError(f"Expected {self._value!r} to equal {expected!r}")
        return self

    def to_contain(self, substring: str) -> ValueExpect:
        """Assert that the string value contains the substring.

        Raises:
            AssertionError: If substring not in value.
        """
        if substring not in str(self._value):
            raise AssertionError(
                f"Expected {self._value!r} to contain {substring!r}"
            )
        return self


def _interaction_from_response(response: ChatResponse) -> Any:
    """Wrap a ChatResponse in an Interaction for ResponseExpect."""
    from .types import Interaction
    return Interaction(request={}, response=response)
