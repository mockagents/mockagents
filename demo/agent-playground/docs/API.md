# API reference

The contract is [`openapi.yaml`](../openapi.yaml) (OpenAPI 3.1). A running server serves it at
`/openapi.yaml` and `/openapi.json`, and the UI's **API** tab renders it. A contract test keeps the
document, the route table and the response shapes in sync (see [VERIFICATION.md](VERIFICATION.md)).

Base URL: `http://127.0.0.1:7070` (`playground serve --addr`).

## Conventions

- JSON in and out. Request bodies are decoded **strictly**: unknown fields are a 400.
- Errors: `{"error": {"code": "...", "message": "...", "details": ...}}`. Common codes:
  `invalid_request`, `invalid_input` (workflow input or options), `invalid_config` (with field-level
  `details`), `not_found`, `action_not_allowed`, `not_pending`, `already_finished`,
  `approval_required`, `mock_unreachable`.
- **Auth** is optional: with `--token T`, every mutating `/api` request needs `Authorization: Bearer T`.
  Reads stay open.
- **Long operations** are asynchronous. Runs return immediately (201) unless you pass `?wait=N`
  (≤120 s), which returns once the run is terminal *or waiting on a human*.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` · `/readyz` | Liveness; readiness (mock reachable) |
| GET | `/metrics` | Prometheus metrics |
| GET | `/openapi.yaml` · `/openapi.json` | This contract |
| GET | `/api/info` | Version, mock URL and mode, config version, run counts, pending reviews |
| GET | `/api/workflows` · `/api/workflows/{name}` | Catalog, input schema, examples |
| POST | `/api/workflows/{name}/runs` | Start a run: `{"input": {...}, "options": {...}}`, `?wait=N` |
| GET | `/api/runs` | List (`workflow`, `status`, `limit`) + counts per status |
| GET | `/api/runs/{id}` | Run: status, phase, steps, stats, events, output, error |
| GET | `/api/runs/{id}/trace` | Span tree |
| GET | `/api/runs/{id}/events` | **SSE**: `run` snapshots until `done` |
| POST | `/api/runs/{id}/cancel` | Fail the run with `cancelled` (409 if already finished) |
| GET | `/api/reviews` | Filter by `status`, `run_id`, `kind`, `blocking` |
| GET | `/api/reviews/{id}` | One review item |
| POST | `/api/reviews/{id}/decision` | `{"action": "approve" \| "reject" \| "revise" \| "flag", "comment", "reviewer"}` |
| GET | `/api/agents` · `/api/agents/{name}` | Catalog with effective route and retry policy |
| PATCH | `/api/agents/{name}` | JSON merge patch (RFC 7386); validated; atomic |
| POST | `/api/agents/{name}/invoke` | One agent call; `"stream": true` for SSE |
| GET | `/api/tools` | Tools and their schemas |
| POST | `/api/tools/{name}/execute` | Run a read-only tool directly (`issue_refund` → 403) |
| GET · PATCH | `/api/config` | Live config; merge patch for defaults, router, workflows, pricing |
| POST | `/api/config/reset` | Restore the startup config |
| GET | `/api/mock/status` | mockagents health + fixture catalog |
| GET | `/api/mock/logs` | mockagents interaction log (`session_prefix=<run id>` for one run) |
| GET | `/api/mock/costs` | mockagents cost rollup |
| GET | `/api/mock/pipelines` · POST `/api/mock/pipelines/{name}/run` | Native mockagents pipelines |
| GET · PUT | `/api/mock/agents/{name}` | Read (JSON or `?format=yaml`) / replace a fixture at runtime |
| POST | `/api/mock/agents/{name}/reload` | Reload a fixture from disk (re-arms `fail_first`) |
| POST | `/api/mock/validate` | Validate a mockagents YAML document |

## Examples

### Start a run and wait

```bash
curl -s 'http://127.0.0.1:7070/api/workflows/support-triage/runs?wait=30' \
  -H 'Content-Type: application/json' \
  -d '{"input":{"ticket":"I was charged twice for order ORD-1001."},"options":{"review_mode":"manual"}}'
