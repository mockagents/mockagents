# Management API

The management API provides server introspection and control. All endpoints are prefixed with `/api/v1/`.

## Health Check

```
GET /api/v1/health
```

```bash
curl http://localhost:8080/api/v1/health
```

```json
{ "status": "ok", "version": "0.1.0", "uptime": "5m32s" }
```

## List Agents

```
GET /api/v1/agents
```

```bash
curl http://localhost:8080/api/v1/agents
```

```json
[
  {
    "name": "customer-support",
    "description": "Customer support mock agent",
    "model": "gpt-4o",
    "protocol": "openai-chat-completions",
    "scenario_count": 3,
    "tool_count": 2,
    "tags": ["support"]
  }
]
```

## Get Agent Detail

```
GET /api/v1/agents/{name}
```

```bash
curl http://localhost:8080/api/v1/agents/customer-support
```

Returns the full agent definition as JSON. Returns 404 with available agent names if not found.

## Reload Agent

```
POST /api/v1/agents/{name}/reload
```

Re-reads the agent's YAML file from disk, re-validates, and replaces the in-memory definition.

```bash
curl -X POST http://localhost:8080/api/v1/agents/customer-support/reload
```

```json
{ "status": "reloaded", "agent": "customer-support" }
```

## List Interaction Logs

```
GET /api/v1/logs
```

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `agent` | string | | Filter by agent name |
| `session_id` | string | | Filter by session ID |
| `since` | string | | ISO 8601 timestamp lower bound |
| `until` | string | | ISO 8601 timestamp upper bound |
| `limit` | int | 50 | Max results (1-1000) |
| `offset` | int | 0 | Pagination offset |

```bash
curl "http://localhost:8080/api/v1/logs?agent=customer-support&limit=10"
```

## Get Log Detail

```
GET /api/v1/logs/{id}
```

Returns full request/response bodies for a single interaction.

```bash
curl http://localhost:8080/api/v1/logs/42
```

## Clear Logs

```
DELETE /api/v1/logs
```

```bash
curl -X DELETE http://localhost:8080/api/v1/logs
```

```json
{ "deleted_count": 150 }
```
