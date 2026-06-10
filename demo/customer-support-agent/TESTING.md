# Testing MockAgents with the Acme Support demo

This guide walks through testing **MockAgents** end-to-end using the demo app,
covering all four feature areas:

1. **Scenarios + tool calls** — the agentic tool-calling loop.
2. **Chaos / fault injection** — retry/backoff against injected 503s.
3. **Streaming + load-target latency** — SSE deltas and free load testing.
4. **Multi-tenancy + quotas + cost** — per-tenant keys, rate/spend caps, costs.

It is documented for two deployment paths, **separately**:

- [Path A — Docker Compose](#path-a--docker-compose)
- [Path B — Kubernetes / Helm](#path-b--kubernetes--helm)

The feature walkthroughs ([§ Feature tests](#feature-tests)) are written against
`http://localhost:8080`; both paths tell you how to get there.

---

## Path A — Docker Compose

**Prerequisites:** Docker + Docker Compose v2. Run everything from
`demo/customer-support-agent/`.

### A1. Start MockAgents

```bash
docker compose up --build -d mockagents
docker compose logs -f mockagents        # watch it load the 3 agents, Ctrl-C to detach
```

Verify it's serving and sees all three agents:

```bash
curl -s http://localhost:8080/api/v1/health
curl -s http://localhost:8080/v1/models   # expect gpt-4o, gpt-4o-mini, gpt-3.5-turbo
```

### A2. Run the demo app

The `demo` service is a one-shot runner — use `docker compose run`:

```bash
docker compose run --rm demo                                  # triage demo (default)
docker compose run --rm demo python -m app.deterministic_smoke
docker compose run --rm demo python -m app.streaming_demo
docker compose run --rm demo python -m app.resilience_demo
```

Now jump to [§ Feature tests](#feature-tests) — the curl/script commands there run
against the published port `localhost:8080`.

### A3. Tear down

```bash
docker compose --profile demo down -v
```

---

## Path B — Kubernetes / Helm

**Prerequisites:** a cluster (kind, minikube, k3s, or real), `kubectl`, and `helm`.
The existing chart lives at `deploy/helm/mockagents`. Run from
`demo/customer-support-agent/k8s/`.

### B1. Build images the cluster can pull

```bash
# From the repo root — MockAgents server image:
docker build -t mockagents/mockagents:demo .

# From demo/customer-support-agent — the demo app image:
docker build -t acme-support-demo:latest .
```

Load them into a local cluster (skip if you push to a registry the cluster pulls from):

```bash
# kind:
kind load docker-image mockagents/mockagents:demo
kind load docker-image acme-support-demo:latest
# minikube:  minikube image load <image>
# k3s:       sudo k3s ctr images import <(docker save <image>)
```

### B2. Create the namespace and the agents ConfigMap

The chart reads agent YAML from a ConfigMap. Build it from the demo's
`mockagents/` directory (single source of truth):

```bash
kubectl create namespace acme-demo
kubectl create configmap acme-agents --from-file=../mockagents/ -n acme-demo
```

### B3. Install MockAgents via Helm

```bash
helm install acme ../../../deploy/helm/mockagents \
  -n acme-demo \
  -f mockagents-values.yaml \
  --set image.repository=mockagents/mockagents \
  --set image.tag=demo

kubectl -n acme-demo rollout status deploy/acme-mockagents
```

> `mockagents-values.yaml` sets `agents.existingConfigMap: acme-agents`, a
> NodePort on `30080`, and enables the interaction-log volume (for `/costs`).

### B4. Reach the server

```bash
# Port-forward (works on any cluster):
kubectl -n acme-demo port-forward svc/acme-mockagents 8080:8080
# now http://localhost:8080 is the server — run the feature tests below.
```

(On kind/minikube/k3s you can alternatively hit the NodePort `30080` on the node IP.)

### B5. Run the demo app in-cluster

```bash
kubectl apply -f demo-job.yaml -n acme-demo
kubectl logs -f job/acme-demo -n acme-demo
```

Override the entrypoint to run a different scenario by editing `command:` in
`demo-job.yaml` (e.g. `["python","-m","app.streaming_demo"]`).

### B6. Tear down

```bash
helm uninstall acme -n acme-demo
kubectl delete namespace acme-demo
```

---

## Feature tests

These run against `http://localhost:8080` (Compose published port, or the
port-forward from B4). Convenience: `export BASE=http://localhost:8080`.

### 1. Scenarios + tool calls

**Via the agent app** (the real agentic loop):

```bash
# Docker:
docker compose run --rm demo python -m app.triage_demo
# Local:
python -m app.triage_demo
```

You should see four conversations resolve: greeting (1 turn), order lookup,
refund, and escalation (each 2 turns — tool call then final answer).

**Framework-free contract check** (asserts the wire contract):

```bash
python -m app.deterministic_smoke
# turn 1: finish_reason=tool_calls  (mock returns lookup_order)
# turn 2: finish_reason=stop        (mock returns the final answer)
# SMOKE PASSED ✅
```

**Raw curl** (also in `scripts/smoke.sh`):

```bash
# Turn 1 — expect "finish_reason":"tool_calls" and a lookup_order call:
curl -s $BASE/v1/chat/completions -H 'content-type: application/json' \
  -H 'x-session-id: demo-1' \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"where is my order"}]}'

# Same session, turn 2 (send the tool result back) — expect "finish_reason":"stop":
curl -s $BASE/v1/chat/completions -H 'content-type: application/json' \
  -H 'x-session-id: demo-1' \
  -d '{"model":"gpt-4o","messages":[
    {"role":"user","content":"where is my order"},
    {"role":"assistant","content":null,"tool_calls":[{"id":"call_0","type":"function","function":{"name":"lookup_order","arguments":"{\"order_id\":\"ORD-12345\"}"}}]},
    {"role":"tool","tool_call_id":"call_0","content":"{\"status\":\"shipped\"}"}]}'
```

Inspect what the server matched:

```bash
curl -s "$BASE/api/v1/logs?limit=10"     # scenario + agent per interaction
```

### 2. Chaos / fault injection

The flaky agent (model `gpt-4o-mini`) 503s its first 2 requests then recovers.

```bash
# Docker:
docker compose run --rm demo python -m app.resilience_demo
# Local:
python -m app.resilience_demo
```

Expected:

```
attempt 1: 503 (retry-after=-) — backing off 0.5s
attempt 2: 503 (retry-after=-) — backing off 1.0s
attempt 3: 200 OK -> Recovered — Acme support is healthy again. ...
Recovered. Retry/backoff worked.
```

> The fail-first counter is **per-agent and resets on server restart**. To
> re-arm: `docker compose restart mockagents` (Path A) or
> `kubectl -n acme-demo rollout restart deploy/acme-mockagents` (Path B).

Quick curl version:

```bash
for i in 1 2 3; do
  curl -s -o /dev/null -w "attempt $i: %{http_code}\n" $BASE/v1/chat/completions \
    -H 'content-type: application/json' \
    -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
done
```

### 3. Streaming + load-target latency

**Streaming deltas** through the agent app:

```bash
docker compose run --rm demo python -m app.streaming_demo    # or: python -m app.streaming_demo
```

Raw SSE:

```bash
curl -N -s $BASE/v1/chat/completions -H 'content-type: application/json' \
  -d '{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}'
```

**Free load testing** against the load-target agent (model `gpt-3.5-turbo`, realistic p50/p95 TTFT/ITL):

```bash
# Install k6: https://grafana.com/docs/k6/latest/set-up/install-k6/
k6 run scripts/k6-loadtest.js
k6 run -e BASE=$BASE -e VUS=50 -e DURATION=1m scripts/k6-loadtest.js
```

You're load-testing your client's streaming path with production-like timing and
no token bill.

### 4. Multi-tenancy + quotas + cost

This requires MockAgents in **multi-tenant mode**.

**Enable it:**

- *Path A (Docker):* uncomment the `MOCKAGENTS_MULTI_TENANT` block in
  `docker-compose.yml` under the `mockagents` service, then
  `docker compose up -d --force-recreate mockagents`.
- *Path B (K8s):* uncomment the `env:` block in `mockagents-values.yaml`, then
  `helm upgrade acme ../../../deploy/helm/mockagents -n acme-demo -f mockagents-values.yaml`.

**Grab the bootstrap platform key** (printed once on first start):

```bash
# Path A:
docker compose logs mockagents | grep "Bootstrap admin key"
# Path B:
kubectl -n acme-demo logs deploy/acme-mockagents | grep "Bootstrap admin key"
```

**Run the walkthrough** (needs `jq`):

```bash
BOOTSTRAP_KEY=<the-key-from-logs> ./scripts/multitenant_walkthrough.sh
```

It mints an app API key on the bootstrap key's own `default` tenant, sets a tiny
quota (1 req/s, burst 2, $0.001/month), bursts traffic to trip **429 (rate)** /
**402 (spend)**, then reads the tenant's quota usage and cost aggregate. (It also
creates a second tenant `acme` to show the platform tenant-management capability;
note that a *new* tenant's first key is provisioned out of band — by design, a
platform key can only mint keys for its own tenant.)

> Costs are non-zero only for model names in MockAgents' price table — the demo
> agents use real names (`gpt-4o`, `gpt-4o-mini`, `gpt-3.5-turbo`) precisely so
> the cost dashboard and the **402 spend cap** work without a custom price file.
> (To price arbitrary names, point `MOCKAGENTS_PRICING` at a YAML override.)

Point the **agent app** at a tenant key to see quota/cost accrue from real agent
traffic:

```bash
MOCKAGENTS_API_KEY=<tenant-key> python -m app.triage_demo
curl -s $BASE/api/v1/quota  -H "authorization: Bearer <tenant-key>"
curl -s $BASE/api/v1/costs  -H "authorization: Bearer <tenant-key>"
```

---

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| Agent loop errors with "max turns exceeded" | The `X-Session-Id` header isn't reaching the server, so every request is turn 1 and the tool-call scenario fires forever. The demo sets it via `new_conversation_client()`; for raw calls, send a stable `X-Session-Id`. |
| `404 ... agent not found` | The request `model` doesn't match any agent. Use `gpt-4o` (triage), `gpt-4o-mini` (flaky), or `gpt-3.5-turbo` (load target); check `GET /v1/models`. |
| Agents SDK tries `/v1/responses` (404/400) | `set_default_openai_api("chat_completions")` wasn't called — `mock_setup.py` does this. |
| Resilience demo never 503s | The fail-first counter already advanced past 2. Restart MockAgents to reset. |
| `/api/v1/costs` shows nothing | Interaction logging needs the SQLite store. Compose mounts a volume; the Helm values enable `persistence`. Make some traffic first. |
| Multi-tenant 401s | You're in multi-tenant mode without a valid key. Use the bootstrap key (platform) or a minted tenant key as `Authorization: Bearer`. |