```

```json
{
  "id": "run_101500_0001",
  "workflow": "support-triage",
  "status": "in_progress",
  "phase": "awaiting_approval",
  "current_step": "resolve: billing-specialist",
  "steps": [ { "name": "classify", "status": "completed", "tier": "slm", "model": "gemini/slm-classifier", "...": "..." } ],
  "stats": { "llm_calls": 1, "slm_calls": 1, "retries": 0, "cost_usd": 0.00001, "...": "..." },
  "deadline": "2026-10-07T10:45:00Z"
}
```

### Decide the pending approval, then the reply gate

```bash
curl -s 'http://127.0.0.1:7070/api/reviews?status=pending&blocking=true&run_id=run_101500_0001'
curl -s http://127.0.0.1:7070/api/reviews/rev_0002/decision -H 'Content-Type: application/json' \
  -d '{"action":"approve","reviewer":"dana"}'
curl -s http://127.0.0.1:7070/api/reviews/rev_0004/decision -H 'Content-Type: application/json' \
  -d '{"action":"revise","comment":"Add an apology for the delay.","reviewer":"dana"}'
```

A second decision on the same item returns **409** `not_pending`. `revise` on a tool approval returns
**400** `action_not_allowed`, with the allowed actions in `details`.

### Invoke an agent (streaming)

```bash
curl -N http://127.0.0.1:7070/api/agents/drill-streamcut/invoke \
  -H 'Content-Type: application/json' -d '{"input":"status","stream":true}'
```

```
event: run
data: {"run_id":"run_101612_0003","status":"in_progress"}

event: route
data: {"agent":"drill-streamcut","model":"openai/llm-streamcut","reason":"policy: role \"reason\" -> llm","tier":"llm"}

event: delta
data: {"text":"The full "}
...
event: reset
data: {"agent":"drill-streamcut","reason":"stream ended without a finish frame"}

event: retry
data: {"agent":"drill-streamcut","attempt":1,"delay_ms":104,"reason":"stream_integrity", ...}

event: done
data: {"run_id":"run_101612_0003","status":"completed","result":{"output":"The full status report: ...","stream_fallback":true, ...}}
```

Streaming event types: `run`, `route`, `delta`, `reset`, `retry`, `fallback`, `tool_call`,
`tool_result`, `approval_requested`, `continuation`, `escalation`, `review_requested`,
`review_decided`, `human_escalation`, `done`.

If a side-effecting tool needs approval, a non-streaming invoke returns **202** with `pending_reviews`.

### Reconfigure at runtime

```bash
# Merge patch: change only what you send; null removes an optional field.
curl -s -X PATCH http://127.0.0.1:7070/api/agents/summarizer \
  -H 'Content-Type: application/json' -d '{"tier":"llm","retry":{"max_retries":5}}'

curl -s -X PATCH http://127.0.0.1:7070/api/config \
  -H 'Content-Type: application/json' -d '{"router":{"escalation_threshold":0.8}}'
```

Invalid changes return 400 `invalid_config` with `details: [{"path": "agents[2].retry", "message": "max_retries must be between 0 and 5 (got 6)"}]`,
and the previous configuration stays live. Every accepted change increments the config `version`
(in the `/api/config` response body, and in the `X-Config-Version` header on `GET /api/config` and
agent patches).

### Correlate with the mock

```bash
curl -s 'http://127.0.0.1:7070/api/mock/logs?session_prefix=run_101500_0001&limit=50'
```

Each entry is one request mockagents served for that run: `agent_name`, `scenario_name`,
`response_status`, `prompt_tokens`, `completion_tokens`, `cost_usd`, `session_id`.

## Clients

- **Postman:** [`postman/agent-playground.postman_collection.json`](../postman/agent-playground.postman_collection.json).
  41 requests in 6 folders; folder 3 walks the human-review flow in order.
- **curl:** [`scripts/demo.sh`](../scripts/demo.sh).
- **CLI:** `playground --help` (uses the same API).
- **Code generation:** any OpenAPI 3.1 generator works on `openapi.yaml`.
