#!/usr/bin/env bash
# =============================================================================
# cleanup-homelab.sh — reverse deploy-homelab.sh and reclaim resources
#
# Uninstalls the Helm release and (by default) deletes the mockagents
# namespace. With --all it also tears down the in-cluster registry and prunes
# the mockagents image from every node's containerd cache. K3s itself is left
# running. Idempotent — every step checks for existence first.
#
# What it does, in order:
#   1. Inventory what exists.
#   2. helm uninstall + delete the namespace (unless --keep-data).
#   3. (--all) delete the registry namespace + hostPath + prune node images.
#
# Usage:
#   ./homelabsetup/cleanup-homelab.sh [flags]
#
# Flags:
#   --keep-data   Uninstall the release but keep the namespace + PVCs.
#   --all         Also tear down the registry + prune node image caches.
#   --no-ssh      Skip the per-node containerd prune (used with --all).
#   --dry-run     Print every action without running it.
#   -y, --yes     Skip the confirmation prompt.
#   -h, --help    Show this help.
# =============================================================================
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
log()    { echo -e "${GREEN}[+]${NC} $1"; }
warn()   { echo -e "${YELLOW}[!]${NC} $1"; }
error()  { echo -e "${RED}[x]${NC} $1" >&2; exit 1; }
header() { echo -e "\n${CYAN}══════════════════════════════════════════${NC}\n  $1\n${CYAN}══════════════════════════════════════════${NC}"; }

NAMESPACE="mockagents"
RELEASE="mockagents"
read -r -a NODES <<< "${HOMELAB_NODES:-192.168.0.101 192.168.0.102 192.168.0.103}"
NODE_USER="${HOMELAB_NODE_USER:-labadmin}"
CONTROL_NODE="${NODES[0]}"
REGISTRY_NS="registry"
REGISTRY_HOSTPATH="/opt/registry"
NODE_IMAGE_MATCH="mockagents/mockagents"

KEEP_DATA=false; ALL=false; NO_SSH=false; DRY_RUN=false; YES=false

for arg in "$@"; do
  case "$arg" in
    --keep-data) KEEP_DATA=true ;;
    --all|--full) ALL=true ;;
    --no-ssh)    NO_SSH=true ;;
    --dry-run)   DRY_RUN=true ;;
    -y|--yes)    YES=true ;;
    -h|--help)   sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)           error "Unknown flag: $arg (try --help)" ;;
  esac
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
confirm() {
  $YES || $DRY_RUN && return 0
  read -rp "$1 Type 'yes' to confirm: " ans; [ "$ans" = "yes" ]
}

command -v kubectl >/dev/null 2>&1 || error "kubectl is required."
kubectl get nodes >/dev/null 2>&1 || error "kubectl cannot reach the cluster."

header "Step 1 — Inventory"
kubectl get ns "$NAMESPACE" >/dev/null 2>&1 && log "namespace ${NAMESPACE} present" || warn "namespace ${NAMESPACE} not found"
helm -n "$NAMESPACE" status "$RELEASE" >/dev/null 2>&1 && log "release ${RELEASE} installed" || warn "release ${RELEASE} not installed"
$ALL && warn "--all: will also remove the in-cluster registry + node image cache"

confirm "This reclaims MockAgents resources." || error "aborted"

header "Step 2 — Remove the app"
command -v helm >/dev/null 2>&1 && run_ok helm -n "$NAMESPACE" uninstall "$RELEASE"
if $KEEP_DATA; then
  warn "--keep-data: keeping namespace ${NAMESPACE} + PVCs"
else
  run_ok kubectl delete namespace "$NAMESPACE" --ignore-not-found --timeout=120s
  log "namespace ${NAMESPACE} deleted"
fi

if $ALL; then
  header "Step 3 — Registry teardown"
  run_ok kubectl delete namespace "$REGISTRY_NS" --ignore-not-found
  run_ok kubectl delete pv registry-pv --ignore-not-found
  run_ok ssh_node "$CONTROL_NODE" "sudo rm -rf ${REGISTRY_HOSTPATH}/*"
  log "registry removed"

  header "Step 4 — Prune node image caches"
  for n in "${NODES[@]}"; do
    run_ok ssh_node "$n" "sudo k3s crictl images -q --filter 'reference=*${NODE_IMAGE_MATCH}*' 2>/dev/null | xargs -r sudo k3s crictl rmi"
    log "pruned ${NODE_IMAGE_MATCH} on ${n}"
  done
  run_ok docker image prune -f >/dev/null 2>&1
fi

header "Cleanup complete"
echo "To redeploy: ./homelabsetup/deploy-homelab.sh"
