# Agentic patterns catalog

Every pattern below runs in the playground and is covered by a test ([VERIFICATION.md](VERIFICATION.md)).
Each entry links to its code.

## Orchestration

| Pattern | What it solves | Where |
|---|---|---|
| **Orchestrator-worker** | A planner decomposes the task; specialized workers execute; the orchestrator assembles the result. | `research-brief`, [`research_brief.go`](../internal/workflows/research_brief.go) |
| **Router / dispatcher** | Classify once, then send the work to the right specialist instead of one agent doing everything. | `support-triage`, [`support_triage.go`](../internal/workflows/support_triage.go) |
| **Parallel fan-out** | Independent work runs concurrently; the step waits for all of it, or cancels the rest on the first error. | `decision-review` proposers; [`workflow.Parallel`](../internal/workflow/exec.go) |
| **Structured output with graceful degradation** | Models answer in JSON; the parser tolerates fences and prose, and a broken plan falls back to a default instead of killing the run. | [`guard.ExtractJSON`](../internal/guard/guard.go), planner step |
| **Native pipeline as a contract** | mockagents runs the agent graph itself (no tools or retries) to pin node order in CI. | [`pipeline-native-brief.yaml`](../mockagents/pipeline-native-brief.yaml), [`native-pipeline-suite.yaml`](../mock-tests/native-pipeline-suite.yaml) |

## Tool use

| Pattern | What it solves | Where |
|---|---|---|
| **Tool loop** | Model requests a tool → the client executes it → the result goes back → repeat until a final answer. Capped by `max_tool_turns`. | [`agents/runtime.go`](../internal/agents/runtime.go) `invoke` |
| **Parallel tool calls** | Several calls in one turn run concurrently. | researcher (`search_kb` ‖ `calculator`); `runTools` |
| **Argument validation before execution** | Model output is untrusted: malformed or mistyped arguments never reach a tool. The model receives a structured error and can self-correct. | [`tools.ParseArgs`](../internal/tools/tools.go); drill `bad-tool-args` |
| **Approval-gated side effects** | A human approves money-moving actions; with no approver or on timeout the call is declined (fail safe). | `issue_refund`; [`Exec.approveTool`](../internal/workflow/exec.go) |
| **Idempotent tools** | Retries and loops make duplicate calls likely; a second refund returns the first refund's reference. | [`orders.go`](../internal/tools/orders.go) |
| **Human feedback injection** | When a human declines an action, the decision is fed back to the model as a user turn, so the next answer reflects it. | `runtime.invoke` (declined tools) |
| **Safe computation** | Arithmetic through a bounded parser, never `eval`. | [`calculator.go`](../internal/tools/calculator.go) |

## Model routing (SLM + LLM)

| Pattern | What it solves | Where |
|---|---|---|
| **Role-based tiering** | Cheap SLMs for summarize/classify/extract/generate; LLMs for plan/reason/verify/judge. SLM tokens cost about 1/16 as much here. | [`router.Route`](../internal/router/router.go), `router.policy` |
| **SLM-first, LLM-on-doubt** | Run the cheap model first and escalate only when its confidence is below a threshold or its output is unusable. | classifier escalation (0.48 < 0.6) |
| **Escalation on guard failure** | A summary that fails grounding is regenerated on the stronger tier. | research-brief regeneration |
| **Runtime overrides** | Pin a tier per agent (config) or per run (`options.tiers`), or per call (`tier`). | API, UI, CLI |

## Hallucination reduction

