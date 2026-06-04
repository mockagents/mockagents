#!/usr/bin/env bash
# =============================================================================
# bootstrap-homelab.sh — provision a K3s homelab cluster for MockAgents
#
# Brings a set of fresh Linux nodes (reachable over SSH) from nothing to a
# kubectl-ready K3s cluster, then optionally deploys MockAgents on top.
#
# What it does, in order:
#   1. Preflight: local tools (ssh, scp, kubectl, curl, helm), SSH key.
#   2. Distribute the SSH public key to every node (idempotent).
#   3. Ensure passwordless sudo on every node (probe; optionally configure).
#   4. Install K3s server on the control node, agents on the workers.
#   5. Fetch the kubeconfig to the dev machine.
#   6. Install MetalLB (L2 LoadBalancer) — unless --skip-prereqs.
#   7. Deploy an in-cluster image registry + configure the containerd mirror
#      on every node (so dev-machine `docker push` reaches the cluster).
#   8. Optionally hand off to deploy-homelab.sh (--deploy).
#
# Usage:
#   ./homelabsetup/bootstrap-homelab.sh [flags]
#
# Flags:
#   --deploy           After bootstrap, run deploy-homelab.sh.
#   --skip-prereqs     Install K3s + registry only (no MetalLB).
#   --configure-sudo   Auto-write /etc/sudoers.d/<user> for passwordless sudo.
#   --teardown         Uninstall K3s on every node (DESTRUCTIVE), then exit.
#   -h, --help         Show this help.
#
# Config via env (defaults in brackets):
#   HOMELAB_NODES        Space-separated node IPs  [192.168.0.101 .102 .103]
#   HOMELAB_NODE_USER    SSH user on the nodes      [labadmin]
#   HOMELAB_NODE{1,2,3}_PASS  Per-node password (else prompted once)
#   K3S_VERSION          Pinned K3s version         [v1.30.6+k3s1]
#   METALLB_RANGE        L2 address pool            [192.168.0.200-192.168.0.220]
# =============================================================================
set -euo pipefail

# --- shared conventions (kept in lockstep across the homelabsetup scripts) ---
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
log()    { echo -e "${GREEN}[+]${NC} $*"; }
warn()   { echo -e "${YELLOW}[!]${NC} $*"; }
error()  { echo -e "${RED}[x]${NC} $*" >&2; exit 1; }
header() { echo -e "\n${CYAN}━━━ $* ━━━${NC}\n"; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || error "$1 is required but not installed."; }

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# --- config -----------------------------------------------------------------
read -r -a NODES <<< "${HOMELAB_NODES:-192.168.0.101 192.168.0.102 192.168.0.103}"
NODE_USER="${HOMELAB_NODE_USER:-labadmin}"
CONTROL_NODE="${NODES[0]}"
WORKER_NODES=("${NODES[@]:1}")

K3S_VERSION="${K3S_VERSION:-v1.30.6+k3s1}"
METALLB_VERSION="${METALLB_VERSION:-v0.14.8}"
METALLB_RANGE="${METALLB_RANGE:-192.168.0.200-192.168.0.220}"

REGISTRY_HOST="registry.local"
REGISTRY_NODEPORT="30500"
REGISTRY_MIRROR="${REGISTRY_HOST}:5000"
LOCAL_KUBECONFIG="${HOME}/.kube/config-mockagents-homelab"
SSH_KEY="${HOME}/.ssh/id_ed25519"

DEPLOY=false; SKIP_PREREQS=false; CONFIGURE_SUDO=false; TEARDOWN=false

for arg in "$@"; do
  case "$arg" in
    --deploy)         DEPLOY=true ;;
    --skip-prereqs)   SKIP_PREREQS=true ;;
    --configure-sudo) CONFIGURE_SUDO=true ;;
    --teardown)       TEARDOWN=true ;;
    -h|--help)        sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)                error "Unknown flag: $arg (try --help)" ;;
  esac
done

# --- ssh helpers (key auth first, password fallback) ------------------------
get_password() { # idx -> echoes the node password from env or prompts once
  local idx="$1" var="HOMELAB_NODE$((idx + 1))_PASS"
  local val="${!var:-}"
  if [ -z "$val" ]; then read -rsp "Password for ${NODE_USER}@${NODES[$idx]}: " val; echo >&2; fi
  echo "$val"
}
nssh() { # idx cmd...
  local idx="$1"; shift; local node="${NODES[$idx]}"
  if ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 \
       "${NODE_USER}@${node}" true 2>/dev/null; then
    ssh -o StrictHostKeyChecking=accept-new "${NODE_USER}@${node}" "$@"
  elif command -v sshpass >/dev/null 2>&1; then
    SSHPASS="$(get_password "$idx")" sshpass -e ssh -o StrictHostKeyChecking=accept-new "${NODE_USER}@${node}" "$@"
  else
    warn "sshpass not found — using interactive ssh to ${node}"
    ssh -o StrictHostKeyChecking=accept-new "${NODE_USER}@${node}" "$@"
  fi
}

