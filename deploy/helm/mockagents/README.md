# mockagents Helm chart (v0.2)

Deploy [MockAgents](https://github.com/mockagents/mockagents) on
Kubernetes. The chart runs the existing Docker image as a non-root
Deployment, exposes it via a Service, and mounts agent definitions from
a ConfigMap. v0.2 adds opt-in HPA, PDB, NetworkPolicy, and a
Prometheus Operator ServiceMonitor — all off by default so the
upgrade from v0.1 → v0.2 is a no-op for existing users.

## Install

```bash
helm install demo ./deploy/helm/mockagents \
  --set agents.inline."echo.yaml"="$(cat examples/minimal-agent.yaml)"
```

Or, more ergonomically, pass a values file:

```yaml
# my-values.yaml
image:
  tag: "0.1.0"

agents:
  inline:
    echo.yaml: |
      apiVersion: mockagents/v1
      kind: Agent
      metadata:
        name: echo-agent
      spec:
        protocol: openai-chat-completions
        behavior:
          scenarios:
            - name: default
              response:
                content: "hello from k8s"

service:
  type: ClusterIP
  port: 8080
```

```bash
helm install demo ./deploy/helm/mockagents -f my-values.yaml
helm test demo
```

## Providing agent definitions

Two modes, pick one:

- **`agents.inline`** — a map of filename → YAML string. The chart
  renders a ConfigMap for you. Good for small demos, CI fixtures, and
  single-agent deployments.
- **`agents.existingConfigMap`** — name of a ConfigMap you manage
  yourself (Kustomize, GitOps, etc.). Each key in the CM becomes a
  file under `/agents/`. Good for production.

When both are empty the server starts but every request fails with
`no valid agent definitions found`. `helm install` prints a warning
in the NOTES in that case.

## Common overrides

| Value                                | Purpose                                                  |
| ------------------------------------ | -------------------------------------------------------- |
| `image.tag`                          | Pin a specific Docker image tag.                         |
| `replicaCount`                       | Replicas. State is per pod, so >1 is refused unless `env.MOCKAGENTS_TENANCY_DSN` is set or `multiReplica.acknowledged=true` (see below). |
| `multiReplica.acknowledged`          | Run N independent single-tenant mock servers behind one Service (logs, audit and the write API differ per pod). |
| `service.type`                       | `ClusterIP` (default), `NodePort`, or `LoadBalancer`.    |
| `ingress.enabled` + `ingress.hosts`  | Put an Ingress in front of the service.                  |
| `persistence.enabled`                | Create a PVC (size/accessModes/storageClass) for `/data`; off = emptyDir. `/data` is always mounted and `MOCKAGENTS_DATA_DIR` points at it, so the interaction log, audit log and tenancy DB survive container restarts; a PVC makes them survive rescheduling. |
| `persistence.existingClaim`          | Mount a pre-created PVC instead of the chart-managed one. |
| `existingSecret`                     | Secret exposed via `envFrom` for `MOCKAGENTS_BOOTSTRAP_KEY`, `MOCKAGENTS_OIDC_CLIENT_SECRET`, a `MOCKAGENTS_TENANCY_DSN` with a password. `env` values render in clear text. |
| `extraEnvFrom`                       | Additional `envFrom` entries (configMapRef / secretRef). |
| `env.OTEL_EXPORTER_OTLP_ENDPOINT`    | Ship traces to an OTLP/HTTP collector.                   |
| `logLevel`                           | `debug`, `info`, `warn`, `error`.                        |
| `host`                               | Bind address inside the pod; defaults to `0.0.0.0`.      |
| `extraArgs`                          | Extra flags appended to `mockagents start`.              |
| `autoscaling.enabled`                | **v0.2** — render an HPA (`autoscaling/v2`) targeting CPU and/or memory. |
| `podDisruptionBudget.enabled`        | **v0.2** — render a PDB so node drains can't take every replica at once. |
| `networkPolicy.enabled`              | **v0.2** — render a NetworkPolicy locking down ingress + egress. |
| `serviceMonitor.enabled`             | **v0.2** — render a Prometheus Operator ServiceMonitor for `/metrics` (the endpoint itself landed with the R9 operability slice). |

## Verify before installing

```bash
helm lint ./deploy/helm/mockagents
helm template demo ./deploy/helm/mockagents -f my-values.yaml | kubectl apply --dry-run=client -f -
```

## Uninstall

```bash
helm uninstall demo
```

## What's new in v0.2

- **HorizontalPodAutoscaler** — opt in with `autoscaling.enabled=true`.
  Targets CPU utilization by default; supports memory utilization,
  configurable min/max replicas, and a passthrough `behavior` block
  for stabilization windows. Renders against `autoscaling/v2`.
- **PodDisruptionBudget** — opt in with
  `podDisruptionBudget.enabled=true`. Off by default because the
  default `replicaCount` is 1 (a `minAvailable: 1` PDB would block
  drains entirely); flip it on once you raise replicas or enable
  autoscaling.
- **NetworkPolicy** — opt in with `networkPolicy.enabled=true`.
  Three knobs: `allowSameNamespace` (default true),
  `allowExternalIngress` for cluster-wide controllers, and
  user-supplied `ingressFrom` / `egressRules` arrays. DNS egress
  is allowed by default via `allowDNS`.
- **ServiceMonitor** — opt in with `serviceMonitor.enabled=true`.
  Requires the Prometheus Operator CRDs; defaults to a 30s scrape
  interval against the named `http` port. Forwards user-supplied
  `relabelings` / `metricRelabelings` / extra `labels` so the right
  Prometheus instance picks it up. In multi-tenant mode `/metrics`
  needs a viewer-or-above API key, so add a `bearerTokenSecret` to the
  endpoint; single-tenant deployments scrape it unauthenticated.

All four are off by default. With every flag enabled, `helm template`
renders 10 resources; defaults still render the same 6 as v0.1.

## Probes

Liveness and readiness point at **different** paths, and that is the point:

| Probe | Path | Question it answers |
| --- | --- | --- |
| `livenessProbe` | `/api/v1/health` | Is the process running? Returns 200 unconditionally — a pod that is alive but misconfigured must not be restarted, because a restart fixes nothing. |
| `readinessProbe` | `/api/v1/ready` | Can this instance serve a mock? Verifies agent fixtures are loaded and the interaction-log store answers; returns 503 naming the failing dependency otherwise. |

Both paths are overridable via `probes.liveness.path` / `probes.readiness.path`.
Neither requires credentials, even in multi-tenant mode — a kubelet has no API
key, and the readiness body carries no agent, tenant, or configuration data.

## What's still deferred

- Cluster-tier RBAC + admission controls — bring your own cluster
  defaults. Tenancy secrets go in a Secret referenced by `existingSecret`,
  never in a ConfigMap.
- Shared interaction/audit logs across replicas: both stores are SQLite
  per pod today; only tenancy (and the spend ledger) can be shared, via
  `MOCKAGENTS_TENANCY_DSN`.
