# Observability: metrics, readiness, tracing

MockAgents exposes three operability surfaces. Two of them are the subject of
this guide — a Prometheus scrape target and a readiness signal that is a
genuinely different question from liveness — and the third, OpenTelemetry
tracing, is covered at the end.

## Liveness vs readiness

They answer different questions, and pointing both probes at the same endpoint
throws away the distinction.

| Endpoint | Question | Behavior |
| --- | --- | --- |
| `GET /api/v1/health` | Is the process running? | Always `200` while the process is up. Checks nothing else — on purpose. |
| `GET /api/v1/ready` | Can this instance serve a mock? | `200` when every dependency check passes, `503` naming the failing one otherwise. |

The reason liveness checks nothing is that a failed liveness probe means
*restart the container*, and restarting a process that is alive but pointed at
an empty agents directory fixes nothing — it just produces a crash loop that
hides the real problem. A failed readiness probe means *stop sending it
traffic*, which is exactly right for a mock server with no fixtures loaded.

Readiness runs these checks:

| Check | Passes when | Why it matters |
| --- | --- | --- |
| `fixtures` | At least one agent is loaded | A registry with zero agents can only return 404s. |
| `log_store` | The SQLite interaction log answers a ping | Only present when interaction logging is configured. |

```console
$ curl -s localhost:8080/api/v1/ready
{"status":"ready","checks":[{"name":"fixtures","status":"ok"},{"name":"log_store","status":"ok"}]}
```

`mockagents start` refuses to boot with zero agent definitions, so the
`fixtures` check is not about a bad startup — it guards the states you can
reach *while running*: agents removed through the write API
(`DELETE /api/v1/agents/{name}`), and embedded use where a caller constructs a
server around a registry it fills later. Delete the last agent and readiness
flips immediately:

```console
$ curl -s -X DELETE localhost:8080/api/v1/agents/echo-agent
{"status":"deleted","agent":"echo-agent","persisted":false,"file":"minimal-agent.yaml"}

$ curl -s -w " %{http_code}
" localhost:8080/api/v1/ready
{"status":"not_ready","checks":[{"name":"fixtures","status":"failed","error":"no agent fixtures loaded"},{"name":"log_store","status":"ok"}]} 503
```

Note that the passing check is still reported, so the body says *which*
dependency broke rather than just that something did. Liveness is unchanged in
that state — the process really is alive, and restarting it would not bring the
agent back:

```console
$ curl -s -w " %{http_code}
" localhost:8080/api/v1/health
{"agents_loaded":0,"status":"ok","uptime":"11.3s","uptime_seconds":11,"version":"0.4.0"} 200
```

Both endpoints stay **unauthenticated even in multi-tenant mode**: a kubelet or
load balancer carries no API key, and neither response contains agent, tenant,
or configuration data.

## Prometheus metrics

`GET /metrics` renders the Prometheus text exposition format (version 0.0.4).
No configuration, no flag — it is always on, and every family is declared even
before the first request, so `curl /metrics | grep chaos` tells you "zero
injections" rather than leaving you wondering whether the build has the feature.

### Families

| Metric | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `mockagents_requests_total` | counter | `protocol`, `agent`, `status` | Request volume and error rate per mocked surface. |
| `mockagents_request_duration_seconds` | histogram | `protocol` | Latency, *including* any injected chaos latency. |
| `mockagents_scenario_matches_total` | counter | `agent`, `scenario`, `kind` | Which fixtures traffic actually hits. `kind` is `rule`, `default`, or `fallback`. |
| `mockagents_chaos_injections_total` | counter | `agent`, `kind` | Faults injected: `latency`, `error`, `rate_limit`, `connection`. |
| `mockagents_agents_loaded` | gauge | — | Fixtures in the registry. |
| `mockagents_ready` | gauge | — | `1`/`0` — the same verdict `/api/v1/ready` returns. |
| `mockagents_uptime_seconds` | gauge | — | Seconds since start. |
| `mockagents_build_info` | gauge | `version`, `go_version` | Always `1`; identifies the binary. |
| `mockagents_metrics_series_dropped_total` | counter | — | Observations dropped at the cardinality ceiling (1000 label sets per family). Non-zero means the other counters are undercounts. |

