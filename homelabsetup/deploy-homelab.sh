#!/usr/bin/env bash
# =============================================================================
# deploy-homelab.sh — deploy MockAgents onto a bootstrapped K3s homelab
#
# Builds the mockagents image, pushes it to the in-cluster registry, renders
# the bundled example agents into a ConfigMap, and installs the Helm chart
# (deploy/helm/mockagents) with a Traefik ingress on mockagents.local.
#
# What it does, in order:
#   0. Preflight: kubectl/docker/helm, cluster reachable, registry reachable.
#   1. Build mockagents:build-<ts>-<commit> and push to the in-cluster registry.
#   2. Create the namespace.
#   3. Render examples/*.yaml into the mockagents-agents ConfigMap.
#   4. helm upgrade --install (image, ingress, agents, persistence, [tenancy]).
#   5. Wait for the rollout; probe /api/v1/health through Traefik.
#   6. If --multi-tenant: copy the bootstrap admin key from its 0600 data file.
#   7. Print URLs and the path to the local 0600 credentials file.
#
# Usage:
#   ./homelabsetup/deploy-homelab.sh [flags]
#
# Flags:
#   --skip-build       Reuse the newest build-* tag already in the registry.
#   --multi-tenant     Enable API-key auth + RBAC (MOCKAGENTS_MULTI_TENANT=1).
#   --persist          Persist the SQLite interaction log on a PVC.
#   --skip-dns         Skip the /etc/hosts reminder.
#   --teardown         helm uninstall the release (keeps the namespace/PVC).
#   --teardown-all     Delete the whole namespace (DESTRUCTIVE).
#   -h, --help         Show this help.
#
# Config via env (defaults in brackets):
#   HOMELAB_NODES / HOMELAB_NODE_USER   (shared with bootstrap)
#   APP_HOST          Ingress host        [mockagents.local]
#   MULTI_TENANT      Same as --multi-tenant when =1
# =============================================================================
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
log()    { echo -e "${GREEN}[+]${NC} $*"; }
warn()   { echo -e "${YELLOW}[!]${NC} $*"; }
error()  { echo -e "${RED}[x]${NC} $*" >&2; exit 1; }
header() { echo -e "\n${CYAN}━━━ $* ━━━${NC}\n"; }
check_command() { command -v "$1" >/dev/null 2>&1 || error "$1 is required but not installed."; }

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${REPO_ROOT}/homelabsetup/lib/platform-key.sh"

# --- config (kept in lockstep across the homelabsetup scripts) --------------
NAMESPACE="mockagents"
RELEASE="mockagents"
CHART="${REPO_ROOT}/deploy/helm/mockagents"
read -r -a NODES <<< "${HOMELAB_NODES:-192.168.0.101 192.168.0.102 192.168.0.103}"
NODE_USER="${HOMELAB_NODE_USER:-labadmin}"
CONTROL_NODE="${NODES[0]}"

REGISTRY_PUSH="registry.local:30500"       # dev-machine push target (NodePort)
REGISTRY_MIRROR="registry.local:5000"      # in-cluster containerd mirror name
IMAGE_REPO="mockagents/mockagents"
APP_HOST="${APP_HOST:-mockagents.local}"
AGENTS_CM="${RELEASE}-agents"
CREDS_FILE="${REPO_ROOT}/homelabsetup/.homelab-credentials"

SKIP_BUILD=false; PERSIST=false; SKIP_DNS=false; TEARDOWN=false; TEARDOWN_ALL=false
MULTI_TENANT="${MULTI_TENANT:-0}"; [ "$MULTI_TENANT" = "1" ] && MULTI_TENANT=true || MULTI_TENANT=false

for arg in "$@"; do
  case "$arg" in
    --skip-build)    SKIP_BUILD=true ;;
    --multi-tenant)  MULTI_TENANT=true ;;
    --persist)       PERSIST=true ;;
    --skip-dns)      SKIP_DNS=true ;;
    --teardown)      TEARDOWN=true ;;
    --teardown-all)  TEARDOWN_ALL=true ;;
    -h|--help)       sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)               error "Unknown flag: $arg (try --help)" ;;
  esac
done

# --- teardown branches ------------------------------------------------------
if $TEARDOWN_ALL; then
  header "Teardown ALL (DESTRUCTIVE)"
  read -rp "Type 'yes' to delete namespace ${NAMESPACE} and all data: " ans
  [ "$ans" = "yes" ] || error "aborted"
  kubectl delete namespace "$NAMESPACE" --ignore-not-found
  rm -f "$CREDS_FILE"
  log "namespace ${NAMESPACE} deleted"; exit 0
