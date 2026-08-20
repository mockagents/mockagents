# Framework recipes — runnable, not prose

One agent fixture, four frameworks, fifteen tests. Every file here is executed
in CI, so a recipe that stops working is a red build rather than a stale
paragraph in a guide.

| Framework | Recipe | Tests |
| --- | --- | --- |
| OpenAI Agents SDK | [`python/test_openai_agents.py`](python/test_openai_agents.py) | 2 |
| LangGraph / LangChain | [`python/test_langgraph.py`](python/test_langgraph.py) | 5 |
| CrewAI | [`python/test_crewai.py`](python/test_crewai.py) | 5 |
| Vercel AI SDK | [`typescript/vercel-ai.test.ts`](typescript/vercel-ai.test.ts) | 3 |

They all point at the same fixture,
[`agents/support-agent.yaml`](agents/support-agent.yaml): a two-turn
tool-calling agent that returns a `lookup_order` call on turn 1 and a final
answer on turn 2.

## Run them

You need the `mockagents` binary (`make build` at the repo root) and, because
the Python frameworks have conflicting pins, **one virtualenv per framework**:

```bash
# from examples/frameworks/python
python -m venv .venv-langgraph && . .venv-langgraph/bin/activate
pip install -r requirements-langgraph.txt
pip install -e ../../../sdk/python          # the SDK isn't on PyPI yet
MOCKAGENTS_BIN=../../../mockagents pytest test_langgraph.py
```

Swap `langgraph` for `openai-agents` or `crewai` for the other two. TypeScript:

```bash
cd examples/frameworks/typescript
npm install
MOCKAGENTS_BIN=../../../mockagents npm test
```

> **CrewAI and the OpenAI Agents SDK cannot share an environment.** CrewAI pins
> `openai<3`; the Agents SDK requires `openai>=3`. Installing the second one
> silently downgrades the first. This is not a MockAgents constraint — it is
> just true today, and it is why the CI job builds a venv per framework.

## What each recipe is actually teaching

The redirect is one line in every framework. What differs — and what these
files exist to pin down — is the thing each one gets wrong the first time.

**OpenAI Agents SDK.** The SDK builds its own client, so patching
`OPENAI_BASE_URL` redirects nothing; you must call `set_default_openai_client`.
And `set_tracing_disabled(True)` is not optional: without it the SDK posts a
trace to the *real* OpenAI backend on every run, so your offline suite quietly
phones home.

**LangGraph / LangChain.** A chat model instance takes `base_url` directly. An
agent built from a provider string (`"openai:gpt-4o"`) does not — it constructs
its own client, and `patched_env` is the only seam. The recipe shows both, and
the difference between them that matters: only the instance form can carry an
`X-Session-Id` header, which is what makes turn-gated scenarios advance to turn
2. Without it every request is a fresh session, `turn_number` is always 1, and
you get turn 1's answer forever — though the loop still terminates, because the
engine drops a tool call whose result the model has already been given.

**CrewAI.** Everything goes through LiteLLM, which needs the `openai/` model
prefix or it auto-detects a provider from the model name and routes elsewhere.
Note where the prefix lands: you pass `openai/gpt-4o`, and CrewAI 1.x splits it
into `provider="openai"` + `model="gpt-4o"`. Also, `LLM.call` returns a
**string** for a content answer and a **list of tool calls** when the model
asked for a tool — code that assumes a string breaks the first time routing
works, which the mock lets you cover deterministically.

**Vercel AI SDK.** The provider factory takes `baseURL`, so there is no env var
in the picture at all. The recipe covers `generateText`, tool calls (asserting
the exact arguments the mock chose), and `streamText` — checking that the text
arrives as *multiple* deltas over real SSE, which is where a streaming parser
bug shows up.

## The fixture's turn gating

Scenario matching keys on the latest **user** message, and that does not change
while an agent works its tool loop — a tool result comes back with role `tool`,
not `user`. So a scenario that returns a tool call would return the *same* tool
call forever.

Two things prevent that, and the recipes exercise both:

1. **Turn gating** (`turn_number: 1` → tool call, `turn_number: 2` → answer),
   which needs a stable `X-Session-Id` per conversation to advance.
2. **The engine's convergence guard**, which drops a tool call identical to one
   whose result the model has already been given, and answers with content only.
   This is the backstop when no session id is available.

## See also

- [Testing with Agent Frameworks](../../site/docs/guides/framework-testing.md) — the prose version, including the Claude Agent SDK and Google ADK
- [Testing AI Agents](../../site/docs/guides/testing-agents.md) — tool-call and MCP cookbooks
- [`demo/rag-agent`](../../demo/rag-agent) — a complete app, not just a recipe
