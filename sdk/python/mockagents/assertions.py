"""Fluent assertion library for testing MockAgents responses.

Trajectory assertions
---------------------
``to_have_tool_call_sequence`` and ``to_have_tool_call_count`` mirror the
``tool_call_sequence`` / ``tool_call_count`` assertions of ``kind: TestSuite``
YAML, so the same check reads the same way whether you run it through
``mockagents test`` or from your own test file. Both read the **aggregate**
across every turn of a scenario, matching the Go runner: a multi-turn
conversation's trajectory is every tool call it made, in order — not just the
final turn's.

``to_have_node_sequence`` applies the same exact-order semantics to a typed
``PipelineResult`` returned by ``MockAgentClient.run_pipeline``.

Outcome assertions
------------------
``to_have_response_containing``, ``to_have_status`` and
``to_have_finish_reason`` read the **final** turn, like the YAML runner and
the TypeScript and Go SDKs: a conversation that recovers from an error passes,
one that ends in an error fails. ``to_have_any_response_containing`` checks
every turn when that is what a test means.
"""

from __future__ import annotations

from typing import Any, Optional, Union

from .scenario import ScenarioResult
from .types import ChatResponse, PipelineResult

# Distinguishes "argument absent" from "argument is None" (review K-03).
_MISSING = object()


def expect(value: Any) -> Union[ResponseExpect, PipelineExpect, ValueExpect]:
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
    if isinstance(value, PipelineResult):
        return PipelineExpect(value)
    return ValueExpect(value)


class PipelineExpect:
    """Fluent assertions for an HTTP pipeline result."""

    def __init__(self, result: PipelineResult):
        self._result = result

    def to_have_node_sequence(self, node_ids: list[str]) -> PipelineExpect:
        """Assert exact ordered node ids; this is not a subsequence check."""
        expected = list(node_ids)
        got = self._result.node_sequence
        if got != expected:
            raise AssertionError(f"Expected node sequence {expected}, but got {got}")
        return self