fi
if $TEARDOWN; then
  header "Teardown release ${RELEASE}"
  helm -n "$NAMESPACE" uninstall "$RELEASE" 2>/dev/null || warn "release not installed"
  log "release uninstalled (namespace + PVC kept)"; exit 0
fi

# --- 0. preflight -----------------------------------------------------------
header "Step 0 — Preflight"
check_command kubectl; check_command helm
kubectl get nodes >/dev/null 2>&1 || error "kubectl cannot reach the cluster (set KUBECONFIG?)"
[ -d "$CHART" ] || error "Helm chart not found at ${CHART}"
if ! $SKIP_BUILD; then
  check_command docker
  docker info >/dev/null 2>&1 || error "docker daemon not reachable"
fi
curl -fsS "http://${REGISTRY_PUSH}/v2/_catalog" >/dev/null 2>&1 \
  || warn "registry ${REGISTRY_PUSH} not reachable yet — run bootstrap-homelab.sh first if the build push fails"
log "cluster reachable; $(kubectl get nodes --no-headers | wc -l | tr -d ' ') node(s)"

# --- 1. build + push --------------------------------------------------------
header "Step 1 — Image"
if $SKIP_BUILD; then
  BUILD_TAG="$(curl -fsS "http://${REGISTRY_PUSH}/v2/${IMAGE_REPO}/tags/list" 2>/dev/null \
    | tr ',' '\n' | grep -oE 'build-[0-9]{8}-[0-9]{6}-[0-9a-f]{12}' | sort | tail -1)"
  [ -n "$BUILD_TAG" ] || error "--skip-build but no build-* tag in the registry; run without --skip-build first"
  log "reusing existing tag ${BUILD_TAG}"
  SOURCE_COMMIT="${BUILD_TAG##*-}"
else
  SOURCE_COMMIT="$(git -C "$REPO_ROOT" rev-parse HEAD)"
  BUILD_TAG="build-$(date -u +%Y%m%d-%H%M%S)-${SOURCE_COMMIT:0:12}"
  PUSH_IMG="${REGISTRY_PUSH}/${IMAGE_REPO}:${BUILD_TAG}"
  log "building ${PUSH_IMG}"
  docker build --label "org.opencontainers.image.revision=${SOURCE_COMMIT}" \
    -t "$PUSH_IMG" -f "${REPO_ROOT}/Dockerfile" "$REPO_ROOT" || error "docker build failed"
  docker push "$PUSH_IMG" || error "docker push failed — is the insecure registry trusted? (see DEPLOY_MOCKAGENTS.md Step 0b)"
  log "pushed ${PUSH_IMG}"
fi
# The Deployment pulls via the in-cluster mirror name, not the NodePort host.
DEPLOY_IMAGE_REPO="${REGISTRY_MIRROR}/${IMAGE_REPO}"

# --- 2. namespace -----------------------------------------------------------
header "Step 2 — Namespace"
kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || kubectl create namespace "$NAMESPACE"
log "namespace ${NAMESPACE} ready"

# --- 3. agents ConfigMap ----------------------------------------------------
header "Step 3 — Agent definitions"
[ -d "${REPO_ROOT}/examples" ] || error "examples/ directory not found"
kubectl -n "$NAMESPACE" create configmap "$AGENTS_CM" \
  --from-file="${REPO_ROOT}/examples/" \
  --dry-run=client -o yaml | kubectl apply -f -
log "ConfigMap ${AGENTS_CM} rendered from examples/ ($(ls "${REPO_ROOT}/examples"/*.yaml | wc -l | tr -d ' ') files)"

# --- 4. helm upgrade --install ----------------------------------------------
header "Step 4 — Helm deploy"
HELM_ARGS=(
  upgrade --install "$RELEASE" "$CHART"
  --namespace "$NAMESPACE"
  --set "image.repository=${DEPLOY_IMAGE_REPO}"
  --set "image.tag=${BUILD_TAG}"
  --set "image.pullPolicy=IfNotPresent"
  --set "agents.existingConfigMap=${AGENTS_CM}"
  --set "ingress.enabled=true"
  --set "ingress.hosts[0].host=${APP_HOST}"
  --set "ingress.hosts[0].paths[0].path=/"
  --set "ingress.hosts[0].paths[0].pathType=Prefix"
  --wait --timeout 300s
)
$PERSIST && HELM_ARGS+=( --set "persistence.enabled=true" --set "persistence.size=1Gi" )
if $MULTI_TENANT; then
  HELM_ARGS+=( --set "env.MOCKAGENTS_MULTI_TENANT=1" )
  warn "multi-tenant mode ON — a bootstrap platform key will be minted on first start"
