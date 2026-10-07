# Agents and tools

Agents are configured in [`config/playground.json`](../config/playground.json). Each agent's model
names select its mockagents fixture in [`mockagents/`](../mockagents). This page lists both, plus the
tools and every configuration field.

- [Agent catalog](#agent-catalog)
- [Tools](#tools)
- [Configuration reference](#configuration-reference)
- [Routing rules](#routing-rules)
- [How an agent maps to a fixture](#how-an-agent-maps-to-a-fixture)

## Agent catalog

| Agent | Role → default tier | Models (tier: provider/model) | Tools | Mock fixture | Used by |
|---|---|---|---|---|---|
| `planner` | plan → LLM | llm: anthropic/llm-planner | | `pg-planner` | research-brief |
| `researcher` | reason → LLM | llm: openai/llm-researcher | search_kb, calculator | `pg-researcher` | research-brief |
| `summarizer` | summarize → **SLM** | slm: openai/slm-summarizer · llm: anthropic/llm-summarizer | | `pg-summarizer-slm`, `pg-summarizer-llm` | research-brief |
| `verifier` | verify → LLM | llm: anthropic/llm-verifier | | `pg-verifier` | research-brief |
| `classifier` | classify → **SLM** | slm: **gemini**/slm-classifier · llm: anthropic/llm-classifier | | `pg-classifier-slm`, `pg-classifier-llm` | support-triage |
| `billing-specialist` | reason → LLM | llm: anthropic/llm-billing | lookup_order, issue_refund | `pg-billing-specialist` | support-triage |
| `tech-specialist` | reason → LLM | llm: openai/llm-tech | search_kb | `pg-tech-specialist` | support-triage |
| `reply-writer` | generate → SLM | slm: openai/slm-writer | | `pg-reply-writer` | support-triage |
| `editor` | generate → SLM | slm: openai/slm-editor | | `pg-editor` | every review loop (revise) |
| `proposer-a` | reason → LLM | llm: openai/llm-proposer-a | | `pg-proposer-a` | decision-review |
| `proposer-b` | reason → LLM | llm: anthropic/llm-proposer-b | | `pg-proposer-b` | decision-review |
| `critic` | judge → LLM | llm: openai/llm-critic | | `pg-critic` | decision-review |
| `arbiter` | judge → LLM | llm: anthropic/llm-arbiter | | `pg-arbiter` | decision-review |
| `assistant` | chat → LLM | llm: openai/llm-assistant (streams) | | `pg-assistant` | Chat tab |
| `backup` | generate → SLM | slm: openai/slm-backup | | `pg-backup` | fallback target |
| `drill-flaky` | reason → LLM | llm: openai/llm-flaky (retry 3) | | `pg-drill-flaky` | resilience-drill |
| `drill-ratelimited` | reason → LLM | llm: openai/llm-ratelimited (retry 2, fallback backup) | | `pg-drill-ratelimited` | resilience-drill |
| `drill-overloaded` | reason → LLM | llm: openai/llm-overloaded (retry 2, no fallback) | | `pg-drill-overloaded` | resilience-drill |
| `drill-slow` | reason → LLM | llm: openai/llm-slow (attempt timeout 800 ms, retry 1, fallback backup) | | `pg-drill-slow` | resilience-drill |
| `drill-connreset` | reason → LLM | llm: openai/llm-connreset (retry 2) | | `pg-drill-connreset` | resilience-drill |
| `drill-truncated` | reason → LLM | llm: openai/llm-truncated | | `pg-drill-truncated` | resilience-drill |
| `drill-badargs` | reason → LLM | llm: openai/llm-badargs | lookup_order | `pg-drill-badargs` | resilience-drill |
| `drill-streamcut` | reason → LLM | llm: openai/llm-streamcut (streams, retry 2) | | `pg-drill-streamcut` | resilience-drill |

`GET /api/agents` returns each agent with its **effective route** (the tier, model and reason the
router would pick right now) and **effective retry policy**.

## Tools

Real provider APIs, and mockagents faithfully, only *request* tool calls. The playground executes them.

| Tool | Arguments | Side effects | Notes |
|---|---|---|---|
| `search_kb` | `query: string` | none | Keyword search over 8 knowledge-base articles (KB-100…KB-107); returns the top 3. Its results are the evidence the grounding guard checks against. |
| `calculator` | `expression: string` | none | Recursive-descent parser for `+ - * / ^ ( )`. No `eval`; depth and length limits. |
| `lookup_order` | `order_id: string` | none | Demo orders ORD-1001 (charged twice), ORD-1002, ORD-1003. |
| `issue_refund` | `order_id: string, amount: number, reason: string` | **yes** | **Requires human approval.** Idempotent per order (a second call returns the original `refund_ref`). Amount-bounded. Calling it directly via `POST /api/tools/issue_refund/execute` returns 403. |

The runtime processes every tool call the same way:

1. **Validate.** The raw arguments must be a JSON object; required fields must be present and
   primitive types must match. On failure the tool does **not** run, and the model receives
   `{"error":"invalid_arguments","detail":…}` so it can correct itself.
2. **Gate.** For side-effecting tools, create a `tool_approval` review and wait. With no approver, or
   on timeout, the call is declined (fail safe).
3. **Execute** with a 10 s timeout. Multiple calls in one turn run **in parallel**.
4. **Report.** Each call is a `tool` span in the trace, a `tool_call` / `tool_result` event, and an
   entry in the result's `tool_executions`.

## Configuration reference

### Agent fields

| Field | Type | Meaning |
|---|---|---|
| `name` | kebab-case | Unique, immutable. |
| `description` | string | Shown in the UI and API. |
| `role` | plan · reason · verify · judge · generate · summarize · classify · extract · chat | Drives tier selection when `tier` is `auto`. |
| `tier` | auto · slm · llm | `auto` follows `router.policy`; `slm`/`llm` pins the tier. |
| `models` | `{ "slm"?: ModelRef, "llm"?: ModelRef }` | The model per tier. `ModelRef = {provider: openai\|anthropic\|gemini, model}`. |
| `system_prompt` | string ≤8000 | Sent as the system prompt. |
| `temperature` | 0-2 | Optional. |
| `max_tokens` | 0-32000 | Optional (Anthropic requires a value; 1024 is used when 0). |
| `tools` | tool names | Must exist; the Gemini client is text-only, so tool agents must use OpenAI or Anthropic. |
| `fallback` | ModelRef[] | Tried in order once the primary model's retries are exhausted. |
| `retry` | RetryPolicy \| null | Overrides `defaults.retry`; `null` (in a merge patch) removes the override. |
| `attempt_timeout_ms` | 100-300000 | Overrides `defaults.attempt_timeout_ms`. |
| `stream` | bool | Ask the provider for SSE by default. |
| `mock` | string[] | Documentation: the fixture(s) behind the agent. |

### RetryPolicy

| Field | Range | Default |
|---|---|---|
| `max_retries` | **0-5** (6+ is rejected, not clamped) | 3 |
| `initial_backoff_ms` | 0-60000 | 200 |
| `max_backoff_ms` | 0-120000 | 4000 |
| `multiplier` | 1-10 | 2.0 |
| `jitter` | 0-1 (±fraction) | 0.2 |

Delay before retry *n* = `min(initial × multiplier^(n-1), max) × (1 ± jitter)`, raised to the server's
`Retry-After` when present (capped at 30 s). A retry whose delay would pass the run deadline is not
attempted.

Retryable: HTTP 408, 409, 425, 429, 5xx, transport errors (reset, refused, EOF, garbled), broken
SSE streams, per-attempt timeouts. Permanent: other 4xx and caller cancellation.

### Global sections

| Path | Meaning | Default |
|---|---|---|
| `defaults.attempt_timeout_ms` | One HTTP attempt | 15000 |
| `defaults.run_timeout_ms` | Whole-run deadline | 1800000 (30 min) |
| `defaults.max_tool_turns` | Tool-loop cap per agent call | 6 |
| `defaults.review.mode` | manual · auto | manual |
| `defaults.review.timeout_ms` | Human-wait bound; must be < run timeout | 900000 (15 min) |
| `defaults.review.on_timeout` | fail · approve | fail |
| `defaults.review.max_revisions` | Revise rounds per gate | 2 |
| `router.policy` | role → slm\|llm | summarize/classify/extract/generate → slm; plan/reason/verify/judge/chat → llm |
| `router.escalation_threshold` | SLM confidence below this escalates | 0.6 |
| `router.escalate_on_guard_failure` | Regenerate guard failures on the LLM tier | true |
| `workflows.arbitration_threshold` | Arbiter confidence below this needs a human | 0.6 |
| `workflows.max_grounding_regenerations` | 0-3 | 1 |
| `workflows.watchdog_stall_ms` | No-progress limit for runs not waiting on a human. Must exceed the longest silent wait (the largest attempt timeout, `max_backoff_ms`, or the 30 s Retry-After cap) by 5 s, or validation rejects it | 120000 |
| `pricing.<model>` | `{input_per_1k, output_per_1k}` USD | LLM ≈ gpt-4o, SLM ≈ gpt-4o-mini |

## Routing rules

1. A per-call override (`tier` on invoke, or `options.tiers` on a run) wins.
2. Otherwise a pinned agent tier (`slm`/`llm`) applies.
3. Otherwise `router.policy[role]` applies.
4. If the chosen tier has no model, the other tier is used, and the reason says so.
5. **Escalation** (workflow-driven): low SLM confidence (classifier) or a failed grounding guard
   (summarizer) re-runs the call on the agent's LLM model, recorded as an `escalation` event, a
   step flag and the `playground_escalations_total` metric.

## How an agent maps to a fixture

mockagents picks the fixture by **model name**. A fixture's scenarios match the prompt the playground
sends:

```yaml
# mockagents/classifier-slm.yaml (excerpt)
spec:
  protocol: google-gemini
  model: slm-classifier                 # <- config: classifier.models.slm.model
  behavior:
    scenarios:
      - name: mixed-signals             # billing AND technical words -> low confidence
        match: { content_regex: "(?is)ticket:.*((invoice|refund|charge|billing).*(crash|error|bug|timeout)|…)" }
        response: { content: '{"category": "billing", "confidence": 0.48, "urgency": "medium"}' }
```

Multi-turn tool loops use `turn_number`: the playground sends a stable `X-Session-Id` per agent
conversation (`<run_id>.<agent>.<random>`), so turn 1 can request a tool and turn 2 can answer. Adding
or changing an agent is covered in [ONBOARDING.md](ONBOARDING.md#add-an-agent).
