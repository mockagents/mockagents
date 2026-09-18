# Agent framework

An agent is a validated `AgentDefinition` held in an in-memory registry. It supplies response behavior for provider adapters; it is not a background worker, LLM inference process, or autonomous planner. The defining types are [agent.go](../../internal/types/agent.go) and [behavior.go](../../internal/types/behavior.go); all tagged fields appear in the [model dictionary](reference/model-fields.md).

## Lifecycle

1. Load a YAML, YML, or JSON file through `internal/config`; preserve a YAML node tree for line/column errors.
2. Apply defaults and validate. The default model is `mock-agent`; a present streaming block defaults chunk size to 4 and an omitted chunk delay to 50 ms. Explicit delay zero is retained.
3. Register the validated definition with its source path and tenant ownership. Startup logs and skips invalid agents; at least one valid agent is required for the main server.
4. Resolve each inbound request by explicit agent name, then model, within the authenticated tenant's visible catalog. Anonymous/single-tenant engine resolution can fall back to its sole global agent; tenant-bound callers do not receive this convenience fallback. Adapters can still require fields such as `model` before engine lookup.
5. Process a request in an existing scoped session or disposable session, then translate the response into the requested wire protocol.
6. Reload, replace, or delete through the CLI/watch/management paths. Source persistence and runtime registration are separate facts; management receipts and revision headers expose them.

Management writes stamp ownership from the caller. A tenant cannot use a request body or `X-Mockagents-Tenant` header to impersonate another tenant. Tenant-owned names can shadow global names. Pipeline references are exact agent names and deliberately do not use the anonymous single-agent fallback.

## Scenario matching

Scenarios are inspected in declaration order. Every supplied field within a runtime match rule must pass (AND). Authoring validation forbids combining `content_contains` with `content_regex`; either can be combined with turn/image conditions. The runtime matcher can evaluate both text fields only when a programmatic caller bypasses that validation.

| Field | Semantics |
| --- | --- |
| `content_contains` | Case-insensitive substring of the latest user text |
| `content_regex` | Go regular expression; case-sensitive unless the pattern changes it; named groups populate template `.Match` |
| `turn_number` | Exact proposed session turn number; first successful turn is 1 |
| `has_image` | Presence/absence of parsed image parts on the latest user turn |

Turns begin at 1 in normal engine execution. The Go validator does not reject zero/negative `turn_number` values; zero is even allowed by the current JSON Schema. Such a rule cannot match a normal proposed turn. Author positive values and do not treat validation success as proof that the rule is reachable. Evidence: [matcher](../../internal/engine/scenario_matcher.go), [validator](../../internal/config/validator.go), [session transaction](../../internal/engine/state/session.go).

The first matching explicit rule wins. A scenario without `match` is a default; the matcher remembers the first default and uses it only if no explicit rule matches. An empty match object has no failing conditions and therefore matches immediately. If there is no matching rule or default, the engine emits `_fallback` with `Mock response from <agent-name>` and records a fallback metric. This is a successful fixture fallback, not a routing error.

Matching uses the latest user message extracted by the adapter. It does not search the entire transcript for semantic intent. Image presence is a signal, not visual understanding. The neutral request also retains tool-result and echoed-call information so empty tool-result turns and loop convergence can be handled.

## Content generation and determinism

Response content uses Go `text/template`. Context contains `.Agent`, `.Message`, `.TurnNumber`, `.SessionID`, `.Vars`, and regex `.Match`; the context type also has a `Timestamp` field, but the engine's normal construction does not populate it. Use the implemented time functions when time output is intended.

Functions include `now`, `timestamp`, `date_offset`, `date_format`, `uuid`, `random_int`, `random_float`, `random_string`, `random_choice`, `upper`, `lower`, `title`, `to_json`, and `fake_name`, `fake_email`, `fake_phone`, `fake_company`, `fake_username`. These functions and their exact arguments are defined in [response_generator.go](../../internal/engine/response_generator.go). Time and random helpers make outputs variable; the chaos seed does not seed all template and tool randomness.

Only content is passed through this renderer; do not assume nested metadata, arguments, or tool response objects are recursively templated. Missing map keys use the template engine's lenient behavior and can render `<no value>`; malformed templates return generation errors. Templates and regexes are cached across requests. Repeatedly installing distinct fixtures can grow these caches for the process lifetime.

## State and tool behavior

Session identity is tenant + agent + client session ID. An omitted ID creates throwaway state, so turn-number scenarios require a stable supported session input (for example `X-Session-Id` on the provider adapters that read it). The memory store defaults to a 30-minute TTL, 100,000 sessions, and 256 retained messages. History eviction does not reset the accumulated turn number. Restart loses engine sessions.

`Session.ApplyTurn` proposes a turn and variables snapshot while holding the session mutex. A failed generation leaves history, variables, and turn count unchanged. Once generation commits, later transport failure or cancellation need not undo that turn. This is not an exactly-once request API; retrying with the same session can advance state.

Scenario tool calls are checked against agent tool definitions, yielding individual success/error results. `tool_choice: none` suppresses calls. Strict-tools settings add round-trip ID checks, required/named choice enforcement, parallel-call caps, and strict schema-subset checks. The fleet default is off; an agent block overrides it, and a present block with no level implies strict. `warn` reports violations without enforcement; `strict` returns provider-shaped 400s for invalid requests or applies choice forcing.

After an application returns a tool result, an identical immediate tool-call reissue is suppressed to allow convergence. A different configured call remains possible. See [tools](tools.md) for result-rule precedence and [orchestration](orchestration.md) for trajectories.

## Faults and observability

Chaos pre-checks can fail a request before matching. Post-generation delay occurs after state mutation. Stream pacing/faults happen when the adapter writes frames. `finish_reason`, `refusal`, hallucination labels, and raw OpenAI argument strings are authored semantic test cases, distinct from transport failures.

The engine stamps agent/model/scenario metadata, logs matched scenarios and strict/tool errors, emits scenario metrics, and creates an OTel processing span when enabled. Provider capture records HTTP interactions; pipeline execution uses a `NodeRecorder`. Sensitive messages can appear in configured logs: body policy and recording redaction are separate settings.

Relevant regression tests: [engine tests](../../internal/engine/engine_test.go), [matcher tests](../../internal/engine/scenario_matcher_test.go), [state tests](../../internal/engine/state/store_test.go), and [strict tests](../../internal/engine/strict_test.go).