fi
helm "${HELM_ARGS[@]}" || error "helm upgrade --install failed"
kubectl -n "$NAMESPACE" rollout status "deployment/${RELEASE}" --timeout=240s
DEPLOYED_IMAGE_ID="$(kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=${RELEASE}" \
  -o jsonpath='{.items[0].status.containerStatuses[0].imageID}')"
[ -n "$DEPLOYED_IMAGE_ID" ] || error "running pod did not report an imageID digest"
log "release ${RELEASE} rolled out at image tag ${BUILD_TAG}"

# --- 5. multi-tenant bootstrap key ------------------------------------------
BOOTSTRAP_KEY=""
BOOTSTRAP_KEY_FROM_POD=false
TRAEFIK_IP="$(kubectl -n kube-system get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)"
if $MULTI_TENANT; then
  header "Step 5 — Bootstrap platform key"
  POD_NAME="$(kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=${RELEASE}" \
    -o jsonpath='{.items[0].metadata.name}')"
  [ -n "$POD_NAME" ] || error "could not identify the running ${RELEASE} pod"
  [ -n "$TRAEFIK_IP" ] || error "Traefik has no load-balancer IP; cannot validate a retained platform credential"
  resolve_platform_key "$NAMESPACE" "$POD_NAME" "$CREDS_FILE" "http://${TRAEFIK_IP}" "$APP_HOST"
fi

# --- 6. DNS reminder --------------------------------------------------------
if ! $SKIP_DNS; then
  header "Step 6 — DNS"
  if [ -n "$TRAEFIK_IP" ]; then
    warn "Add to your hosts file so ${APP_HOST} resolves to the Traefik VIP:"
    echo "    ${TRAEFIK_IP} ${APP_HOST}"
  else
    warn "Traefik LoadBalancer IP not assigned yet — is MetalLB installed? (bootstrap --skip-prereqs skips it)"
  fi
fi

# --- 7. verify --------------------------------------------------------------
header "Step 7 — Verify"
if [ -n "$TRAEFIK_IP" ]; then
  if curl -fsS -H "Host: ${APP_HOST}" "http://${TRAEFIK_IP}/api/v1/health" >/dev/null 2>&1; then
    log "health check OK via Traefik (${TRAEFIK_IP})"
  else
    warn "health check did not pass yet — the pod may still be starting"
  fi
fi

# --- credentials file -------------------------------------------------------
CREDS_TMP="${CREDS_FILE}.tmp.$$"
trap 'rm -f "$CREDS_TMP"' EXIT
( umask 077
{
  echo "# MockAgents homelab — generated $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "APP_URL=http://${APP_HOST}"
  echo "IMAGE_TAG=${BUILD_TAG}"
  echo "IMAGE_ID=${DEPLOYED_IMAGE_ID}"
  echo "SOURCE_COMMIT=${SOURCE_COMMIT}"
  [ -n "$BOOTSTRAP_KEY" ] && echo "MOCKAGENTS_BOOTSTRAP_ADMIN_KEY=${BOOTSTRAP_KEY}"
} > "$CREDS_TMP"
)
chmod 600 "$CREDS_TMP" 2>/dev/null || true
mv -f "$CREDS_TMP" "$CREDS_FILE"
trap - EXIT
if $BOOTSTRAP_KEY_FROM_POD; then
  remove_pod_platform_key "$NAMESPACE" "$POD_NAME"
  log "platform key saved locally; pod-side plaintext copy removed"
fi

header "Deploy complete"
cat <<EOF
${GREEN}MockAgents is deployed.${NC}

  API / OpenAI endpoint : http://${APP_HOST}/v1/chat/completions
  Anthropic endpoint    : http://${APP_HOST}/v1/messages
  Health                : http://${APP_HOST}/api/v1/health
  Agents catalog (API)  : http://${APP_HOST}/api/v1/agents

EOF
if $MULTI_TENANT && [ -n "$BOOTSTRAP_KEY" ]; then
  echo "  Bootstrap platform key saved to homelabsetup/.homelab-credentials (mode 0600; gitignored)."
  echo "  Load it from that file when running the regression harness."
  echo
fi
cat <<EOF
Useful:
  kubectl -n ${NAMESPACE} get pods
  kubectl -n ${NAMESPACE} logs deployment/${RELEASE} -f
  curl -H "Host: ${APP_HOST}" http://${TRAEFIK_IP:-<traefik-ip>}/api/v1/health
  helm -n ${NAMESPACE} status ${RELEASE}
EOF
