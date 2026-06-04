#!/usr/bin/env bash
# =============================================================================
# stop-homelab.sh — gracefully pause the MockAgents homelab
#
# Records the current replica count, scales the MockAgents Deployment to 0,
# then (unless --apps-only) stops K3s on the workers and the control node.
# Persistent volumes are preserved. Pairs with restart-homelab.sh.
#
# What it does, in order:
#   1. Snapshot replicas into an annotation (so restart can restore them).
#   2. Scale the Deployment to 0 and wait for the pods to drain.
#   3. (unless --apps-only) systemctl stop k3s-agent (workers), then k3s.
#
# Usage:
#   ./homelabsetup/stop-homelab.sh [flags]
#
# Flags:
#   --apps-only        Scale workloads to 0 but leave K3s running.
#   --skip-scale       Stop K3s without draining workloads first.
#   --drain-timeout N  Seconds to wait for pods to terminate [90].
#   --no-ssh           Don't stop K3s over SSH (scale only).
#   --dry-run          Print every action without running it.
#   -y, --yes          Skip the confirmation prompt.
#   -h, --help         Show this help.
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
DRAIN_TIMEOUT=90

APPS_ONLY=false; SKIP_SCALE=false; NO_SSH=false; DRY_RUN=false; YES=false

for arg in "$@"; do
  case "$arg" in
    --apps-only)       APPS_ONLY=true ;;
    --skip-scale)      SKIP_SCALE=true ;;
    --drain-timeout=*) DRAIN_TIMEOUT="${arg#*=}" ;;
    --drain-timeout)   shift; DRAIN_TIMEOUT="${1:-90}" ;;
    --no-ssh)          NO_SSH=true ;;
    --dry-run)         DRY_RUN=true ;;
    -y|--yes)          YES=true ;;
    -h|--help)         sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)                 error "Unknown flag: $arg (try --help)" ;;
  esac
done
$SKIP_SCALE && $APPS_ONLY && error "--skip-scale and --apps-only are mutually exclusive"

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
kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || { warn "namespace ${NAMESPACE} not found — nothing to stop"; exit 0; }

if ! $YES && ! $DRY_RUN; then
  echo "This will scale MockAgents to 0$( $APPS_ONLY || echo ' and stop K3s' )."
  read -rp "Continue? [y/N] " ans; case "$ans" in [yY]|[yY][eE][sS]) ;; *) error "aborted" ;; esac
fi

header "Step 1 — Snapshot + scale to 0"
if ! $SKIP_SCALE; then
  cur="$(kubectl -n "$NAMESPACE" get deployment "$RELEASE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo 1)"
  run_ok kubectl -n "$NAMESPACE" annotate deployment "$RELEASE" "${REPLICAS_ANNOTATION}=${cur}" --overwrite
  log "snapshot replicas=${cur}"
  run kubectl -n "$NAMESPACE" scale deployment "$RELEASE" --replicas=0
  # wait for drain
  if ! $DRY_RUN; then
    for _ in $(seq 1 "$DRAIN_TIMEOUT"); do
      [ "$(kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/name=${RELEASE}" --no-headers 2>/dev/null | wc -l | tr -d ' ')" = "0" ] && break
      sleep 1
    done
  fi
  log "workloads drained"
else
  warn "--skip-scale: not draining workloads"
fi

if $APPS_ONLY; then
  header "Stop complete (apps only)"
  echo "Resume with: ./homelabsetup/restart-homelab.sh --apps-only"
  exit 0
fi

header "Step 2 — Stop K3s"
for w in "${WORKER_NODES[@]}"; do run_ok ssh_node "$w" "sudo systemctl stop k3s-agent"; log "stopped k3s-agent on ${w}"; done
run_ok ssh_node "$CONTROL_NODE" "sudo systemctl stop k3s"; log "stopped k3s on ${CONTROL_NODE}"

header "Stop complete"
echo "Resume with: ./homelabsetup/restart-homelab.sh"
