#!/usr/bin/env bash
# =============================================================================
# restart-homelab.sh — resume the MockAgents homelab after stop-homelab.sh
#
# Starts K3s (control node first, then workers), waits for every node Ready,
# then scales the MockAgents Deployment back to its pre-shutdown replica count
# (read from the annotation set by stop-homelab.sh) and probes /api/v1/health.
#
# What it does, in order:
#   1. (unless --apps-only) systemctl start k3s (control), then k3s-agent.
#   2. Wait for the API server + every node Ready.
#   3. Scale the Deployment back to its annotated replica count.
#   4. rollout status + health probe through Traefik.
#
# Usage:
#   ./homelabsetup/restart-homelab.sh [flags]
#
# Flags:
#   --apps-only          Scale workloads back up; assume K3s is already running.
#   --api-timeout N      Seconds to wait for the K3s API [180].
#   --ready-timeout N    Seconds to wait for all nodes Ready [240].
#   --workload-timeout N Seconds to wait for rollout status [240].
#   --no-ssh             Don't start K3s over SSH (scale only).
#   --dry-run            Print every action without running it.
#   -h, --help           Show this help.
# =============================================================================
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
log()    { echo -e "${GREEN}[+]${NC} $1"; }
warn()   { echo -e "${YELLOW}[!]${NC} $1"; }
error()  { echo -e "${RED}[x]${NC} $1" >&2; exit 1; }
header() { echo -e "\n${CYAN}══════════════════════════════════════════${NC}\n  $1\n${CYAN}══════════════════════════════════════════${NC}"; }
check_command() { command -v "$1" >/dev/null 2>&1 || error "$1 is required but not installed."; }

NAMESPACE="mockagents"
RELEASE="mockagents"
read -r -a NODES <<< "${HOMELAB_NODES:-192.168.0.101 192.168.0.102 192.168.0.103}"
NODE_USER="${HOMELAB_NODE_USER:-labadmin}"
CONTROL_NODE="${NODES[0]}"
WORKER_NODES=("${NODES[@]:1}")
REPLICAS_ANNOTATION="mockagents.io/pre-shutdown-replicas"
APP_HOST="${APP_HOST:-mockagents.local}"
API_TIMEOUT=180; READY_TIMEOUT=240; WORKLOAD_TIMEOUT=240

APPS_ONLY=false; NO_SSH=false; DRY_RUN=false

while [ $# -gt 0 ]; do
  case "$1" in
    --apps-only)          APPS_ONLY=true ;;
    --api-timeout)        API_TIMEOUT="$2"; shift ;;
    --api-timeout=*)      API_TIMEOUT="${1#*=}" ;;
    --ready-timeout)      READY_TIMEOUT="$2"; shift ;;
    --ready-timeout=*)    READY_TIMEOUT="${1#*=}" ;;
    --workload-timeout)   WORKLOAD_TIMEOUT="$2"; shift ;;
    --workload-timeout=*) WORKLOAD_TIMEOUT="${1#*=}" ;;
    --no-ssh)             NO_SSH=true ;;
    --dry-run)            DRY_RUN=true ;;
    -h|--help)            sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)                    error "Unknown flag: $1 (try --help)" ;;
  esac
  shift
done

run()    { if $DRY_RUN; then echo -e "${YELLOW}[dry-run]${NC} $*"; else "$@"; fi; }
run_ok() { if $DRY_RUN; then echo -e "${YELLOW}[dry-run]${NC} $*"; else "$@" || true; fi; }
ssh_node() {
  local node="$1"; shift
  if $NO_SSH; then warn "--no-ssh: skipping on ${node}: $*"; return 0; fi
  if $DRY_RUN; then echo -e "${YELLOW}[dry-run]${NC} ssh ${NODE_USER}@${node} $*"; return 0; fi
  ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 "${NODE_USER}@${node}" "$@" 2>&1 \
    || { warn "SSH to ${node} failed"; return 1; }
}

check_command kubectl
$NO_SSH || $APPS_ONLY || check_command ssh

if ! $APPS_ONLY; then
  header "Step 1 — Start K3s"
  run_ok ssh_node "$CONTROL_NODE" "sudo systemctl start k3s"; log "started k3s on ${CONTROL_NODE}"

  header "Step 2 — Wait for the API server"
  if ! $DRY_RUN; then
    ok=false
    for _ in $(seq 1 "$((API_TIMEOUT / 3))"); do
      kubectl get nodes >/dev/null 2>&1 && { ok=true; break; }; sleep 3
    done
    $ok || error "K3s API did not respond within ${API_TIMEOUT}s"
  fi
  log "API server is up"

  header "Step 3 — Start K3s agents"
  for w in "${WORKER_NODES[@]}"; do run_ok ssh_node "$w" "sudo systemctl start k3s-agent"; log "started k3s-agent on ${w}"; done

  header "Step 4 — Wait for nodes Ready"
  run_ok kubectl wait --for=condition=Ready node --all --timeout="${READY_TIMEOUT}s"
fi

kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || { warn "namespace ${NAMESPACE} not found — has deploy-homelab.sh run?"; exit 0; }

header "Step 5 — Scale workloads back up"
target="$(kubectl -n "$NAMESPACE" get deployment "$RELEASE" -o jsonpath="{.metadata.annotations.${REPLICAS_ANNOTATION//./\\.}}" 2>/dev/null || true)"
[ -n "$target" ] || target=1
run kubectl -n "$NAMESPACE" scale deployment "$RELEASE" --replicas="$target"
log "scaled ${RELEASE} to ${target}"
run_ok kubectl -n "$NAMESPACE" rollout status "deployment/${RELEASE}" --timeout="${WORKLOAD_TIMEOUT}s"

header "Step 6 — Health probe"
if ! $DRY_RUN; then
  TRAEFIK_IP="$(kubectl -n kube-system get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)"
  if [ -n "$TRAEFIK_IP" ] && curl -fsS -H "Host: ${APP_HOST}" "http://${TRAEFIK_IP}/api/v1/health" >/dev/null 2>&1; then
    log "health OK at http://${APP_HOST}/api/v1/health"
  else
    warn "health probe didn't pass yet — give the pod a few seconds"
  fi
fi

header "Restart complete"
echo "Check: curl -H \"Host: ${APP_HOST}\" http://<traefik-ip>/api/v1/health"
