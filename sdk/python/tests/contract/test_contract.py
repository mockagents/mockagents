"""Cross-SDK contract runner (Python).

Loads the shared case file ``sdk/contract/cases.json``, executes each case
through the SDK's public client + assertion API against a running server, and
checks that every assertion's verdict (pass / fail / error) equals the verdict
the case file expects. The TypeScript and Go runners read the same file, so the
three SDKs reach identical verdicts by construction.

Skipped unless ``MOCKAGENTS_CONTRACT_URL`` points at a server started with
``--agents-dir sdk/contract/agents``. With ``MOCKAGENTS_CONTRACT_TENANT_KEY``
set (the multi-tenant leg) only the cases carrying an ``auth`` field run;
without it only the cases that do not. See ``sdk/contract/README.md``.
"""

from __future__ import annotations

import json
import os
import uuid
from pathlib import Path
from typing import Any, Callable, Optional

import pytest

from mockagents import (
    ChatResponse,
    Interaction,
    MockAgentClient,
    Scenario,
    ScenarioResult,
    expect,
    run_scenario,
)

SDK = "python"
CASES_PATH = Path(__file__).resolve().parents[3] / "contract" / "cases.json"
BASE_URL = os.environ.get("MOCKAGENTS_CONTRACT_URL", "").strip()
TENANT_KEY = os.environ.get("MOCKAGENTS_CONTRACT_TENANT_KEY", "").strip()

_SUITE: dict[str, Any] = json.loads(CASES_PATH.read_text(encoding="utf-8"))

pytestmark = pytest.mark.skipif(
    not BASE_URL, reason="MOCKAGENTS_CONTRACT_URL is not set (cross-SDK contract suite)"
)


def _divergence(case: dict[str, Any]) -> Optional[str]:
    """The known_divergence note that applies to this SDK, if any.

    The field is ``"<sdk>: <one line>"``; several SDKs may be listed as an
    array of such strings.
    """
    raw = case.get("known_divergence")
    if not raw:
        return None
    notes = raw if isinstance(raw, list) else [raw]
    for note in notes:
        sdk, _, reason = str(note).partition(":")
        if sdk.strip().lower() == SDK:
            return reason.strip()
    return None


def _client(case: dict[str, Any]) -> MockAgentClient:
    auth = case.get("auth", "none")
    if auth == "tenant":
        return MockAgentClient(BASE_URL, api_key=TENANT_KEY)
    if auth == "wrong":
        return MockAgentClient(BASE_URL, api_key=_SUITE["wrong_api_key"])
    return MockAgentClient(BASE_URL)


def _run_plain(client: MockAgentClient, case: dict[str, Any], model: str) -> ScenarioResult:
    scenario = Scenario(
        name=case["name"],
        steps=[{"role": "user", "content": step} for step in case["steps"]],
        protocol=case["protocol"],
        model=model,
        session_id=f"contract-{SDK}-{uuid.uuid4().hex[:12]}",
    )
    return run_scenario(client, scenario)


def _run_streamed(client: MockAgentClient, case: dict[str, Any], model: str) -> ScenarioResult:
    """Stream every user step through ``iter_stream`` and fold each turn's
    chunks into a ChatResponse, so the ordinary matchers can read it."""
    session_id = f"contract-{SDK}-{uuid.uuid4().hex[:12]}"
    conversation: list[dict[str, Any]] = []
    result = ScenarioResult(scenario=None)  # type: ignore[arg-type]
    for step in case["steps"]:
        conversation.append({"role": "user", "content": step})
        text: list[str] = []
        finish_reason = ""
        finished = False
        for chunk in client.iter_stream(
            list(conversation), protocol=case["protocol"], model=model, session_id=session_id
        ):
            text.append(chunk.text)
            if chunk.finished:
                finish_reason = chunk.finish_reason
                finished = True
        if not finished:
            raise RuntimeError(f"stream for step {step!r} ended without a finished chunk")
        response = ChatResponse(content="".join(text), model=model, finish_reason=finish_reason)
        result.interactions.append(
            Interaction(request={"role": "user", "content": step}, response=response)
        )
        conversation.append({"role": "assistant", "content": response.content})
    return result


def _check(kind: str, args: dict[str, Any], result: ScenarioResult, nonstream: Callable[[], str]) -> None:
    """Run one assertion; raises AssertionError when it fails."""
    if kind == "response_contains":
        expect(result).to_have_response_containing(args["text"])
    elif kind == "tool_call":
        expect(result).to_have_tool_call(args["name"], args.get("arguments"))
    elif kind == "tool_call_count":
        expect(result).to_have_tool_call_count(args["count"], name=args.get("name"))
    elif kind == "tool_call_sequence":
        expect(result).to_have_tool_call_sequence(args["names"])
    elif kind == "finish_reason":
        expect(result).to_have_finish_reason(args["reason"])
    elif kind == "status_code":
        expect(result).to_have_status(args["code"])
    elif kind == "stream_text_matches_nonstream":
        streamed = result.interactions[-1].response.content
        plain = nonstream()
        if streamed != plain:
            raise AssertionError(f"streamed text {streamed!r} != non-streamed text {plain!r}")
    else:
        raise ValueError(f"unknown assertion kind {kind!r}")


def _selected_cases() -> list[Any]:
    multi_tenant = bool(TENANT_KEY)
    params = []
    for case in _SUITE["cases"]:
        marks = []
        if ("auth" in case) != multi_tenant:
            leg = "multi-tenant" if "auth" in case else "single-tenant"
            marks.append(pytest.mark.skip(reason=f"{leg} case (set/unset MOCKAGENTS_CONTRACT_TENANT_KEY)"))
        note = _divergence(case)
        if note:
            marks.append(pytest.mark.skip(reason=f"known divergence: {note}"))
        params.append(pytest.param(case, id=case["name"], marks=marks))
    return params


@pytest.mark.parametrize("case", _selected_cases())
def test_contract_case(case: dict[str, Any]) -> None:
    model = case.get("model") or _SUITE["default_model"]
    client = _client(case)
    try:
        error: Optional[BaseException] = None
        result: Optional[ScenarioResult] = None
        try:
            if case.get("stream"):
                result = _run_streamed(client, case, model)
            else:
                result = _run_plain(client, case, model)
        except Exception as exc:  # the SDK raised: every verdict is "error"
            error = exc

        cache: dict[str, str] = {}

        def nonstream() -> str:
            if "text" not in cache:
                with _client(case) as plain_client:
                    cache["text"] = _run_plain(plain_client, case, model).interactions[-1].response.content
            return cache["text"]

        mismatches = []
        for i, assertion in enumerate(case["assertions"]):
            if error is not None:
                verdict, detail = "error", repr(error)
            else:
                try:
                    assert result is not None
                    _check(assertion["kind"], assertion.get("args") or {}, result, nonstream)
                    verdict, detail = "pass", ""
                except AssertionError as exc:
                    verdict, detail = "fail", str(exc)
            if verdict != assertion["expect"]:
                mismatches.append(
                    f"#{i} {assertion['kind']} {json.dumps(assertion.get('args'), ensure_ascii=False)}: "
                    f"expected {assertion['expect']}, got {verdict} ({detail})"
                )
        assert not mismatches, f"{case['name']} [{SDK}]:\n  " + "\n  ".join(mismatches)
    finally:
        client.close()