A live scrape after driving traffic at the `flaky-agent` example, which injects
latency, errors, and a rate limit:

```
mockagents_requests_total{protocol="openai-chat-completions",agent="flaky-agent",status="200"} 17
mockagents_requests_total{protocol="openai-chat-completions",agent="flaky-agent",status="429"} 5
mockagents_requests_total{protocol="openai-chat-completions",agent="flaky-agent",status="503"} 1
mockagents_requests_total{protocol="openai-chat-completions",agent="flaky-agent",status="504"} 2
mockagents_chaos_injections_total{agent="flaky-agent",kind="error"} 3
mockagents_chaos_injections_total{agent="flaky-agent",kind="latency"} 17
mockagents_chaos_injections_total{agent="flaky-agent",kind="rate_limit"} 5
```

Note that the faulted requests carry the real agent name, not `unknown` — an
injected fault aborts before a response exists, so the agent is recorded at
resolution time specifically so "which agent is failing" stays answerable.

### The one worth alerting on

`kind="fallback"` in `mockagents_scenario_matches_total` means a request matched
no rule *and* the agent declares no default, so the engine answered with a
built-in placeholder. In a test suite that is almost always a fixture gap
rather than an intended path:

```promql
sum(rate(mockagents_scenario_matches_total{kind="fallback"}[5m])) > 0
```

The scenario-match rate itself:

```promql
sum(rate(mockagents_scenario_matches_total{kind="rule"}[5m]))
  / sum(rate(mockagents_scenario_matches_total[5m]))
```

### Scraping

Single-tenant mode (the default) serves `/metrics` unauthenticated:

```yaml
scrape_configs:
  - job_name: mockagents
    static_configs:
      - targets: ["localhost:8080"]
```

In **multi-tenant mode** `/metrics` requires a `viewer`-or-above API key, because
agent and scenario names are metric labels. Health and readiness stay open;
metrics do not:

```yaml
scrape_configs:
  - job_name: mockagents
    authorization:
      credentials_file: /etc/prometheus/mockagents.key
    static_configs:
      - targets: ["mockagents:8080"]
```

### Kubernetes

The Helm chart ships a Prometheus Operator `ServiceMonitor` pointing at
`/metrics`, off by default:

```bash
helm install demo ./deploy/helm/mockagents \
  --set serviceMonitor.enabled=true
```

The chart's probes are already split — `livenessProbe` on `/api/v1/health`,
`readinessProbe` on `/api/v1/ready` — and both paths are overridable via
`probes.liveness.path` and `probes.readiness.path`.

### What is not counted

`mockagents_requests_total` and the latency histogram cover the HTTP
request/response surfaces: `/v1/chat/completions`, `/v1/responses`,
`/v1/embeddings`, `/v1/messages`, `/v1/moderations`, the Gemini
`/v1beta/models/*:generateContent` routes, the Azure `/openai/*` surfaces, and
`/v1/engines/process`. Two deliberate exclusions:

- **Management API traffic** (`/api/v1/*`, including the scrape itself) is not
  counted. Mixing console polling into the request rate would bury the signal.
- **The Realtime WebSocket** generates responses in-process on an established
  socket, so there is no per-request status or latency to record. Realtime
  traffic still appears in `mockagents_scenario_matches_total` and in the
  interaction log.

## OpenTelemetry tracing

Tracing is opt-in; the tracer provider is a no-op until an exporter is
configured, so there is no runtime cost otherwise.

| Env var | Effect |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT=https://…` | Send spans to an OTLP/HTTP collector |
| `MOCKAGENTS_OTEL_STDOUT=1` | Pretty-print spans to stdout (local development) |

Each request produces an outer `http.request` span and an inner
`engine.process_request` span carrying `agent.name`, `agent.model`,
`agent.protocol`, `agent.scenario`, and `agent.tool_calls`.

## Related

- [Chaos & Fault Injection](chaos.md) — what produces `mockagents_chaos_injections_total`
- [Management API](management-api.md) — the rest of the `/api/v1` surface
- [Multi-Tenant & Control Plane](https://github.com/mockagents/mockagents/blob/main/docs/guides/multi-tenant.md) — RBAC floors, including the one on `/metrics`