preflight() {
  header "Preflight"
  require_cmd ssh; require_cmd scp; require_cmd kubectl; require_cmd curl; require_cmd helm
  [ "${#NODES[@]}" -ge 1 ] || error "No nodes configured (set HOMELAB_NODES)."
  if [ ! -f "${SSH_KEY}.pub" ]; then
    log "Generating SSH key ${SSH_KEY}"
    ssh-keygen -t ed25519 -N "" -f "$SSH_KEY" >/dev/null
  fi
  log "Nodes: ${NODES[*]} (control=${CONTROL_NODE}, user=${NODE_USER})"
}

distribute_keys() {
  header "Distributing SSH key"
  local pub; pub="$(cat "${SSH_KEY}.pub")"
  for i in "${!NODES[@]}"; do
    nssh "$i" "mkdir -p ~/.ssh && chmod 700 ~/.ssh && grep -qxF '${pub}' ~/.ssh/authorized_keys 2>/dev/null || echo '${pub}' >> ~/.ssh/authorized_keys" \
      && log "key present on ${NODES[$i]}" || warn "could not push key to ${NODES[$i]}"
  done
}

ensure_passwordless_sudo() {
  header "Checking passwordless sudo"
  for i in "${!NODES[@]}"; do
    if nssh "$i" "sudo -n true" 2>/dev/null; then
      log "passwordless sudo OK on ${NODES[$i]}"
    elif $CONFIGURE_SUDO; then
      warn "configuring passwordless sudo on ${NODES[$i]}"
      nssh "$i" "echo '${NODE_USER} ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/${NODE_USER} >/dev/null && sudo chmod 440 /etc/sudoers.d/${NODE_USER}"
    else
      error "passwordless sudo not configured on ${NODES[$i]}. Re-run with --configure-sudo, or add: ${NODE_USER} ALL=(ALL) NOPASSWD:ALL"
    fi
  done
}

k3s_install_server() {
  header "Installing K3s server on ${CONTROL_NODE}"
  if nssh 0 "command -v k3s" >/dev/null 2>&1; then
    log "k3s already installed on control node"
  else
    nssh 0 "curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='${K3S_VERSION}' INSTALL_K3S_EXEC='server --disable=servicelb --node-name=$(node_hostname 0) --tls-san=${CONTROL_NODE}' sh -" \
      || error "k3s server install failed"
  fi
  log "waiting for the API server"
  local ok=false
  for _ in $(seq 1 30); do
    if nssh 0 "sudo k3s kubectl get nodes" >/dev/null 2>&1; then ok=true; break; fi
    sleep 2
  done
  $ok || error "K3s API did not come up on ${CONTROL_NODE}"
}
node_hostname() { echo "k3s-node$(( $1 + 1 ))"; }

k3s_install_agents() {
  [ "${#WORKER_NODES[@]}" -gt 0 ] || { log "single-node cluster — no agents"; return; }
  header "Installing K3s agents"
  local token; token="$(nssh 0 "sudo cat /var/lib/rancher/k3s/server/node-token")"
  for j in "${!WORKER_NODES[@]}"; do
    local idx=$(( j + 1 ))
    if nssh "$idx" "command -v k3s-agent || systemctl is-active k3s-agent" >/dev/null 2>&1; then
      log "agent already installed on ${WORKER_NODES[$j]}"; continue
    fi
    nssh "$idx" "curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='${K3S_VERSION}' K3S_URL='https://${CONTROL_NODE}:6443' K3S_TOKEN='${token}' INSTALL_K3S_EXEC='agent --node-name=$(node_hostname "$idx")' sh -" \
      && log "agent joined: ${WORKER_NODES[$j]}" || error "agent install failed on ${WORKER_NODES[$j]}"
  done
}

fetch_kubeconfig() {
  header "Fetching kubeconfig"
  mkdir -p "$(dirname "$LOCAL_KUBECONFIG")"
  nssh 0 "sudo cat /etc/rancher/k3s/k3s.yaml" | sed "s/127.0.0.1/${CONTROL_NODE}/" > "$LOCAL_KUBECONFIG"
  export KUBECONFIG="$LOCAL_KUBECONFIG"
  kubectl wait --for=condition=Ready node --all --timeout=120s || warn "not all nodes Ready yet"
  log "kubeconfig written to ${LOCAL_KUBECONFIG}"
}

install_metallb() {
  $SKIP_PREREQS && { warn "--skip-prereqs: skipping MetalLB"; return; }
  header "Installing MetalLB ${METALLB_VERSION}"
  KUBECONFIG="$LOCAL_KUBECONFIG" kubectl get ns metallb-system >/dev/null 2>&1 \
    || KUBECONFIG="$LOCAL_KUBECONFIG" kubectl apply -f "https://raw.githubusercontent.com/metallb/metallb/${METALLB_VERSION}/config/manifests/metallb-native.yaml"
  KUBECONFIG="$LOCAL_KUBECONFIG" kubectl -n metallb-system wait --for=condition=Available deployment/controller --timeout=180s
  KUBECONFIG="$LOCAL_KUBECONFIG" kubectl apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata: { name: homelab-pool, namespace: metallb-system }
spec: { addresses: ["${METALLB_RANGE}"] }
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata: { name: homelab-l2, namespace: metallb-system }
spec: { ipAddressPools: [homelab-pool] }
EOF
  log "MetalLB pool ${METALLB_RANGE} advertised (Traefik will claim the first VIP)"
}

