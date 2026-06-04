# Deploying MockAgents on a K3s Homelab

A self-contained script suite under `homelabsetup/` that provisions a K3s
cluster on your homelab nodes and deploys MockAgents on it via the bundled
Helm chart (`deploy/helm/mockagents`). MockAgents is a single Go binary with an
embedded pure-Go SQLite store — there is **no external database, cache, or
object store** to stand up, so the footprint is tiny.

> **Scope.** These scripts deploy the **MockAgents server** (the OpenAI /
> Anthropic / MCP mock API). The Next.js web console (`gui/`) has no container
> image yet; run it locally with `npm run dev` pointed at the cluster
> (`MOCKAGENTS_API_URL=http://mockagents.local`) if you want the UI.

---

## Architecture on the Homelab

```
Dev PC (kubectl + docker)
    │
    ▼  http://mockagents.local  →  Traefik (K3s built-in)  →  MetalLB VIP .200
    │
┌───┴───────────────────────────────────────────────────────┐
│  K3s Cluster — Namespace: mockagents                       │
│                                                            │
│    ┌──────────────────────────────────────────────────┐   │
│    │  mockagents Deployment (Go binary, :8080)         │   │
│    │  - agents mounted read-only from a ConfigMap      │   │
│    │  - SQLite interaction log (emptyDir or PVC)       │   │
│    └──────────────────────────────────────────────────┘   │
│                                                            │
│  In-cluster registry (registry:2, NodePort 30500)          │
│                                                            │
│  Node 1 (.101)        Node 2 (.102)        Node 3 (.103)   │
│  control-plane        worker               worker          │
└────────────────────────────────────────────────────────────┘
```

**Resource budget:** the binary idles under ~30 MiB; the Helm defaults request
`50m / 64Mi` and cap at `500m / 256Mi`. A single small node is plenty.

---

## Prerequisites

On the **dev machine** (Git Bash on Windows, or any bash): `kubectl`, `helm`,
`docker`, `ssh`, `curl`. On the **nodes**: a fresh Linux install reachable over
SSH with passwordless `sudo` (the bootstrap script can configure it).

Set your homelab specifics via env (or edit the config block at the top of each
script — they are kept in lockstep):

```bash
export HOMELAB_NODES="192.168.0.101 192.168.0.102 192.168.0.103"  # control first
export HOMELAB_NODE_USER="labadmin"
export APP_HOST="mockagents.local"                                 # ingress host
```

### Configure your dev Docker for the insecure registry

