# Testing with Agent Frameworks

The big agent frameworks — OpenAI Agents SDK, Anthropic's Claude Agent SDK,
Google ADK, CrewAI, LangChain/LangGraph — each wrap a provider's wire API. None
of them ship an official "mock the model" story, so teams end up hitting the real
API in CI (slow, costly, flaky) or hand-rolling brittle `unittest.mock` patches
of the SDK internals.

MockAgents is the missing layer: it **speaks the provider wire formats**
(OpenAI, Anthropic, Gemini), so the only thing your framework needs is a
different **base URL**. Your agent code — tools, instructions, the run loop —
runs unchanged, fully deterministic, with zero tokens.

This guide gives a copy-pasteable recipe for each framework. The shape is always
the same:

1. **Start the mock** with your fixtures: `mockagents start ./agents`.
2. **Redirect the framework** at it (an env var or a client option — table below).
3. **Run your agent unchanged** and assert on the outcome
   ([§ Asserting on what happened](#asserting-on-what-happened)).

| Framework | How it's redirected at the mock | Wire surface | Runnable demo |
|---|---|---|---|
| [OpenAI Agents SDK](#openai-agents-sdk) | `set_default_openai_client(...)` + `set_default_openai_api(...)` | `/v1/responses` (default) or `/v1/chat/completions` | [`demo/responses-api-agent`](https://github.com/mockagents/mockagents/tree/main/demo/responses-api-agent), [`demo/customer-support-agent`](https://github.com/mockagents/mockagents/tree/main/demo/customer-support-agent) |
| [Claude Agent SDK](#anthropic-claude-agent-sdk) | `ANTHROPIC_BASE_URL` via `ClaudeAgentOptions(env=...)` | `/v1/messages` | [`demo/customer-support-agent-claude`](https://github.com/mockagents/mockagents/tree/main/demo/customer-support-agent-claude) |
| [Google ADK](#google-adk) | `GOOGLE_GEMINI_BASE_URL` (native) or `LiteLlm(api_base=...)` | `/v1beta/models/...` or `/v1/chat/completions` | [`demo/customer-support-agent-google-adk`](https://github.com/mockagents/mockagents/tree/main/demo/customer-support-agent-google-adk) |
| [CrewAI](#crewai) | `crewai.LLM(base_url=..., api_key=...)` (LiteLLM) | `/v1/chat/completions` | — (adapter: `mockagents.adapters.crewai_mock_llm`) |
| [LangChain / LangGraph](#langchain--langgraph) | `base_url=` on the chat model, or `patched_env(...)` | `/v1/chat/completions`, `/v1/messages` | — (adapter: `mockagents.adapters`) |

> **A note on the `/v1` suffix.** OpenAI-compatible clients want the base URL
> *with* `/v1` (`http://localhost:8080/v1`); the Anthropic and Gemini clients
> want the server **root** (`http://localhost:8080`) and append their own path.
> The table and recipes below get this right per framework — it's the single
> most common setup mistake.

---

## OpenAI Agents SDK

The Agents SDK (`openai-agents`) defaults to the **Responses API**
(`/v1/responses`) — which MockAgents implements — and can also be switched to
**Chat Completions** (`/v1/chat/completions`). You point it at the mock by
installing a default `AsyncOpenAI` client and selecting the API:

```python
import os
from openai import AsyncOpenAI
from agents import (
    set_default_openai_client,
    set_default_openai_api,
    set_tracing_disabled,
)

def configure_mockagents() -> AsyncOpenAI:
    client = AsyncOpenAI(
        base_url=os.environ.get("MOCKAGENTS_BASE_URL", "http://localhost:8080/v1"),
        api_key="mock-key",          # any non-empty value in single-tenant mode
        max_retries=5,               # so injected 503s exercise your retry path
    )
    set_default_openai_client(client, use_for_tracing=False)
    set_default_openai_api("responses")    # or "chat_completions"
    set_tracing_disabled(True)             # don't phone home to real OpenAI for traces
    return client
```

`set_tracing_disabled(True)` matters: without it the SDK posts traces to the real
OpenAI backend on every run.

### Terminating the tool-calling loop

MockAgents keys turn/session state off the **`X-Session-Id`** header. Turn-gated
scenarios (`turn_number: 1` → tool call, turn 2 → final answer) need a stable
session id per conversation so the loop terminates deterministically. Install a
client that sends one:

```python
import uuid
session_id = f"conv-{uuid.uuid4()}"
client = AsyncOpenAI(
    base_url="http://localhost:8080/v1",
    api_key="mock-key",
    default_headers={"X-Session-Id": session_id},
)
set_default_openai_client(client, use_for_tracing=False)
set_default_openai_api("responses")
set_tracing_disabled(True)
# ... now Runner.run(agent, "where is my order?") — every turn shares session_id
```

Swapping the *global* default client means conversations must run sequentially.
For concurrent runs, pass the header per run instead:

```python
from agents import RunConfig, ModelSettings
cfg = RunConfig(model_settings=ModelSettings(extra_headers={"X-Session-Id": session_id}))
await Runner.run(agent, "where is my order?", run_config=cfg)
```

> **Two working demos.** [`demo/responses-api-agent`](https://github.com/mockagents/mockagents/tree/main/demo/responses-api-agent)
> runs the SDK on its **default** Responses transport (incl. streaming and
> stateful `previous_response_id`); [`demo/customer-support-agent`](https://github.com/mockagents/mockagents/tree/main/demo/customer-support-agent)
> shows the `chat_completions` path plus chaos, streaming, and multi-tenant
> walkthroughs. Both have a full [`TESTING.md`](https://github.com/mockagents/mockagents/blob/main/demo/customer-support-agent/TESTING.md).

---

## Anthropic Claude Agent SDK

The Claude Agent SDK (`claude-agent-sdk`) doesn't make HTTP calls itself — it
spawns the **`claude` CLI** as a subprocess, and the CLI calls the model API. The
CLI honors `ANTHROPIC_BASE_URL` + `ANTHROPIC_API_KEY`, which you pass through
`ClaudeAgentOptions(env=...)`. The base URL is the server **root** (no `/v1` — the
CLI appends `/v1/messages` itself):

```python
from claude_agent_sdk import ClaudeAgentOptions

options = ClaudeAgentOptions(
    model="claude-3-5-sonnet-latest",
    system_prompt="You are a helpful support agent.",
    mcp_servers={"acme": build_acme_mcp_server()},   # in-process @tool server
    allowed_tools=["mcp__acme__lookup_order", ...],
    permission_mode="bypassPermissions",             # keep the run non-interactive
    setting_sources=[],                              # ignore this machine's settings
    env={
        "ANTHROPIC_BASE_URL": "http://localhost:8080",   # ROOT, not /v1
        "ANTHROPIC_API_KEY": "mock-key",
        "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", # suppress telemetry/title calls
    },
)
```

Two Claude-specific gotchas the demo encodes:

- **Tool names are MCP-namespaced.** In-process SDK tools reach the CLI as
  `mcp__<server>__<tool>`. If you register an MCP server `acme`, your fixture's
  scenario must emit `mcp__acme__lookup_order` (not bare `lookup_order`) as the
  tool call.
- **Match on content, not turns.** The CLI sends no `X-Session-Id`, so the turn
  counter can't disambiguate the loop. Have each tool return a unique marker and
  match the marker in a `*-resolved` scenario (listed *first*) to emit the final
  answer and stop the loop. The [demo fixture](https://github.com/mockagents/mockagents/blob/main/demo/customer-support-agent-claude/mockagents/support-agent.yaml)
  shows the pattern.

If you're running *inside* Claude Code, also blank `CLAUDE_CODE_ENTRYPOINT`,
`CLAUDECODE`, `AI_AGENT`, etc. in `env` so the parent session's context isn't
injected into the user message (the demo's `mock_setup.py` does this).

> **Demo:** [`demo/customer-support-agent-claude`](https://github.com/mockagents/mockagents/tree/main/demo/customer-support-agent-claude)
> — the Claude twin of the OpenAI demo over `/v1/messages`, with the full
> [`TESTING.md`](https://github.com/mockagents/mockagents/blob/main/demo/customer-support-agent-claude/TESTING.md).

---

## Google ADK

The Google Agent Development Kit has **two** ways to reach MockAgents.

### Native Gemini (primary)

ADK builds a `google-genai` client internally; the genai SDK reads
`GOOGLE_GEMINI_BASE_URL`. Set it (plus select the Gemini API over Vertex) **before
the agent runs** — the client is created lazily:

```python
import os

def configure_gemini_env() -> None:
    os.environ["GOOGLE_GENAI_USE_VERTEXAI"] = "0"          # Gemini API, not Vertex
    os.environ["GOOGLE_API_KEY"] = "mock-key"
    os.environ["GOOGLE_GEMINI_BASE_URL"] = "http://localhost:8080"   # ROOT, not /v1
```

ADK then talks to MockAgents' native Gemini endpoint
(`/v1beta/models/<model>:generateContent`) with no change to the agent code. Your
fixtures use the `gemini` protocol and a real Gemini model name (e.g.
`gemini-2.0-flash`) so the cost table and quota route apply.

### LiteLLM bridge (alternative)

ADK's `LiteLlm` wrapper points at MockAgents' OpenAI-compatible surface:

```python
from google.adk.models.lite_llm import LiteLlm

model = LiteLlm(
    model="openai/gpt-4o",                       # openai/ prefix forces the OpenAI route
    api_base="http://localhost:8080/v1",         # WITH /v1
    api_key="mock-key",
)
```

> **Matching note.** ADK is a library (no CLI subprocess), so the genai client
> sends no `X-Session-Id` — native-Gemini fixtures match on **content** (use the
> marker pattern from the Claude section). The LiteLLM bridge sends OpenAI-format
> requests where the tool result is a `role:"tool"` message, so that fixture can
> be `turn_number`-gated with a per-conversation `X-Session-Id` via LiteLLM
> `extra_headers`.
>
> **Demo:** [`demo/customer-support-agent-google-adk`](https://github.com/mockagents/mockagents/tree/main/demo/customer-support-agent-google-adk)
> ships both backends with a full [`TESTING.md`](https://github.com/mockagents/mockagents/blob/main/demo/customer-support-agent-google-adk/TESTING.md).

---

## CrewAI

CrewAI delegates LLM calls to LiteLLM, so any OpenAI-compatible endpoint works by
passing `base_url` + `api_key` to `crewai.LLM`. The Python SDK ships a one-liner
adapter that wraps the boilerplate:

```python
from mockagents.adapters import crewai_mock_llm
from crewai import Agent

# Pass a base URL string or a MockAgentServer instance:
llm = crewai_mock_llm("http://localhost:8080", model="gpt-4o")

agent = Agent(
    role="Support Triage",
    goal="Resolve customer issues",
    backstory="...",
    llm=llm,            # every call now hits MockAgents
)
```

Under the hood the adapter appends `/v1` and prefixes the model with `openai/`
(so `gpt-4o` → `openai/gpt-4o`) to force LiteLLM's OpenAI route onto
`/v1/chat/completions`. To do it by hand without the adapter:

```python
from crewai import LLM
llm = LLM(model="openai/gpt-4o", base_url="http://localhost:8080/v1", api_key="mock-key")
```

---

## LangChain / LangGraph

LangChain chat models take a `base_url`/`api_key` directly. The SDK ships
adapters so you can pass a `MockAgentServer` (or URL) and skip the boilerplate:

```python
from mockagents.adapters import chat_openai, chat_anthropic

llm = chat_openai("http://localhost:8080", model="gpt-4o")
# or the Anthropic surface:
claude = chat_anthropic("http://localhost:8080", model="claude-3-5-sonnet-latest")
```

Equivalently, by hand:

```python
from langchain_openai import ChatOpenAI
llm = ChatOpenAI(model="gpt-4o", base_url="http://localhost:8080/v1", api_key="mock-key")
```

### LangGraph prebuilt agents

Prebuilt agents (and anything that constructs its own chat model from env vars)
won't accept a `base_url` argument. Use the `patched_env` context manager — it
sets `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL` (with the right `/v1` handling) and
restores them on exit:

```python
from mockagents.adapters import patched_env
from langgraph.prebuilt import create_react_agent

with patched_env("http://localhost:8080"):     # patches OPENAI_BASE_URL + ANTHROPIC_BASE_URL
    agent = create_react_agent("openai:gpt-4o", tools=[...])
    result = agent.invoke({"messages": [("user", "where is my order?")]})
```

---

## Asserting on what happened

Pointing the framework at the mock is half the job; the other half is asserting
on the outcome. Three options, smallest-footprint first:

1. **A declarative `TestSuite`** — assert tool calls, arguments, and *which
   scenario matched* with no test code, JUnit output straight into CI. See
   [Testing AI Agents → Option A](testing-agents.md#option-a--a-declarative-testsuite-no-code).

2. **The `mockagents` pytest fixture** (`pip install mockagents`) — spawns the
   server and patches `OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL`, and
   `GOOGLE_GEMINI_BASE_URL` for the test, so your *existing* agent code is
   redirected with zero changes:

   ```python
   def test_order_question_calls_lookup(mockagents):
       # All three provider base-URL env vars now point at the mock.
       from openai import OpenAI
       resp = OpenAI().chat.completions.create(
           model="gpt-4o",
           messages=[{"role": "user", "content": "where is my order?"}],
       )
       assert resp.choices[0].message.tool_calls[0].function.name == "lookup_order"
   ```

   ```console
   $ pytest --mockagents-agents-dir ./agents
   ```

3. **The interaction log** — for ad-hoc inspection, every request is recorded:

   ```bash
   curl -s "http://localhost:8080/api/v1/logs?limit=10"   # scenario + agent per call
   ```

---

## See also

- [Testing AI Agents](testing-agents.md) — the tool-call + MCP cookbooks the
  recipes above build on.
- [Drop-in Recipes](drop-in-recipes.md) — base-URL snippets for the raw SDKs,
  Vercel AI, and LlamaIndex.
- [Python SDK](../sdk/python-sdk.md) — the fixture, launcher, and adapters in
  full.
