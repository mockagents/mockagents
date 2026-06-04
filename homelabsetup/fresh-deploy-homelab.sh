#!/usr/bin/env bash
# =============================================================================
# fresh-deploy-homelab.sh — clean redeploy of MockAgents
#
# Orchestrator: drains the existing deployment (delegates to
# cleanup-homelab.sh) and redeploys (delegates to deploy-homelab.sh),
# preserving the K3s cluster and the in-cluster registry. Use when you want a
# freshly-deployed state without re-bootstrapping K3s.
#
# What it does, in order:
#   1. Verify the kube context + cluster health.
#   2. Confirm.
#   3. Run cleanup-homelab.sh.
#   4. Wait for the namespace to finalize.
#   5. Run deploy-homelab.sh (flags pass through).
#   6. Verify.
#
# Usage:
#   ./homelabsetup/fresh-deploy-homelab.sh [flags]
#
# Flags:
#   --skip-build      Pass through: reuse the newest build-* image tag.
#   --multi-tenant    Pass through: enable API-key auth + RBAC.
#   --persist         Pass through: persist the SQLite log on a PVC.
#   --reset-registry  Also tear down + recreate the in-cluster registry.
#   -y, --yes         Skip the confirmation prompt.
#   --dry-run         Print every action without running it.
#   -h, --help        Show this help.
#
# Exit codes: 0 success · 1 aborted/precheck · 2 cleanup failed · 3 deploy failed
# =============================================================================
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
log()  { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
err()  { echo -e "${RED}[x]${NC} $1" >&2; exit "${2:-1}"; }
header() { echo -e "\n${CYAN}══════════════════════════════════════════${NC}\n  $1\n${CYAN}══════════════════════════════════════════${NC}"; }

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NAMESPACE="mockagents"
APP_HOST="${APP_HOST:-mockagents.local}"

SKIP_BUILD=false; MULTI_TENANT=false; PERSIST=false; RESET_REGISTRY=false; YES=false; DRY_RUN=false

for arg in "$@"; do
  case "$arg" in
    --skip-build)     SKIP_BUILD=true ;;
    --multi-tenant)   MULTI_TENANT=true ;;
    --persist)        PERSIST=true ;;
    --reset-registry) RESET_REGISTRY=true ;;
    -y|--yes)         YES=true ;;
    --dry-run)        DRY_RUN=true ;;
    -h|--help)        sed -n '2,34p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)                err "Unknown flag: $arg (try --help)" ;;
  esac
done

run() { if $DRY_RUN; then echo -e "${YELLOW}[dry-run]${NC} $*"; else "$@"; fi; }
confirm() { $YES || $DRY_RUN && return 0; read -rp "$1 [y/N] " a; [[ "$a" =~ ^[Yy]$ ]]; }

command -v kubectl >/dev/null 2>&1 || err "kubectl is required."

header "Step 1 — Context check"
ctx="$(kubectl config current-context 2>/dev/null || echo unknown)"
kubectl get nodes >/dev/null 2>&1 || err "kubectl cannot reach the cluster" 1
log "context: ${ctx}"
notready="$(kubectl get nodes --no-headers 2>/dev/null | awk '$2!="Ready"{c++} END{print c+0}')"
[ "$notready" = "0" ] || warn "${notready} node(s) not Ready"

header "Step 2 — Confirm"
confirm "Drain and redeploy MockAgents in namespace '${NAMESPACE}'?" || err "aborted" 1

# Build child flag sets.
CLEANUP_FLAGS=(--yes); DEPLOY_FLAGS=()
$DRY_RUN && { CLEANUP_FLAGS+=(--dry-run); DEPLOY_FLAGS+=(); }
$RESET_REGISTRY && CLEANUP_FLAGS+=(--all)
$SKIP_BUILD   && DEPLOY_FLAGS+=(--skip-build)
$MULTI_TENANT && DEPLOY_FLAGS+=(--multi-tenant)
$PERSIST      && DEPLOY_FLAGS+=(--persist)

header "Step 3 — Drain (cleanup-homelab.sh)"
run "${REPO_ROOT}/homelabsetup/cleanup-homelab.sh" "${CLEANUP_FLAGS[@]}" || err "cleanup failed" 2

header "Step 4 — Wait for namespace finalization"
if ! $DRY_RUN; then
  for _ in $(seq 1 90); do kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || break; sleep 1; done
  kubectl get ns "$NAMESPACE" >/dev/null 2>&1 && warn "namespace still terminating — deploy will recreate it" || log "namespace finalized"
fi

header "Step 5 — Deploy (deploy-homelab.sh)"
run "${REPO_ROOT}/homelabsetup/deploy-homelab.sh" "${DEPLOY_FLAGS[@]}" || err "deploy failed" 3

header "Step 6 — Verify"
if ! $DRY_RUN; then
  sleep 5
  bad="$(kubectl -n "$NAMESPACE" get pods --no-headers 2>/dev/null | awk '$3!="Running" && $3!="Completed"{c++} END{print c+0}')"
  [ "$bad" = "0" ] && log "all pods Running" || warn "${bad} pod(s) still settling"
fi

header "Fresh deploy complete"
echo "App: http://${APP_HOST}  ·  kubectl -n ${NAMESPACE} get pods"
