# Orchestration and multi-agent coordination

MockAgents has two in-process coordinators: `PipelineExecutor` connects agent calls, and `Runner` executes test cases against an agent or pipeline. MCP and A2A are protocol simulators with separate state; they do not imply a distributed agent scheduler.

## Pipeline contract

A `PipelineDefinition` declares `metadata.name`, `spec.topology`, `spec.agents` (node `id` and exact agent `ref`), and graph `edges` (`from`, `to`, optional `when_contains`). Load-time validators check node uniqueness, references, topology, graph endpoints and cycles. Runtime execution also guards against missing agents, unsupported topology, and cycles for programmatic callers. Prefer validated definitions rather than relying on defensive runtime checks alone.

| Topology | Input and order | Failure behavior |
| --- | --- | --- |
| `sequential` | Declaration order; each node receives previous response content | Stops at failing node; earlier and failing node results remain |
| `parallel` | All nodes get original input concurrently; results returned in declaration order | Waits for invocations; joins errors and retains results |
| `graph` | Roots in declaration order, depth-first traversal of outgoing edges in declaration order | Stops on traversal error; returns visited prefix |

Graph `when_contains` is a **case-sensitive substring** check on source content. Empty guards always fire. Multiple matching edges can fire; this is not an exclusive branch. A visited target runs only once: the first traversed path determines its input. Consequently a diamond does not wait for or merge both parents. Disconnected roots run too; edges whose guards fail prune that route. Pipeline output exposes nodes; `FinalResponse()` means the last non-nil response in returned order, not a synthesized aggregate answer.

The executor passes request context to each agent for tenant scope and cancellation. Node session IDs are `<run-session>::<pipeline-name>::<node-id>`, subsequently scoped by tenant and agent in the engine. Reusing a run session intentionally continues each node's session; separate runs should use unique IDs. The HTTP handler supplies a request ID when `session_id` is absent. Direct programmatic callers must choose their own isolation policy.

## HTTP execution and editing

`POST /api/v1/pipelines/{name}/run` accepts a single JSON object with nonblank `input` and optional `session_id`; unknown fields, concatenated objects, and bodies over 1 MiB are rejected. It returns `PipelineResult` with `pipeline_name`, `topology`, `nodes`, and `latency`. Each node has `node_id`, `agent_name`, `response`, and `latency`. Durations are Go `time.Duration` encoded as integer **nanoseconds**, unlike interaction-log `latency_ms`.

An execution error returns `422` with `error`, `code`, and partial `result`. Codes are `missing_dependency`, `invalid_pipeline`, or `node_failed`. A missing pipeline returns `404`; an unconfigured executor returns `503`. Execution is viewer-accessible in multi-tenant mode and resolves all agent references using the caller's tenant visibility.

Pipeline definitions are globally registered. Updating one is therefore platform-only in multi-tenant mode. PUT updates an existing definition, validates agent references, requires the GET ETag in `If-Match`, persists via temporary file/rename, then replaces the live registration. Missing precondition is `428`; mismatch is `412`. There is no pipeline-creation POST route in this baseline.

The server supplies a nonblocking node recorder so every attempted node can produce an interaction with source `pipeline`. Such rows have no genuine HTTP method/path/status. Use the node-scoped session prefix to find the trajectory; allow asynchronous log visibility.

## Declarative suites

`TestSuite` targets exactly one agent or pipeline, contains named cases with steps, and attaches assertions. The runner extracts user steps and executes them in order as turns; it is not a verbatim replay engine for arbitrary supplied system/assistant/tool transcripts. Each case receives a runner/run/case-specific session namespace; sessions are deleted after the case. This isolates conversation history, not every process-wide chaos counter or replay cursor.

| Assertions | Observation |
| --- | --- |
| `response_contains`, `response_matches`, `refusal` | Final response content/refusal |
| `scenario_matched` | Selected scenario in the relevant final response |
| `tool_call`, `tool_call_args` | Named calls; the latter supports dotted argument paths and tolerant numeric comparison |
| `tool_call_count`, `tool_call_sequence`, `no_tool_call` | Tool trajectory across user turns |
| `node_sequence` | Ordered pipeline nodes accumulated across turns |
| `tool_error` | Simulated tool error result in the trajectory |
| `handles_tool_error` | Earlier error plus clean, nonempty, non-refusal final answer without final-turn tool errors |
| `latency_ms_lt` | Measured case latency against `max_ms` |

`node_id` can select a node response in a pipeline assertion; inspect the runner for the specific assertion's scope. For pipeline recovery, `handles_tool_error` treats the entire final turn as the unit: an error in an earlier node of that same last turn still prevents it being considered clean.

Use framework/application tests to verify the client's actual orchestration logic over HTTP, then use YAML suites to verify fixture/pipeline contracts. Do not substitute fixture self-tests for client integration coverage.

Evidence: [pipeline executor](../../internal/engine/pipeline.go), [pipeline validator](../../internal/config/pipeline_validator.go), [run/edit handlers](../../internal/server/pipeline_handlers.go), [runner](../../internal/runner/runner.go), [suite types](../../internal/types/testsuite.go), and [pipeline tests](../../internal/engine/pipeline_test.go).
