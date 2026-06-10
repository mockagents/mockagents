# Testing MockAgents with the Acme Support demo (Claude Agent SDK)

This guide tests **MockAgents** end-to-end using the Claude demo over the
**Anthropic Messages API**, covering all four feature areas:

1. **Scenarios + tool calls** — the agentic tool-calling loop.
2. **Chaos / fault injection** — retry/backoff against injected 503s.
3. **Streaming + load-target latency** — Anthropic SSE deltas + free load testing.
4. **Multi-tenancy + quotas + cost** — per-tenant keys, rate/spend caps, costs.

Documented for two deployment paths, **separately**:

- [Path A — Docker Compose](#path-a--docker-compose)
- [Path B — Kubernetes / Helm](#path-b--kubernetes--helm)

The feature walkthroughs run against `http://localhost:8080`; both paths get you there.

> **Two clients, on purpose.** `triage_demo` uses the **Claude Agent SDK**, which
> drives the `claude` CLI subprocess. The other scripts (`deterministic_smoke`,
> `streaming_demo`, `resilience_demo`) use the **plain Anthropic SDK** — no CLI
> needed — so you can verify the wire contract even without Node installed.

---

## Path A — Docker Compose

**Prerequisites:** Docker + Docker Compose v2. Run from
`demo/customer-support-agent-claude/`. (The demo image bundles Node + the `claude`
CLI, so you don't need them on the host for this path.)

### A1. Start MockAgents

```bash
docker compose up --build -d mockagents
docker compose logs -f mockagents          # watch it load the 3 agents; Ctrl-C to detach
```

Verify:

```bash
curl -s http://localhost:8080/api/v1/health
curl -s http://localhost:8080/v1/models    # claude-3-5-sonnet-latest, -20241022, claude-3-sonnet-20240229
```

### A2. Run the demo app

```bash
docker compose run --rm demo                                  # triage (Agent SDK, default)
docker compose run --rm demo python -m app.deterministic_smoke
docker compose run --rm demo python -m app.streaming_demo
docker compose run --rm demo python -m app.resilience_demo
```

Then jump to [§ Feature tests](#feature-tests).

### A3. Tear down

```bash
docker compose --profile demo down -v
```

---

## Path B — Kubernetes / Helm

**Prerequisites:** a cluster (kind/minikube/k3s/real), `kubectl`, `helm`. The chart
is at `deploy/helm/mockagents`. Run from `demo/customer-support-agent-claude/k8s/`.

### B1. Build images the cluster can pull

```bash
# Repo root — MockAgents server image:
docker build -t mockagents/mockagents:demo .
# demo/customer-support-agent-claude — the demo app image (Python + Node + claude CLI):
docker build -t acme-claude-support-demo:latest .
```

Load into a local cluster (skip if pushing to a registry):

```bash
kind load docker-image mockagents/mockagents:demo
kind load docker-image acme-claude-support-demo:latest
# minikube:  minikube image load <image>
```

### B2. Namespace + agents ConfigMap

```bash
kubectl create namespace acme-claude
kubectl create configmap acme-claude-agents --from-file=../mockagents/ -n acme-claude
```

### B3. Install MockAgents

```bash
helm install acme ../../../deploy/helm/mockagents \
  -n acme-claude -f mockagents-values.yaml \
  --set image.repository=mockagents/mockagents --set image.tag=demo

kubectl -n acme-claude rollout status deploy/acme-mockagents
```

### B4. Reach the server

```bash
kubectl -n acme-claude port-forward svc/acme-mockagents 8080:8080
# http://localhost:8080 is now the server — run the feature tests below.
```

### B5. Run the demo app in-cluster

```bash
kubectl apply -f demo-job.yaml -n acme-claude
kubectl logs -f job/acme-claude-demo -n acme-claude
```

### B6. Tear down

```bash
helm uninstall acme -n acme-claude
kubectl delete namespace acme-claude
```

---

## Feature tests

Run against `http://localhost:8080` (Compose port, or the port-forward from B4).
Convenience: `export BASE=http://localhost:8080`.

> Local (non-Docker) runs of `triage_demo` need the `claude` CLI on PATH
> (`npm i -g @anthropic-ai/claude-code`) plus `pip install -r requirements.txt`.
> The other scripts only need `pip install -r requirements.txt`.

### 1. Scenarios + tool calls

**Via the agent app** (real agentic loop through the `claude` CLI):

```bash
docker compose run --rm demo python -m app.triage_demo      # or: python -m app.triage_demo
```

Four conversations resolve: greeting (1 turn), order lookup, refund, escalation
(each 2 turns — `tool_use` then the final answer). The tool calls show as
`mcp__acme__lookup_order`, etc.

**Framework-free contract check** (plain Anthropic SDK, asserts the wire contract):

```bash
python -m app.deterministic_smoke
# turn 1: stop_reason=tool_use   (mock returns mcp__acme__lookup_order)
# turn 2: stop_reason=end_turn   (mock returns the final answer)
# SMOKE PASSED ✅
```

**Raw curl** (also in `scripts/smoke.sh`):

```bash
# Turn 1 — expect "stop_reason":"tool_use" and a mcp__acme__lookup_order block:
curl -s $BASE/v1/messages \
  -H 'x-api-key: mock-key' -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
  -d '{"model":"claude-3-5-sonnet-latest","max_tokens":256,"messages":[{"role":"user","content":"where is my order"}]}'

# Turn 2 — send the tool_result back, expect "stop_reason":"end_turn":
curl -s $BASE/v1/messages \
  -H 'x-api-key: mock-key' -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
  -d '{"model":"claude-3-5-sonnet-latest","max_tokens":256,"messages":[
    {"role":"user","content":"where is my order"},
    {"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"mcp__acme__lookup_order","input":{"order_id":"ORD-12345"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"ORDER_RESULT shipped"}]}]}]}'
```

### 2. Chaos / fault injection

The flaky agent (model `claude-3-5-sonnet-20241022`) 503s its first 2 requests then recovers.

```bash
docker compose run --rm demo python -m app.resilience_demo   # or: python -m app.resilience_demo
```

Expected:

```
attempt 1: 503 (retry-after=-) — backing off 0.5s
attempt 2: 503 (retry-after=-) — backing off 1.0s
attempt 3: 200 OK -> Recovered — Acme support is healthy again. ...
Recovered. Retry/backoff worked.
```

> The fail-first counter is **per-agent and resets on server restart**. Re-arm:
> `docker compose restart mockagents` (A) or
> `kubectl -n acme-claude rollout restart deploy/acme-mockagents` (B).

### 3. Streaming + load-target latency

**Streaming deltas** (plain Anthropic SDK):

```bash
docker compose run --rm demo python -m app.streaming_demo    # or: python -m app.streaming_demo
```

Raw SSE:

```bash
curl -N -s $BASE/v1/messages \
  -H 'x-api-key: mock-key' -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
  -d '{"model":"claude-3-5-sonnet-latest","max_tokens":256,"stream":true,"messages":[{"role":"user","content":"hello"}]}'
# event: message_start / content_block_delta / message_stop ...
```

**Free load testing** against the load-target agent (`claude-3-sonnet-20240229`):

```bash
# Install k6: https://grafana.com/docs/k6/latest/set-up/install-k6/
k6 run scripts/k6-loadtest.js
k6 run -e BASE=$BASE -e VUS=50 -e DURATION=1m scripts/k6-loadtest.js
```

### 4. Multi-tenancy + quotas + cost

Requires MockAgents in **multi-tenant mode**.

**Enable it:**

- *Path A (Docker):* uncomment the `MOCKAGENTS_MULTI_TENANT` block in
  `docker-compose.yml` under `mockagents`, then `docker compose up -d --force-recreate mockagents`.
- *Path B (K8s):* uncomment the `env:` block in `mockagents-values.yaml`, then
  `helm upgrade acme ../../../deploy/helm/mockagents -n acme-claude -f mockagents-values.yaml`.

**Grab the bootstrap platform key** (printed once on first start):

```bash
docker compose logs mockagents | grep "Bootstrap admin key"                       # Path A
kubectl -n acme-claude logs deploy/acme-mockagents | grep "Bootstrap admin key"   # Path B
```

**Run the walkthrough** (needs `jq`):

```bash
BOOTSTRAP_KEY=<the-key-from-logs> ./scripts/multitenant_walkthrough.sh
```

It mints an app key on the bootstrap key's `default` tenant, sets a tiny quota
(1 req/s, burst 2, $0.001/month), bursts `/v1/messages` traffic to trip **429
(rate)** / **402 (spend)**, then reads quota usage and the cost aggregate. (It also
creates a second tenant `acme` to show the platform capability; a new tenant's
first key is provisioned out of band — a platform key can only mint keys for its
own tenant.)

> Costs are non-zero because the demo uses real, priced Anthropic model names —
> so the cost dashboard and the **402 spend cap** work without a custom price file.

---

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `triage_demo` can't start the CLI | The `claude` CLI isn't on PATH. `npm i -g @anthropic-ai/claude-code` (or use the Docker path, which bundles it). |
| Every query routes to `lookup_order` | You're running *inside* Claude Code, which injects this session's skills into the user message. `mock_setup.py` already blanks the triggering env vars + sets `setting_sources=[]`; ensure you didn't override `env`. |
| `400 ... empty user message` on the tool turn | You're on an old MockAgents without the `tool_result`-as-array fix. Rebuild from this repo. |
| `400 ... cannot unmarshal array into ... system` | Old MockAgents without the `system`-as-array fix. Rebuild from this repo. |
| `404 ... agent not found` | The request `model` matches no agent. Use `claude-3-5-sonnet-latest` (triage), `claude-3-5-sonnet-20241022` (flaky), or `claude-3-sonnet-20240229` (load); check `GET /v1/models`. |
| `401` from `/v1/messages` | MockAgents requires `x-api-key` (or `Authorization: Bearer`). Set `MOCKAGENTS_API_KEY` / `ANTHROPIC_API_KEY`. |
| Resilience demo never 503s | The fail-first counter already advanced. Restart MockAgents. |
| Base URL confusion | For the Anthropic surface, `MOCKAGENTS_BASE_URL` is the server **root** (no `/v1`) — the CLI and SDK append `/v1/messages`. |