configure_registry_mirrors() {
  header "Deploying in-cluster registry + containerd mirror"
  nssh 0 "sudo mkdir -p /opt/registry"
  KUBECONFIG="$LOCAL_KUBECONFIG" kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata: { name: registry }
---
apiVersion: v1
kind: PersistentVolume
metadata: { name: registry-pv }
spec:
  capacity: { storage: 20Gi }
  accessModes: [ReadWriteOnce]
  hostPath: { path: /opt/registry }
  nodeAffinity:
    required:
      nodeSelectorTerms:
        - matchExpressions:
            - { key: kubernetes.io/hostname, operator: In, values: ["$(node_hostname 0)"] }
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: registry-data, namespace: registry }
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: ""
  volumeName: registry-pv
  resources: { requests: { storage: 20Gi } }
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: registry, namespace: registry }
spec:
  replicas: 1
  selector: { matchLabels: { app: registry } }
  template:
    metadata: { labels: { app: registry } }
    spec:
      nodeSelector: { kubernetes.io/hostname: "$(node_hostname 0)" }
      containers:
        - name: registry
          image: registry:2
          ports: [{ containerPort: 5000 }]
          volumeMounts: [{ name: data, mountPath: /var/lib/registry }]
      volumes:
        - name: data
          persistentVolumeClaim: { claimName: registry-data }
---
apiVersion: v1
kind: Service
metadata: { name: registry, namespace: registry }
spec:
  type: NodePort
  selector: { app: registry }
  ports: [{ port: 5000, targetPort: 5000, nodePort: ${REGISTRY_NODEPORT} }]
EOF
  KUBECONFIG="$LOCAL_KUBECONFIG" kubectl -n registry wait --for=condition=ready pod -l app=registry --timeout=120s
  # Point every node's containerd at the in-cluster registry via the mirror name.
  for i in "${!NODES[@]}"; do
    nssh "$i" "sudo mkdir -p /etc/rancher/k3s && printf 'mirrors:\n  \"${REGISTRY_MIRROR}\":\n    endpoint:\n      - \"http://${CONTROL_NODE}:${REGISTRY_NODEPORT}\"\n' | sudo tee /etc/rancher/k3s/registries.yaml >/dev/null"
    log "registry mirror configured on ${NODES[$i]}"
  done
  # Restart so the mirror takes effect (control first, then agents).
  nssh 0 "sudo systemctl restart k3s" || true
  for j in "${!WORKER_NODES[@]}"; do
    nssh "$(( j + 1 ))" "sudo /usr/local/bin/k3s-killall.sh 2>/dev/null; sudo systemctl start k3s-agent" || true
  done
  sleep 5
  KUBECONFIG="$LOCAL_KUBECONFIG" kubectl wait --for=condition=Ready node --all --timeout=120s || warn "nodes still settling after mirror restart"
}

teardown() {
  header "Tearing down K3s (DESTRUCTIVE)"
  read -rp "Type 'yes' to uninstall K3s on ${NODES[*]}: " ans
  [ "$ans" = "yes" ] || error "aborted"
  for j in "${!WORKER_NODES[@]}"; do
    nssh "$(( j + 1 ))" "sudo /usr/local/bin/k3s-agent-uninstall.sh 2>/dev/null || true"
    log "uninstalled agent ${WORKER_NODES[$j]}"
  done
  nssh 0 "sudo /usr/local/bin/k3s-uninstall.sh 2>/dev/null || true"
  log "uninstalled server ${CONTROL_NODE}"
}

main() {
  if $TEARDOWN; then preflight; teardown; exit 0; fi
  preflight
  distribute_keys
  ensure_passwordless_sudo
  k3s_install_server
  k3s_install_agents
  fetch_kubeconfig
  install_metallb
  configure_registry_mirrors

  header "Bootstrap complete"
  cat <<EOF
${GREEN}K3s homelab is ready.${NC}

  export KUBECONFIG=${LOCAL_KUBECONFIG}
  kubectl get nodes

Push images to:  ${REGISTRY_HOST}:${REGISTRY_NODEPORT}  (mirror name: ${REGISTRY_MIRROR})
Next:            ./homelabsetup/deploy-homelab.sh
EOF

  if $DEPLOY; then
    header "Handing off to deploy-homelab.sh"
    KUBECONFIG="$LOCAL_KUBECONFIG" exec "${REPO_ROOT}/homelabsetup/deploy-homelab.sh"
  fi
}

main "$@"
