# mockagents Helm chart

Deploy [MockAgents](https://github.com/mockagents/mockagents) on
Kubernetes. The chart runs the existing Docker image as a non-root
Deployment, exposes it via a Service, and mounts agent definitions from
a ConfigMap.

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
| `replicaCount`                       | Horizontal scale (mock server is read-mostly).           |
| `service.type`                       | `ClusterIP` (default), `NodePort`, or `LoadBalancer`.    |
| `ingress.enabled` + `ingress.hosts`  | Put an Ingress in front of the service.                  |
| `persistence.enabled`                | Mount a PVC at `/data` for the SQLite interaction log.   |
| `env.OTEL_EXPORTER_OTLP_ENDPOINT`    | Ship traces to an OTLP/HTTP collector.                   |
| `logLevel`                           | `debug`, `info`, `warn`, `error`.                        |
| `extraArgs`                          | Extra flags appended to `mockagents start`.              |

## Verify before installing

```bash
helm lint ./deploy/helm/mockagents
helm template demo ./deploy/helm/mockagents -f my-values.yaml | kubectl apply --dry-run=client -f -
```

## Uninstall

```bash
helm uninstall demo
```

## What's not in v0.1

- No HorizontalPodAutoscaler (straightforward to add via `autoscaling.enabled`).
- No NetworkPolicy (bring your own cluster defaults).
- No ServiceMonitor (OTel tracing is wired via env vars; a Prometheus
  metrics endpoint is a separate slice).