| Pattern | What it solves | Where |
|---|---|---|
| **Deterministic grounding guard** | Every figure and citation in an output must exist in the retrieved evidence; citations must be real and retrieved. | [`guard.CheckGrounding`](../internal/guard/guard.go) |
| **LLM-as-judge, augmented by deterministic checks** | The judge receives the guard's findings rather than reasoning from scratch; an unparseable verdict fails closed. | verifier step |
| **Bounded regeneration** | Regenerate with a strict-grounding instruction at most N times, then fail with `guard_failed`. Never loop. | `max_grounding_regenerations` |
| **Cross-model consistency** | Two proposers on different providers must agree; disagreement is surfaced, not averaged away. | decision-review |
| **Debate / critic** | A critic names the evidence gaps in both proposals before judgement. | `critic` agent |
| **Confidence-based human escalation** | A low-confidence verdict is never auto-trusted: the gate is titled `[NEEDS HUMAN DECISION]`. | arbiter threshold 0.6 |
| **Prompt-injection hygiene for structured prompts** | User text is flattened to one line before being embedded, so it cannot forge a `Guard flags:` line or break JSON-templated fixtures. | [`guard.SanitizeLine`](../internal/guard/guard.go) |

## Resilience and error handling

| Pattern | What it solves | Where |
|---|---|---|
| **Exponential backoff with jitter** | Spreads retries out and avoids thundering herds. | [`retry.Policy.Delay`](../internal/retry/retry.go) |
| **Retry-After** | A 429's hint is honoured even above `max_backoff_ms` (capped at 30 s). | drill `rate-limited` |
| **Retry classification** | Retry 408/409/425/429/5xx, transport errors, broken streams and attempt timeouts; never retry other 4xx or a cancelled caller. | [`llm.Classify`](../internal/llm/llm.go) |
| **Per-attempt timeout** | A hung upstream costs one attempt, not the whole run. The attempt timeout is distinguished from the caller's own deadline. | `llm.WithAttemptTimeout`; drill `timeout` |
| **Deadline-aware retry budget** | Never sleep into a guaranteed failure: give up if the next backoff would pass the deadline. | `retry.ErrBudgetExceeded` |
| **Fallback chain** | After the primary's retries are exhausted, try the next model (possibly another provider). Answers are labelled. | `agents.complete`; drills `rate-limited`, `timeout` |
| **Stream integrity + downgrade** | An SSE stream without its terminal frame is an error; the retry runs without streaming, and clients get a `reset` event. | [`openai.go`](../internal/llm/openai.go) `readStream`; drill `stream-cut` |
| **Continuation** | `finish_reason: length` triggers "continue from where you stopped" (bounded). | drill `truncated` |
| **Bounded failure + isolation** | A hard outage fails its step cleanly; the drill continues (`continue_on_error`) or ends the run (`fail_hard`). | drill `outage` |
| **Re-armable chaos** | Stateful mock faults are reset through the management API so tests are repeatable. | drill arm step; [`mockctl.Arm`](../internal/mockctl/mockctl.go) |

## Run safety

| Pattern | What it solves | Where |
|---|---|---|
| **Three-state lifecycle** | `in_progress → completed | failed` only, with first-writer-wins finish. | [`workflow/types.go`](../internal/workflow/types.go), `Store.finish` |
| **Watchdog** | Fails runs past their deadline, or stalled (no heartbeat) while not waiting on a human. | `Engine.CheckStale` |
| **Crash recovery** | Persisted in-flight runs are failed (`interrupted`) on restart, never resumed into limbo. | `Store.loadState` |
| **Graceful shutdown** | In-flight runs are failed with `shutdown`; state is flushed. | `Engine.Shutdown` |

## Human in the loop

| Pattern | What it solves | Where |
|---|---|---|
| **Audit everything** | Every AI output becomes a non-blocking review item that can be approved or flagged later. | `Exec.Invoke` → `audit` |
| **Blocking gate** | Final outputs need approve / reject / revise. | `Exec.ReviewLoop` |
| **Bounded revise loop** | Revisions go through an editor agent and back to the gate, up to `max_revisions`. | `ReviewLoop` |
| **Review timeouts with policy** | A forgotten review fails the run (or approves it, if configured), so a run never waits forever. | `Exec.waitDecision` |
| **Auto mode** | CI-friendly: gates are approved by policy and recorded as `auto_approved`. | `review_mode: auto` |