class ResponseExpect:
    """Fluent assertion wrapper for ChatResponse and ScenarioResult objects."""

    def __init__(self, result: ScenarioResult):
        self._result = result

    def _final(self) -> ChatResponse:
        if not self._result.interactions:
            raise AssertionError("Expected at least one response, but the result has none")
        return self._result.interactions[-1].response

    def to_have_response_containing(self, text: str) -> ResponseExpect:
        """Assert that the final response contains the given substring.

        Raises:
            AssertionError: If the final response does not contain the text.
        """
        content = self._final().content
        if text not in content:
            raise AssertionError(
                f"Expected the final response to contain {text!r}, but got {content!r}"
            )
        return self

    def to_have_any_response_containing(self, text: str) -> ResponseExpect:
        """Assert that at least one response, at any turn, contains the substring.

        Raises:
            AssertionError: If no response contains the text.
        """
        for interaction in self._result.interactions:
            if text in interaction.response.content:
                return self
        contents = [i.response.content for i in self._result.interactions]
        raise AssertionError(
            f"Expected a response containing {text!r}, "
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
            arguments: Expected arguments (partial match: every given key must
                be present with an equal value, so ``{"x": None}`` requires an
                explicit ``null`` and does not match an absent ``x``).

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
                tc.arguments.get(k, _MISSING) == v for k, v in arguments.items()
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

    def to_have_tool_call_sequence(self, names: list[str]) -> ResponseExpect:
        """Assert the exact ordered sequence of tool-call names.

        Reads the aggregate across every interaction, in invocation order, and
        compares for **full equality** — not a subsequence. This is the
        ``tool_call_sequence`` assertion of ``kind: TestSuite`` YAML, so a check
        that passes here passes there.

        Args:
            names: Expected tool names, in the order they should be called.

        Raises:
            AssertionError: If the observed sequence differs in content or order.

        Example:
            expect(result).to_have_tool_call_sequence(["search", "summarize"])
        """
        expected = list(names)
        got = [tc.name for tc in self._result.tool_calls]
        if got != expected:
            raise AssertionError(
                f"Expected tool call sequence {expected}, but got {got}"
            )
        return self

    def to_have_tool_call_count(
        self,
        count: int,
        name: Optional[str] = None,
    ) -> ResponseExpect:
        """Assert how many tool calls were made.

        With no ``name`` this is the ``tool_call_count`` assertion of
        ``kind: TestSuite`` YAML: the **total** across every interaction, compared
        for exact equality.

        Passing ``name`` narrows the count to one tool. That is an SDK-only
        convenience with no YAML equivalent — reach for the unnamed form when you
        want a check that transfers to a YAML suite.

        Args:
            count: Expected number of tool calls.
            name: Count only calls to this tool. ``None`` counts every call.

        Raises:
            AssertionError: If the observed count differs.

        Example:
            expect(result).to_have_tool_call_count(3)
            expect(result).to_have_tool_call_count(1, name="search")
        """
        calls = self._result.tool_calls
        if name is None:
            got = len(calls)
            if got != count:
                raise AssertionError(
                    f"Expected {count} tool call(s), but got {got}: "
                    f"{[tc.name for tc in calls]}"
                )
        else:
            got = sum(1 for tc in calls if tc.name == name)
            if got != count:
                raise AssertionError(
                    f"Expected {count} call(s) to tool {name!r}, but got {got}: "
                    f"{[tc.name for tc in calls]}"
                )
        return self

    def to_have_malformed_tool_arguments(self, name: Optional[str] = None) -> ResponseExpect:
        """Assert that a tool call (optionally to ``name``) carried arguments
        that are not a JSON object, as a malformed-arguments fixture produces.

        Raises:
            AssertionError: If every matching tool call had valid arguments.
        """
        calls = [tc for tc in self._result.tool_calls if name is None or tc.name == name]
        if any(not tc.arguments_valid for tc in calls):
            return self
        target = f"to {name!r} " if name else ""
        raise AssertionError(
            f"Expected a tool call {target}with malformed arguments, but got: "
            f"{[(tc.name, tc.raw_arguments) for tc in calls]}"
        )

    def to_have_tool_error(self, code: str, tool: Optional[str] = None) -> ResponseExpect:
        """Assert that a simulated tool call, at any turn, resolved to an
        error fixture with the given code (``tool_error`` in TestSuite YAML).

        The server reports these in the ``X-Mockagents-Tool-Errors`` header,
        which the client reads into ``ChatResponse.tool_errors``.

        Args:
            code: Expected error code, e.g. ``"NOT_FOUND"``.
            tool: Only match errors from this tool.

        Raises:
            AssertionError: If no matching tool error is found.
        """
        seen = []
        for interaction in self._result.interactions:
            for err in interaction.response.tool_errors:
                if err.code == code and (tool is None or err.tool == tool):
                    return self
                seen.append(f"{err.tool}={err.code}")
        source = f" from tool {tool!r}" if tool else ""
        raise AssertionError(f"Expected tool error with code {code!r}{source}, but got: {seen}")

    def to_have_status(self, status_code: int) -> ResponseExpect:
        """Assert that the final interaction has the given HTTP status code.

        The client raises ``requests.HTTPError`` on a non-2xx status, so this
        only ever sees successful statuses.

        Raises:
            AssertionError: If the final status differs.
        """
        got = self._final().status_code
        if got != status_code:
            raise AssertionError(f"Expected final status code {status_code}, but got {got}")
        return self

    def to_have_finish_reason(self, reason: str) -> ResponseExpect:
        """Assert that the final response has the given finish reason.

        Raises:
            AssertionError: If the final finish reason differs.
        """
        got = self._final().finish_reason
        if got != reason:
            raise AssertionError(f"Expected final finish reason {reason!r}, but got {got!r}")
        return self


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

    def to_contain(self, item: Any) -> ValueExpect:
        """Assert that a string contains a substring, or a collection (list,
        tuple, set, dict keys) contains an item.

        Raises:
            AssertionError: If the item is not contained.
            TypeError: If the value is neither a string nor a collection. The
                value is no longer passed through ``str()`` first, which made
                ``expect(None).to_contain("None")`` pass.
        """
        if isinstance(self._value, str):
            if not isinstance(item, str):
                raise TypeError(f"to_contain on a string needs a string, got {item!r}")
        elif not isinstance(self._value, (list, tuple, set, frozenset, dict)):
            raise TypeError(
                f"to_contain needs a string or a collection, got {type(self._value).__name__}"
            )
        if item not in self._value:
            raise AssertionError(f"Expected {self._value!r} to contain {item!r}")
        return self


def _interaction_from_response(response: ChatResponse) -> Any:
    """Wrap a ChatResponse in an Interaction for ResponseExpect."""
    from .types import Interaction
    return Interaction(request={}, response=response)