The in-cluster registry serves plain HTTP. Trust it so `docker push` works.
Add to your Docker daemon config (`/etc/docker/daemon.json`, or Rancher
Desktop's provisioning script) and restart Docker:

```json
{ "insecure-registries": ["registry.local:30500", "192.168.0.101:30500"] }
```

### Hosts file

Add to your hosts file (`C:\Windows\System32\drivers\etc\hosts` or `/etc/hosts`):

```
192.168.0.101 registry.local k3s-node1
192.168.0.102 k3s-node2
192.168.0.103 k3s-node3
192.168.0.200 mockagents.local
```

> `mockagents.local` points at the MetalLB VIP (the Traefik LoadBalancer IP),
> **not** a node IP. `deploy-homelab.sh` prints the exact line to add.

---

## One-shot path

```bash
# Provision K3s + MetalLB + registry, then deploy MockAgents in one go:
./homelabsetup/bootstrap-homelab.sh --deploy
```

Or run the two phases separately:

```bash
export KUBECONFIG="$HOME/.kube/config-mockagents-homelab"
./homelabsetup/bootstrap-homelab.sh      # cluster
./homelabsetup/deploy-homelab.sh         # app
```

---

## Scripts

| Script | Purpose |
|--------|---------|
| `bootstrap-homelab.sh` | Install K3s (server + agents), MetalLB, and the in-cluster registry + containerd mirror. Run once per cluster. |
| `deploy-homelab.sh` | Build & push the image, render `examples/` into a ConfigMap, `helm upgrade --install`, verify, print URLs. |
| `fresh-deploy-homelab.sh` | Clean redeploy: cleanup → deploy (keeps the cluster + registry). |
| `stop-homelab.sh` | Scale to 0 and stop K3s (preserves PVCs). Pairs with restart. |
| `restart-homelab.sh` | Start K3s and scale back to the pre-shutdown replica count. |
| `cleanup-homelab.sh` | Uninstall the release / delete the namespace; `--all` also tears down the registry. |

`.homelab-credentials` is **generated at deploy time** (gitignored) and holds
the app URL, image tag, and — in multi-tenant mode — the bootstrap admin key.

---

## Deploy steps (what `deploy-homelab.sh` does)

| Step | Action |
|------|--------|
| 0 | Preflight — `kubectl`/`helm`/`docker`, cluster + registry reachable. |
| 1 | Build `mockagents:build-<UTC-timestamp>` and push to the in-cluster registry (immutable tag, never `:latest`). `--skip-build` reuses the newest existing tag. |
| 2 | Create the `mockagents` namespace. |
| 3 | Render `examples/*.yaml` into the `mockagents-agents` ConfigMap (mounted read-only at `/agents`). |
| 4 | `helm upgrade --install` with the registry image, the agents ConfigMap, and a Traefik ingress on `APP_HOST`. `--persist` adds a PVC for the SQLite log; `--multi-tenant` sets `MOCKAGENTS_MULTI_TENANT=1`. |
| 5 | In multi-tenant mode, scrape the **bootstrap admin key** from the pod log. |
| 6 | Print the `/etc/hosts` line for `APP_HOST` → Traefik VIP. |
| 7 | `curl -H "Host: APP_HOST" .../api/v1/health` through Traefik. |

### Common flags

```bash
./homelabsetup/deploy-homelab.sh --multi-tenant      # API-key auth + RBAC
./homelabsetup/deploy-homelab.sh --persist           # keep the SQLite log on a PVC
./homelabsetup/deploy-homelab.sh --skip-build        # redeploy newest image, no rebuild
./homelabsetup/deploy-homelab.sh --teardown          # helm uninstall (keep ns/PVC)
./homelabsetup/deploy-homelab.sh --teardown-all      # delete namespace (DESTRUCTIVE)
```

---

## Verify

```bash
kubectl -n mockagents get pods
TRAEFIK_IP=$(kubectl -n kube-system get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Health
curl -H "Host: mockagents.local" "http://$TRAEFIK_IP/api/v1/health"

# Drop-in OpenAI call
curl -H "Host: mockagents.local" -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}' \
  "http://$TRAEFIK_IP/v1/chat/completions"
```

Once the hosts entry is in place you can use `http://mockagents.local` directly
as the `base_url` for any OpenAI/Anthropic SDK.

---

## Updating the app

```bash
# Rebuild, push a fresh immutable tag, and roll the deployment:
./homelabsetup/deploy-homelab.sh

# Or redeploy the most recent image without rebuilding:
./homelabsetup/deploy-homelab.sh --skip-build
```

Because every build uses a unique `build-<timestamp>` tag (never `:latest`),
K3s always pulls the new content — no `crictl rmi` cache dance needed.

---

## Pause / Resume

```bash
./homelabsetup/stop-homelab.sh        # scale to 0, then stop K3s (PVCs preserved)
./homelabsetup/restart-homelab.sh     # start K3s, scale back, health-probe

# Faster pause that leaves K3s running:
./homelabsetup/stop-homelab.sh --apps-only
./homelabsetup/restart-homelab.sh --apps-only
```

Both accept `--dry-run` (print every `kubectl`/`ssh` without running it) and
`--no-ssh` (scale only; you manage K3s by hand).

---

## Tear down

```bash
./homelabsetup/cleanup-homelab.sh                # uninstall + delete the namespace
./homelabsetup/cleanup-homelab.sh --keep-data    # uninstall but keep ns + PVCs
./homelabsetup/cleanup-homelab.sh --all          # also remove the registry + node images
./homelabsetup/bootstrap-homelab.sh --teardown   # uninstall K3s on every node
```

---

## Troubleshooting

**`docker push` → `http: server gave HTTP response to HTTPS client`** — the
insecure-registry config isn't applied. Add `registry.local:30500` to
`insecure-registries` and restart Docker (see Prerequisites).

**Pod `ErrImagePull` from `registry.local:5000`** — a node is missing the
containerd mirror. Re-run `bootstrap-homelab.sh` (it writes
`/etc/rancher/k3s/registries.yaml` on every node) or check:
`ssh <user>@<node> cat /etc/rancher/k3s/registries.yaml`.

**Server exits `no valid agent definitions found`** — the agents ConfigMap is
empty. Confirm `examples/*.yaml` exist and re-run Step 3 (the deploy script
recreates the ConfigMap each run).

**`mockagents.local` doesn't resolve / no Traefik IP** — MetalLB didn't assign
a VIP. Ensure bootstrap ran **without** `--skip-prereqs`, then
`kubectl -n kube-system get svc traefik` should show an EXTERNAL-IP in the
MetalLB range. Add that IP to your hosts file. (ICMP `ping` to a MetalLB VIP
often fails even when HTTP works — test with `curl`, not `ping`.)

**Where's the multi-tenant admin key?** Printed once at deploy time and saved
to `homelabsetup/.homelab-credentials`. To re-read it from a running pod:
`kubectl -n mockagents logs deployment/mockagents | grep mak_`.
