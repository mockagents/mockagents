# Testing MockAgents with the Acme Support demo (Google ADK)

This guide tests **MockAgents** end-to-end using the Google ADK demo, covering all
four feature areas:

1. **Scenarios + tool calls** — the agentic tool-calling loop.
2. **Chaos / fault injection** — retry/backoff against injected 503s.
3. **Streaming + load-target latency** — Gemini SSE deltas + free load testing.
4. **Multi-tenancy + quotas + cost** — per-tenant keys, rate/spend caps, costs.

Documented for two deployment paths, **separately**:

- [Path A — Docker Compose](#path-a--docker-compose)
- [Path B — Kubernetes / Helm](#path-b--kubernetes--helm)

…and two ADK backends: **native Gemini** (primary) and the **LiteLLM bridge**
(alternative, OpenAI wire format). ADK is pure Python — no extra CLI to install.

The feature walkthroughs run against `http://localhost:8080`; both paths get you there.

---

## Path A — Docker Compose

**Prerequisites:** Docker + Docker Compose v2. Run from
`demo/customer-support-agent-google-adk/`.

### A1. Start MockAgents

```bash
docker compose up --build -d mockagents
docker compose logs -f mockagents          # watch it load the 4 agents; Ctrl-C to detach
```

Verify:

```bash
curl -s http://localhost:8080/api/v1/health
curl -s http://localhost:8080/v1/models    # gemini-2.0-flash, gemini-2.5-flash, gemini-1.5-flash, gpt-4o
```

### A2. Run the demo app

```bash
docker compose run --rm demo                                  # native-Gemini triage (default)
docker compose run --rm demo python -m app.litellm_demo       # LiteLLM bridge
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

**Prerequisites:** a cluster (kind/minikube/k3s/real), `kubectl`, `helm`. Chart at
`deploy/helm/mockagents`. Run from `demo/customer-support-agent-google-adk/k8s/`.

### B1. Build images the cluster can pull

```bash
# Repo root — MockAgents server image:
docker build -t mockagents/mockagents:demo .
# demo/customer-support-agent-google-adk — the demo app image:
docker build -t acme-adk-support-demo:latest .
```

Load into a local cluster (skip if pushing to a registry):

```bash
kind load docker-image mockagents/mockagents:demo
kind load docker-image acme-adk-support-demo:latest
# minikube:  minikube image load <image>
```

### B2. Namespace + agents ConfigMap

```bash
kubectl create namespace acme-adk
kubectl create configmap acme-adk-agents --from-file=../mockagents/ -n acme-adk
```

### B3. Install MockAgents

```bash
helm install acme ../../../deploy/helm/mockagents \
  -n acme-adk -f mockagents-values.yaml \
  --set image.repository=mockagents/mockagents --set image.tag=demo

kubectl -n acme-adk rollout status deploy/acme-mockagents
```

### B4. Reach the server

```bash
kubectl -n acme-adk port-forward svc/acme-mockagents 8080:8080
```

### B5. Run the demo app in-cluster

```bash
kubectl apply -f demo-job.yaml -n acme-adk
kubectl logs -f job/acme-adk-demo -n acme-adk
```

### B6. Tear down

```bash
helm uninstall acme -n acme-adk
kubectl delete namespace acme-adk
```

---

## Feature tests

Run against `http://localhost:8080`. Convenience: `export BASE=http://localhost:8080`.

> Local (non-Docker) runs: `pip install -r requirements.txt`, then
> `export MOCKAGENTS_BASE_URL=http://localhost:8080` (server root, no `/v1`).

### 1. Scenarios + tool calls

**Native Gemini** (the headline agentic loop):

```bash
docker compose run --rm demo python -m app.triage_demo      # or: python -m app.triage_demo
```

Four conversations resolve: greeting (1 turn), order lookup, refund, escalation
(each 2 turns — a `functionCall` then the final answer).

**LiteLLM bridge** (same agent, OpenAI surface):

```bash
docker compose run --rm demo python -m app.litellm_demo     # or: python -m app.litellm_demo
```

**Framework-free contract check** (raw Gemini HTTP):

```bash
python -m app.deterministic_smoke
# turn 1: functionCall=lookup_order ...
# turn 2: finishReason=STOP
# SMOKE PASSED ✅
```

**Raw curl** (also in `scripts/smoke.sh`):

```bash
# Turn 1 — expect a functionCall part for lookup_order:
curl -s "$BASE/v1beta/models/gemini-2.0-flash:generateContent" \
  -H 'x-goog-api-key: mock-key' -H 'content-type: application/json' \
  -d '{"contents":[{"role":"user","parts":[{"text":"where is my order"}]}],
       "tools":[{"functionDeclarations":[{"name":"lookup_order","parameters":{"type":"object","properties":{"order_id":{"type":"string"}}}}]}]}'

# Turn 2 — send the functionResponse back, expect finishReason STOP:
curl -s "$BASE/v1beta/models/gemini-2.0-flash:generateContent" \
  -H 'x-goog-api-key: mock-key' -H 'content-type: application/json' \
  -d '{"contents":[
    {"role":"user","parts":[{"text":"where is my order"}]},
    {"role":"model","parts":[{"functionCall":{"name":"lookup_order","args":{"order_id":"ORD-12345"}}}]},
    {"role":"user","parts":[{"functionResponse":{"name":"lookup_order","response":{"ORDER_RESULT":true,"status":"shipped"}}}]}]}'
```

### 2. Chaos / fault injection

The flaky agent (model `gemini-2.5-flash`) 503s its first 2 requests then recovers.

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
> `kubectl -n acme-adk rollout restart deploy/acme-mockagents` (B).

### 3. Streaming + load-target latency

**Streaming deltas** (genai client):

```bash
docker compose run --rm demo python -m app.streaming_demo    # or: python -m app.streaming_demo
```

Raw SSE:

```bash
curl -N -s "$BASE/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H 'x-goog-api-key: mock-key' -H 'content-type: application/json' \
  -d '{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}'
```

**Free load testing** against the load-target agent (`gemini-1.5-flash`):

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
  `helm upgrade acme ../../../deploy/helm/mockagents -n acme-adk -f mockagents-values.yaml`.

**Grab the bootstrap platform key** (printed once on first start):

```bash
docker compose logs mockagents | grep "Bootstrap admin key"                  # Path A
kubectl -n acme-adk logs deploy/acme-mockagents | grep "Bootstrap admin key" # Path B
```

**Run the walkthrough** (needs `jq`):

```bash
BOOTSTRAP_KEY=<the-key-from-logs> ./scripts/multitenant_walkthrough.sh
```

It mints an app key on the bootstrap key's `default` tenant, sets a tiny quota
(2 req/s, burst 3, $0.00003/month — Gemini is cheap), bursts `generateContent`
traffic to trip **429 (rate)** / **402 (spend)**, then reads quota usage and the
cost aggregate.

> **Gemini auth gotcha:** pass the tenant key as **`Authorization: Bearer <key>`**,
> not the genai client's usual `x-goog-api-key` header — MockAgents resolves the
> tenant from the Authorization/X-Api-Key header. The walkthrough script does this.
> Costs are non-zero because the demo uses real, priced Gemini model names.

---

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `404 ... agent not found` | The request `model` matches no agent. Use `gemini-2.0-flash` (triage), `gemini-2.5-flash` (flaky), `gemini-1.5-flash` (load), or `gpt-4o` (LiteLLM bridge); check `GET /v1/models`. |
| ADK calls the real Gemini API | `GOOGLE_GEMINI_BASE_URL` isn't set (or set after the client was built). `app/mock_setup.py` sets it; ensure `configure_gemini_env()` runs first. |
| Multi-tenant quota/cost shows nothing for Gemini | You passed the key as `x-goog-api-key`. Use `Authorization: Bearer` so the tenant resolves (the genai key header isn't used for tenant resolution). |
| `/v1/costs` shows `(unknown)` / zero for Gemini | You're on an old MockAgents without the Gemini cost-extractor / pricing / loggable-path fixes. Rebuild from this repo. |
| Resilience demo never 503s | The fail-first counter already advanced. Restart MockAgents. |
| LiteLLM bridge loops / wrong scenario | The OpenAI fixture is `turn_number`-gated; `app/litellm_demo.py` sets a per-conversation `X-Session-Id` via LiteLLM `extra_headers` — keep that. |
| Base URL confusion | `MOCKAGENTS_BASE_URL` is the server **root** (no `/v1`). Native Gemini uses it directly; the LiteLLM bridge appends `/v1`. |
