# Deployment

## Topologies

| Topology | When | Command |
|---|---|---|
| **Embedded mock** (default) | Demos, local dev, CI | `playground serve` |
| **External mockagents** | Mirror production (the app and the "provider" are separate processes); share one mock across apps | `mockagents start --agents-dir demo/agent-playground/mockagents` + `playground serve --mock http://host:8080` |
| **Container** | Anywhere Docker runs | `docker build -f demo/agent-playground/Dockerfile -t mockagents-playground .` |
| **Compose** | Both containers together | `cd demo/agent-playground && docker compose --profile external up --build` |
| **Kubernetes** | Shared environments | mockagents via the repo's Helm chart + a Deployment for the playground (below) |

## Binary

```bash
go build -o playground ./demo/agent-playground/cmd/playground
./playground serve --addr 0.0.0.0:7070 --state-file /var/lib/playground/runs.json --token "$TOKEN"
```

The binary embeds its UI, OpenAPI document, default config and fixtures, so it runs from any directory.

### Flags and environment

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `--addr` | `PLAYGROUND_ADDR` | `127.0.0.1:7070` | Listen address (use `0.0.0.0:7070` in containers) |
| `--mock` | `PLAYGROUND_MOCK_URL` | `embedded` | `embedded` or an external mockagents base URL |
| `--mock-key` | `PLAYGROUND_MOCK_API_KEY` | (placeholder) | Key sent to provider endpoints and the management API (required by multi-tenant mockagents) |
| `--mock-addr` | `PLAYGROUND_MOCK_ADDR` | `127.0.0.1:0` | Embedded mock listen address |
| `--config` | `PLAYGROUND_CONFIG` | embedded `config/playground.json` | Your own config file |
| `--fixtures` | `PLAYGROUND_FIXTURES` | embedded `mockagents/` | Fixture directory for the embedded mock |
| `--state-file` | `PLAYGROUND_STATE_FILE` | none (memory only) | Persist runs, traces and reviews (atomic writes every 2 s and on every finish) |
| `--review-mode` | `PLAYGROUND_REVIEW_MODE` | from config (`manual`) | `manual` or `auto` |
| `--token` | `PLAYGROUND_TOKEN` | none | Require `Authorization: Bearer` on mutating `/api` calls |
| `--log-level` | `PLAYGROUND_LOG_LEVEL` | `info` | debug · info · warn · error |
| CLI `--server` | `PLAYGROUND_URL` | `http://127.0.0.1:7070` | Where CLI commands send requests |

Probes: `GET /healthz` (liveness), `GET /readyz` (the mock is reachable). Metrics: `GET /metrics`.

Shutdown: on SIGINT/SIGTERM the server drains, fails in-flight runs with `shutdown`, and flushes state.
On the next start with the same `--state-file`, any run the previous process did not get to finish is
marked `failed` / `interrupted`.

## External mockagents

The mock must load the playground fixtures and price table, and the fixtures must exist on **its**
disk: the resilience drill re-arms `fail_first` counters with `POST /api/v1/agents/{name}/reload`,
which re-reads the file.

```bash
MOCKAGENTS_PRICING=demo/agent-playground/config/mock-pricing.yaml \
  go run ./cmd/mockagents start --port 8080 --agents-dir demo/agent-playground/mockagents
go run ./demo/agent-playground/cmd/playground serve --mock http://127.0.0.1:8080
```

In multi-tenant mode (`MOCKAGENTS_MULTI_TENANT=1`), pass a tenant API key with `--mock-key`, and see
the mockagents [multi-tenant guide](../../../docs/guides/multi-tenant.md) for how agents are scoped to
tenants. Only the single-tenant setup is exercised by the playground's tests.

## Docker

```bash
docker build -f demo/agent-playground/Dockerfile -t mockagents-playground .   # from the repo root
docker run --rm -p 7070:7070 -v playground-data:/data mockagents-playground
```

The image runs as a non-root user with `/data` as its working directory (state file) and has a
readiness `HEALTHCHECK`. Override the command for an external mock:
`docker run … mockagents-playground serve --addr 0.0.0.0:7070 --mock http://mockagents:8080`.

## Kubernetes

Deploy mockagents with the repo's chart, mounting the fixtures (for example from a ConfigMap built
with `kubectl create configmap pg-fixtures --from-file=demo/agent-playground/mockagents`). Then run
the playground:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: agent-playground }
spec:
  replicas: 1                      # single-process engine; see scaling notes
  selector: { matchLabels: { app: agent-playground } }
  template:
    metadata: { labels: { app: agent-playground } }
    spec:
      containers:
        - name: playground
          image: mockagents-playground:latest
          args: ["serve", "--addr", "0.0.0.0:7070", "--mock", "http://mockagents:8080", "--state-file", "/data/runs.json"]
          env:
            - name: PLAYGROUND_TOKEN
              valueFrom: { secretKeyRef: { name: playground, key: token } }
          ports: [{ containerPort: 7070 }]
          readinessProbe: { httpGet: { path: /readyz, port: 7070 } }
          livenessProbe: { httpGet: { path: /healthz, port: 7070 } }
          volumeMounts: [{ name: data, mountPath: /data }]
      volumes:
        - name: data
          persistentVolumeClaim: { claimName: agent-playground }
```

## Scaling notes

The engine is single-process by design: runs, review waits and the watchdog live in memory, and the
state file is a single-writer snapshot. To scale beyond one replica you would move the run store and
review book to a shared database, and make the watchdog lease-based. `workflow.Store` and
`workflow.ReviewBook` are the types to put behind interfaces for that. The agent runtime itself is stateless and safe to call
concurrently.

## Security checklist

- Bind to `127.0.0.1` (the default) unless you need remote access; then set `--token`.
- Treat the state file as sensitive: it contains prompts, outputs and reviewer comments.
- The `/api/mock/*` proxy forwards to the mock's management API, including fixture writes, so protect
  it with `--token` when the playground is exposed.
- All model output is HTML-escaped in the UI, and tool arguments are validated before execution.
